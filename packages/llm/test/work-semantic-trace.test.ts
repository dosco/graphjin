import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
import { describe, expect, it, vi } from "vitest";
import type { AgentControlPlane } from "../src/work/control-plane";
import { startAgentBroker } from "../src/work/broker";
import {
  WORK_SEMANTIC_TRACE_SCHEMA_VERSION,
  recordWorkSemanticHostEvent,
  registerWorkSemanticTraceSink,
  traceAgentControlPlane,
  workSemanticDigest,
  type WorkSemanticTraceEvent,
} from "../src/work/semantic-trace";
import { buildWorkMemoryServer } from "../src/work/tools";

function stubControlPlane(overrides: Record<string, unknown>): AgentControlPlane {
  const unused = async () => {
    throw new Error("control-plane method not configured for this test");
  };
  return new Proxy(overrides, {
    get(target, property) {
      return Reflect.has(target, property)
        ? Reflect.get(target, property)
        : unused;
    },
  }) as unknown as AgentControlPlane;
}

function postBroker(
  port: number,
  token: string,
  path: string,
  body: Record<string, unknown>,
): Promise<Response> {
  return fetch(new URL(path, `http://127.0.0.1:${port}`), {
    method: "POST",
    headers: {
      authorization: `Bearer ${token}`,
      "content-type": "application/json",
    },
    body: JSON.stringify(body),
  });
}

function eventFor<TOperation extends WorkSemanticTraceEvent["operation"]>(
  events: WorkSemanticTraceEvent[],
  operation: TOperation,
): Extract<WorkSemanticTraceEvent, { operation: TOperation }> {
  const event = events.find((candidate) => candidate.operation === operation);
  if (!event) throw new Error(`missing semantic trace event: ${operation}`);
  return event as Extract<WorkSemanticTraceEvent, { operation: TOperation }>;
}

function digestValues(value: unknown): string[] {
  if (Array.isArray(value)) return value.flatMap(digestValues);
  if (!value || typeof value !== "object") return [];
  return Object.entries(value as Record<string, unknown>).flatMap(
    ([key, child]) =>
      key.endsWith("Digest") && typeof child === "string"
        ? [child]
        : digestValues(child),
  );
}

