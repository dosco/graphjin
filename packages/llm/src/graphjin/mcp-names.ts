/** Logical server name before the sandbox bridge multiplexes OpenNeko MCPs. */
export const OPENNEKO_GRAPHJIN_MCP_SERVER_NAME = "neko_graphjin" as const;

/**
 * The ACP title for a native GraphJin tool after the physical `neko`
 * multiplexer restores the logical server prefix.
 */
export function graphjinMcpToolTitle(nativeToolName: string): string {
  return `mcp_${OPENNEKO_GRAPHJIN_MCP_SERVER_NAME}_${nativeToolName}`;
}

export const GRAPHJIN_EXECUTE_GRAPHQL_TOOL_TITLE = graphjinMcpToolTitle(
  "execute_graphql",
);
export const GRAPHJIN_QUERY_CATALOG_TOOL_TITLE = graphjinMcpToolTitle(
  "query_catalog",
);
export const GRAPHJIN_VALIDATE_WHERE_TOOL_TITLE = graphjinMcpToolTitle(
  "validate_where_clause",
);

/** The ask tool of the neko_graphjin_agent server (GraphJin's server-side agent). */
export const GRAPHJIN_AGENT_ASK_TOOL_TITLE = "mcp_neko_graphjin_agent_ask" as const;
