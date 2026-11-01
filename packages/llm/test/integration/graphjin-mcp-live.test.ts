// Live validation of the stateless GraphJin MCP client against a REAL
// GraphJin 3.20 sources-mode server (auth: jwt). Skips unless one answers
// tools/list at OPENNEKO_TEST_GJ_SOURCES_URL (default :8090, the same rig
// agentic-knowledge-live uses). Rig recipe: packages/llm/README.md.

import { describe, expect, it } from "vitest";
import {
  callGraphjinMcpTool,
  listGraphjinMcpTools,
} from "../../src/graphjin/mcp-client";
import { mintGraphjinToken } from "../../src/graphjin/token";

const BASE =
  process.env.OPENNEKO_TEST_GJ_SOURCES_URL ?? "http://127.0.0.1:8090";
const MCP_URL = `${BASE}/api/v1/mcp`;
const ORG_ID = "org-gj4-live";

function serviceAuth(): Record<string, string> {
  return {
    authorization: `Bearer ${mintGraphjinToken({
      orgId: ORG_ID,
      userId: null,
      role: "service",
    })}`,
  };
}

async function serverReachable(): Promise<boolean> {
  try {
    await listGraphjinMcpTools({
      baseUrl: MCP_URL,
      headers: serviceAuth(),
      signal: AbortSignal.timeout(2500),
    });
    return true;
  } catch (e) {
    console.warn(
      `[graphjin-mcp-live] skipping: no GraphJin MCP at ${MCP_URL} (${e instanceof Error ? e.message : e})`,
    );
    return false;
  }
}

const reachable = await serverReachable();
const describeIfLive = reachable ? describe : describe.skip;

describeIfLive("GraphJin MCP live contract", () => {
  it("lists the native tool catalog over the stateless 2026-07-28 path", async () => {
    const tools = await listGraphjinMcpTools({
      baseUrl: MCP_URL,
      headers: serviceAuth(),
      signal: AbortSignal.timeout(10_000),
    });
    const names = tools.map((tool) => tool.name);
    // query_catalog appears only once a source is configured; the rig may
    // run with `sources: []`, so the assertion covers the role-independent set.
    for (const expected of [
      "execute_graphql",
      "validate_where_clause",
      "graphql_help",
      "execute_saved_query",
    ]) {
      expect(names).toContain(expected);
    }
    for (const tool of tools) {
      expect(tool.inputSchema.type).toBe("object");
    }
  });

  it("returns a native tools/call result unchanged", async () => {
    const result = await callGraphjinMcpTool(
      {
        baseUrl: MCP_URL,
        headers: serviceAuth(),
        signal: AbortSignal.timeout(10_000),
      },
      { name: "graphql_help", arguments: { for: "discovery" } },
    );
    expect(result.isError).not.toBe(true);
    expect(result.content.length).toBeGreaterThan(0);
  });

  it("GraphJin refuses a mutation through execute_graphql", async () => {
    const result = await callGraphjinMcpTool(
      {
        baseUrl: MCP_URL,
        headers: serviceAuth(),
        signal: AbortSignal.timeout(10_000),
      },
      {
        name: "execute_graphql",
        arguments: { query: 'mutation { orders(insert: { note: "x" }) { id } }' },
      },
    );
    // A read_only database source answers with a GraphQL error payload; a
    // config with allow_mutations off answers with an MCP tool error.
    const first = result.content[0];
    const text =
      first?.type === "text" ? first.text : JSON.stringify(result.content);
    expect(text).toMatch(/blocked|not allowed|read-only/i);
    if (result.isError !== true) {
      expect((JSON.parse(text) as { data: unknown }).data).toBeNull();
    }
  });
});
