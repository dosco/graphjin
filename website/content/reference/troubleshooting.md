---
title: "Troubleshooting"
description: "Common GraphJin debugging paths for ordering, auth, production mode, MCP, OpenAPI, and dialect support."
nav_group: "reference"
doc_kind: "reference"
weight: 40
---

## Results appear in the wrong order

Add `order_by`. SQL result ordering is undefined without it, even if a local PostgreSQL test looked stable.

```graphql
query {
  products(limit: 10, order_by: { id: asc }) {
    id
    name
  }
}
```

## A production query is rejected

Production mode expects reviewed saved queries unless production security is deliberately disabled. Confirm the operation is saved and the client calls the saved name with variables.

For MCP, search or list saved queries first, inspect the variable contract, then call `execute_saved_query`.

## An agent cannot find a field

Use `gj_catalog(search: "...")` first. If the field exists but is hidden, inspect `gj_security` for policy, source capabilities, read-only state, and high-risk findings.

```graphql
query {
  gj_catalog(search: "orders customer relationship") {
    id
    kind
    name
    evidence_json
  }
}
```

## OpenAPI operation is missing

Check boot logs. GraphJin skips unsupported operation shapes such as mutating verbs, binary responses, unsupported auth, and ambiguous path parameters unless explicitly configured.

Confirm the operation has an `operationId`, a JSON success response, supported auth, and a classifiable path or explicit operation override.

## Cursor pagination hangs or repeats rows

Treat cursors as opaque. Use the returned cursor string or the MCP cursor ID. Do not hardcode `gj-`, `__gj-enc:`, or any cursor prefix; GraphJin uses a dynamic security prefix and encrypted cursor recognition depends on it.

Also verify that the cursor field is requested at the same root level as the list:

```graphql
query ProductsPage($cursor: Cursor) {
  products(first: 20, after: $cursor, order_by: { id: asc }) {
    id
    name
  }
  products_cursor
}
```

{{< verified by="Example_subscriptionWithCursor" file="tests/subs_test.go" line="95" >}}
{{< verified by="TestExpandCursorIDs_AlreadyEncrypted" file="serv/mcp_cursor_test.go" line="161" >}}

## A filter fails validation

Keep the filter object in the query and pass variables only as leaf values:

```graphql
query Products($ids: [Int!]!) {
  products(where: { id: { in: $ids } }) {
    id
  }
}
```

If a model is writing the query, call `get_query_syntax` or validate the where clause before execution.

## MCP OAuth fails

Verify the advertised protected-resource metadata, authorization server metadata, scopes, and token audience/resource. Wrong-audience tokens should fail.

## A source-mode config update fails

Use preview/validate first, then apply. Plaintext secrets require the keystore path, source patches use source-scoped reloads when possible, and unsafe source patch inputs should be rejected before the live runtime changes.

{{< verified by="TestHandleUpdateCurrentConfig_TransactionalFailureLeavesLiveStateUntouched" file="serv/mcp_config_transaction_test.go" line="97" >}}
