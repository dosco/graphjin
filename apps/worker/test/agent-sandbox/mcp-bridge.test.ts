import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it, vi } from "vitest";
import { GRAPHJIN_DIRECT_GOVERNED_POLICY } from "@neko/llm/sandbox-runtime";
import {
  buildBridgeServer,
  buildMultiplexedBridgeServer,
  multiplexedToolName,
} from "../../src/agent-sandbox/mcp-bridge.js";
import { BrokerControlPlane } from "../../src/agent-sandbox/broker-client.js";

const SERVERS = [
  "neko_interaction",
  "neko_ui",
  "neko_graphjin",
  "neko_graphjin_agent",
  "neko_skills",
  "neko_memory",
  "neko_records",
  "neko_workflow_builder",
  "neko_workflow_output",
  "neko_action",
  "neko_rule_builder",
  "neko_user_manager",
  "neko_channel_manager",
  "neko_data_source_manager",
  "neko_source_config_manager",
  "neko_audit",
  "neko_plugin_actions",
  "neko_pack_actions",
];

function ctx() {
  return {
    runKind: "work" as const,
    orgId: "org-1",
    threadId: "11111111-1111-4111-8111-111111111111",
    runId: "22222222-2222-4222-8222-222222222222",
    skillsRoot: "/tmp/skills",
    workflowRunId: "33333333-3333-4333-8333-333333333333",
    triggeredByObservationId: null,
    pluginActions: [
      {
        kind: "records.create",
        description: "create",
        scope: "external" as const,
      },
    ],
    packActions: [
      {
        kind: "magento.manage_catalog",
        description: "Change Magento catalog data.",
        scope: "external" as const,
        default_mode: "ask" as const,
      },
    ],
    controlPlane: new BrokerControlPlane("http://127.0.0.1:9", "tok"),
  };
}

async function callGraphjinTool(runKind: "work" | "agent-job") {
  const listGraphjinTools = vi.fn(async () => [
    {
      name: "query_catalog",
      description: "Search the GraphJin catalog",
      inputSchema: {
        type: "object" as const,
        properties: { search: { type: "string" } },
      },
    },
    {
      name: "validate_where_clause",
      description: "Validate a GraphJin filter",
      inputSchema: {
        type: "object" as const,
        properties: {
          table: { type: "string" },
          where: { type: "object", additionalProperties: true },
        },
        required: ["table", "where"],
      },
    },
    {
      // A future/admin tool proves OpenNeko does not maintain an allow-list.
      name: "future_graphjin_tool",
      description: "A caller-visible future tool",
      inputSchema: { type: "object" as const, properties: {} },
    },
  ]);
  const callGraphjinTool = vi.fn(async () => ({
    content: [{ type: "text" as const, text: '{"cards":[]}' }],
  }));
  const logical = buildBridgeServer("neko_graphjin", {
    ...ctx(),
    runKind,
    controlPlane: {
      listGraphjinTools,
      callGraphjinTool,
    } as unknown as BrokerControlPlane,
  });
  const [clientTransport, serverTransport] = InMemoryTransport.createLinkedPair();
  await logical.instance.connect(serverTransport);
  const client = new Client({ name: "graphjin-identity-test", version: "1.0.0" });
  await client.connect(clientTransport);
  try {
    const listed = await client.listTools();
    expect(listed.tools.map((tool) => tool.name)).toEqual([
      "query_catalog",
      "validate_where_clause",
      "future_graphjin_tool",
    ]);
    expect(listed.tools[1]?.inputSchema).toEqual({
      type: "object",
      properties: {
        table: { type: "string" },
        where: { type: "object", additionalProperties: true },
      },
      required: ["table", "where"],
    });
    await client.callTool({
      name: "query_catalog",
      arguments: { search: "recent orders" },
    });
  } finally {
    await client.close();
  }
  return { listGraphjinTools, callGraphjinTool };
}

