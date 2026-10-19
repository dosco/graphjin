---
title: "Ordering And Cursors"
description: "Use explicit ordering, distinct, offset, custom order lists, and cursor pagination."
nav_group: "core"
doc_kind: "reference"
weight: 40
---

## Explicit ordering

```graphql
query {
  products(order_by: { price: desc, id: asc }, limit: 5) {
    id
    price
  }
}
```

Always specify `order_by` when the order matters. SQL databases do not guarantee result ordering without it.

Ordering can target multiple columns, nested related tables, custom value lists, and null placement:

```graphql
query Products($ids: [Int!]) {
  products(
    order_by: {
      id: [$ids, "asc"]
      price: { dir: desc, nulls: last }
    }
    where: { id: { in: $ids } }
    limit: 5
  ) {
    id
    price
  }
}
```

{{< verified by="Example_queryWithOrderByList" file="tests/query_test.go" line="306" >}}

## Distinct and offset

```graphql
query {
  products(
    limit: 5
    offset: 10
    distinct: [price]
    order_by: { price: desc }
  ) {
    id
    price
  }
}
```

{{< verified by="Example_queryWithLimitOffsetOrderByDistinctAndWhere" file="tests/query_test.go" line="338" >}}

## Order by related tables

```graphql
query {
  products(order_by: { users: { email: desc }, id: desc }, limit: 5) {
    id
    price
  }
}
```

{{< verified by="Example_queryWithNestedOrderBy" file="tests/query_test.go" line="279" >}}

## Named cursors

Cursor fields let clients page through stable result sets. Request the cursor at the root level and pass it back through variables.

```graphql
query ProductsPage($products_cursor: Cursor) {
  products(
    first: 10
    after: $products_cursor
    order_by: { id: asc }
  ) {
    id
    name
  }

  products_cursor
}
```

```json
{ "products_cursor": null }
```

On the next page, set `products_cursor` to the returned value. Cursors are opaque; do not construct or parse them.

{{< verified by="Example_queryWithNamedCursorPagination" file="tests/query_test.go" line="2086" >}}
{{< verified by="Example_queryWithNestedIndependentCursors" file="tests/query_test.go" line="2296" >}}

## Cursor rules

| Rule | Reason |
| --- | --- |
| Use `after: $products_cursor` or `before: $products_cursor`. | Cursors must be variables, not string literals embedded in the query. |
| Request `products_cursor` at the query root. | Cursor fields describe the page, not each row. |
| Keep variable names cursor-shaped, such as `products_cursor` or `cursor`. | MCP cursor expansion only touches cursor variables. |
| Treat returned values as opaque. | GraphJin uses encrypted cursor payloads and a dynamic security prefix. |
| Do not hardcode `gj-` or `__gj-enc:`. | `gj-...:` is generated from the active security context, and `__gj-enc:` is an MCP cache transport detail. |

{{< verified by="TestExpandCursorIDs_AlreadyEncrypted" file="serv/mcp_cursor_test.go" line="161" >}}
{{< verified by="Example_queryWithNamedCursorInvalidVariable" file="tests/query_test.go" line="2152" >}}

## Backward pagination

```graphql
query PreviousProducts($products_cursor: Cursor) {
  products(
    last: 10
    before: $products_cursor
    order_by: { id: asc }
  ) {
    id
    name
  }

  products_cursor
}
```

Backward and forward pagination use the same root-level cursor field. Keep the ordering stable in both directions.

{{< verified by="Example_queryWithBackwardCompatibleCursor" file="tests/query_test.go" line="2091" >}}
