---
title: "MCP"
description: "Connect AI clients to GraphJin through catalog-first Model Context Protocol tools."
nav_group: "agentic"
doc_kind: "guide"
weight: 10
---

## Install locally

```bash
graphjin mcp add codex
graphjin mcp add claude
```

For hosted GraphJin:

```bash
codex mcp add graphjin --url https://graphjin.example.com/api/v1/mcp
claude mcp add --transport http graphjin https://graphjin.example.com/api/v1/mcp
```

{{< svg "mcp-flow" "MCP discovery to governed action flow" >}}

## Tool philosophy

GraphJin's MCP surface starts with discovery:

- Search catalog rows before writing a query.
- Ask for query syntax and examples before choosing operators.
- Inspect `gj_security` before writes, workflows, config changes, file access, or code changes.
- Execute through saved queries or validated GraphQL.

{{< verified by="TestMCPCLIParity" file="cmd/mcp_parity_test.go" line="18" >}}
{{< verified by="TestProcessCursorsForMCP" file="serv/mcp_cursor_test.go" line="20" >}}

## Cursor IDs

MCP responses replace opaque GraphJin cursor strings with short cursor IDs when cursor caching is available. Clients pass those IDs back to `execute_graphql` or `execute_saved_query`; GraphJin expands them to the original encrypted cursor before execution.

Do not hardcode `gj-`, `__gj-enc:`, or any cursor prefix in an MCP client. GraphJin uses a dynamic security prefix, and prefix guessing can make encrypted cursor recognition fail.

```json
{
  "name": "ProductsPage",
  "variables": {
    "cursor": "cursor_01H..."
  }
}
```

{{< verified by="TestMCP_CursorRoundtripIntegration" file="serv/mcp_test.go" line="282" >}}
{{< verified by="TestMCP_AlreadyEncryptedCursorUnchanged" file="serv/mcp_test.go" line="669" >}}

## Production identity

HTTP MCP endpoints can be protected by OAuth or the same JWT/OIDC context as the main API. Stdio mode is useful for local development.

## Capability-aware tools

In sources mode, the MCP tool list reflects the caller's source capabilities. Catalog tools are advertised only when a `kind: graphjin` source enables catalog access, raw GraphQL execution follows the raw GraphQL capability/config gate, and workflow tools follow workflow capabilities.

{{< verified by="TestRegisterTools_SourcesUsedRawGraphQLCapabilityControlsTool" file="serv/mcp_registration_test.go" line="316" >}}
{{< verified by="TestMCPCallerCapabilityProfileReflectsSourceRootAccess" file="serv/mcp_registration_test.go" line="551" >}}