describe("mcp-bridge buildBridgeServer", () => {
  it("constructs a connectable server for every name hermes mounts", () => {
    for (const name of SERVERS) {
      const server = buildBridgeServer(name, ctx());
      expect(server.instance, name).toBeTruthy();
      expect(typeof server.instance.connect, name).toBe("function");
    }
  });

  it("throws on unknown server names", () => {
    expect(() => buildBridgeServer("neko_nope", ctx())).toThrow(/unknown server/);
  });

  it("binds work GraphJin reads to the actor run", async () => {
    const graphjin = await callGraphjinTool("work");
    expect(graphjin.listGraphjinTools).toHaveBeenCalledWith({
      orgId: "org-1",
      runId: "22222222-2222-4222-8222-222222222222",
    });
    expect(graphjin.callGraphjinTool).toHaveBeenCalledWith({
      orgId: "org-1",
      runId: "22222222-2222-4222-8222-222222222222",
      name: "query_catalog",
      arguments: { search: "recent orders" },
    });
  });

  it("omits a run binding for service-identity agent jobs", async () => {
    const graphjin = await callGraphjinTool("agent-job");
    expect(graphjin.listGraphjinTools).toHaveBeenCalledWith({
      orgId: "org-1",
    });
    expect(graphjin.callGraphjinTool).toHaveBeenCalledWith({
      orgId: "org-1",
      name: "query_catalog",
      arguments: { search: "recent orders" },
    });
  });

  it("rebuilds the GraphJin server with the per-run direct-governed policy", async () => {
    const listGraphjinTools = vi.fn(async () => [
      {
        name: "execute_graphql",
        description: "Execute GraphQL",
        inputSchema: {
          type: "object" as const,
          properties: { query: { type: "string" } },
          required: ["query"],
        },
        annotations: { readOnlyHint: false, destructiveHint: true },
      },
      {
        name: "ask_graphjin_agent",
        description: "Delegate to GraphJin",
        inputSchema: { type: "object" as const, properties: {} },
      },
    ]);
    const callGraphjinTool = vi.fn(async () => ({
      content: [{ type: "text" as const, text: "ok" }],
    }));
    const logical = buildBridgeServer("neko_graphjin", {
      ...ctx(),
      graphjinToolPolicy: GRAPHJIN_DIRECT_GOVERNED_POLICY,
      controlPlane: {
        listGraphjinTools,
        callGraphjinTool,
      } as unknown as BrokerControlPlane,
    });
    const [clientTransport, serverTransport] = InMemoryTransport.createLinkedPair();
    await logical.instance.connect(serverTransport);
    const client = new Client({ name: "graphjin-policy-bridge-test", version: "1.0.0" });
    await client.connect(clientTransport);
    try {
      const listed = await client.listTools();
      expect(listed.tools.map((tool) => tool.name)).toEqual(["execute_graphql"]);
      expect(listed.tools[0]?.annotations).toMatchObject({
        readOnlyHint: false,
        destructiveHint: true,
      });
      await client.callTool({
        name: "execute_graphql",
        arguments: {
          query:
            'mutation { external_create_resource(call: {body: {name: "Example"}}) { ok status_code } }',
        },
      });
      await client.callTool({
        name: "execute_graphql",
        arguments: {
          query:
            "query EvalHealth { salesorderheader(limit: 1) { salesorderid } }",
        },
      });
      await expect(
        client.callTool({
          name: "ask_graphjin_agent",
          arguments: { instruction: "delegate" },
        }),
      ).rejects.toThrow(/direct-governed policy blocked tool/);
      expect(callGraphjinTool).toHaveBeenCalledTimes(2);
    } finally {
      await client.close();
    }
  });

  it("preserves every native GraphJin tool and schema through the multiplexer", async () => {
    const listGraphjinTools = vi.fn(async () => [
      {
        name: "query_catalog",
        description: "Catalog",
        inputSchema: {
          type: "object" as const,
          properties: { ids: { type: "array", items: { type: "string" } } },
          required: ["ids"],
        },
      },
      {
        name: "future_graphjin_tool",
        description: "Future",
        inputSchema: { type: "object" as const, properties: {} },
      },
    ]);
    const callGraphjinTool = vi.fn(async () => ({
      content: [{ type: "text" as const, text: "ok" }],
    }));
    const multiplexed = await buildMultiplexedBridgeServer(
      ["neko_graphjin"],
      {
        ...ctx(),
        controlPlane: {
          listGraphjinTools,
          callGraphjinTool,
        } as unknown as BrokerControlPlane,
      },
    );
    const [clientTransport, serverTransport] = InMemoryTransport.createLinkedPair();
    await multiplexed.instance.connect(serverTransport);
    const client = new Client({ name: "graphjin-multiplexer-test", version: "1.0.0" });
    await client.connect(clientTransport);
    try {
      const listed = await client.listTools();
      expect(listed.tools.map((tool) => tool.name)).toEqual([
        "graphjin_query_catalog",
        "graphjin_future_graphjin_tool",
      ]);
      expect(listed.tools[0]?.inputSchema).toEqual({
        type: "object",
        properties: { ids: { type: "array", items: { type: "string" } } },
        required: ["ids"],
      });
      await client.callTool({
        name: "graphjin_query_catalog",
        arguments: { ids: ["table:app.public.orders"] },
      });
      expect(callGraphjinTool).toHaveBeenCalledWith({
        orgId: "org-1",
        runId: "22222222-2222-4222-8222-222222222222",
        name: "query_catalog",
        arguments: { ids: ["table:app.public.orders"] },
      });
    } finally {
      await client.close();
      await multiplexed.instance.close();
    }
  });

  it("multiplexes logical servers while preserving Hermes-facing tool names", async () => {
    const skillsRoot = await mkdtemp(join(tmpdir(), "neko-mcp-skills-"));
    const multiplexed = await buildMultiplexedBridgeServer(
      ["neko_skills", "neko_memory"],
      { ...ctx(), skillsRoot },
    );
    const [clientTransport, serverTransport] = InMemoryTransport.createLinkedPair();
    await multiplexed.instance.connect(serverTransport);
    const client = new Client({ name: "multiplexer-test", version: "1.0.0" });
    await client.connect(clientTransport);

    try {
      const listed = await client.listTools();
      const names = listed.tools.map((tool) => tool.name);
      expect(names).toContain("skills_create_skill");
      expect(names).toContain("memory_search");
      expect(names).toContain("memory_save");
      expect(new Set(names).size).toBe(names.length);
      expect(`mcp_neko_${multiplexedToolName("neko_memory", "search")}`).toBe(
        "mcp_neko_memory_search",
      );

      const result = await client.callTool({
        name: "skills_create_skill",
        arguments: {
          name: "benchmark-helper",
          description: "A deterministic bridge routing test.",
          body: "Return the benchmark marker.",
        },
      });
      expect(result.isError).not.toBe(true);
      expect(JSON.stringify(result.content)).toContain("benchmark-helper");
    } finally {
      await client.close();
      await multiplexed.instance.close();
      await rm(skillsRoot, { recursive: true, force: true });
    }
  });
});

