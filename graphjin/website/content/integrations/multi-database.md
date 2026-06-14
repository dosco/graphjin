---
title: "Multi-Database"
description: "Map tables across named databases while preserving routing and cache isolation."
nav_group: "integrations"
doc_kind: "guide"
weight: 10
---

## Database map

Configure multiple databases when a graph spans operational, analytics, or source-specific stores.

```yaml
databases:
  primary:
    type: postgres
    url: ${PRIMARY_DATABASE_URL}
  analytics:
    type: snowflake
    url: ${ANALYTICS_DATABASE_URL}
```

Tables can be assigned in the database config or on individual table entries.

```yaml
tables:
  - name: users
    schema: public
    database: primary
  - name: audit_logs
    schema: main
    database: local
  - name: events
    database: mongodb
    columns:
      - name: user_id
        foreign_key: users.id
```

{{< verified by="Example_multiDBTableMapping" file="tests/multidb_test.go" line="205" >}}

## Root-level multi-database queries

Independent root fields can be grouped by database, executed against the correct compiler and connection pool, then merged into one JSON object.

```graphql
query Dashboard {
  users(limit: 1, order_by: { id: asc }) {
    id
    full_name
  }

  audit_logs(limit: 1, order_by: { id: asc }) {
    id
    action
  }

  events(limit: 1, order_by: { id: asc }) {
    id
    type
  }
}
```

Duplicate root keys across database results fail instead of silently overwriting data.

{{< verified by="Example_multiDBQueryPostgres" file="tests/multidb_test.go" line="39" >}}
{{< verified by="Example_multiDBQuerySQLite" file="tests/multidb_test.go" line="74" >}}
{{< verified by="Example_multiDBQueryMongoDB" file="tests/multidb_test.go" line="107" >}}
{{< verified by="TestMergeRootResults" file="core/multidb_test.go" line="273" >}}

## Nested database joins

GraphJin can reason about tables from multiple configured databases and keep query/cache identity scoped by database.

When a nested relationship crosses a database boundary, GraphJin does not attempt to emit one impossible SQL statement. It:

1. fetches the parent rows,
2. extracts the parent join key from the JSON result,
3. builds a child GraphQL query filtered by the foreign key,
4. compiles and executes that child query with the target database's compiler,
5. replaces the placeholder field in the parent JSON.

```graphql
query {
  users(limit: 5, order_by: { id: asc }) {
    id
    full_name
    events {
      id
      type
    }
  }
}
```

The exact availability of a nested cross-database path depends on discovered or configured relationship metadata.

{{< verified by="TestBuildChildGraphQLQueryNestedDatabaseJoin" file="core/multidb_test.go" line="809" >}}
{{< verified by="TestResolveDatabaseJoinsNullID" file="core/multidb_test.go" line="1045" >}}

## Cache isolation and compiler isolation

Each database context has its own type, schema, QCode compiler, SQL/DSL compiler, and connection pool. Query cache keys include the database identity so the same operation name cannot accidentally reuse SQL from another dialect.

{{< verified by="Example_multiDBCacheKeyIsolation" file="tests/multidb_test.go" line="141" >}}
{{< verified by="TestCacheKeyIncludesDatabase" file="core/multidb_test.go" line="14" >}}

## Operational advice

Use explicit database names when the same table name exists in more than one source. Ambiguous unqualified lookups should fail early so query authors fix the source reference.

{{< verified by="TestGetTableSchema_AmbiguousAcrossDatabases" file="core/api_multidb_test.go" line="12" >}}

## What to document in reviews

For multi-database changes, describe:

- which source owns each table-like root,
- whether the query is root-level composition or nested database join,
- whether a relationship is same-source, remote API, MongoDB lookup, or database join,
- which features are expected to be portable across the selected dialects,
- and how cache invalidation should identify source-owned row references.
