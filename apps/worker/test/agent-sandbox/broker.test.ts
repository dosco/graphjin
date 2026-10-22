import type { AddressInfo } from "node:net";
import type { Server } from "node:http";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import type { AgentEvent } from "@neko/llm";
import type { AgentControlPlane } from "@neko/llm/work";
import { createAgentBroker, type RunBinding } from "../../src/agent-sandbox/broker";
import {
  BrokerControlPlane,
  postAgentEvents,
} from "../../src/agent-sandbox/broker-client";

interface Call {
  method: string;
  input: Record<string, unknown>;
}

function makeFakeControlPlane() {
  const calls: Call[] = [];
  const cp: AgentControlPlane = {
    async evaluateActionPolicy(input) {
      calls.push({ method: "evaluate", input: input as Record<string, unknown> });
      return { decision: "allow", policy: { id: "p1", name: "P" }, mode: "auto" } as Awaited<
        ReturnType<AgentControlPlane["evaluateActionPolicy"]>
      >;
    },
    async createActionRequest(input) {
      calls.push({ method: "create", input: input as Record<string, unknown> });
      return { id: "ar1" };
    },
    async enqueueActionExecute(input) {
      calls.push({ method: "enqueue", input });
    },
    async waitForActionExecution(input) {
      calls.push({ method: "wait", input });
      return {
        status: "succeeded",
        outcome: { result: { text: "live result" } },
      };
    },
    async rememberWorkMemory(input) {
      calls.push({ method: "remember", input: input as Record<string, unknown> });
      return { id: "m1" };
    },
    async searchWorkMemoryByContext(args) {
      calls.push({ method: "search", input: args as Record<string, unknown> });
      return [] as Awaited<
        ReturnType<AgentControlPlane["searchWorkMemoryByContext"]>
      >;
    },
    async queryGraphjinRead(input) {
      calls.push({ method: "graphjin-read", input: input as Record<string, unknown> });
      return { data: {} };
    },
    async listGraphjinTools(input) {
      calls.push({ method: "graphjin-tools-list", input: input as Record<string, unknown> });
      return [
        {
          name: "query_catalog",
          inputSchema: { type: "object", properties: {} },
        },
      ];
    },
    async callGraphjinTool(input) {
      calls.push({ method: "graphjin-tool-call", input: input as Record<string, unknown> });
      return { content: [{ type: "text", text: '{"cards":[]}' }] };
    },
    async askGraphjinDataAgent(input) {
      calls.push({ method: "graphjin-agent", input: input as Record<string, unknown> });
      return {
        response: {
          status: "answered",
          answer: "31,465 orders",
          data: { count: 31_465 },
        },
      };
    },
    async listRecordCatalog(input) {
      calls.push({ method: "records-catalog", input: input as Record<string, unknown> });
      return { apps: [] };
    },
    async findRecords(input) {
      calls.push({ method: "records-find", input: input as Record<string, unknown> });
      return {
        app: { appId: input.appId, label: "Equipment" },
        object: {
          apiName: input.objectApiName,
          label: "Loan",
          pluralLabel: "Loans",
        },
        columns: [],
        rows: [],
        total: 0,
        cursor: null,
      };
    },
    async getRecord(input) {
      calls.push({ method: "records-get", input: input as Record<string, unknown> });
      return {
        app: { appId: input.appId, label: "Equipment" },
        object: {
          apiName: input.objectApiName,
          label: "Loan",
          pluralLabel: "Loans",
        },
        columns: [],
        row: null,
      };
    },
    async findRecycledRecords(input) {
      calls.push({
        method: "records-recycle-find",
        input: input as Record<string, unknown>,
      });
      return {
        app: { appId: input.appId, label: "Equipment" },
        object: {
          apiName: input.objectApiName,
          label: "Loan",
          pluralLabel: "Loans",
        },
        rows: [],
        total: 0,
        cursor: null,
      };
    },
    async getRecycledRecord(input) {
      calls.push({
        method: "records-recycle-get",
        input: input as Record<string, unknown>,
      });
      return {
        app: { appId: input.appId, label: "Equipment" },
        object: {
          apiName: input.objectApiName,
          label: "Loan",
          pluralLabel: "Loans",
        },
        row: null,
      };
    },
    async listRecordBlueprints(input) {
      calls.push({
        method: "records-blueprints",
        input: input as Record<string, unknown>,
      });
      return { blueprints: [] };
    },
    async saveWorkflowWithTrigger(input) {
      calls.push({ method: "wf-save", input: input as Record<string, unknown> });
      return { action: "created", workflow: { id: "w1", name: "W" } } as Awaited<
        ReturnType<AgentControlPlane["saveWorkflowWithTrigger"]>
      >;
    },
    async emitWorkflowOutput(input) {
      calls.push({ method: "wf-output", input: input as Record<string, unknown> });
      return {
        id: "out1",
        orgId: input.orgId,
        workflowRunId: input.workflowRunId,
        workRunId: input.workRunId,
        kind: input.kind,
        title: input.title ?? "",
        body: input.body ?? "",
        payload: input.payload ?? {},
        artifactPath: input.artifactPath ?? null,
        scope: input.scope ?? null,
        topic: input.topic ?? null,
        mood: input.mood ?? null,
        timeWindowStart: input.timeWindowStart?.toISOString() ?? null,
        timeWindowEnd: input.timeWindowEnd?.toISOString() ?? null,
        freshnessTtlSeconds: input.freshnessTtlSeconds ?? null,
        seenCount: 1,
        lastSeenAt: "2026-01-01T00:00:00.000Z",
        createdAt: "2026-01-01T00:00:00.000Z",
      } as Awaited<ReturnType<AgentControlPlane["emitWorkflowOutput"]>>;
    },
    async listWorkflowsWithTriggers(input) {
      calls.push({ method: "wf-list", input: input as Record<string, unknown> });
      return { total: 0, workflows: [] };
    },
    async deleteWorkflow(input) {
      calls.push({ method: "wf-delete", input: input as Record<string, unknown> });
      return { found: true, name: "W" };
    },
    async upsertActionPolicyByName(input) {
      calls.push({ method: "rule-save", input: input as Record<string, unknown> });
      return { action: "created", policy: { id: "p1", name: "R" } } as Awaited<
        ReturnType<AgentControlPlane["upsertActionPolicyByName"]>
      >;
    },
    async listActionPolicies(input) {
      calls.push({ method: "rule-list", input: input as Record<string, unknown> });
      return { total: 0, policies: [] };
    },
    async listPlugins(input) {
      calls.push({ method: "plugins-list", input: input as Record<string, unknown> });
      return { installed: [], available: [] };
    },
    async listUsers(input) {
      calls.push({ method: "users-list", input: input as Record<string, unknown> });
      return { users: [] };
    },
    async listChannels(input) {
      calls.push({ method: "channels-list", input: input as Record<string, unknown> });
      return { workspaces: [], identities: [] };
    },
    async listDataSources(input) {
      calls.push({ method: "datasources-list", input: input as Record<string, unknown> });
      return { sources: [] };
    },
    async describeSourceGraph(input) {
      calls.push({ method: "source-graph", input: input as Record<string, unknown> });
      return { reachable: false };
    },
    async listSourceSecretNames(input) {
      calls.push({ method: "source-secrets", input: input as Record<string, unknown> });
      return { names: [] };
    },
    async importOpenApiSpec(input) {
      calls.push({ method: "openapi-import", input: input as Record<string, unknown> });
      return { error: "not exercised" };
    },
    async listOpenApiSpecs(input) {
      calls.push({ method: "openapi-list", input: input as Record<string, unknown> });
      return { assets: [] };
    },
    async askSourceConfigAgent(input) {
      calls.push({ method: "source-config-agent", input: input as Record<string, unknown> });
      return { response: { status: "answered", answer: "read-only" } };
    },
    async previewSourceConfigChange(input) {
      calls.push({ method: "source-config-preview", input: input as Record<string, unknown> });
      return {
        preview: {
          valid: true,
          patchHash: "hash",
          catalogRevision: "rev",
          expiresAt: null,
          scope: "source",
          reloadMode: "source_scoped",
          changes: [],
          findings: [],
          errors: [],
        },
      };
    },
    async listAuditTrail(input) {
      calls.push({ method: "audit-list", input: input as Record<string, unknown> });
      return { requests: [], alerts: [], gatewaySummary: [] };
    },
  };
  return { cp, calls };
}

