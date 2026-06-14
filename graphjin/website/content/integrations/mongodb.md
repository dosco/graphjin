---
title: "MongoDB"
description: "GraphJin generates JSON DSL that the MongoDB driver translates into aggregation pipelines."
nav_group: "integrations"
doc_kind: "guide"
weight: 20
---

## Different backend, same GraphQL shape

MongoDB does not receive SQL. GraphJin emits a JSON DSL that the custom `mongodriver` translates into aggregation pipelines and mutation operations.

```json
{
  "operation": "aggregate",
  "collection": "users",
  "pipeline": [
    { "$match": { "_id": 1 } },
    { "$project": { "_id": 0, "id": "$_id", "email": 1 } }
  ]
}
```

## What is supported

MongoDB support covers basic queries, filtering, ordering, limits, relationships through `$lookup`, array-column joins with `$in`, embedded JSON virtual tables, mutations, subscriptions, and cursor handling.

{{< verified by="TestQueryDSLParsing" file="mongodriver/driver_test.go" line="17" >}}
{{< verified by="Example_multiDBQueryMongoDB" file="tests/multidb_test.go" line="107" >}}

## Relationships become `$lookup`

GraphJin keeps the GraphQL shape the same and changes the backend plan. A nested relationship compiles into `$lookup` with a pipeline, then projects the requested fields back into the GraphQL response shape.

```json
{
  "$lookup": {
    "from": "products",
    "let": { "userId": "$_id" },
    "pipeline": [
      { "$match": { "$expr": { "$eq": ["$owner_id", "$$userId"] } } },
      { "$project": { "_id": 0, "id": "$_id", "name": 1 } }
    ],
    "as": "products"
  }
}
```

Array-column joins use `$in` instead of `$eq`, and embedded JSON virtual tables use `$unwind` plus `$group` when GraphJin has to preserve the parent document shape.

## Cursors and IDs

GraphQL uses `id`; MongoDB stores `_id`. The dialect and driver translate between them. Cursor pagination normalizes cursor values for MongoDB seek filters and supports ascending/descending order.

{{< verified by="TestNormalizeCursorForSeek" file="mongodriver/query_cursor_test.go" line="5" >}}
{{< verified by="TestBuildCursorSeekFilterDescWithID" file="mongodriver/query_cursor_test.go" line="48" >}}

## Boundaries

Aggregation functions and custom database functions are not the same feature on MongoDB as they are on SQL dialects. Treat those as explicit support boundaries when designing portable queries.

MongoDB supports subscription polling, but it does not use SQL functions, recursive CTEs, or SQL aggregate rendering. Check [Aggregations And Functions](/core/aggregations-functions/) before assuming an aggregate-heavy SQL query is portable.
