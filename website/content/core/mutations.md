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

## Insert or get the existing row

For a single insert on PostgreSQL or SQLite, `on_conflict: get` makes the insert idempotent without turning it into an update:

```graphql
mutation {
  users(
    insert: {
      email: "ada@example.com"
      full_name: "Submitted Name"
    }
    on_conflict: get
  ) {
    id
    email
    full_name
  }
}
```

If no row conflicts, GraphJin inserts and returns the new row. If `email` already exists, it returns that stored row unchanged. For example, an existing `full_name: "Stored Name"` remains `"Stored Name"`; GraphJin does not run an update, fire update triggers, or change `updated_at`. `onConflict: get` is accepted as the camel-case spelling.

GraphJin infers the conflict target from schema metadata after trusted insert presets are applied. The payload must supply exactly one complete candidate:

- one unconditional single-column unique key, or
- every column of the primary key, including a composite primary key.

Generated or defaulted key values are not candidates unless input or a preset supplies them. Supplying no candidate is an error. Supplying both a primary key and another unique key is ambiguous and is also an error. Composite non-primary unique constraints are not inferred in this version.

This policy applies only to a single, non-nested insert object. It is rejected for bulk lists, nested inserts, updates, upserts, and deletes. A conflict on a different constraint remains a database error. Omitting `on_conflict` keeps normal error-on-conflict behavior.

The returned row is subject to the role's normal read filters and selected-column permissions. GraphJin does not reveal an existing conflicting row that the caller cannot read; an empty result caused by the role filter is reported as an authorization error. If a pre-19 PostgreSQL or SQLite fallback remains empty after its one complete-statement retry, GraphJin returns a stable retryable-concurrency error.

Use `upsert` when conflict should update submitted fields. Use `on_conflict: get` when conflict must return the stored row without changing it.

{{< verified by="TestInsertOnConflictGetReturnsStoredRowUnchanged" file="tests/insert_test.go" line="57" >}}

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

The compiler lowers writes differently by dialect. PostgreSQL 19 uses native `ON CONFLICT (...) DO SELECT` for `on_conflict: get`; earlier PostgreSQL versions use an atomic insert-or-select CTE with one retry for the statement-snapshot race. SQLite uses targeted `ON CONFLICT (...) DO NOTHING` inside its transactional linear mutation path and selects by the inferred key, also with one empty-result retry. Other dialects reject this policy in v1 instead of approximating it with update or unrestricted ignore semantics.

For other mutations, PostgreSQL can use CTE-heavy nested plans; MySQL, MariaDB, SQLite, SQL Server, Oracle, Snowflake, and other backends use dialect-specific linear or fallback strategies where needed. Shared mutation changes should be validated against relevant dialect scripts.
