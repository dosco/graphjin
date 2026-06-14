---
title: "Subscriptions"
description: "Stream query changes over WebSocket or SSE with the same compiler and policy model."
nav_group: "core"
doc_kind: "reference"
weight: 80
---

## Transport

GraphJin supports subscription clients over WebSocket and SSE. The subscription query still goes through authentication, role matching, query compilation, saved-query policy, and cursor handling.

{{< svg "subscriptions-transport" "Subscription transports" >}}

## Query shape

```graphql
subscription {
  products(limit: 10, order_by: { id: asc }) {
    id
    name
  }
}
```

{{< verified by="Example_subscription" file="tests/subs_test.go" line="40" >}}

## Cursor resume

Subscriptions use the same cursor machinery as queries. The client requests a cursor field, stores the cursor GraphJin returns, and sends it back as a variable on reconnect or the next poll.

```graphql
subscription LiveChats($chats_cursor: Cursor) {
  chats(
    first: 10
    after: $chats_cursor
    order_by: { id: asc }
  ) {
    id
    message
    created_at
  }

  chats_cursor
}
```

Cursor fields are root-level fields such as `chats_cursor`; they are not nested inside the row selection.

## Dynamic security prefix

Subscription cursors must use GraphJin's dynamic security prefix. Hardcoding cursor prefixes can make clients hang because encrypted cursor recognition fails.

The prefix is generated from the active GraphJin security context, for example a timestamp-based `gj-...:` value. Clients must treat the whole cursor as opaque. Do not build a cursor string yourself, do not strip or hardcode `gj-`, and do not assume MCP's LLM-friendly `__gj-enc:` cache representation is the underlying GraphQL cursor.

Do not hardcode any cursor prefix in a client or subscription resume implementation.

{{< verified by="Example_subscriptionWithCursor" file="tests/subs_test.go" line="95" >}}
{{< verified by="TestProcessCursorsForMCP" file="serv/mcp_cursor_test.go" line="20" >}}

## MCP cursor IDs

MCP can replace long encrypted cursors in tool output with short numeric IDs backed by an in-memory or Redis cursor cache. The next MCP request can pass that numeric ID in a cursor variable, and GraphJin expands it back to the original encrypted cursor before execution.

```json
{
  "variables": {
    "products_cursor": "2"
  }
}
```

Only cursor-shaped variable names are expanded. Non-cursor variables are left alone, and already-encrypted cursors pass through unchanged.

{{< verified by="TestExpandCursorIDs_VariousKeyNames" file="serv/mcp_cursor_test.go" line="201" >}}
{{< verified by="TestMCP_CursorRoundtripIntegration" file="serv/mcp_test.go" line="282" >}}

## Operational settings

```yaml
subs_poll_duration: "2s"
subs_max_clients: 10000

http:
  sse: true
  websocket: true
```

Use explicit `order_by` with cursor subscriptions. Result ordering is undefined without it, and cursor resume depends on a stable order.
