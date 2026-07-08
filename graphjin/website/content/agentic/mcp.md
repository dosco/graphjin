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

`query_catalog` and `execute_saved_query` use the same artifact overlay as HTTP GraphQL. With a configured artifact store and `user_id`, the caller sees global config files plus their own `gj_artifacts` rows.

{{< verified by="TestMCPCLIParity" file="cmd/mcp_parity_test.go" line="18" >}}
{{< verified by="TestProcessCursorsForMCP" file="serv/mcp_cursor_test.go" line="20" >}}
{{< verified by="TestUserArtifactSavedQueryOverridesGlobalOnlyForOwner" file="serv/artifact_overlay_test.go" line="94" >}}

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

The MCP tool list is catalog-first in both sources and non-sources configs. `graphql_help`, `query_catalog`, `execute_saved_query`, and `validate_where_clause` form the stable bootstrap surface; raw GraphQL and the server-side agent appear only when their MCP/agent gates enable them.

For local development, named query auto-save and workflow saves fall back to config files only when there is no `user_id` or no artifact store.

{{< verified by="TestRegisterTools_SourcesUsedRawGraphQLCapabilityControlsTool" file="serv/mcp_registration_test.go" line="316" >}}
{{< verified by="TestMCPCallerCapabilityProfileReflectsSourceRootAccess" file="serv/mcp_registration_test.go" line="551" >}}

## Watch event resource

When watches are enabled, MCP exposes one caller-scoped resource:

```text
graphjin://watch-events/unseen
```

Clients may subscribe to it and receive `notifications/resources/updated` when their own unseen watch events appear. A resource read returns compact metadata only: event IDs, watch IDs, timestamps, data hashes, truncation flags, and delivery status. Full event payloads remain behind `gj_watch_event` or the REST/GraphQL watch APIs.

Unsubscribing from this MCP resource only removes the in-memory resource subscription. It never pauses, expires, deletes, or cleans up watch definitions.

{{< verified by="TestWatchMCPUnseenResourceAndSubscriptionNotification" file="serv/watches_test.go" line="1337" >}}
