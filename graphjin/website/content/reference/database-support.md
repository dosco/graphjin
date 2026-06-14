---
title: "Database Support"
description: "Database and source support matrix with dialect differences and important boundaries."
nav_group: "reference"
doc_kind: "reference"
weight: 10
---

## Core support

| Source | Status | Notes |
| --- | --- | --- |
| PostgreSQL | Supported | Broadest SQL feature coverage |
| MySQL / MariaDB | Supported | Linear mutation strategy |
| SQLite | Supported | Local/dev and embedded use |
| SQL Server | Supported | Dialect-specific limit/offset and JSON handling |
| Oracle | Supported | Boolean/function return types need care |
| MongoDB | Supported | JSON DSL to aggregation pipeline |
| Snowflake | Supported | Warehouse dialect surface |
| Redshift | Supported | Analytics-oriented SQL |
| ClickHouse | Configured source | Use with feature-specific validation |
| Filesystems | Supported | Local/S3/GCS virtual tables |
| OpenAPI | Supported for selected GETs | Remote fields and joins |
| CodeSQL | Supported | Source-code index tables |

## Dialect rule

Do not rely on implicit ordering. If a test or API response depends on order, specify `order_by`.

```graphql
query {
  products(limit: 5, order_by: { id: asc }) {
    id
    name
  }
}
```

This is not a style preference. SQL result ordering is undefined without `ORDER BY`, so adding implicit ordering in a dialect would add cost and hide a client/test bug.

## Feature boundaries

Some features differ by dialect: JSON aggregation, recursive CTE syntax, boolean representation, function return types, and MongoDB aggregation support.

| Feature | Important boundary |
| --- | --- |
| JSON aggregation | SQL dialects render different aggregate functions; MongoDB uses native documents and a JSON DSL. |
| Recursive relationships | PostgreSQL/MySQL/SQLite use `WITH RECURSIVE`; Oracle/MSSQL do not use the `RECURSIVE` keyword. |
| Boolean values | Oracle and SQL Server may need JSON conversion or explicit return-type config for boolean-like function results. |
| Geo filters | PostGIS-style SQL and MongoDB `near` have different backend operators. |
| Analytics directives | Window rendering is dialect-specific and unsupported dialects should fail clearly. |
| Cursor pagination | Cursor fields are root-level synthetic fields and cursor strings are opaque. |

{{< verified by="TestAnalytics_DialectUnsupported" file="core/internal/psql/window_test.go" line="106" >}}
{{< verified by="TestQueryDSLParsing" file="mongodriver/driver_test.go" line="17" >}}

## Test command map

```bash
./scripts/test-postgres.sh
./scripts/test-mysql.sh
./scripts/test-sqlite.sh
./scripts/test-mongo.sh
./scripts/test-snowflake.sh
```

Docs-only edits do not need the full dialect matrix, but compiler or shared query-planning changes should run the affected dialect scripts.
