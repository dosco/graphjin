---
title: "First Query"
description: "Understand how a GraphQL selection maps to nested database results."
nav_group: "start"
doc_kind: "guide"
weight: 30
---

## Shape the response

GraphJin treats the database schema as the source of truth. A table becomes a root field, columns become fields, and foreign-key relationships become nested selections.

```graphql
query {
  products(id: $id) {
    id
    name
    owner {
      email
    }
  }
}
```

```json
{ "id": 2 }
```

When you query by primary key, GraphJin returns a single object. When you query a collection, it returns a list.

{{< verified by="Example_queryByID" file="tests/query_test.go" line="558" >}}

Primary-key lookup works because GraphJin has discovered the table primary key from the database schema. For composite keys or manually mapped tables, keep table metadata explicit in config so the compiler can still distinguish single-row lookup from collection lookup.

## Use aliases

GraphQL aliases let you expose API-friendly names without changing the database:

```graphql
query {
  users {
    id
    fullName: full_name
  }
}
```

{{< verified by="Example_queryWithAlternateFieldNames" file="tests/query_test.go" line="532" >}}

## Add variables

Variables replace scalar values, not arbitrary GraphQL fragments:

```graphql
query Products($ownerID: ID!, $limit: Int = 10) {
  products(
    where: { owner_id: { eq: $ownerID } }
    limit: $limit
    order_by: { id: asc }
  ) {
    id
    name
  }
}
```

{{< verified by="Example_queryWithVariablesDefaultValue" file="tests/query_test.go" line="942" >}}

## Keep order explicit

SQL result order is undefined without `order_by`. Tests and production clients should ask for deterministic ordering whenever order matters.

```graphql
query {
  products(limit: 5, order_by: { id: asc }) {
    id
    name
  }
}
```

This rule matters across dialects. A result that appears stable on PostgreSQL without `order_by` is still relying on undefined SQL ordering.
