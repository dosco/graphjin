// OL4 — watchers (polling v1): a condition over a GraphJin query fires
// the linked workflow once per debounce window; query errors are
// recorded without firing; `changed` fires on value movement.

import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { data_source, db, eq, pool, watcher } from "@neko/db";
import {
  createTestOrg,
  dbReachable,
  deleteTestOrg,
  uniqueOrgId,
} from "@neko/db/test-helpers";
import {
  extractValueAtPath,
  sweepWatchers,
  upsertWatcher,
  watcherConditionMet,
} from "../../src/workflows/watchers";
import { saveWorkflowWithTrigger } from "../../src/workflows/save-workflow-with-trigger";
import { saveWorkflow } from "../../src/workflows/store";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[watchers] skipping: Postgres unreachable.");
}

describe("watcher condition evaluation (pure)", () => {
  it("extracts dotted paths through objects and arrays", () => {
    const data = { orders: [{ count: 7 }], totals: { revenue: 1200 } };
    expect(extractValueAtPath(data, "orders.0.count")).toBe(7);
    expect(extractValueAtPath(data, "totals.revenue")).toBe(1200);
    expect(extractValueAtPath(data, "missing.path")).toBeUndefined();
  });

  it("numeric ops, eq/ne, and changed semantics", () => {
    expect(watcherConditionMet("gt", 5, 3, null)).toBe(true);
    expect(watcherConditionMet("lte", 3, 3, null)).toBe(true);
    expect(watcherConditionMet("gt", "not-a-number", 3, null)).toBe(false);
    expect(watcherConditionMet("eq", "P0", "P0", null)).toBe(true);
    expect(watcherConditionMet("ne", "P1", "P0", null)).toBe(true);
    // changed never fires on the first observation.
    expect(watcherConditionMet("changed", 5, null, undefined)).toBe(false);
    expect(watcherConditionMet("changed", 5, null, 5)).toBe(false);
    expect(watcherConditionMet("changed", 6, null, 5)).toBe(true);
  });
});

describeIfDb("OL4 watcher sweep", () => {
  const orgId = uniqueOrgId("ol4");
  let workflowId: string;

  beforeAll(async () => {
    await createTestOrg(orgId);
    await db().insert(data_source).values({
      org_id: orgId,
      kind: "graphjin",
      graphql_url: "http://gj.test/api/v1/graphql",
      name: "default",
      is_default: true,
    });
    const { workflow } = await saveWorkflow({
      orgId,
      name: "Refund spike response",
      steps: [{ id: "s1", description: "investigate refunds" }],
    });
    workflowId = workflow.id;
  });

  afterAll(async () => {
    await deleteTestOrg(orgId);
    await pool().end();
  });

  it("fires the linked workflow when the condition holds, debounced", async () => {
    await upsertWatcher({
      orgId,
      workflowId,
      name: "refund-rate",
      query: "{ refunds_aggregate { rate } }",
      valuePath: "refunds_aggregate.rate",
      op: "gt",
      threshold: 2,
      cadenceSeconds: 60,
      debounceSeconds: 3600,
      severity: "high",
    });

    const fires: Array<Record<string, unknown>> = [];
    const deps = {
      query: async () => ({ data: { refunds_aggregate: { rate: 4.2 } } }),
      enqueueFire: async (payload: Record<string, unknown>) => {
        fires.push(payload);
      },
    };

    const first = await sweepWatchers(orgId, deps as never);
    expect(first.checked).toBe(1);
    expect(first.fired).toHaveLength(1);
    expect(fires[0]).toMatchObject({ workflowId, triggerKind: "watcher" });

    // Cadence not elapsed → not even checked again.
    const second = await sweepWatchers(orgId, deps as never);
    expect(second.checked).toBe(0);

    // Cadence elapsed but inside debounce → checked, no fire.
    await db()
      .update(watcher)
      .set({ last_checked_at: new Date(Date.now() - 120_000) })
      .where(eq(watcher.org_id, orgId));
    const third = await sweepWatchers(orgId, deps as never);
    expect(third.checked).toBe(1);
    expect(third.fired).toHaveLength(0);
    expect(fires).toHaveLength(1);
  });

  it("reports every change, including a return to an earlier value, and skips repeats", async () => {
    const values = [120, 125, 120, 120];
    let i = 0;
    const fires: Array<Record<string, unknown>> = [];
    const deps = {
      query: async () => ({ data: { stock: { total: values[i] } } }),
      enqueueFire: async (payload: Record<string, unknown>) => {
        fires.push(payload);
      },
    };
    for (const [op, threshold] of [["changed", null], ["gt", 100]] as const) {
      fires.length = 0;
      await db().delete(watcher).where(eq(watcher.org_id, orgId));
      await upsertWatcher({
        orgId,
        workflowId,
        name: `stock-${op}`,
        query: "{ stock { total } }",
        valuePath: "stock.total",
        op,
        ...(threshold === null ? {} : { threshold }),
        cadenceSeconds: 60,
        debounceSeconds: 0,
      });
      const seen: unknown[] = [];
      for (i = 0; i < values.length; i++) {
        await db()
          .update(watcher)
          .set({ last_checked_at: new Date(Date.now() - 120_000) })
          .where(eq(watcher.org_id, orgId));
        const result = await sweepWatchers(orgId, deps as never);
        seen.push(...result.fired.map((f) => f.value));
      }
      expect(seen).toEqual(op === "changed" ? [125, 120] : [120, 125, 120]);
    }
  });

  it("query errors are recorded and never fire", async () => {
    await upsertWatcher({
      orgId,
      workflowId,
      name: "broken-watch",
      query: "{ nope }",
      valuePath: "nope",
      op: "gt",
      threshold: 0,
      cadenceSeconds: 60,
    });
    const result = await sweepWatchers(orgId, {
      query: async () => {
        throw new Error("connect ECONNREFUSED");
      },
      enqueueFire: async () => {
        throw new Error("must not fire");
      },
    } as never);
    expect(result.fired).toHaveLength(0);
    const [row] = await db()
      .select({ err: watcher.last_error })
      .from(watcher)
      .where(and_eq(orgId, "broken-watch"));
    expect(row.err).toMatch(/ECONNREFUSED/);
  });

  it("saveWorkflowWithTrigger persists a watch trigger", async () => {
    const result = await saveWorkflowWithTrigger({
      orgId,
      name: "Stock coverage response",
      steps: [{ id: "s1", description: "reorder" }],
      triggers: {
        watch: {
          query: "{ stock { coverage_days } }",
          value_path: "stock.coverage_days",
          op: "lt",
          threshold: 14,
        },
      },
    });
    expect(result.triggerError).toBeUndefined();
    expect(result.watcher).toMatchObject({
      name: "Stock coverage response",
      op: "lt",
      threshold: 14,
      workflowId: result.workflow.id,
    });
  });
});

import { and, eq as drizzleEq } from "@neko/db";
function and_eq(orgId: string, name: string) {
  return and(eq(watcher.org_id, orgId), drizzleEq(watcher.name, name));
}
