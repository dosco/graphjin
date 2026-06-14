---
title: "Filters"
description: "Use WHERE operators, logical groups, related-table filters, JSON operators, and geo filters."
nav_group: "core"
doc_kind: "reference"
weight: 30
---

## Filter shape

GraphJin filters live inside an inline `where` object. Keep the object shape in the query and use variables as leaf values:

```graphql
query Products($min: Float!, $max: Float!, $ids: [Int!]) {
  products(
    where: {
      price: { gte: $min, lte: $max }
      id: { in: $ids }
    }
    order_by: { id: asc }
  ) {
    id
    name
    price
  }
}
```

```json
{ "min": 10, "max": 100, "ids": [1, 2, 3] }
```

Do not pass the entire filter as `where: $where`. Whole-object filters are rejected so saved queries and allow-list review keep a stable operation shape.

## Scalar and list operators

| Family | Operators | Notes |
| --- | --- | --- |
| Equality | `eq`, `neq` | Use real booleans/numbers, not strings like `"true"` or `"50"`. |
| Ordered comparisons | `gt`, `gte`, `lt`, `lte` | Aliases such as `lteq`, `lesser_or_equals`, and `greater_or_equals` are covered by tests. |
| Lists | `in`, `nin` | Values must be arrays, even for one value. |
| Nulls | `is_null` | Use `true` for null checks and combine with `not` for not-null checks. |
| Text | `like`, `ilike`, `regex`, `iregex`, `similar` | Pattern semantics are dialect-specific; `ilike` needs `%` wildcards for partial matches. |

```graphql
query {
  products(
    where: { price: { gt: 10, lt: 100 } }
    order_by: { id: asc }
    limit: 3
  ) {
    id
    name
    price
  }
}
```

Common operators include `eq`, `neq`, `gt`, `gte`, `lt`, `lte`, `in`, `nin`, `is_null`, and `iregex`.

{{< verified by="Example_queryWithWhereGreaterThanOrLesserThan" file="tests/query_test.go" line="471" >}}
{{< verified by="Example_queryWithWhereIn" file="tests/query_test.go" line="411" >}}

## Logical groups

`and`, `or`, and `not` can wrap column filters, relationship filters, and spatial filters.

```graphql
query {
  products(where: {
    and: [
      { not: { id: { is_null: true } } },
      { price: { gt: 10 } }
    ]
  }) {
    id
  }
}
```

{{< verified by="Example_queryWithWhereNotIsNullAndGreaterThan" file="tests/query_test.go" line="438" >}}

## Text search filters

```graphql
query Products($name: String!) {
  products(
    where: {
      id: [3, 34]
      or: {
        name: { iregex: $name }
        description: { iregex: $name }
      }
    }
    order_by: { id: asc }
  ) {
    id
  }
}
```

```json
{ "name": "Product 3" }
```

The `id: [3, 34]` shorthand is useful for primary-key lists, while the explicit operator form is clearer when the filter will be reused in saved queries.

{{< verified by="Example_queryWithWhere1" file="tests/query_test.go" line="377" >}}

## Related-table filters

```graphql
query ($user_id: ID!) {
  products(
    where: { owner: { id: { or: [{ eq: $user_id }, { eq: 3 }] } } }
    order_by: { id: asc }
    limit: 2
  ) {
    id
    owner { email }
  }
}
```

{{< verified by="Example_queryWithWhereOnRelatedTable" file="tests/query_test.go" line="504" >}}

Relationship filters compile through the same schema relationship graph as nested selections. Do not guess relationship names in agentic workflows; search `gj_catalog` for relationship rows first.

## JSON filters and path fields

JSON columns can use key operators and path-derived fields.

```graphql
query {
  products(
    where: { metadata: { has_key_any: ["foo", "bar"] } }
    order_by: { id: asc }
    limit: 3
  ) {
    id
  }
}
```

```graphql
query {
  products(
    where: { metadata_foo: { eq: true } }
    order_by: { id: asc }
    limit: 10
  ) {
    id
    metadata_foo
  }
}
```

Underscore notation maps to JSON path access for supported JSON columns. Key operators include `has_key`, `has_key_any`, `has_key_all`, `contains`, and `contained_in`.

