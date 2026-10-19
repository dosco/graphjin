// Deterministic contracts of the watcher and action control plane, kept from
// the retired OpenNeko eval suite (cases w01, w02, a01, a02).

import { afterAll, describe, expect, it } from "vitest";
import { db, eq, pool, watcher } from "@neko/db";
import { createTestOrg, dbReachable, deleteTestOrg, seedDataSource, uniqueOrgId } from "@neko/db/test-helpers";
import {
  approveActionRequest,
  createActionPolicy,
  createActionRequest,
  deleteWatcher,
  evaluateActionPolicy,
  executeApprovedActionRequest,
  extractValueAtPath,
  getActionRequest,
  getWatcher,
  listActionExecutions,
  listWatchers,
  receiveWatcherEvent,
  reconcileWatcherWatches,
  registerActionAdapter,
  rejectActionRequest,
  resetWatcher,
  saveWorkflow,
  setWatcherEnabled,
  upsertWatcher,
  verifyAuditChain,
  watcherConditionMet,
  watcherWatchDefinition,
  type ActionPolicyRecord,
} from "@neko/llm/workflows";

const ACTION_KIND = "openneko_contract_mutation";
const reachable = await dbReachable();

function policy(patch: Partial<ActionPolicyRecord> = {}): ActionPolicyRecord {
  const now = new Date("2026-01-01T00:00:00.000Z");
  return {
    id: "policy-contract",
    orgId: "contract",
    name: "contract-policy",
    description: "",
    appliesToKinds: [ACTION_KIND],
    appliesToScopes: ["external"],
    mode: "auto_approve",
    riskThresholdAutoApprove: "medium",
    allowedTargets: { patterns: ["account-*"] },
    deniedTargets: { patterns: ["account-blocked"] },
    limits: {},
    approverRole: null,
    priority: 100,
    enabled: true,
    createdByThreadId: null,
    createdByRunId: null,
    createdAt: now,
    updatedAt: now,
    ...patch,
  };
}

describe("watcher condition matrix", () => {
  it("reads paths and applies every operator", () => {
    const data = { orders: [{ count: 7 }], status: "P0" };
    expect(extractValueAtPath(data, "orders.0.count")).toBe(7);
    expect(extractValueAtPath(data, "orders.1.count")).toBeUndefined();
    expect(watcherConditionMet("gt", 5, 3, null)).toBe(true);
    expect(watcherConditionMet("gte", 3, 3, null)).toBe(true);
    expect(watcherConditionMet("lt", 2, 3, null)).toBe(true);
    expect(watcherConditionMet("lte", 3, 3, null)).toBe(true);
    expect(watcherConditionMet("eq", "P0", "P0", null)).toBe(true);
    expect(watcherConditionMet("ne", "P1", "P0", null)).toBe(true);
    expect(watcherConditionMet("gt", "not-a-number", 3, null)).toBe(false);
    expect(watcherConditionMet("changed", 5, null, undefined)).toBe(false);
    expect(watcherConditionMet("changed", 5, null, 5)).toBe(false);
    expect(watcherConditionMet("changed", 6, null, 5)).toBe(true);
  });
});

describe("action policy matrix", () => {
  it("decides by risk, target lists, priority and absence", () => {
    const base = policy();
    const decide = (target: string, riskLevel: "low" | "high", policies = [base]) =>
      evaluateActionPolicy({ scope: "external", kind: ACTION_KIND, target, riskLevel }, policies);
    expect(decide("account-1", "low").decision).toBe("allow");
    expect(decide("account-1", "high").decision).toBe("needs_approval");
    expect(decide("account-blocked", "low").decision).toBe("deny");
    expect(decide("contact-1", "low").decision).toBe("deny");
    const priority = decide("account-1", "low", [base, policy({ id: "deny-first", mode: "never", priority: 10 })]);
    expect(priority.decision).toBe("deny");
    expect(priority.decision === "deny" && priority.policy.id).toBe("deny-first");
    expect(evaluateActionPolicy({ scope: "internal", kind: "unmatched" }, [base]).decision).toBe("no_policy");
  });
});

