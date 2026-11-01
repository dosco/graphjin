import { afterEach, describe, expect, it, vi } from "vitest";
import {
  callGraphjinMcpTool,
  listGraphjinMcpTools,
} from "../src/graphjin/mcp-client";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("GraphJin stateless MCP client", () => {
  it("lists every page and preserves native tool schemas", async () => {
    const fetchMock = vi.fn(async (_url: string, init?: RequestInit) => {
      const request = JSON.parse(String(init?.body)) as {
        method: string;
        params: { cursor?: string; _meta?: Record<string, unknown> };
      };
      expect(request.method).toBe("tools/list");
      expect(request.params._meta).toMatchObject({
        "io.modelcontextprotocol/protocolVersion": "2026-07-28",
        "io.modelcontextprotocol/clientCapabilities": {},
        "io.modelcontextprotocol/clientInfo": {
          name: "openneko",
          version: "1.0.0",
        },
      });
      return new Response(
        JSON.stringify({
          jsonrpc: "2.0",
          id: 1,
          result: request.params.cursor
            ? {
                tools: [
                  {
                    name: "validate_where_clause",
                    inputSchema: {
                      type: "object",
                      required: ["table", "where"],
                      properties: {
                        table: { type: "string" },
                        where: { type: "object" },
                      },
                    },
                  },
                ],
              }
            : {
                tools: [
                  {
                    name: "query_catalog",
                    title: "Query catalog",
                    description: "catalog",
                    inputSchema: {
                      type: "object",
                      properties: { search: { type: "string" } },
                    },
                    outputSchema: {
                      type: "object",
                      properties: {
                        cards: { type: "array", items: true },
                        data: true,
                        impossible: false,
                      },
                    },
                    annotations: { readOnlyHint: true },
                    _meta: { "graphjin/catalog-revision": "rev-7" },
                  },
                ],
                nextCursor: "page-2",
              },
        }),
        { headers: { "content-type": "application/json" } },
      );
    });
    vi.stubGlobal("fetch", fetchMock);

    const tools = await listGraphjinMcpTools({
      baseUrl: "https://graphjin.example/api/v1/mcp",
      headers: { authorization: "Bearer actor" },
    });

    expect(tools.map((tool) => tool.name)).toEqual([
      "query_catalog",
      "validate_where_clause",
    ]);
    expect(tools[1]?.inputSchema.required).toEqual(["table", "where"]);
    expect(tools[0]).toMatchObject({
      title: "Query catalog",
      outputSchema: {
        type: "object",
        properties: {
          cards: { type: "array", items: {} },
          data: {},
          impossible: { not: {} },
        },
      },
      annotations: { readOnlyHint: true },
      _meta: { "graphjin/catalog-revision": "rev-7" },
    });
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(fetchMock.mock.calls[0]?.[1]?.headers).toMatchObject({
      authorization: "Bearer actor",
      "MCP-Protocol-Version": "2026-07-28",
      "Mcp-Method": "tools/list",
    });
    // GraphJin's current MCP contract is stateless: no initialize request.
    expect(
      fetchMock.mock.calls.some(([, init]) =>
        String(init?.body).includes('"method":"initialize"'),
      ),
    ).toBe(false);
  });

  it("calls a native tool and accepts Streamable HTTP SSE responses", async () => {
    const fetchMock = vi.fn(async (_url: string, init?: RequestInit) => {
      expect(JSON.parse(String(init?.body))).toMatchObject({
        method: "tools/call",
        params: {
          name: "query_catalog",
          arguments: { search: "orders" },
          _meta: {
            "io.modelcontextprotocol/protocolVersion": "2026-07-28",
            "io.modelcontextprotocol/clientCapabilities": {},
            "io.modelcontextprotocol/clientInfo": {
              name: "openneko",
              version: "1.0.0",
            },
          },
        },
      });
      expect(init?.headers).toMatchObject({
        "MCP-Protocol-Version": "2026-07-28",
        "Mcp-Method": "tools/call",
        "Mcp-Name": "query_catalog",
      });
      return new Response(
        'event: message\ndata: {"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"{\\"cards\\":[]}"}],"structuredContent":{"cards":[]},"_meta":{"graphjin":{"revision":"rev-7"}}}}\n\n',
        { headers: { "content-type": "text/event-stream" } },
      );
    });
    vi.stubGlobal("fetch", fetchMock);

    const result = await callGraphjinMcpTool(
      { baseUrl: "https://graphjin.example/api/v1/mcp" },
      { name: "query_catalog", arguments: { search: "orders" } },
    );
    expect(result.content).toEqual([
      { type: "text", text: '{"cards":[]}' },
    ]);
    expect(result.structuredContent).toEqual({ cards: [] });
    expect(result._meta).toEqual({ graphjin: { revision: "rev-7" } });
  });

  it("surfaces JSON-RPC tool errors with their code and data", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        new Response(
          JSON.stringify({
            jsonrpc: "2.0",
            id: 3,
            error: {
              code: -32602,
              message: "invalid table",
              data: { table: "missing" },
            },
          }),
          { headers: { "content-type": "application/json" } },
        ),
      ),
    );

    await expect(
      callGraphjinMcpTool(
        { baseUrl: "https://graphjin.example/api/v1/mcp" },
        { name: "describe_table", arguments: { table: "missing" } },
      ),
    ).rejects.toThrow(/-32602.*invalid table.*missing/);
  });
});