describe("agent broker", () => {
  let server: Server;
  let baseUrl: string;
  let fake: ReturnType<typeof makeFakeControlPlane>;
  let events: Array<{ binding: RunBinding; evs: AgentEvent[] }>;

  beforeEach(async () => {
    fake = makeFakeControlPlane();
    events = [];
    server = createAgentBroker({
      controlPlane: fake.cp,
      // These tests exercise the direct GraphJin routes; an org on the
      // default agent path is refused them (covered below).
      graphjinAgentOnly: async () => false,
      resolveRun: (t) =>
        t === "good"
          ? { runId: "r1", orgId: "o1", kind: "work" }
          : t === "job"
            ? { runId: "j1", orgId: "o1", kind: "agent-job" }
            : undefined,
      onEvents: async (binding, evs) => {
        events.push({ binding, evs });
      },
    });
    await new Promise<void>((r) => server.listen(0, "127.0.0.1", r));
    baseUrl = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
  });

  afterEach(() => new Promise<void>((r) => server.close(() => r())));

  it("forces orgId/workRunId from the token binding, not the request body", async () => {
    const cp = new BrokerControlPlane(baseUrl, "good");
    await cp.evaluateActionPolicy({
      orgId: "SPOOF",
      scope: "external",
      kind: "k",
      target: null,
      riskLevel: null,
    } as Parameters<AgentControlPlane["evaluateActionPolicy"]>[0]);
    await cp.createActionRequest({
      orgId: "SPOOF",
      workRunId: "SPOOF",
      scope: "external",
      kind: "k",
      payload: {},
      status: "approved",
    } as Parameters<AgentControlPlane["createActionRequest"]>[0]);
    await cp.searchWorkMemoryByContext({ orgId: "SPOOF", query: "q" } as Parameters<
      AgentControlPlane["searchWorkMemoryByContext"]
    >[0]);
    await cp.listRecordCatalog({
      orgId: "SPOOF",
      runId: "SPOOF",
      appId: "equipment",
    });
    await cp.findRecycledRecords({
      orgId: "SPOOF",
      runId: "SPOOF",
      appId: "equipment",
      objectApiName: "loan",
      first: 5,
      search: "Camera",
    });
    await cp.getRecycledRecord({
      orgId: "SPOOF",
      runId: "SPOOF",
      appId: "equipment",
      objectApiName: "loan",
      recordId: "loan-deleted",
    });
    await cp.listRecordBlueprints({
      orgId: "SPOOF",
      blueprintId: "crm",
    });

    expect(fake.calls.find((c) => c.method === "evaluate")?.input.orgId).toBe("o1");
    const create = fake.calls.find((c) => c.method === "create")?.input;
    expect(create?.orgId).toBe("o1");
    expect(create?.workRunId).toBe("r1");
    expect(fake.calls.find((c) => c.method === "search")?.input.orgId).toBe("o1");
    expect(fake.calls.find((c) => c.method === "records-catalog")?.input).toEqual({
      orgId: "o1",
      runId: "r1",
      appId: "equipment",
    });
    expect(fake.calls.find((c) => c.method === "records-recycle-find")?.input).toEqual({
      orgId: "o1",
      runId: "r1",
      appId: "equipment",
      objectApiName: "loan",
      first: 5,
      search: "Camera",
    });
    expect(fake.calls.find((c) => c.method === "records-recycle-get")?.input).toEqual({
      orgId: "o1",
      runId: "r1",
      appId: "equipment",
      objectApiName: "loan",
      recordId: "loan-deleted",
    });
    expect(fake.calls.find((c) => c.method === "records-blueprints")?.input).toEqual({
      orgId: "o1",
      blueprintId: "crm",
    });
  });

  it("binds GraphJin data-agent delegation to the authenticated org and run", async () => {
    const cp = new BrokerControlPlane(baseUrl, "good");
    const result = await cp.askGraphjinDataAgent({
      orgId: "SPOOF",
      runId: "SPOOF",
      instruction: "Count all orders",
      maxSteps: 6,
    });

    expect(result.response?.data).toEqual({ count: 31_465 });
    expect(fake.calls.find((call) => call.method === "graphjin-agent")?.input).toEqual({
      orgId: "o1",
      runId: "r1",
      instruction: "Count all orders",
      maxSteps: 6,
      // GraphJin's steps go to the run as status events.
      onAction: expect.any(Function),
    });
  });

  it("lets a job ask GraphJin's agent without run entitlements", async () => {
    await new BrokerControlPlane(baseUrl, "job").askGraphjinDataAgent({ orgId: "SPOOF", runId: "SPOOF", instruction: "Count all orders" });
    expect(fake.calls.find((call) => call.method === "graphjin-agent")?.input).toEqual({ orgId: "o1", instruction: "Count all orders", onAction: expect.any(Function) });
  });

  it("refuses direct GraphJin routes when the org uses GraphJin's agent", async () => {
    const agentOnly = createAgentBroker({
      controlPlane: fake.cp,
      resolveRun: () => ({ runId: "r1", orgId: "o1", kind: "work" }),
      onEvents: async () => {},
      graphjinAgentOnly: async () => true,
    });
    await new Promise<void>((r) => agentOnly.listen(0, "127.0.0.1", r));
    try {
      const cp = new BrokerControlPlane(`http://127.0.0.1:${(agentOnly.address() as AddressInfo).port}`, "good");
      await expect(cp.listGraphjinTools({ orgId: "o1", runId: "r1" })).rejects.toThrow(/GraphJin agent/);
      await expect(cp.queryGraphjinRead({ orgId: "o1", runId: "r1", query: "query { a { id } }" })).rejects.toThrow(/GraphJin agent/);
      const answer = await cp.askGraphjinDataAgent({ orgId: "o1", runId: "r1", instruction: "Count all orders" });
      expect(answer.response?.data).toEqual({ count: 31_465 });
      expect(fake.calls.some((call) => call.method === "graphjin-tools-list" || call.method === "graphjin-read")).toBe(false);
    } finally {
      await new Promise<void>((r) => agentOnly.close(() => r()));
    }
  });

  it("uses actor identity for work GraphJin reads and service identity for jobs", async () => {
    const work = new BrokerControlPlane(baseUrl, "good");
    await work.queryGraphjinRead({
      orgId: "SPOOF",
      runId: "SPOOF",
      query: "query { sales_order { entity_id } }",
    });
    await work.listGraphjinTools({ orgId: "SPOOF", runId: "SPOOF" });
    await work.callGraphjinTool({
      orgId: "SPOOF",
      runId: "SPOOF",
      name: "query_catalog",
      arguments: { search: "orders" },
    });

    const job = new BrokerControlPlane(baseUrl, "job");
    await job.queryGraphjinRead({
      orgId: "SPOOF",
      runId: "SPOOF",
      query: "query { sales_order { entity_id } }",
    });
    await job.listGraphjinTools({ orgId: "SPOOF", runId: "SPOOF" });
    await job.callGraphjinTool({
      orgId: "SPOOF",
      runId: "SPOOF",
      name: "query_catalog",
      arguments: { search: "orders" },
    });

    const reads = fake.calls.filter((call) => call.method === "graphjin-read");
    expect(reads[0]?.input).toEqual({
      orgId: "o1",
      runId: "r1",
      query: "query { sales_order { entity_id } }",
    });
    expect(reads[1]?.input).toEqual({
      orgId: "o1",
      query: "query { sales_order { entity_id } }",
    });
    const lists = fake.calls.filter((call) => call.method === "graphjin-tools-list");
    expect(lists[0]?.input).toEqual({ orgId: "o1", runId: "r1" });
    expect(lists[1]?.input).toEqual({ orgId: "o1" });
    const calls = fake.calls.filter((call) => call.method === "graphjin-tool-call");
    expect(calls[0]?.input).toEqual({
      orgId: "o1",
      runId: "r1",
      name: "query_catalog",
      arguments: { search: "orders" },
    });
    expect(calls[1]?.input).toEqual({
      orgId: "o1",
      name: "query_catalog",
      arguments: { search: "orders" },
    });
  });

  it("round-trips return values and forces enqueue orgId", async () => {
    const cp = new BrokerControlPlane(baseUrl, "good");
    expect(
      await cp.createActionRequest({
        scope: "external",
        kind: "k",
        payload: {},
        status: "approved",
      } as Parameters<AgentControlPlane["createActionRequest"]>[0]),
    ).toEqual({ id: "ar1" });
    await cp.enqueueActionExecute({ orgId: "ignored", actionRequestId: "ar1" });
    expect(
      await cp.waitForActionExecution({
        orgId: "ignored",
        actionRequestId: "ar1",
        timeoutMs: 1234,
      }),
    ).toEqual({
      status: "succeeded",
      outcome: { result: { text: "live result" } },
    });
    expect(fake.calls.find((c) => c.method === "enqueue")?.input).toEqual({
      orgId: "o1",
      actionRequestId: "ar1",
    });
    expect(fake.calls.find((c) => c.method === "wait")?.input).toEqual({
      orgId: "o1",
      actionRequestId: "ar1",
      timeoutMs: 1234,
    });
  });

  it("forces builder save/list org + run provenance from the binding", async () => {
    const cp = new BrokerControlPlane(baseUrl, "good");
    await cp.saveWorkflowWithTrigger({
      orgId: "SPOOF",
      createdByRunId: "SPOOF",
      name: "n",
      steps: [],
    } as unknown as Parameters<AgentControlPlane["saveWorkflowWithTrigger"]>[0]);
    await cp.upsertActionPolicyByName({
      orgId: "SPOOF",
      createdByRunId: "SPOOF",
      name: "r",
      description: "",
      appliesToKinds: [],
      appliesToScopes: [],
      mode: "approval_required",
      priority: 0,
      enabled: true,
    } as unknown as Parameters<AgentControlPlane["upsertActionPolicyByName"]>[0]);
    await cp.listWorkflowsWithTriggers({ orgId: "SPOOF" });
    await cp.listActionPolicies({ orgId: "SPOOF" });

    const wfSave = fake.calls.find((c) => c.method === "wf-save")?.input;
    expect(wfSave?.orgId).toBe("o1");
    expect(wfSave?.createdByRunId).toBe("r1");
    const ruleSave = fake.calls.find((c) => c.method === "rule-save")?.input;
    expect(ruleSave?.orgId).toBe("o1");
    expect(ruleSave?.createdByRunId).toBe("r1");
    expect(fake.calls.find((c) => c.method === "wf-list")?.input.orgId).toBe("o1");
    expect(fake.calls.find((c) => c.method === "rule-list")?.input.orgId).toBe("o1");
  });

  it("emits workflow outputs through the binding and restores date fields", async () => {
    const cp = new BrokerControlPlane(baseUrl, "good");
    const output = await cp.emitWorkflowOutput({
      orgId: "SPOOF",
      workRunId: "SPOOF",
      workflowRunId: "wf-run-1",
      kind: "report",
      title: "Daily report",
      body: "Done",
      payload: {},
      timeWindowStart: new Date("2026-01-02T03:04:05.000Z"),
      timeWindowEnd: null,
    });

    expect(output).toMatchObject({
      id: "out1",
      orgId: "o1",
      workRunId: "r1",
      workflowRunId: "wf-run-1",
      timeWindowStart: "2026-01-02T03:04:05.000Z",
      timeWindowEnd: null,
    });
    const input = fake.calls.find((c) => c.method === "wf-output")?.input;
    expect(input?.orgId).toBe("o1");
    expect(input?.workRunId).toBe("r1");
    expect(input?.workflowRunId).toBe("wf-run-1");
    expect(input?.timeWindowStart).toBeInstanceOf(Date);
    expect((input?.timeWindowStart as Date).toISOString()).toBe(
      "2026-01-02T03:04:05.000Z",
    );
    expect(input?.timeWindowEnd).toBeNull();
  });

  it("forces delete org from the binding, keeps the workflowId from the body", async () => {
    const cp = new BrokerControlPlane(baseUrl, "good");
    const result = await cp.deleteWorkflow({ orgId: "SPOOF", workflowId: "w1" });
    expect(result).toEqual({ found: true, name: "W" });
    const wfDelete = fake.calls.find((c) => c.method === "wf-delete")?.input;
    expect(wfDelete?.orgId).toBe("o1");
    expect(wfDelete?.workflowId).toBe("w1");
  });

  it("rejects an invalid token (401)", async () => {
    const cp = new BrokerControlPlane(baseUrl, "bad");
    await expect(
      cp.createActionRequest({
        scope: "external",
        kind: "k",
        payload: {},
        status: "approved",
      } as Parameters<AgentControlPlane["createActionRequest"]>[0]),
    ).rejects.toThrow(/401/);
  });

  it("delivers events to the host sink with the run binding", async () => {
    await postAgentEvents(baseUrl, "good", [
      { type: "message", text: "hi" } as unknown as AgentEvent,
    ]);
    expect(events).toHaveLength(1);
    expect(events[0]?.binding).toEqual({ runId: "r1", orgId: "o1", kind: "work" });
    expect(events[0]?.evs[0]).toMatchObject({ type: "message", text: "hi" });
  });

  it("rejects events from an invalid token", async () => {
    await expect(
      postAgentEvents(baseUrl, "bad", [{ type: "message", text: "x" } as unknown as AgentEvent]),
    ).rejects.toThrow(/401/);
    expect(events).toHaveLength(0);
  });
});
