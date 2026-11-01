import type { Tool } from "@modelcontextprotocol/sdk/types.js";

/**
 * Per-run GraphJin MCP policy used by backend-parity evaluations. Keeping the
 * policy at the OpenNeko MCP boundary makes it independent of whether a
 * backend consumes the server in-process or through the ACP stdio bridge.
 * GraphJin remains the authority for source-aware operation policy, including
 * explicitly exposed OpenAPI `call` mutations.
 */
export type GraphjinMcpToolPolicy = Readonly<{
  mode: "direct-governed";
}>;

export const GRAPHJIN_DIRECT_GOVERNED_POLICY: GraphjinMcpToolPolicy =
  Object.freeze({ mode: "direct-governed" });

/**
 * How a run reaches customer data: only GraphJin's server-side agent, the
 * default, or GraphJin's direct tools when an admin opted into them.
 */
export type GraphjinDataPath = "direct" | "agent";

/**
 * GraphJin's own agent tool. Runs reach that agent only through the
 * neko_graphjin_agent server, which the org's data-path setting controls.
 */
export const GRAPHJIN_SERVER_AGENT_TOOL = "ask_graphjin_agent";

export const GRAPHJIN_TOOL_POLICY_ENV =
  "OPENNEKO_MCP_GRAPHJIN_TOOL_POLICY" as const;

/**
 * Strict allow-list for the primitive direct GraphJin surface. In particular,
 * execute_saved_query is excluded because it hides the operation being
 * attributed, and ask_graphjin_agent is excluded because it would delegate the
 * candidate's reasoning to GraphJin's internal agent. Unknown future tools
 * fail closed until their semantics are deliberately classified here.
 */
const DIRECT_GOVERNED_TOOL_NAMES = new Set([
  "graphql_help",
  "query_catalog",
  "validate_where_clause",
  "execute_graphql",
  // Read-only SQL over the tables GraphJin derives from sheets and CSV
  // files; GraphJin checks the caller can read each table's file.
  "query_file_tables",
]);

export function serializeGraphjinMcpToolPolicy(
  policy: GraphjinMcpToolPolicy,
): string {
  return policy.mode;
}

/** Parse the trusted bridge env. An unknown non-empty value fails closed. */
export function parseGraphjinMcpToolPolicy(
  value: string | undefined,
): GraphjinMcpToolPolicy | undefined {
  const mode = value?.trim();
  if (!mode) return undefined;
  if (mode === GRAPHJIN_DIRECT_GOVERNED_POLICY.mode) {
    return GRAPHJIN_DIRECT_GOVERNED_POLICY;
  }
  throw new Error(`unsupported GraphJin MCP tool policy: ${mode}`);
}

function directGovernedTool(tool: Tool): Tool | null {
  if (!DIRECT_GOVERNED_TOOL_NAMES.has(tool.name)) return null;
  if (tool.name !== "execute_graphql") return tool;

  // Preserve the potentially-destructive annotation: GraphJin may authorize a
  // single-root OpenAPI `call` mutation even though this benchmark's frozen
  // AdventureWorks source rejects database writes. The server owns that
  // source-aware decision; OpenNeko only removes delegated/non-parity tools.
  return {
    ...tool,
    description: [
      tool.description,
      "OpenNeko exposes direct execution only; GraphJin enforces source and operation authorization.",
    ]
      .filter(Boolean)
      .join(" "),
  };
}

export function applyGraphjinMcpToolPolicy(
  tools: readonly Tool[],
  policy: GraphjinMcpToolPolicy | undefined,
): Tool[] {
  if (!policy) return tools.filter((tool) => tool.name !== GRAPHJIN_SERVER_AGENT_TOOL);
  switch (policy.mode) {
    case "direct-governed":
      return tools.flatMap((tool) => {
        const allowed = directGovernedTool(tool);
        return allowed ? [allowed] : [];
      });
  }
}

/**
 * Enforce the call side independently of tools/list. MCP callers are allowed
 * to invoke a name without listing first, so filtering the catalog alone is
 * never a security boundary.
 */
export function assertGraphjinMcpToolCallAllowed(
  policy: GraphjinMcpToolPolicy | undefined,
  input: { name: string; arguments?: Record<string, unknown> },
): void {
  if (!policy) {
    if (input.name === GRAPHJIN_SERVER_AGENT_TOOL) {
      throw new Error(`GraphJin MCP tool ${GRAPHJIN_SERVER_AGENT_TOOL} is reached through neko_graphjin_agent`);
    }
    return;
  }
  if (!DIRECT_GOVERNED_TOOL_NAMES.has(input.name)) {
    throw new Error(
      `GraphJin MCP ${policy.mode} policy blocked tool ${JSON.stringify(input.name)}`,
    );
  }
  if (input.name !== "execute_graphql") return;

  const query = input.arguments?.query;
  if (typeof query !== "string" || !query.trim()) {
    throw new Error(
      "GraphJin MCP direct-governed policy requires execute_graphql.query to be a non-empty string",
    );
  }
}
