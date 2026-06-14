---
title: "Quick Start"
description: "Run the smallest useful GraphJin query against a discovered schema."
nav_group: "start"
doc_kind: "guide"
weight: 20
---

## Minimal config

Point GraphJin at your database in `config/dev.yml`. New source-aware projects should use `sources:`:

```yaml
sources:
  - name: app
    kind: database
    type: postgres
    default: true
    connection_string: ${DATABASE_URL}
    schema: public
```

Then start the service:

```bash
graphjin serve
```

## First query

GraphJin discovers tables and relationships, then compiles GraphQL into database work.

```graphql
query {
  products(limit: 3, order_by: { id: asc }) {
    id
    name
    owner {
      id
      fullName: full_name
    }
  }
}
```

The response shape follows the GraphQL selection. There are no user-written resolvers for database fields.

{{< verified by="Example_query" file="tests/query_test.go" line="18" >}}

## Call the endpoint

```bash
curl http://localhost:8080/api/v1/graphql \
  -H 'content-type: application/json' \
  -d '{"query":"query { products(limit: 3, order_by: { id: asc }) { id name } }"}'
```

Pass variables as JSON. Keep filters inside the GraphQL query shape and pass leaf values through variables:

```graphql
query Products($maxPrice: Float!) {
  products(where: { price: { lteq: $maxPrice } }, order_by: { price: asc }) {
    id
    name
    price
  }
}
```

```json
{ "maxPrice": 25 }
```

## Next steps

- Learn the [query language](/core/query-language/).
- Add [filters](/core/filters/) and [cursor pagination](/core/ordering-cursors/).
- Lock down production with [saved queries](/start/saved-queries/) and [RBAC](/configure/auth-rbac/).
