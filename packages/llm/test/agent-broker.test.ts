import { describe, expect, it, vi } from "vitest";
import type { AgentControlPlane } from "../src/work/control-plane";
import {
  ensureAgentBroker,
  registerAgentBrokerEventSink,
  shutdownAgentBroker,
  startAgentBroker,
} from "../src/work/broker";

// /v1/events is the only path exercised here and it never touches the control
// plane (it routes to deps.onEvents), so a stub that satisfies the interface
// is enough — the registry/auth behaviour is what's under test.
function stubControlPlane(): AgentControlPlane {
  const unused = () => {
    throw new Error("not exercised by these tests");
  };
  return {
    evaluateActionPolicy: unused as AgentControlPlane["evaluateActionPolicy"],
    createActionRequest: unused as AgentControlPlane["createActionRequest"],
    enqueueActionExecute: unused as AgentControlPlane["enqueueActionExecute"],
    waitForActionExecution:
      unused as AgentControlPlane["waitForActionExecution"],
    rememberWorkMemory: unused as AgentControlPlane["rememberWorkMemory"],
    searchWorkMemoryByContext:
      unused as AgentControlPlane["searchWorkMemoryByContext"],
    queryGraphjinRead: unused as AgentControlPlane["queryGraphjinRead"],
    listGraphjinTools: unused as AgentControlPlane["listGraphjinTools"],
    callGraphjinTool: unused as AgentControlPlane["callGraphjinTool"],
    askGraphjinDataAgent:
      unused as AgentControlPlane["askGraphjinDataAgent"],
    listRecordCatalog: unused as AgentControlPlane["listRecordCatalog"],
    findRecords: unused as AgentControlPlane["findRecords"],
    getRecord: unused as AgentControlPlane["getRecord"],
    findRecycledRecords:
      unused as AgentControlPlane["findRecycledRecords"],
    getRecycledRecord: unused as AgentControlPlane["getRecycledRecord"],
    listRecordBlueprints: unused as AgentControlPlane["listRecordBlueprints"],
    saveWorkflowWithTrigger:
      unused as AgentControlPlane["saveWorkflowWithTrigger"],
    emitWorkflowOutput: unused as AgentControlPlane["emitWorkflowOutput"],
    listWorkflowsWithTriggers:
      unused as AgentControlPlane["listWorkflowsWithTriggers"],
    deleteWorkflow: unused as AgentControlPlane["deleteWorkflow"],
    upsertActionPolicyByName:
      unused as AgentControlPlane["upsertActionPolicyByName"],
    listActionPolicies: unused as AgentControlPlane["listActionPolicies"],
    listUsers: unused as AgentControlPlane["listUsers"],
    listChannels: unused as AgentControlPlane["listChannels"],
    listDataSources: unused as AgentControlPlane["listDataSources"],
    listAuditTrail: unused as AgentControlPlane["listAuditTrail"],
    describeSourceGraph: unused as AgentControlPlane["describeSourceGraph"],
    listSourceSecretNames:
      unused as AgentControlPlane["listSourceSecretNames"],
    importOpenApiSpec: unused as AgentControlPlane["importOpenApiSpec"],
    listOpenApiSpecs: unused as AgentControlPlane["listOpenApiSpecs"],
    askSourceConfigAgent:
      unused as AgentControlPlane["askSourceConfigAgent"],
    previewSourceConfigChange:
      unused as AgentControlPlane["previewSourceConfigChange"],
  };
}

function postEvents(
  port: number,
  token: string,
  events: unknown[] = [],
): Promise<Response> {
  return fetch(new URL("/v1/events", `http://127.0.0.1:${port}`), {
    method: "POST",
    headers: { authorization: `Bearer ${token}`, "content-type": "application/json" },
    body: JSON.stringify({ events }),
  });
}

