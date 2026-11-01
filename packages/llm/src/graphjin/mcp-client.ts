import { packUserConnectionHeaders } from "./pack-user-connections";
import {
  CallToolResultSchema,
  ListToolsResultSchema,
  type CallToolResult,
  type Tool,
} from "@modelcontextprotocol/sdk/types.js";

type JsonRpcResponse = {
  jsonrpc?: string;
  id?: string | number | null;
  result?: unknown;
  error?: { code?: number; message?: string; data?: unknown };
};

export type GraphjinMcpRequestOptions = {
  baseUrl: string;
  headers?: Record<string, string>;
  signal?: AbortSignal;
};

let requestSequence = 0;

/** GraphJin 3.20's stateless MCP era; avoids the legacy session/init path. */
export const GRAPHJIN_MCP_PROTOCOL_VERSION = "2026-07-28" as const;
const GRAPHJIN_MCP_CLIENT_VERSION = "1.0.0" as const;

const SCHEMA_MAP_KEYWORDS = new Set([
  "$defs",
  "definitions",
  "dependentSchemas",
  "patternProperties",
  "properties",
]);
const SCHEMA_ARRAY_KEYWORDS = new Set([
  "allOf",
  "anyOf",
  "oneOf",
  "prefixItems",
]);
const SCHEMA_SINGLE_KEYWORDS = new Set([
  "additionalItems",
  "additionalProperties",
  "contains",
  "contentSchema",
  "else",
  "if",
  "items",
  "not",
  "propertyNames",
  "then",
  "unevaluatedItems",
  "unevaluatedProperties",
]);

/**
 * JSON Schema permits boolean schemas at every schema position. The MCP SDK
 * version shipped by OpenNeko currently models those positions as objects and
 * rejects GraphJin's valid `true`/`false` subschemas while parsing tools/list.
 * Convert them to exactly equivalent object schemas at the protocol boundary:
 * `{}` accepts everything and `{ not: {} }` accepts nothing.
 */
function normalizeBooleanJsonSchemas(schema: unknown): unknown {
  if (schema === true) return {};
  if (schema === false) return { not: {} };
  if (!schema || typeof schema !== "object" || Array.isArray(schema)) {
    return schema;
  }

  const normalized: Record<string, unknown> = {
    ...(schema as Record<string, unknown>),
  };
  for (const [key, value] of Object.entries(normalized)) {
    if (
      SCHEMA_MAP_KEYWORDS.has(key) &&
      value &&
      typeof value === "object" &&
      !Array.isArray(value)
    ) {
      normalized[key] = Object.fromEntries(
        Object.entries(value as Record<string, unknown>).map(([name, child]) => [
          name,
          normalizeBooleanJsonSchemas(child),
        ]),
      );
      continue;
    }
    if (SCHEMA_ARRAY_KEYWORDS.has(key) && Array.isArray(value)) {
      normalized[key] = value.map(normalizeBooleanJsonSchemas);
      continue;
    }
    if (SCHEMA_SINGLE_KEYWORDS.has(key)) {
      normalized[key] = Array.isArray(value)
        ? value.map(normalizeBooleanJsonSchemas)
        : normalizeBooleanJsonSchemas(value);
    }
  }
  return normalized;
}

function normalizeListedToolSchemas(result: unknown): unknown {
  if (!result || typeof result !== "object" || Array.isArray(result)) {
    return result;
  }
  const value = result as Record<string, unknown>;
  if (!Array.isArray(value.tools)) return result;
  return {
    ...value,
    tools: value.tools.map((tool) => {
      if (!tool || typeof tool !== "object" || Array.isArray(tool)) return tool;
      const normalized = { ...(tool as Record<string, unknown>) };
      if ("inputSchema" in normalized) {
        normalized.inputSchema = normalizeBooleanJsonSchemas(
          normalized.inputSchema,
        );
      }
      if ("outputSchema" in normalized) {
        normalized.outputSchema = normalizeBooleanJsonSchemas(
          normalized.outputSchema,
        );
      }
      return normalized;
    }),
  };
}

function modernRequestMeta(): Record<string, unknown> {
  return {
    "io.modelcontextprotocol/protocolVersion": GRAPHJIN_MCP_PROTOCOL_VERSION,
    "io.modelcontextprotocol/clientCapabilities": {},
    "io.modelcontextprotocol/clientInfo": {
      name: "openneko",
      version: GRAPHJIN_MCP_CLIENT_VERSION,
    },
  };
}

