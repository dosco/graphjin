---
title: "Quick Start"
description: "Run the smallest useful GraphJin query against a discovered schema."
nav_group: "start"
doc_kind: "guide"
weight: 20
---

Want seeded data and the built-in agent immediately? Boot a [demo vertical](/start/demos/) first - this page is for pointing GraphJin at your own database.

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

## Open the Web UI

Open [http://localhost:8080/](http://localhost:8080/) after the service starts. The Runtime view is a quick visual check that GraphJin discovered the configured sources and is serving the built-in system roots.

<figure class="doc-screenshot">
  <img src="/assets/webui-runtime-overview.webp" alt="GraphJin Web UI Runtime view showing ready sources, table count, catalog readiness, and security posture." loading="lazy">
  <figcaption>The Web UI is served by GraphJin itself and reads from the same runtime, catalog, and security GraphQL roots.</figcaption>
</figure>

Use the Workbench at [http://localhost:8080/workbench](http://localhost:8080/workbench) to try queries interactively. It sends requests to the same `/api/v1/graphql` endpoint that applications and command-line smoke tests use.

<figure class="doc-screenshot">
  <img src="/assets/webui-workbench-query.webp" alt="GraphJin Web UI Workbench running a products query and showing JSON results." loading="lazy">
  <figcaption>Workbench is useful for exploring the schema and result shape before you copy the same query into an API call.</figcaption>
</figure>

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

Keep a raw endpoint call in your smoke test even if you used the Workbench. This verifies the HTTP API path your application or integration will call.

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

- Let GraphJin run the loop for you: the [built-in agent](/agentic/server-agent/) turns one instruction into a typed, evidence-backed answer.
- Learn the [query language](/core/query-language/).
- Add [filters](/core/filters/) and [cursor pagination](/core/ordering-cursors/).
- Lock down production with [saved queries](/start/saved-queries/) and [RBAC](/configure/auth-rbac/).
