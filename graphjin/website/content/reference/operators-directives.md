---
title: "Operators And Directives"
description: "Quick reference for GraphJin filter operators, directives, JSON operators, and expression surfaces."
nav_group: "reference"
doc_kind: "reference"
weight: 30
---

## Filters

| Operator | Meaning |
| --- | --- |
| `eq`, `neq` | Equals / not equals |
| `gt`, `gte`, `lt`, `lte` | Numeric or ordered comparisons |
| `in`, `nin` | Inclusion or exclusion lists |
| `is_null` | Null checks |
| `like`, `ilike`, `regex`, `iregex`, `similar` | Text pattern matching where supported |
| `has_key`, `has_key_any`, `has_key_all` | JSON key checks |
| `contains`, `contained_in` | JSON/array containment where supported |
| `st_dwithin`, `st_within`, `st_contains`, `st_intersects`, `st_coveredby`, `st_covers`, `st_touches`, `st_overlaps`, `near` | Spatial and MongoDB geo filters |

{{< verified by="TestGeoStDWithinPoint" file="core/internal/qcode/geo_test.go" line="21" >}}

## Logical operators

Use `and`, `or`, and `not` to compose filters.

```graphql
where: {
  and: [
    { price: { gt: 10 } }
    { not: { id: { is_null: true } } }
  ]
}
```

Whole-object variables are not valid filter syntax. Use variables only at leaf values:

```graphql
query($label: String) {
  user_fields(where: { label: { eq: $label } }) {
    id
    label
  }
}
```

## Pagination and ordering

| Feature | Syntax |
| --- | --- |
| Limit/offset | `limit: 10, offset: 20` |
| Forward cursor | `first: 10, after: $products_cursor` |
| Backward cursor | `last: 10, before: $products_cursor` |
| Cursor field | `products_cursor` at query root |
| Distinct | `distinct: [category_id]` |
| Nested order | `order_by: { owner: { email: asc } }` |
| Custom list | `order_by: { id: [$ids, "asc"] }` |
| Null placement | `order_by: { price: { dir: desc, nulls: last } }` |

Cursor values are opaque and must come from GraphJin. Do not hardcode cursor prefixes.

## Aggregates and expressions

```graphql
query {
  products(distinct: [category_id], order_by: { revenue: desc }, limit: 10) {
    category_id
    count_id
    revenue: sum(expr: { mul: [price, quantity] })
  }
}
```

Expression aggregate operators include `add`, `sub`, `mul`, `div`, `mod`, `coalesce`, `nullif`, `case`, `cast`, aggregate nodes such as `{ sum: id }`, column refs, and literals.

## Analytics directives

| Directive | Meaning |
| --- | --- |
| `@running(aggregate:)` | Running aggregate without collapsing rows. |
| `@moving(aggregate:, rows:)` | Trailing moving aggregate over a fixed row window. |
| `@previous`, `@next` | Previous/next row value. |
| `@first`, `@last` | First/last value in an ordered group. |
| `@rank`, `@denseRank`, `@rowNumber` | Ranking and row numbering. |

```graphql
query {
  orders {
    account_id
    month
    total
    running_total: total @running(aggregate: sum, by: "account_id", orderBy: { month: asc })
    rank_by_total: total @rank(by: "account_id", order: desc)
  }
}
```

{{< verified by="TestAnalytics_DialectRendering" file="core/internal/psql/window_test.go" line="56" >}}

## Analytics directives and GraphQL directives

GraphJin supports GraphQL conditional directives plus GraphJin-specific directives.

| Directive | Use |
| --- | --- |
| `@include(ifRole:)`, `@skip(ifRole:)` | Role-aware field selection. |
| `@include(ifVar:)`, `@skip(ifVar:)` | Variable-aware field selection. |
| `@object` | Return a single object instead of an array. |
| `@schema(name:)` | Target a specific database schema. |
| `@through(table:)` | Name the intermediate join table for many-to-many relationships. |
| `@through(column:)` | Name the FK column to follow when multiple relationships are possible. |
| `@notRelated` | Disable automatic relationship detection for a field. |
| `@cacheControl(maxAge:)` | Set a query cache TTL in seconds. |
| `@database(name:)` | Assign a schema/table definition to a named database. |

## Keep MCP docs aligned

When operators or syntax change, update the MCP syntax documentation in `serv/mcp_syntax.go` so AI clients can discover the new shape.

{{< verified by="TestQuerySyntaxReference_HasAnalyticsDirectives" file="serv/mcp_test.go" line="1619" >}}