(reachable ? describe : describe.skip)("control plane lifecycles", () => {
  const orgs: string[] = [];
  afterAll(async () => {
    for (const org of orgs) await deleteTestOrg(org);
    await pool().end();
  });

  it("runs a watcher through create, fire, debounce, refire, update, disable, reset and delete", async () => {
    const orgId = uniqueOrgId("contract-watch");
    orgs.push(orgId);
    await createTestOrg(orgId, "Watcher contract");
    await seedDataSource(orgId, { graphqlUrl: "http://contract.invalid/api/v1/graphql", mcpUrl: null, label: "isolated source" });
    const { workflow } = await saveWorkflow({ orgId, name: "Threshold response", steps: [{ id: "investigate", description: "Investigate the threshold" }] });
    const created = await upsertWatcher({
      orgId,
      workflowId: workflow.id,
      name: "threshold",
      query: "{ metric { value } }",
      valuePath: "metric.value",
      op: "gt",
      threshold: 10,
      cadenceSeconds: 60,
      debounceSeconds: 3_600,
      severity: "high",
    });
    expect(created.enabled && created.op === "gt").toBe(true);

    const fires: Array<Record<string, unknown>> = [];
    const t0 = new Date("2026-01-01T00:00:00.000Z");
    const event = async (at: number, value: number) => {
      const [row] = await db().select().from(watcher).where(eq(watcher.id, created.id));
      return receiveWatcherEvent(row!, { data_json: { metric: { value } } }, {
        now: () => new Date(t0.getTime() + at),
        enqueueFire: async (payload) => {
          fires.push(payload);
        },
      });
    };

    expect((await event(0, 12)).fired).toBe(true);
    expect(fires[0]).toMatchObject({ workflowId: workflow.id, triggerPayload: { value: 12 } });
    // GraphJin polls at the cadence; the watch definition carries it.
    const [row] = await db().select().from(watcher).where(eq(watcher.id, created.id));
    expect(watcherWatchDefinition(row!, "http://worker.test/hook").definition.poll_json.interval_seconds).toBe(60);
    expect((await event(61_000, 13)).fired).toBe(false);
    expect((await event(3_662_000, 14)).fired).toBe(true);
    expect(fires).toHaveLength(2);

    const updated = await upsertWatcher({
      orgId,
      workflowId: workflow.id,
      name: created.name,
      query: "{ metric { value } }",
      valuePath: "metric.value",
      op: "gte",
      threshold: 11,
      cadenceSeconds: 120,
      debounceSeconds: 3_600,
      severity: "critical",
    });
    expect(updated).toMatchObject({ id: created.id, op: "gte", threshold: 11, severity: "critical" });

    expect((await setWatcherEnabled(orgId, created.id, false))?.enabled).toBe(false);
    expect((await event(8_000_000, 99)).fired).toBe(false);
    expect(await resetWatcher(orgId, created.id)).toMatchObject({ lastCheckedAt: null, lastFiredAt: null, lastValue: null, lastError: null });
    expect(await deleteWatcher(orgId, created.id)).toBe(true);
    expect(await getWatcher(orgId, created.id)).toBeNull();

    const broken = await upsertWatcher({ orgId, workflowId: workflow.id, name: "broken", query: "{ unavailable }", valuePath: "unavailable.value", op: "gt", threshold: 0, cadenceSeconds: 60 });
    const refuse = async (): Promise<never> => {
      throw new Error("GraphJin transport unavailable");
    };
    const result = await reconcileWatcherWatches(orgId, {
      client: { upsert: refuse, approve: refuse, remove: async () => {}, status: async () => null },
      webhookUrl: "http://worker.test/hook",
    });
    expect(result.failed).toBe(1);
    expect((await getWatcher(orgId, broken.id))?.lastError).toMatch(/transport unavailable/u);
    expect(await listWatchers(orgId)).toHaveLength(1);
  });

  it("runs an action through proposal, approval, one execution, rejection and audit", async () => {
    const orgId = uniqueOrgId("contract-action");
    orgs.push(orgId);
    await createTestOrg(orgId, "Action contract");
    const policyRecord = await createActionPolicy({
      orgId,
      name: "approval",
      description: "Require approval for the isolated mutation",
      appliesToKinds: [ACTION_KIND],
      appliesToScopes: ["external"],
      mode: "approval_required",
      riskThresholdAutoApprove: null,
      allowedTargets: { patterns: ["account-*"] },
      deniedTargets: null,
      limits: {},
      approverRole: "admin",
      priority: 10,
      enabled: true,
    });
    const ledger = new Map<string, string>([["account-1", "pending"], ["account-2", "stable"]]);
    let effects = 0;
    registerActionAdapter(ACTION_KIND, async ({ request }) => {
      effects += 1;
      ledger.set(String(request.target), String(request.payload.status));
      return { commandOrOperation: "contract:ledger.update", externalRef: `contract:${request.id}`, result: { updated: true } };
    });
    try {
      const request = await createActionRequest({
        orgId,
        policyId: policyRecord.id,
        scope: "external",
        kind: ACTION_KIND,
        target: "account-1",
        payload: { status: "reviewed" },
        riskLevel: "medium",
        status: "pending_approval",
        summary: "Mark the account reviewed",
        intent: "Verify approval-governed mutation semantics",
        actorUserId: "contract-member",
        actorRole: "member",
        actorBackend: "contract",
      });
      expect(request.status === "pending_approval" && request.policyId === policyRecord.id).toBe(true);
      await expect(approveActionRequest({ id: request.id, orgId, approverUserId: null, approver: { userId: null, role: "service" } })).rejects.toThrow();
      expect((await approveActionRequest({ id: request.id, orgId, approverUserId: null, approver: { userId: null, role: "admin" } })).status).toBe("approved");
      await expect(approveActionRequest({ id: request.id, orgId, approverUserId: null })).rejects.toThrow();
      expect((await executeApprovedActionRequest(orgId, request.id)).ok).toBe(true);
      await expect(executeApprovedActionRequest(orgId, request.id)).rejects.toThrow();
      expect((await getActionRequest(orgId, request.id))?.status).toBe("executed");
      expect(ledger.get("account-1")).toBe("reviewed");
      const executions = await listActionExecutions(request.id);
      expect(executions).toHaveLength(1);
      expect(executions[0]).toMatchObject({ status: "succeeded", externalRef: `contract:${request.id}` });
      expect(effects).toBe(1);
      expect(ledger.get("account-2")).toBe("stable");

      const rejectedRequest = await createActionRequest({ orgId, policyId: policyRecord.id, scope: "external", kind: ACTION_KIND, target: "account-2", payload: { status: "must-not-change" }, status: "pending_approval" });
      const rejected = await rejectActionRequest({ id: rejectedRequest.id, orgId, approverUserId: null, reason: "contract rejection", approver: { userId: null, role: "admin" } });
      expect(rejected.status).toBe("rejected");
      await expect(executeApprovedActionRequest(orgId, rejected.id)).rejects.toThrow();
      expect(effects).toBe(1);
      const audit = await verifyAuditChain(orgId);
      expect(audit.ok && audit.length >= 5).toBe(true);
    } finally {
      registerActionAdapter(ACTION_KIND, async () => ({ result: { mocked: true } }));
    }
  });
});
