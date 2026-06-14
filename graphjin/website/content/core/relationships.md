---
title: "Relationships"
description: "Traverse one-to-one, one-to-many, many-to-many, recursive, and polymorphic relationships."
nav_group: "core"
doc_kind: "reference"
weight: 50
---

GraphJin relationship traversal comes from schema metadata. Foreign keys, configured relationships, embedded JSON virtual tables, remote API joins, and database-join edges all become nested GraphQL selections, but they are not all executed the same way.

| Relationship kind | Execution model |
| --- | --- |
| Same-database SQL join | Compiled into the generated SQL using joins, lateral subqueries, and JSON aggregation. |
| MongoDB | Rendered as JSON DSL and translated to aggregation pipeline stages such as `$lookup`, `$match`, `$project`, `$unwind`, and `$group`. |
| Remote API joins | Parent rows are fetched first, then external operation results are merged into the response. |
| Cross database | Parent rows are fetched first, the join key is extracted, child queries run against the target database context, and placeholder fields are replaced in the JSON response. |

## Parent and child traversal

```graphql
query {
  products(limit: 3, order_by: { id: asc }) {
    id
    owner {
      id
      email
    }
  }
}
```

Relationships come from database schema discovery and explicit table configuration.

{{< verified by="Example_queryParentsWithChildren" file="tests/query_test.go" line="623" >}}
{{< verified by="Example_queryChildrenWithParent" file="tests/query_test.go" line="650" >}}

## Related filters and ordering

Filters and ordering can target relationship paths when the path is known.

```graphql
query {
  products(
    where: { owner: { id: { or: [{ eq: $user_id }, { eq: 3 }] } } }
    order_by: { users: { email: desc }, id: desc }
    limit: 5
  ) {
    id
    owner { id email }
  }
}
```

{{< verified by="Example_queryWithWhereOnRelatedTable" file="tests/query_test.go" line="504" >}}
{{< verified by="Example_queryWithNestedOrderBy" file="tests/query_test.go" line="279" >}}

## Many-to-many

Join tables can expose many-to-many paths without user-written resolver code.

```graphql
query {
  products(limit: 2, order_by: { id: asc }) {
    id
    customers {
      id
      email
    }
  }
}
```

When more than one join table or foreign key path is possible, use `@through(table:)` or `@through(column:)` to disambiguate. Agents should discover relationship rows in `gj_catalog` before guessing.

{{< verified by="Example_queryManyToManyViaJoinTable1" file="tests/query_test.go" line="677" >}}
{{< verified by="TestCompositeFK_ThroughColumn_EmitsFullJoinCondition" file="core/internal/psql/psql_test.go" line="218" >}}

## Recursive relationships

Self-referential tables can model trees such as comments and categories.

```graphql
query {
  comments(id: $id) {
    id
    body
    replies: comments(find: "children") {
      id
      body
    }
  }
}
```

{{< verified by="Example_queryWithRecursiveRelationship1" file="tests/query_test.go" line="1749" >}}

## Polymorphic relationships

Union-style selections let GraphJin expose polymorphic subjects.

```graphql
query {
  comments {
    id
    subject {
      ... on products {
        id
        name
      }
      ... on users {
        id
        email
      }
    }
  }
}
```

{{< verified by="Example_queryWithUnionForPolymorphicRelationships" file="tests/query_test.go" line="1111" >}}

## MongoDB relationships

MongoDB relationships are not SQL joins. GraphJin emits JSON DSL and the MongoDB driver translates related selections to aggregation pipeline `$lookup` stages.

```json
{
  "$lookup": {
    "from": "products",
    "let": { "userId": "$_id" },
    "pipeline": [
      { "$match": { "$expr": { "$eq": ["$owner_id", "$$userId"] } } }
    ],
    "as": "products"
  }
}
```

Array-column joins use `$in`; embedded JSON virtual tables use `$unwind`, `$lookup`, and `$group` rather than a simple lookup.

{{< verified by="TestBuildCursorSeekFilterAsc" file="mongodriver/query_cursor_test.go" line="86" >}}

## Nested database joins

Cross-source joins preserve the same GraphQL shape but use different execution phases:

1. Compile and execute the parent query against its source.
2. Keep a placeholder field such as `__orders_db_join` in the parent JSON.
3. Extract the parent key from the result.
4. Build a child query such as `orders(where: { user_id: { eq: 42 } })`.
5. Execute that child query with the target source's compiler and connection.
6. Replace the placeholder with the child JSON.

That model is why same-database joins are usually cheaper than cross-database joins, and why table/source ownership should be explicit in multi-source deployments.

{{< verified by="TestBuildChildGraphQLQueryNestedDatabaseJoin" file="core/multidb_test.go" line="809" >}}
{{< verified by="TestDatabaseJoinFieldIds" file="core/multidb_test.go" line="544" >}}
