---
title: "Mutations"
description: "Insert, update, connect, validate, and transact across related tables."
nav_group: "core"
doc_kind: "reference"
weight: 70
---

## Inserts

```graphql
mutation {
  users(insert: $data) {
    id
    email
  }
}
```

{{< verified by="Example_insert" file="tests/insert_test.go" line="16" >}}

## Bulk and nested inserts

GraphJin can insert multiple rows and insert across related tables. PostgreSQL uses atomic CTE chains; other dialects use the dialect-appropriate mutation strategy.

```graphql
mutation {
  products(insert: [
    { id: 5001, name: "Desk", price: 199.00 }
    { id: 5002, name: "Lamp", price: 39.00 }
  ]) {
    id
    name
  }
}
```

```graphql
mutation {
  users(insert: $user) {
    id
    products(insert: $products) {
      id
      name
    }
  }
}
```

{{< verified by="Example_insertInlineBulk" file="tests/insert_test.go" line="148" >}}
{{< verified by="Example_insertIntoMultipleRelatedTables" file="tests/insert_test.go" line="278" >}}

## Connect to existing rows

Use connect operations when a mutation should link to existing related records instead of creating new ones.

```graphql
mutation {
  products(insert: $product) {
    id
    categories(connect: { id: { in: [1, 2] } }) {
      id
      name
    }
  }
}
```

{{< verified by="Example_insertIntoTableAndConnectToRelatedTables" file="tests/insert_test.go" line="531" >}}

## Validation and presets

Mutation validation supports required fields, formats, min/max constraints, comparisons, and conditional requirements. Presets can inject server-controlled values.

{{< verified by="Example_insertInlineWithValidation" file="tests/insert_test.go" line="105" >}}
{{< verified by="Example_insertWithPresets" file="tests/insert_test.go" line="184" >}}

Validation belongs in GraphJin config, not client code. Presets are useful for server-controlled fields such as `owner_id`, `created_at`, tenant IDs, or role-derived account IDs.

## Updates

```graphql
mutation {
  products(update: $data, where: { id: { eq: $id } }) {
    id
    name
  }
}
```

{{< verified by="Example_update" file="tests/update_test.go" line="61" >}}

## Deletes

Deletes require a `where` clause. Do not expose unconstrained deletes.

```graphql
mutation DeleteProduct($id: ID!) {
  products(delete: true, where: { id: { eq: $id } }) {
    id
  }
}
```

{{< verified by="TestMultiAliasDelete" file="tests/update_test.go" line="422" >}}

## Read-only and production controls

Mutations run through the same role, source, and production policy gates as queries:

- read-only databases or sources reject writes,
- table-level permissions can block insert/update/upsert/delete separately,
- production mode should use saved operations,
- source-mode capabilities should come from `core/sourcecap`,
- and MCP mutation access should be explicit.

{{< verified by="TestReadOnlyDB_WithRolesAndTables" file="tests/readonly_test.go" line="102" >}}
{{< verified by="TestSourceModeHTTPRuntimeDenialEventsAreRedacted" file="serv/source_mode_http_test.go" line="113" >}}

## Dialect mutation strategies

The compiler lowers writes differently by dialect. PostgreSQL can use CTE-heavy nested mutation plans; MySQL, MariaDB, SQLite, SQL Server, Oracle, Snowflake, and other backends use dialect-specific linear or fallback strategies where needed. Shared mutation changes should be validated against relevant dialect scripts.
