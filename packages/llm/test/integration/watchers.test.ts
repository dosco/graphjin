// Watchers: GraphJin polls each watcher's query as a watch and sends its
// result when it changes; the worker applies the condition, firing the
// linked workflow once per debounce window. `changed` fires on movement.

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
  upsertWatcher,
  watcherConditionMet,
} from "../../src/workflows/watchers";
import {
  receiveWatcherEvent,
  reconcileWatcherWatches,
  watcherForGraphjinWatch,
  type WatcherWatchClient,
} from "../../src/workflows/watcher-native";
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

describeIfDb("watchers on GraphJin watches", () => {
  const orgId = uniqueOrgId("ol4");
  let workflowId: string;

  beforeAll(async () => {
    await createTestOrg(orgId);
    await db().insert(data_source).values({
      org_id: orgId,
      kind: "graphjin",
      graphql_url: "http://gj.test",
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

  async function row(name: string) {
    const [found] = await db().select().from(watcher).where(and_eq(orgId, name));
    return found!;
  }

  /** Feeds one GraphJin watch event carrying `data` to the watcher. */
  async function event(name: string, data: unknown, fires: Array<Record<string, unknown>>) {
    return receiveWatcherEvent(await row(name), { data_json: JSON.stringify(data) }, {
      enqueueFire: async (payload) => {
        fires.push(payload);
      },
    });
  }

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
    expect((await event("refund-rate", { refunds_aggregate: { rate: 4.2 } }, fires)).fired).toBe(true);
    expect(fires[0]).toMatchObject({ workflowId, triggerKind: "watcher", triggerPayload: { value: 4.2, severity: "high" } });

    // A new value inside the debounce window does not fire again.
    expect((await event("refund-rate", { refunds_aggregate: { rate: 5 } }, fires)).fired).toBe(false);
    expect(fires).toHaveLength(1);
    expect((await row("refund-rate")).last_value).toBe(5);
  });

  it("reports every change, including a return to an earlier value, and skips repeats", async () => {
    const values = [120, 125, 120, 120];
    const fires: Array<Record<string, unknown>> = [];
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
      // GraphJin wraps the result in data; both shapes read the same value.
      for (const [i, total] of values.entries()) {
        await event(`stock-${op}`, i % 2 ? { data: { stock: { total } } } : { stock: { total } }, fires);
      }
      expect(fires.map((f) => (f.triggerPayload as { value: unknown }).value)).toEqual(
        op === "changed" ? [125, 120] : [120, 125, 120],
      );
    }
  });

  it("keeps each watcher's GraphJin watch in step with the watcher row", async () => {
    await db().delete(watcher).where(eq(watcher.org_id, orgId));
    await upsertWatcher({
      orgId,
      workflowId,
      name: "late-orders",
      query: "query late($since: String) { orders(where: { created_at: { gte: $since } }) { count_id } }",
      valuePath: "orders.0.count_id",
      op: "gt",
      threshold: 10,
      cadenceSeconds: 300,
    });
    await db()
      .update(watcher)
      .set({ variables_json: { since: { kind: "seconds_ago", seconds: 3600 }, region: { kind: "literal", value: "emea" } } })
      .where(and_eq(orgId, "late-orders"));
    const calls: string[] = [];
    const sent: Array<Record<string, unknown>> = [];
    let status: { status: string; last_error: string | null } = { status: "active", last_error: null };
    const client: WatcherWatchClient = {
      upsert: async ({ endpoint, watchId, definition }) => {
        calls.push(`upsert ${watchId ?? "new"}`);
        expect(endpoint).toBe("http://gj.test/api/v1/graphql");
        sent.push(definition);
        return { id: "watch:abc", action_hash: "h1", action_approval: "pending" };
      },
      approve: async ({ watchId, actionHash }) => {
        calls.push(`approve ${watchId} ${actionHash}`);
        return { id: watchId, action_approval: "approved" };
      },
      remove: async ({ watchId }) => {
        calls.push(`remove ${watchId}`);
      },
      status: async () => status,
      list: async () => [],
    };
    const deps = { client, webhookUrl: "http://worker.test/admin/events/watcher" };

    expect(await reconcileWatcherWatches(orgId, deps)).toEqual({ synced: 1, removed: 0, failed: 0 });
    expect(calls).toEqual(["upsert new", "approve watch:abc h1"]);
    expect(sent[0]).toMatchObject({
      query: expect.stringContaining("orders"),
      variables_json: { region: "emea" },
      poll_json: { interval_seconds: 300, variables: { since: { kind: "seconds_ago", seconds: 3600 } } },
      delivery_json: { kind: "webhook", url: "http://worker.test/admin/events/watcher", secret_source: "keystore" },
    });
    expect(await row("late-orders")).toMatchObject({ graphjin_watch_id: "watch:abc", graphjin_endpoint: "http://gj.test/api/v1/graphql" });
    expect((await watcherForGraphjinWatch("watch:abc"))?.name).toBe("late-orders");

    // Unchanged: only the watch's state is read, and its error is copied.
    calls.length = 0;
    status = { status: "error", last_error: "no such table: orders" };
    expect(await reconcileWatcherWatches(orgId, deps)).toEqual({ synced: 0, removed: 0, failed: 0 });
    expect(calls).toEqual([]);
    expect((await row("late-orders")).last_error).toBe("no such table: orders");

    // A changed definition is sent again to the same watch.
    await db().update(watcher).set({ threshold: 20, cadence_seconds: 600 }).where(and_eq(orgId, "late-orders"));
    expect(await reconcileWatcherWatches(orgId, deps)).toMatchObject({ synced: 1 });
    expect(calls[0]).toBe("upsert watch:abc");
    expect(sent[1]).toMatchObject({ poll_json: { interval_seconds: 600 } });

    // Disabling removes the watch.
    calls.length = 0;
    await db().update(watcher).set({ enabled: false }).where(and_eq(orgId, "late-orders"));
    expect(await reconcileWatcherWatches(orgId, deps)).toEqual({ synced: 0, removed: 1, failed: 0 });
    expect(calls).toEqual(["remove watch:abc"]);
    expect((await row("late-orders")).graphjin_watch_id).toBeNull();
  });

  it("records a GraphJin that refuses the watch and never fires", async () => {
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
    const refuse = async (): Promise<never> => {
      throw new Error("gj_watch query probe failed: unknown table nope");
    };
    const result = await reconcileWatcherWatches(orgId, {
      client: { upsert: refuse, approve: refuse, remove: async () => {}, status: async () => null, list: async () => [] },
      webhookUrl: "http://worker.test/admin/events/watcher",
    });
    expect(result.failed).toBeGreaterThan(0);
    expect((await row("broken-watch")).last_error).toMatch(/unknown table nope/);
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
