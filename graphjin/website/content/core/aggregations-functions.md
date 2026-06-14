---
title: "Aggregations And Functions"
description: "Use aggregate fields, analytics directives, SQL functions, full-text search, and database function fields."
nav_group: "core"
doc_kind: "reference"
weight: 60
---

## Aggregate fields

GraphJin exposes aggregate fields by prefixing a column with the aggregate name.

| Field shape | Meaning |
| --- | --- |
| `count_id` | Count non-null `id` values. |
| `sum_price` | Sum a numeric column. |
| `avg_price` | Average a numeric column. |
| `min_price`, `max_price` | Minimum and maximum values. |

```graphql
query {
  products(where: { id: { lteq: 100 } }) {
    count_id
    min_price
    max_price
    avg_price
  }
}
```

{{< verified by="Example_queryWithAggregation" file="tests/query_test.go" line="769" >}}

If a select contains only aggregates and no `distinct`, GraphJin returns a single global aggregate row instead of a page of per-row aggregate values.

{{< verified by="Example_queryWithGlobalAggBrokenMdFix" file="tests/query_test.go" line="2508" >}}

## Grouped summaries

Use `distinct` to group by one or more columns, then request aggregate fields for each group.

```graphql
query {
  products(
    distinct: [country_code]
    where: { id: { lteq: 100 } }
    order_by: { count_id: desc }
  ) {
    country: country_code
    count_id
    avg_price
  }
}
```

Grouped queries still follow SQL grouping rules. If a nested join would require a non-grouped foreign key, GraphJin should reject the ambiguous shape instead of emitting broken SQL; root the query at the dimension table when you need a nested object per group.

{{< verified by="TestStage3_DistinctAggregateNestedJoinThroughNonDistinctColumn" file="tests/query_test.go" line="2548" >}}

## Expression aggregates

Expression aggregates calculate metrics in the database, not in client code.

```graphql
query {
  products(where: { id: { lteq: 100 } }) {
    doubled: sum(expr: { mul: [id, 2] })
  }
}
```

Expressions support arithmetic operators such as `add`, `sub`, `mul`, `div`, `mod`, literal values, column references, casts, `coalesce`, `nullif`, and `case` expressions.

{{< verified by="Example_queryWithExprMul" file="tests/query_test.go" line="2390" >}}

Ratio-of-aggregates uses a bare expression field whose nested expression contains aggregate nodes:

```graphql
query {
  products(where: { id: { lteq: 100 } }) {
    ratio: ratio(expr: {
      div: [
        { sum: { mul: [id, 2] } }
        { sum: id }
      ]
    })
  }
}
```

{{< verified by="Example_queryWithExprBare" file="tests/query_test.go" line="2428" >}}

Expression arguments go through role allow-list checks. A role that cannot read `price` cannot leak it through `sum(expr: { mul: [price, 2] })`.

{{< verified by="Example_queryWithExprRoleAllowlist" file="tests/query_test.go" line="2468" >}}

## Analytics

Analytics directives keep rows visible while adding report metrics such as running totals, moving averages, previous/next values, first/last values, and rank within a group.

Use them when you need report-style columns but not a one-row-per-group aggregate result.

```graphql
query {
  products {
    id
    price
    running_total: price @running(
      aggregate: sum
      by: "user_id"
      orderBy: { created_at: asc }
    )
    moving_avg: price @moving(
      aggregate: avg
      rows: 6
      by: "user_id"
      orderBy: { created_at: asc }
    )
    previous_price: price @previous(by: "user_id", orderBy: { created_at: asc })
    rank_by_price: price @rank(by: "user_id", order: desc)
  }
}
```

| Directive | Use |
| --- | --- |
| `@running(aggregate:)` | Running `sum`, `avg`, `count`, `min`, or `max`. |
| `@moving(aggregate:, rows:)` | Trailing window over the current and previous rows. |
| `@previous`, `@next` | Period comparisons with `lag` and `lead`. |
| `@first`, `@last` | First or last value in an ordered partition. |
| `@rank`, `@denseRank`, `@rowNumber` | Ranking and row numbering inside an optional partition. |

`by` accepts a column name or list of column names. `orderBy` uses the same object ordering shape as normal queries. `order` is shorthand for ordering by the annotated field. Analytics require a deterministic order.

{{< verified by="TestAnalytics_RendersRunningOverClause" file="core/internal/psql/window_test.go" line="13" >}}
{{< verified by="TestAnalytics_DialectRendering" file="core/internal/psql/window_test.go" line="56" >}}

## Search

Full-text search is available through configured full-text columns and supported dialects.

```graphql
query SearchProducts($query: String!) {
  products(search: $query, limit: 5) {
    id
    name
    search_rank
  }
}
```

{{< verified by="Example_queryBySearch" file="tests/query_test.go" line="586" >}}

## SQL functions

PostgreSQL function fields can expose scalar values, table-returning functions, named args, user-defined return types, and directive behavior.

```graphql
query {
  hotProducts(where: { productID: { eq: 55 } }, order_by: { productID: desc }) {
    productID
    countryCode
    countProductID
  }
}
```

{{< verified by="Example_queryWithFunctionFields" file="tests/query_pg_test.go" line="140" >}}
{{< verified by="Example_queryWithFunctionReturingTablesWithNamedArgs" file="tests/query_pg_test.go" line="270" >}}

## Dialect boundaries

| Feature | Boundary |
| --- | --- |
| Analytics directives | Supported on modern SQL dialects with window functions. MongoDB and older SQL versions reject them with compile-time errors. |
| Expression aggregates | SQL path is covered; MongoDB currently rejects expression aggregates as unsupported. |
| Function fields | Database functions are dialect-specific; PostgreSQL has the broadest tested surface. |
| Role allow-lists | Aggregate and expression arguments must use allowed columns. |

{{< verified by="TestAnalytics_DialectUnsupported" file="core/internal/psql/window_test.go" line="106" >}}
{{< verified by="Example_queryWithAggregationBlockedColumn" file="tests/query_test.go" line="794" >}}