describe("startAgentBroker token registry", () => {
  it("mints one stable token per run and resolves it over HTTP", async () => {
    const handle = await startAgentBroker({ controlPlane: stubControlPlane(), port: 0 });
    try {
      const a = handle.tokenFor({ runId: "r1", orgId: "o1", kind: "work" });
      expect(handle.tokenFor({ runId: "r1", orgId: "o1", kind: "work" })).toBe(a); // reused
      expect(handle.tokenFor({ runId: "r2", orgId: "o1", kind: "work" })).not.toBe(a); // per-run

      expect(handle.url).toBe(`http://127.0.0.1:${handle.port}`);

      expect((await postEvents(handle.port, a)).status).toBe(200);
      expect((await postEvents(handle.port, "unknown")).status).toBe(401);
    } finally {
      await handle.close();
    }
  });

  it("releases a run's token so it stops resolving", async () => {
    const handle = await startAgentBroker({ controlPlane: stubControlPlane(), port: 0 });
    try {
      const tok = handle.tokenFor({ runId: "r1", orgId: "o1", kind: "work" });
      handle.release("r1");
      expect((await postEvents(handle.port, tok)).status).toBe(401);
      // a token minted after release is a fresh one, not the revoked value:
      expect(handle.tokenFor({ runId: "r1", orgId: "o1", kind: "work" })).not.toBe(tok);
    } finally {
      await handle.close();
    }
  });

  it("advertises a custom host alias in the url", async () => {
    const handle = await startAgentBroker({
      controlPlane: stubControlPlane(),
      port: 0,
      hostAlias: "10.200.0.1",
    });
    try {
      expect(handle.url).toBe(`http://10.200.0.1:${handle.port}`);
    } finally {
      await handle.close();
    }
  });

  it("binds native records reads to the token's org and run", async () => {
    const cp = stubControlPlane();
    cp.listRecordCatalog = vi.fn(async () => ({ apps: [] }));
    const handle = await startAgentBroker({ controlPlane: cp, port: 0 });
    try {
      const token = handle.tokenFor({
        runId: "trusted-run",
        orgId: "trusted-org",
        kind: "work",
      });
      const response = await fetch(
        new URL("/v1/records/catalog", `http://127.0.0.1:${handle.port}`),
        {
          method: "POST",
          headers: {
            authorization: `Bearer ${token}`,
            "content-type": "application/json",
          },
          body: JSON.stringify({
            orgId: "other-org",
            runId: "other-run",
            appId: "equipment",
          }),
        },
      );
      expect(response.status).toBe(200);
      expect(cp.listRecordCatalog).toHaveBeenCalledWith({
        orgId: "trusted-org",
        runId: "trusted-run",
        appId: "equipment",
      });
    } finally {
      await handle.close();
    }
  });

  it("decides an action request's status from the rules, not the sandbox", async () => {
    const cp = stubControlPlane();
    cp.evaluateActionPolicy = vi.fn(async () => ({
      decision: "needs_approval" as const,
      mode: "approval_required" as const,
      policy: { id: "ask-policy" } as never,
    }));
    cp.createActionRequest = vi.fn(async () => ({ id: "req-1", status: "pending_approval" }));
    const handle = await startAgentBroker({ controlPlane: cp, port: 0 });
    try {
      const token = handle.tokenFor({ runId: "run-1", orgId: "org-1", kind: "work" });
      const response = await fetch(new URL("/v1/action/request", `http://127.0.0.1:${handle.port}`), {
        method: "POST",
        headers: { authorization: `Bearer ${token}`, "content-type": "application/json" },
        body: JSON.stringify({
          scope: "external",
          kind: "send_email",
          payload: { to: ["x@gmail.com"] },
          status: "approved",
          policyId: "forged",
        }),
      });
      expect(response.status).toBe(200);
      expect(cp.evaluateActionPolicy).toHaveBeenCalledWith(
        expect.objectContaining({ orgId: "org-1", kind: "send_email", payload: { to: ["x@gmail.com"] } }),
      );
      expect(cp.createActionRequest).toHaveBeenCalledWith(
        expect.objectContaining({ status: "pending_approval", policyId: "ask-policy", orgId: "org-1" }),
      );
    } finally {
      await handle.close();
    }
  });

  it("refuses an action request that the rules deny", async () => {
    const cp = stubControlPlane();
    cp.evaluateActionPolicy = vi.fn(async () => ({ decision: "no_policy" as const, reason: "no_matching_policy" as const }));
    cp.createActionRequest = vi.fn();
    const handle = await startAgentBroker({ controlPlane: cp, port: 0 });
    try {
      const token = handle.tokenFor({ runId: "run-1", orgId: "org-1", kind: "work" });
      const response = await fetch(new URL("/v1/action/request", `http://127.0.0.1:${handle.port}`), {
        method: "POST",
        headers: { authorization: `Bearer ${token}`, "content-type": "application/json" },
        body: JSON.stringify({ scope: "external", kind: "send_email", status: "approved" }),
      });
      expect(response.status).toBe(403);
      expect(cp.createActionRequest).not.toHaveBeenCalled();
    } finally {
      await handle.close();
    }
  });
});

describe("ensureAgentBroker (SEC9: always on)", () => {
  it("starts the per-process broker and is idempotent", async () => {
    const prevPort = process.env.OPENNEKO_BROKER_PORT;
    process.env.OPENNEKO_BROKER_PORT = "0";
    try {
      const a = await ensureAgentBroker();
      const b = await ensureAgentBroker();
      expect(a).toBeDefined();
      expect(b).toBe(a);
      const received: unknown[] = [];
      const unregister = registerAgentBrokerEventSink("r-live", async (event) => {
        received.push(event);
      });
      const token = a!.tokenFor({
        runId: "r-live",
        orgId: "o1",
        threadId: "t1",
        kind: "work",
      });
      const event = { type: "status", message: "Approval pending" };
      expect((await postEvents(a!.port, token, [event])).status).toBe(200);
      expect(received).toEqual([event]);
      unregister();
      await shutdownAgentBroker();
    } finally {
      if (prevPort === undefined) delete process.env.OPENNEKO_BROKER_PORT;
      else process.env.OPENNEKO_BROKER_PORT = prevPort;
    }
  });
});