describe("mcp-bridge lazy tool catalogs", () => {
  it("keeps every other tool when the GraphJin catalog is unavailable, then recovers", async () => {
    const listGraphjinTools = vi
      .fn()
      .mockRejectedValueOnce(new Error("no enabled GraphJin MCP endpoint configured"))
      .mockResolvedValue([
        {
          name: "query_catalog",
          description: "Catalog",
          inputSchema: { type: "object" as const, properties: {} },
        },
      ]);
    const controlPlane = new BrokerControlPlane("http://127.0.0.1:9", "tok");
    (controlPlane as unknown as { listGraphjinTools: unknown }).listGraphjinTools =
      listGraphjinTools;
    const multiplexed = await buildMultiplexedBridgeServer(
      ["neko_memory", "neko_graphjin", "neko_ui"],
      { ...ctx(), controlPlane },
    );
    const [clientTransport, serverTransport] = InMemoryTransport.createLinkedPair();
    await multiplexed.instance.connect(serverTransport);
    const client = new Client({ name: "lazy-catalog-test", version: "1.0.0" });
    await client.connect(clientTransport);
    try {
      const first = (await client.listTools()).tools.map((tool) => tool.name);
      expect(first).toContain("memory_search");
      expect(first).toContain("ui_render_cards");
      expect(first.some((name) => name.startsWith("graphjin_"))).toBe(false);

      const second = (await client.listTools()).tools.map((tool) => tool.name);
      expect(second).toContain("graphjin_query_catalog");
      expect(second).toContain("memory_search");
      expect(listGraphjinTools).toHaveBeenCalledTimes(2);
    } finally {
      await client.close();
      await multiplexed.instance.close();
    }
  });
});
