---
title: "Compiler Model"
description: "GraphJin compiles GraphQL into database and source operations instead of resolving fields one by one."
nav_group: "core"
doc_kind: "concept"
weight: 10
---

## The short version

GraphJin is a compiler:

1. Parse GraphQL into an AST.
2. Resolve tables, columns, relationships, roles, variables, and directives against schema metadata.
3. Lower the operation into QCode, GraphJin's internal representation.
4. Emit SQL for SQL databases, JSON DSL for MongoDB, or source-specific work for other surfaces.
5. Return the nested JSON shape requested by the query.

{{< svg "compiler-flow" "GraphJin compiler flow" >}}

## Why that matters

The compiler model avoids resolver sprawl and N+1 query behavior. A nested selection can become one planned database operation with joins, JSON aggregation, filters, and pagination pushed down to the database.

```graphql
query {
  products(limit: 3, order_by: { id: asc }) {
    id
    name
    owner {
      id
      email
    }
  }
}
```

For a SQL source, GraphJin emits dialect-specific SQL that returns the requested nested JSON. For MongoDB, it emits a JSON DSL that the custom driver turns into an aggregation pipeline. For filesystem, OpenAPI, CodeSQL, workflow, or GraphJin system roots, GraphJin still keeps the query shape and policy model centralized even when the execution backend is not SQL.

{{< verified by="Example_query" file="tests/query_test.go" line="18" >}}
{{< verified by="TestQueryDSLParsing" file="mongodriver/driver_test.go" line="17" >}}

## QCode is the boundary

QCode is GraphJin's intermediate representation. It records selected roots, fields, relationship paths, role checks, variables, filters, order clauses, pagination, aggregate functions, remote joins, and database joins before the backend compiler renders anything.

That boundary matters for features like multi-database support. A nested relationship in the same SQL database can stay inside one generated statement; a cross-database child is marked as a database join and resolved after the parent result exists.

{{< verified by="TestSkipTypeDatabaseJoin" file="core/internal/qcode/multidb_test.go" line="22" >}}
{{< verified by="TestAddRelColumnsForDatabaseJoin" file="core/internal/qcode/multidb_test.go" line="159" >}}

## Dialect interface

Shared compiler logic should ask the dialect layer to render database-specific syntax. Row limiting, offset, JSON aggregation, identifier quoting, boolean handling, recursive CTEs, mutation strategy, spatial functions, and analytics windows vary by backend.

When adding a feature, prefer a dialect method over scattering database-specific branches through shared query rendering.

## Where to go deeper

- [Query language](/core/query-language/) for the user-facing GraphQL shape.
- [Relationships](/core/relationships/) for traversal behavior.
- [Database support](/reference/database-support/) for dialect boundaries.