{{< verified by="Example_queryJSONPathOperations" file="tests/query_test.go" line="96" >}}
{{< verified by="Example_queryJSONPathOperationsAlternativeSyntax" file="tests/query_test.go" line="129" >}}
{{< verified by="Example_queryWithWhereHasAnyKey" file="tests/query_test.go" line="2007" >}}

## Geo filters

Spatial filters are available when the dialect and deployment support spatial types. The shared examples cover PostGIS, MySQL 8+, MariaDB, SQLite with SpatiaLite, SQL Server, Oracle Spatial, and MongoDB where the operation maps cleanly.

### Distance

```graphql
query {
  locations(
    where: {
      geom: {
        st_dwithin: {
          point: [-122.4, 37.8]
          distance: 10000
        }
      }
    }
    order_by: { id: asc }
  ) {
    id
    name
  }
}
```

{{< verified by="Example_queryWithGeoFilter" file="tests/geo_test.go" line="10" >}}

Distance can include units and variables:

```graphql
query Nearby($loc: JSON!, $radius: Float!) {
  locations(where: {
    geom: { st_dwithin: { point: $loc, distance: $radius } }
  }) {
    id
    name
  }
}
```

```graphql
query {
  locations(where: {
    geom: { st_dwithin: { point: [-122.4194, 37.7749], distance: 5, unit: "miles" } }
  }) {
    id
    name
  }
}
```

{{< verified by="TestGeoStDWithinVariable" file="core/internal/qcode/geo_test.go" line="63" >}}
{{< verified by="TestGeoStDWithinWithUnit" file="core/internal/qcode/geo_test.go" line="42" >}}

### Shape relationships

```graphql
query {
  locations(where: {
    geom: {
      st_within: {
        polygon: [[-122.5, 37.7], [-122.3, 37.7], [-122.3, 37.9], [-122.5, 37.9], [-122.5, 37.7]]
      }
    }
  }) {
    id
  }
}
```

```graphql
query {
  parcels(where: {
    geom: {
      st_intersects: {
        geometry: {
          type: "Polygon"
          coordinates: [[[-122.5, 37.7], [-122.3, 37.7], [-122.3, 37.9], [-122.5, 37.9], [-122.5, 37.7]]]
        }
      }
    }
  }) {
    id
  }
}
```

Supported spatial operator names include `st_within`, `st_contains`, `st_intersects`, `st_coveredby`, `st_covers`, `st_touches`, and `st_overlaps`.

{{< verified by="Example_queryWithGeoContains" file="tests/geo_test.go" line="46" >}}
{{< verified by="TestGeoStIntersectsGeoJSON" file="core/internal/qcode/geo_test.go" line="133" >}}
{{< verified by="TestGeoStTouches" file="core/internal/qcode/geo_test.go" line="198" >}}
{{< verified by="TestGeoStOverlaps" file="core/internal/qcode/geo_test.go" line="219" >}}
{{< verified by="TestGeoStCoveredBy" file="core/internal/qcode/geo_test.go" line="242" >}}
{{< verified by="TestGeoStCovers" file="core/internal/qcode/geo_test.go" line="265" >}}

### MongoDB near

```graphql
query {
  locations(where: {
    geom: { near: { point: [-122.4194, 37.7749], maxDistance: 5000 } }
  }) {
    id
    name
  }
}
```

{{< verified by="TestGeoNear" file="core/internal/qcode/geo_test.go" line="156" >}}

## Wrong / Right examples

| Wrong | Right | Why |
| --- | --- | --- |
| `where: $where` | `where: { label: { eq: $label } }` | Whole-object filters break saved-query review and are rejected. |
| `where: { id: { in: 1 } }` | `where: { id: { in: [1] } }` | `in` and `nin` require arrays. |
| `where: { price: { gt: "50" } }` | `where: { price: { gt: 50 } }` | Numeric operators need numeric values. |
| `where: { active: { eq: "true" } }` | `where: { active: { eq: true } }` | Boolean values are `true` and `false`. |
| `where: { name: { ilike: "phone" } }` | `where: { name: { ilike: "%phone%" } }` | SQL `LIKE` partial matching needs wildcards. |

These same mistakes are exposed to AI clients through `serv/mcp_syntax.go` so MCP users receive the same guidance.
