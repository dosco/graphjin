---
title: "Saved Queries"
description: "Use reviewed operation files as the production contract."
nav_group: "start"
doc_kind: "guide"
weight: 40
---

## Why saved queries matter

In development, GraphJin can accept dynamic GraphQL. In production, the secure default is to execute reviewed saved operations. This keeps the runtime shape fixed while still letting clients pass variables.

```graphql
# queries/products.graphql
query Products($limit: Int) {
  products(limit: $limit, order_by: { id: asc }) {
    id
    name
  }
}
```

The client calls the saved operation by name rather than sending a new query body.

Saved operations can be namespaced when a deployment needs separate surfaces for web clients, internal jobs, and agents. The allow-list loader stores the compiled operation and invalidates cached compiled state when the operation changes.

Config files under `queries/` are global saved queries. When `artifacts.enabled` is configured and a request has `user_id`, `gj_artifacts` becomes a user-scoped overlay: execution resolves the user's `kind = "saved_query"` artifact first, then falls back to the global file. Fragments use the same rule under `queries/fragments/`.

{{< verified by="TestAllowList" file="tests/core_test.go" line="92" >}}
{{< verified by="TestGetByNameAllowsNamespacedQueries" file="core/internal/allow/allow_test.go" line="68" >}}
{{< verified by="TestUserArtifactSavedQueryOverridesGlobalOnlyForOwner" file="serv/artifact_overlay_test.go" line="94" >}}

## Production call shape

```bash
curl http://localhost:8080/api/v1/rest/Products \
  -H 'content-type: application/json' \
  -d '{"limit":10}'
```

The exact route shape depends on the service mode and client, but the important contract is the same: the caller supplies a saved name plus variables, not new GraphQL text.

For MCP clients, prefer saved-query execution tools when a production server disables raw GraphQL.

REST saved-query routes, MCP `execute_saved_query`, and the server-side agent all use the same merged resolver. A user artifact can override a global saved query for that caller without changing another caller's result.

## What this protects

- Query shape changes require code review.
- RBAC and allow-list checks run against known operations.
- Agents can discover approved capabilities without inventing arbitrary operations.
- Variables still keep the operation reusable.

## Authoring checklist

| Check | Why |
| --- | --- |
| Give the operation a clear name | Logs, cache keys, metrics, and client code become easier to inspect. |
| Add `order_by` when order matters | Tests and pagination stay deterministic across dialects. |
| Keep variables scalar or input values | GraphJin validates the query shape before runtime variable values arrive. |
| Include cursors as returned values | Cursor strings are opaque and must be replayed unchanged. |

## Related controls

Saved queries fit with [role-based access control](/configure/auth-rbac/), [production recommendations](/configure/environment-production/), and [MCP execution](/agentic/mcp/).

For DB-backed user edits and dev-mode fallback behavior, see [Artifacts Overlay](/agentic/artifacts/).