function parseEventStream(text: string): JsonRpcResponse | null {
  const responses: JsonRpcResponse[] = [];
  for (const event of text.split(/\r?\n\r?\n/)) {
    const data = event
      .split(/\r?\n/)
      .filter((line) => line.startsWith("data:"))
      .map((line) => line.slice("data:".length).trimStart())
      .join("\n")
      .trim();
    if (!data) continue;
    try {
      responses.push(JSON.parse(data) as JsonRpcResponse);
    } catch {
      // Ignore keepalive/non-JSON events and use the protocol response below.
    }
  }
  for (let index = responses.length - 1; index >= 0; index -= 1) {
    const response = responses[index]!;
    if ("result" in response || "error" in response) return response;
  }
  return null;
}

function parseResponseBody(text: string, contentType: string): JsonRpcResponse {
  let parsed: JsonRpcResponse | null = null;
  if (contentType.includes("text/event-stream")) {
    parsed = parseEventStream(text);
  } else {
    try {
      parsed = JSON.parse(text) as JsonRpcResponse;
    } catch {
      parsed = parseEventStream(text);
    }
  }
  if (!parsed || typeof parsed !== "object") {
    throw new Error("GraphJin MCP returned an invalid JSON-RPC response");
  }
  return parsed;
}

async function graphjinMcpRequest(
  opts: GraphjinMcpRequestOptions,
  method: "tools/list" | "tools/call",
  params: Record<string, unknown>,
): Promise<unknown> {
  const id = ++requestSequence;
  const response = await fetch(opts.baseUrl, {
    method: "POST",
    headers: await packUserConnectionHeaders(opts.baseUrl, {
      accept: "application/json, text/event-stream",
      "content-type": "application/json",
      "MCP-Protocol-Version": GRAPHJIN_MCP_PROTOCOL_VERSION,
      "Mcp-Method": method,
      ...(method === "tools/call" && typeof params.name === "string"
        ? { "Mcp-Name": params.name }
        : {}),
      ...(opts.headers ?? {}),
    }),
    redirect: "error",
    cache: "no-store",
    body: JSON.stringify({
      jsonrpc: "2.0",
      id,
      method,
      params: { ...params, _meta: modernRequestMeta() },
    }),
    signal: opts.signal,
  });
  const text = await response.text();
  if (!response.ok) {
    throw new Error(
      `GraphJin MCP ${method} failed: ${response.status} ${text.slice(0, 1_000)}`.trim(),
    );
  }
  const body = parseResponseBody(
    text,
    response.headers.get("content-type") ?? "",
  );
  if (body.error) {
    const detail = body.error.data === undefined
      ? ""
      : ` ${JSON.stringify(body.error.data).slice(0, 1_000)}`;
    throw new Error(
      `GraphJin MCP ${method} error ${body.error.code ?? "unknown"}: ${body.error.message ?? "unknown error"}${detail}`,
    );
  }
  if (!("result" in body)) {
    throw new Error(`GraphJin MCP ${method} returned no result`);
  }
  return body.result;
}

/**
 * List every caller-visible GraphJin tool, following MCP cursors. GraphJin's
 * current Streamable HTTP endpoint is stateless and accepts tools/list without
 * an initialize round-trip.
 */
export async function listGraphjinMcpTools(
  opts: GraphjinMcpRequestOptions,
): Promise<Tool[]> {
  const tools: Tool[] = [];
  let cursor: string | undefined;
  for (let page = 0; page < 100; page += 1) {
    const result = ListToolsResultSchema.parse(
      normalizeListedToolSchemas(
        await graphjinMcpRequest(
          opts,
          "tools/list",
          cursor ? { cursor } : {},
        ),
      ),
    );
    tools.push(...result.tools);
    if (!result.nextCursor) return tools;
    cursor = result.nextCursor;
  }
  throw new Error("GraphJin MCP tools/list exceeded 100 pages");
}

/** Call one tool exactly as advertised by the caller-visible GraphJin server. */
export async function callGraphjinMcpTool(
  opts: GraphjinMcpRequestOptions,
  input: { name: string; arguments?: Record<string, unknown> },
): Promise<CallToolResult> {
  return CallToolResultSchema.parse(
    await graphjinMcpRequest(opts, "tools/call", {
      name: input.name,
      arguments: input.arguments ?? {},
    }),
  );
}
