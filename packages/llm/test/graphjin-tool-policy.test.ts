import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
import type { Tool } from "@modelcontextprotocol/sdk/types.js";
import { describe, expect, it, vi } from "vitest";
import type { AgentControlPlane } from "../src/work/control-plane";
import {
  GRAPHJIN_DIRECT_GOVERNED_POLICY,
  parseGraphjinMcpToolPolicy,
} from "../src/work/graphjin-tool-policy";
import { buildGraphjinMcpServer } from "../src/work/tools";

const tools: Tool[] = [
  {
    name: "query_catalog",
    description: "Search the catalog",
    inputSchema: { type: "object", properties: {} },
    annotations: { readOnlyHint: true, destructiveHint: false },
  },
  {
    name: "execute_graphql",
    description: "Execute GraphQL",
    inputSchema: {
      type: "object",
      properties: { query: { type: "string" } },
      required: ["query"],
    },
    annotations: { readOnlyHint: false, destructiveHint: true },
  },
  {
    name: "execute_saved_query",
    description: "Execute an operation that can be a saved mutation",
    inputSchema: { type: "object", properties: {} },
  },
  {
    name: "ask_graphjin_agent",
    description: "Delegate to the server-side agent",
    inputSchema: { type: "object", properties: {} },
    annotations: { readOnlyHint: true, destructiveHint: false },
  },
  {
    name: "future_graphjin_tool",
    description: "An unclassified future tool",
    inputSchema: { type: "object", properties: {} },
    annotations: { readOnlyHint: true, destructiveHint: false },
  },
];

async function connectGraphjinServer(
  toolPolicy: typeof GRAPHJIN_DIRECT_GOVERNED_POLICY | null =
    GRAPHJIN_DIRECT_GOVERNED_POLICY,
) {
  const listGraphjinTools = vi.fn(async () => tools);
  const callGraphjinTool = vi.fn(async () => ({
    content: [{ type: "text" as const, text: "ok" }],
  }));
  const server = buildGraphjinMcpServer({
    orgId: "org-1",
    runId: "run-1",
    controlPlane: {
      listGraphjinTools,
      callGraphjinTool,
    } as unknown as AgentControlPlane,
    ...(toolPolicy ? { toolPolicy } : {}),
  });
  const [clientTransport, serverTransport] = InMemoryTransport.createLinkedPair();
  await server.instance.connect(serverTransport);
  const client = new Client({ name: "graphjin-policy-test", version: "1.0.0" });
  await client.connect(clientTransport);
  return { client, listGraphjinTools, callGraphjinTool };
}

describe("GraphJin direct-governed MCP policy", () => {
  it("publishes only classified direct tools without overriding GraphJin operation risk", async () => {
    const { client } = await connectGraphjinServer();
    try {
      const listed = await client.listTools();
      expect(listed.tools.map((tool) => tool.name)).toEqual([
        "query_catalog",
        "execute_graphql",
      ]);
      const execute = listed.tools.find((tool) => tool.name === "execute_graphql");
      expect(execute?.annotations).toMatchObject({
        readOnlyHint: false,
        destructiveHint: true,
      });
      expect(execute?.description).toContain("GraphJin enforces source and operation authorization");
    } finally {
      await client.close();
    }
  });

  it("blocks delegation first, then forwards an API call mutation before a read", async () => {
    const { client, callGraphjinTool } = await connectGraphjinServer();
    try {
      await expect(
        client.callTool({
          name: "ask_graphjin_agent",
          arguments: { instruction: "answer this for me" },
        }),
      ).rejects.toThrow(/direct-governed policy blocked tool/);
      await expect(
        client.callTool({ name: "future_graphjin_tool", arguments: {} }),
      ).rejects.toThrow(/direct-governed policy blocked tool/);

      const apiMutation = {
        orgId: "org-1",
        runId: "run-1",
        name: "execute_graphql",
        arguments: {
          query:
            'mutation { external_create_resource(call: {body: {name: "Example"}}) { ok status_code } }',
        },
      };
      const mutationResult = await client.callTool({
        name: apiMutation.name,
        arguments: apiMutation.arguments,
      });
      expect(mutationResult.isError).not.toBe(true);

      const read = {
        orgId: "org-1",
        runId: "run-1",
        name: "execute_graphql",
        arguments: { query: "query EvalHealth { salesorderheader(limit: 1) { salesorderid } }" },
      };
      const readResult = await client.callTool({
        name: read.name,
        arguments: read.arguments,
      });
      expect(readResult.isError).not.toBe(true);

      expect(callGraphjinTool).toHaveBeenCalledTimes(2);
      expect(callGraphjinTool.mock.calls).toEqual([[apiMutation], [read]]);
    } finally {
      await client.close();
    }
  });

  it("relays every direct tool but never GraphJin's own agent when no policy is set", async () => {
    const { client, callGraphjinTool } = await connectGraphjinServer(null);
    try {
      expect((await client.listTools()).tools.map((tool) => tool.name)).toEqual(
        tools.map((tool) => tool.name).filter((name) => name !== "ask_graphjin_agent"),
      );
      await expect(
        client.callTool({ name: "ask_graphjin_agent", arguments: { instruction: "production behavior" } }),
      ).rejects.toThrow(/neko_graphjin_agent/);
      expect(callGraphjinTool).not.toHaveBeenCalled();
    } finally {
      await client.close();
    }
  });

  it("fails closed on an unknown bridge policy value", () => {
    expect(parseGraphjinMcpToolPolicy(undefined)).toBeUndefined();
    expect(parseGraphjinMcpToolPolicy("direct-governed")).toBe(
      GRAPHJIN_DIRECT_GOVERNED_POLICY,
    );
    expect(() => parseGraphjinMcpToolPolicy("direct-ish")).toThrow(
      /unsupported GraphJin MCP tool policy/,
    );
  });
});