describe("trusted broker semantic trace", () => {
  it("emits ordered, content-safe evidence after authoritative control-plane calls", async () => {
    const executionOrder: string[] = [];
    const memoryResult = [
      {
        source: "saved_memory",
        score: 0.98,
        memory: {
          id: "memory-1",
          userId: "user-1",
          scope: "global",
          kind: "business_rule",
          text: "SECRET_MEMORY_BODY",
        },
      },
    ];
    const libraryResult = [
      {
        layer: "personal",
        score: 0.93,
        concept: {
          id: "concept-1",
          userId: "user-1",
          status: "stable",
          sourceDocumentId: "document-1",
          body: "SECRET_LIBRARY_BODY",
          sources: [{ resource: "SECRET_LIBRARY_SOURCE" }],
        },
      },
    ];
    const workflowResult = {
      total: 1,
      workflows: [
        {
          id: "workflow-1",
          name: "SECRET_WORKFLOW_NAME",
          enabled: true,
          status: "active",
          definition: { instruction: "SECRET_WORKFLOW_DEFINITION" },
        },
      ],
    };
    const graphjinTools = [
      {
        name: "query_catalog",
        description: "SECRET_TOOL_DESCRIPTION",
        inputSchema: {
          type: "object",
          properties: { query: { type: "string" } },
        },
      },
      {
        name: "execute_graphql",
        inputSchema: {
          type: "object",
          properties: { query: { type: "string" } },
        },
      },
    ];

    const memorySearch = vi.fn(async () => {
      executionOrder.push("cp:memory.search");
      return memoryResult;
    });
    const librarySearch = vi.fn(async () => {
      executionOrder.push("cp:library.search");
      return libraryResult;
    });
    const workflowList = vi.fn(async () => {
      executionOrder.push("cp:workflow.list");
      return workflowResult;
    });
    const toolsList = vi.fn(async () => {
      executionOrder.push("cp:graphjin.tools_list");
      return graphjinTools;
    });
    const toolCall = vi.fn(
      async (input: { name: string; arguments?: Record<string, unknown> }) => {
        const operation =
          input.name === "query_catalog"
            ? "graphjin.catalog"
            : input.name === "execute_graphql"
              ? "graphjin.execute"
              : "graphjin.call";
        executionOrder.push(`cp:${operation}`);
        return {
          isError: false,
          content: [{ type: "text", text: "SECRET_GRAPHJIN_TOOL_RESULT" }],
        };
      },
    );
    const graphjinQuery = vi.fn(async () => {
      executionOrder.push("cp:graphjin.execute-direct");
      return { data: { privateValue: "SECRET_GRAPHJIN_QUERY_RESULT" } };
    });
    const cp = stubControlPlane({
      searchWorkMemoryByContext: memorySearch,
      searchLibraryForRun: librarySearch,
      listWorkflowsWithTriggers: workflowList,
      listGraphjinTools: toolsList,
      callGraphjinTool: toolCall,
      queryGraphjinRead: graphjinQuery,
    });
    const handle = await startAgentBroker({ controlPlane: cp, port: 0 });
    const events: WorkSemanticTraceEvent[] = [];
    const unregister = registerWorkSemanticTraceSink("trusted-run", (event) => {
      executionOrder.push(`trace:${event.operation}`);
      events.push(event);
    });

    try {
      const token = handle.tokenFor({
        runId: "trusted-run",
        orgId: "trusted-org",
        kind: "work",
      });
      const requests: Array<[string, Record<string, unknown>]> = [
        [
          "/v1/memory/search",
          {
            query: "SECRET_MEMORY_QUERY",
            limit: 2,
            includeArchives: false,
            userId: "attacker-user",
          },
        ],
        [
          "/v1/library/search",
          {
            query: "SECRET_LIBRARY_QUERY",
            limit: 3,
            userId: "attacker-user",
          },
        ],
        ["/v1/workflow/list", { limit: 4 }],
        ["/v1/graphjin/tools/list", {}],
        [
          "/v1/graphjin/tools/call",
          {
            name: "query_catalog",
            arguments: { query: "SECRET_CATALOG_QUERY" },
          },
        ],
        [
          "/v1/graphjin/tools/call",
          {
            name: "graphql_help",
            arguments: { topic: "SECRET_HELP_TOPIC" },
          },
        ],
        [
          "/v1/graphjin/tools/call",
          {
            name: "execute_graphql",
            arguments: {
              query: "query PrivateOrders { orders { id } }",
              variables: { tenant: "SECRET_TENANT" },
            },
          },
        ],
        [
          "/v1/graphjin/query",
          {
            query: "query PrivateCustomers { customers { id } }",
            variables: { region: "SECRET_REGION" },
            operationName: "PrivateCustomers",
          },
        ],
      ];
      for (const [path, body] of requests) {
        expect((await postBroker(handle.port, token, path, body)).status).toBe(
          200,
        );
      }

      expect(events.map((event) => event.operation)).toEqual([
        "memory.search",
        "library.search",
        "workflow.list",
        "graphjin.tools_list",
        "graphjin.catalog",
        "graphjin.call",
        "graphjin.execute",
        "graphjin.execute",
      ]);
      expect(events.map((event) => event.sequence)).toEqual([
        1, 2, 3, 4, 5, 6, 7, 8,
      ]);
      expect(executionOrder).toEqual([
        "cp:memory.search",
        "trace:memory.search",
        "cp:library.search",
        "trace:library.search",
        "cp:workflow.list",
        "trace:workflow.list",
        "cp:graphjin.tools_list",
        "trace:graphjin.tools_list",
        "cp:graphjin.catalog",
        "trace:graphjin.catalog",
        "cp:graphjin.call",
        "trace:graphjin.call",
        "cp:graphjin.execute",
        "trace:graphjin.execute",
        "cp:graphjin.execute-direct",
        "trace:graphjin.execute",
      ]);
      expect(events).toEqual(
        events.map((event) =>
          expect.objectContaining({
            schemaVersion: WORK_SEMANTIC_TRACE_SCHEMA_VERSION,
            runId: "trusted-run",
            orgId: "trusted-org",
            runKind: "work",
            source: "trusted-broker",
            status: "ok",
            durationMs: expect.any(Number),
            timestamp: expect.any(String),
            requestDigest: expect.stringMatching(/^sha256:[0-9a-f]{64}$/),
            responseDigest: expect.stringMatching(/^sha256:[0-9a-f]{64}$/),
          }),
        ),
      );

      const memory = eventFor(events, "memory.search");
      expect(memory.evidence).toEqual({
        requestedLimit: 2,
        returnedCount: 1,
        memories: [
          {
            id: "memory-1",
            contentDigest: workSemanticDigest("SECRET_MEMORY_BODY"),
            layer: "personal",
            scope: "global",
            kind: "business_rule",
          },
        ],
      });
      const library = eventFor(events, "library.search");
      expect(library.evidence).toEqual({
        requestedLimit: 3,
        returnedCount: 1,
        concepts: [
          {
            id: "concept-1",
            bodyDigest: workSemanticDigest("SECRET_LIBRARY_BODY"),
            layer: "personal",
            status: "stable",
            sourceDocumentId: "document-1",
            sourceDigests: [workSemanticDigest("SECRET_LIBRARY_SOURCE")],
          },
        ],
      });
      const workflows = eventFor(events, "workflow.list");
      expect(workflows.evidence).toMatchObject({
        requestedLimit: 4,
        total: 1,
        returnedCount: 1,
        workflows: [{ id: "workflow-1", enabled: true, status: "active" }],
      });
      const tools = eventFor(events, "graphjin.tools_list");
      expect(tools.evidence.tools).toEqual([
        {
          name: "query_catalog",
          schemaDigest: workSemanticDigest(graphjinTools[0].inputSchema),
        },
        {
          name: "execute_graphql",
          schemaDigest: workSemanticDigest(graphjinTools[1].inputSchema),
        },
      ]);
      const catalog = eventFor(events, "graphjin.catalog");
      expect(catalog.evidence).toEqual({
        toolName: "query_catalog",
        argumentsDigest: workSemanticDigest({ query: "SECRET_CATALOG_QUERY" }),
      });
      const directExecute = events[7];
      expect(directExecute).toMatchObject({
        operation: "graphjin.execute",
        evidence: {
          toolName: "queryGraphjinRead",
          queryDigest: workSemanticDigest(
            "query PrivateCustomers { customers { id } }",
          ),
          variablesDigest: workSemanticDigest({ region: "SECRET_REGION" }),
          operationType: "query",
        },
      });

      expect(memorySearch).toHaveBeenCalledWith({
        query: "SECRET_MEMORY_QUERY",
        limit: 2,
        includeArchives: false,
        orgId: "trusted-org",
        runId: "trusted-run",
      });
      expect(librarySearch).toHaveBeenCalledWith({
        query: "SECRET_LIBRARY_QUERY",
        limit: 3,
        orgId: "trusted-org",
        runId: "trusted-run",
      });
      expect(digestValues(events).length).toBeGreaterThan(0);
      expect(
        digestValues(events).every((digest) =>
          /^sha256:[0-9a-f]{64}$/.test(digest),
        ),
      ).toBe(true);
      const serialized = JSON.stringify(events);
      for (const secret of [
        "SECRET_MEMORY_BODY",
        "SECRET_LIBRARY_BODY",
        "SECRET_LIBRARY_SOURCE",
        "SECRET_WORKFLOW_NAME",
        "SECRET_WORKFLOW_DEFINITION",
        "SECRET_TOOL_DESCRIPTION",
        "SECRET_GRAPHJIN_TOOL_RESULT",
        "SECRET_MEMORY_QUERY",
        "SECRET_LIBRARY_QUERY",
        "SECRET_CATALOG_QUERY",
        "SECRET_HELP_TOPIC",
        "SECRET_TENANT",
        "SECRET_REGION",
      ]) {
        expect(serialized).not.toContain(secret);
      }
    } finally {
      unregister();
      await handle.close();
    }
  });

  it("traces an actual in-process MCP call and avoids duplicate broker evidence", async () => {
    const memoryResult = [
      {
        source: "saved_memory",
        score: 0.9,
        memory: {
          id: "mcp-memory-1",
          userId: null,
          scope: "global",
          kind: "business_rule",
          text: "SECRET_MCP_MEMORY_BODY",
        },
      },
    ];
    const memorySearch = vi.fn(async () => memoryResult);
    const baseControlPlane = stubControlPlane({
      searchWorkMemoryByContext: memorySearch,
    });
    const binding = {
      runId: "mcp-run",
      orgId: "trusted-org",
      kind: "work" as const,
    };
    const events: WorkSemanticTraceEvent[] = [];
    const unregister = registerWorkSemanticTraceSink(binding.runId, (event) => {
      events.push(event);
    });
    const tracedControlPlane = traceAgentControlPlane(baseControlPlane, binding);
    expect(traceAgentControlPlane(tracedControlPlane, binding)).toBe(
      tracedControlPlane,
    );
    const server = buildWorkMemoryServer(
      {
        orgId: binding.orgId,
        threadId: "thread-1",
        runId: binding.runId,
      },
      { controlPlane: tracedControlPlane },
    );
    const [clientTransport, serverTransport] =
      InMemoryTransport.createLinkedPair();
    await server.instance.connect(serverTransport);
    const client = new Client({
      name: "semantic-trace-mcp-test",
      version: "1.0.0",
    });
    await client.connect(clientTransport);
    let handle: Awaited<ReturnType<typeof startAgentBroker>> | undefined;

    try {
      const result = await client.callTool({
        name: "search",
        arguments: { query: "SECRET_MCP_MEMORY_QUERY" },
      });
      expect(result.isError).not.toBe(true);
      expect(events).toHaveLength(1);
      expect(events[0]).toMatchObject({
        sequence: 1,
        source: "trusted-host",
        operation: "memory.search",
        status: "ok",
        evidence: {
          returnedCount: 1,
          memories: [
            {
              id: "mcp-memory-1",
              contentDigest: workSemanticDigest("SECRET_MCP_MEMORY_BODY"),
            },
          ],
        },
      });

      handle = await startAgentBroker({
        controlPlane: tracedControlPlane,
        port: 0,
      });
      const token = handle.tokenFor(binding);
      expect(
        (
          await postBroker(handle.port, token, "/v1/memory/search", {
            query: "SECRET_BROKER_MEMORY_QUERY",
          })
        ).status,
      ).toBe(200);
      expect(memorySearch).toHaveBeenCalledTimes(2);
      expect(events).toHaveLength(2);
      expect(events[1]).toMatchObject({
        sequence: 2,
        source: "trusted-broker",
        operation: "memory.search",
        status: "ok",
      });
      expect(JSON.stringify(events)).not.toContain("SECRET_MCP_MEMORY_BODY");
      expect(JSON.stringify(events)).not.toContain("SECRET_MCP_MEMORY_QUERY");
      expect(JSON.stringify(events)).not.toContain("SECRET_BROKER_MEMORY_QUERY");
    } finally {
      await client.close();
      await handle?.close();
      unregister();
    }
  });

  it("records privacy-safe action-policy decisions at host and broker boundaries", async () => {
    const decision = {
      decision: "deny" as const,
      mode: "observe_only" as const,
      reason: "target denied",
      policy: { id: "policy-1", name: "Outbound guard" },
    };
    const evaluateActionPolicy = vi.fn(async () => decision);
    const controlPlane = stubControlPlane({ evaluateActionPolicy });
    const binding = {
      runId: "action-policy-run",
      orgId: "trusted-org",
      kind: "workflow" as const,
    };
    const events: WorkSemanticTraceEvent[] = [];
    const unregister = registerWorkSemanticTraceSink(binding.runId, (event) => {
      events.push(event);
    });
    const handle = await startAgentBroker({ controlPlane, port: 0 });
    const target = "channel:SECRET-EXTERNAL-TARGET";

    try {
      await traceAgentControlPlane(controlPlane, binding).evaluateActionPolicy({
        orgId: binding.orgId,
        scope: "external",
        kind: "eval_send_notice",
        target,
        riskLevel: "high",
      });
      const token = handle.tokenFor(binding);
      expect(
        (
          await postBroker(handle.port, token, "/v1/policy/evaluate", {
            scope: "external",
            kind: "eval_send_notice",
            target,
            riskLevel: "high",
          })
        ).status,
      ).toBe(200);

      expect(evaluateActionPolicy).toHaveBeenCalledTimes(2);
      expect(events).toHaveLength(2);
      expect(events.map((event) => event.source)).toEqual([
        "trusted-host",
        "trusted-broker",
      ]);
      for (const event of events) {
        expect(event).toMatchObject({
          operation: "action.policy",
          status: "ok",
          evidence: {
            kind: "eval_send_notice",
            scope: "external",
            decision: "deny",
            mode: "observe_only",
            targetDigest: workSemanticDigest(target),
            policyDigest: workSemanticDigest("policy-1"),
          },
        });
      }
      expect(JSON.stringify(events)).not.toContain(target);
      expect(JSON.stringify(events)).not.toContain("Outbound guard");
    } finally {
      await handle.close();
      unregister();
    }
  });

  it("records typed, digest-only host prefetch and skill-load evidence", async () => {
    const events: WorkSemanticTraceEvent[] = [];
    const binding = {
      runId: "host-event-run",
      orgId: "trusted-org",
      kind: "agent-job" as const,
    };
    const unregister = registerWorkSemanticTraceSink(binding.runId, (event) => {
      events.push(event);
    });
    const warning = vi.spyOn(console, "warn").mockImplementation(() => {});

    try {
      await recordWorkSemanticHostEvent({
        binding,
        operation: "memory.prefetched",
        memories: [
          {
            id: "prefetched-memory-1",
            contentDigest: workSemanticDigest("SECRET_PREFETCHED_MEMORY_BODY"),
            layer: "personal",
            scope: "global",
            kind: "business_rule",
          },
        ],
      });
      await recordWorkSemanticHostEvent({
        binding,
        operation: "skill.loaded",
        skill: {
          id: "aw-tax-brief",
          contentDigest: workSemanticDigest("SECRET_SKILL_BODY"),
          sourceDigest: workSemanticDigest("SECRET_SKILL_PATH"),
        },
      });
      await recordWorkSemanticHostEvent({
        binding,
        operation: "skill.loaded",
        skill: {
          id: "invalid-skill",
          contentDigest: "SECRET_UNHASHED_SKILL_BODY",
        },
      });

      expect(events).toHaveLength(2);
      expect(events).toMatchObject([
        {
          sequence: 1,
          source: "trusted-host",
          operation: "memory.prefetched",
          status: "ok",
          evidence: {
            returnedCount: 1,
            memories: [{ id: "prefetched-memory-1", layer: "personal" }],
          },
        },
        {
          sequence: 2,
          source: "trusted-host",
          operation: "skill.loaded",
          status: "ok",
          evidence: { id: "aw-tax-brief" },
        },
      ]);
      expect(warning).toHaveBeenCalledWith(
        "[work-semantic-trace] emit failed run=host-event-run operation=skill.loaded",
      );
      const serialized = JSON.stringify(events);
      expect(serialized).not.toContain("SECRET_PREFETCHED_MEMORY_BODY");
      expect(serialized).not.toContain("SECRET_SKILL_BODY");
      expect(serialized).not.toContain("SECRET_SKILL_PATH");
      expect(serialized).not.toContain("SECRET_UNHASHED_SKILL_BODY");
    } finally {
      warning.mockRestore();
      unregister();
    }
  });

  it("records thrown and logical control-plane failures without raw error bodies", async () => {
    const thrownMessage = "SECRET_CONTROL_PLANE_FAILURE";
    const logicalFailure = {
      isError: true,
      content: [{ type: "text", text: "SECRET_GRAPHJIN_FAILURE_BODY" }],
    };
    const embeddedGraphqlFailure = {
      isError: false,
      content: [
        {
          type: "text",
          text: JSON.stringify({
            errors: [{ message: "SECRET_GRAPHQL_FAILURE_BODY" }],
          }),
        },
      ],
    };
    let graphjinCalls = 0;
    const cp = stubControlPlane({
      searchWorkMemoryByContext: vi.fn(async () => {
        throw new Error(thrownMessage);
      }),
      callGraphjinTool: vi.fn(async () => {
        graphjinCalls += 1;
        return graphjinCalls === 1 ? logicalFailure : embeddedGraphqlFailure;
      }),
    });
    const handle = await startAgentBroker({ controlPlane: cp, port: 0 });
    const events: WorkSemanticTraceEvent[] = [];
    const unregister = registerWorkSemanticTraceSink("failure-run", (event) => {
      events.push(event);
    });
    const errorLog = vi.spyOn(console, "error").mockImplementation(() => {});

    try {
      const token = handle.tokenFor({
        runId: "failure-run",
        orgId: "trusted-org",
        kind: "workflow",
      });
      expect(
        (
          await postBroker(handle.port, token, "/v1/memory/search", {
            query: "failure query",
          })
        ).status,
      ).toBe(500);
      expect(
        (
          await postBroker(handle.port, token, "/v1/graphjin/tools/call", {
            name: "query_catalog",
            arguments: { query: "failure query" },
          })
        ).status,
      ).toBe(200);
      expect(
        (
          await postBroker(handle.port, token, "/v1/graphjin/tools/call", {
            name: "execute_graphql",
            arguments: { query: "mutation { blocked(update: {}) { id } }" },
          })
        ).status,
      ).toBe(200);

      expect(events).toHaveLength(3);
      expect(events[0]).toMatchObject({
        sequence: 1,
        operation: "memory.search",
        status: "error",
        errorType: "Error",
        errorDigest: workSemanticDigest(thrownMessage),
        evidence: { returnedCount: 0, memories: [] },
      });
      expect(events[0].responseDigest).toBeUndefined();
      expect(events[1]).toMatchObject({
        sequence: 2,
        operation: "graphjin.catalog",
        status: "error",
        errorType: "tool_result_error",
        errorDigest: workSemanticDigest(logicalFailure),
        responseDigest: workSemanticDigest(logicalFailure),
      });
      expect(events[2]).toMatchObject({
        sequence: 3,
        operation: "graphjin.execute",
        status: "error",
        errorType: "tool_result_error",
        errorDigest: workSemanticDigest(embeddedGraphqlFailure),
        evidence: { operationType: "mutation" },
      });
      expect(JSON.stringify(events)).not.toContain(thrownMessage);
      expect(JSON.stringify(events)).not.toContain(
        "SECRET_GRAPHJIN_FAILURE_BODY",
      );
      expect(JSON.stringify(events)).not.toContain(
        "SECRET_GRAPHQL_FAILURE_BODY",
      );
    } finally {
      errorLog.mockRestore();
      unregister();
      await handle.close();
    }
  });

  it("leaves broker results unchanged with no sink or a failing sink", async () => {
    const memorySearch = vi.fn(async () => []);
    const cp = stubControlPlane({ searchWorkMemoryByContext: memorySearch });
    const handle = await startAgentBroker({ controlPlane: cp, port: 0 });
    const warning = vi.spyOn(console, "warn").mockImplementation(() => {});
    let unregister = () => {};

    try {
      const token = handle.tokenFor({
        runId: "optional-sink-run",
        orgId: "trusted-org",
        kind: "agent-job",
      });
      expect(
        (
          await postBroker(handle.port, token, "/v1/memory/search", {
            query: "first",
          })
        ).status,
      ).toBe(200);

      unregister = registerWorkSemanticTraceSink("optional-sink-run", () => {
        throw new Error("semantic trace destination unavailable");
      });
      expect(
        (
          await postBroker(handle.port, token, "/v1/memory/search", {
            query: "second",
          })
        ).status,
      ).toBe(200);
      expect(memorySearch).toHaveBeenCalledTimes(2);
      expect(warning).toHaveBeenCalledWith(
        "[work-semantic-trace] emit failed run=optional-sink-run operation=memory.search",
      );
    } finally {
      unregister();
      warning.mockRestore();
      await handle.close();
    }
  });
});
