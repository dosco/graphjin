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

Declared-task lifecycle and journal writes stay on GraphQL roots `gj_task` and
`gj_task_entry`; GraphJin adds no task-specific MCP tools or resources. The one
MCP addition is optional `task_id` on `ask_graphjin_agent`, which loads the
same-owner open or verifying task as untrusted warm-start context and journals
the run. Task closes can declare a saved-query check in `verify_json`; GraphJin
runs it as the stored owner and returns `verified`, `failed`, or `pending`
state through the same GraphQL row. Agent responses also surface active and
failed task notices. See [Declared Tasks](/agentic/tasks/).

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

The MCP tool list is catalog-first in both sources and non-sources configs. In
`dev` and `agentic`, `graphql_help`, `query_catalog`, `execute_saved_query`,
`validate_where_clause`, and the [server-side agent](/agentic/server-agent/)
are available without feature toggles, with the remaining policy-allowed
primitive tools alongside them. Set `mcp.include_tools_with_agent: false` only
when the agent should be the single front door. Raw GraphQL remains separately
gated by `mcp.allow_raw_queries`.

Streamable HTTP is stateful by default in these modes, so an MCP client can
supply its model through sampling when no server provider key is configured.
Production retains the previous stateless, agent-off defaults.

In **dev mode**, the config tools `get_current_config`, `validate_config`, and `update_current_config` are also exposed — even when the agent is the front door — so a connected AI IDE keeps first-class configuration access. These are dev-only and never appear in agentic or production deployments. See [How Configuration Works](/configure/how-it-works/) for the full set of config interfaces.

For local development, named query auto-save and workflow saves fall back to config files only when there is no `user_id` or no artifact store.

{{< verified by="TestRegisterTools_SourcesUsedRawGraphQLCapabilityControlsTool" file="serv/mcp_registration_test.go" line="316" >}}
{{< verified by="TestMCPCallerCapabilityProfileReflectsSourceRootAccess" file="serv/mcp_registration_test.go" line="551" >}}

## Watch event resource

When watches are enabled, MCP exposes an aggregate caller-scoped resource and a per-watch template:

```text
graphjin://watch-events/unseen
graphjin://watch-events/unseen/{watch_id}
```

Clients should retain the ID returned by `gj_watch`, RFC 6570-expand the template so reserved characters in the ID are percent-encoded, and subscribe to that concrete per-watch URI. Exact subscriptions receive `notifications/resources/updated` only for that watch; the URI identifies which watch changed, and a read returns compact metadata only. The aggregate resource retains owner/account-wide compatibility for hosts without per-URI subscription support; those hosts must filter to the conversation's watch IDs before reading full events or marking them seen. Full event payloads remain behind `gj_watch_event` or the REST/GraphQL watch APIs.

Unsubscribing from this MCP resource only removes the in-memory resource subscription. It never pauses, expires, deletes, or cleans up watch definitions.

Creation, flow preview/approval, autonomous-action approval, pause/resume, and updates all use `gj_watch`. See [Choosing Watches, Flows, and Workflows](/agentic/watch-automation/) for the decision matrix and review examples.

{{< verified by="TestWatchMCPPerWatchRoutingSameOwnerSessions" file="serv/watches_test.go" line="1571" >}}
