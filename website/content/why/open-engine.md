---
title: "An open engine underneath"
headline: "Underneath is GraphJin, and it is still open source."
description: "DeepORG is built on GraphJin, the Apache 2.0 engine that compiles each query into a single database query. It is also a GraphQL API server and a Go library in its own right."
lede: "DeepORG is built on GraphJin, the engine that compiles each query into a single database query. It is also a GraphQL API server and a Go library in its own right, under Apache 2.0."
scene: "engine"
scene_label: "Animated diagram: a nested GraphQL query on the left is compiled into one SQL statement on the right."
scene_caption: "graphql in / one statement out"
actions:
  - label: "Read the compiler docs"
    href: "/core/compiler/"
  - label: "GitHub"
    href: "https://github.com/dosco/deeporg"
landing: true
toc: false
---

{{< band id="how" label="How it works" title="One compiler. Any system. Any client." >}}

Point DeepORG at databases, object storage, source trees, and remote APIs. Underneath is GraphJin, a compiler, not a resolver framework: it learns the live shape, plans the work, and emits one optimized database operation. Then it enforces access rules and serves AI assistants, REST clients, and federated routers from the same engine. No N+1, no resolver code, test-backed against real compiler paths.

{{< svg "compiler-flow" "GraphJin compiler flow" >}}

{{< endband >}}

{{< band label="The compiler" title="A nested question becomes one statement." >}}

Ask for customers, their purchases, and each product's price. A resolver framework runs a query for the customers, then one for each customer's purchases, then one for each product. GraphJin parses the request, resolves it against the schema it discovered, and emits one statement:

{{< hl sql >}}SELECT json_agg(__sj.json) AS customers
FROM customers AS c
LEFT JOIN LATERAL (
  SELECT sum(p.quantity * pr.price) AS total_spent
  FROM purchases p
  JOIN products pr ON pr.id = p.product_id
  WHERE p.customer_id = c.id
) __agg ON true
ORDER BY __agg.total_spent DESC NULLS LAST
LIMIT 5;{{< /hl >}}

The agent benefits from this more than a person does. It writes a compact query and gets back exactly the shape it asked for, in one round trip, with access rules already applied.

The exception is a join that crosses two databases or reaches a remote API. There the child is resolved after the parent result exists.

{{< endband >}}

{{< band id="capabilities" label="On its own" title="A full backend, not just an agent gateway." >}}

The same binary and config that serve the agent also run your realtime, files, remote APIs, sign-in, and federation, with no extra services to operate.

{{< grid >}}

- **A GraphQL API server.** `graphjin serve` in front of your database, with no resolvers to write.
- **A Go library.** Embed the compiler in your own service. The examples in the package docs are tests.
- **Realtime.** Subscribe with the same GraphQL over WebSocket or SSE. Streams resume after a drop.
- **Saved queries.** In production, run reviewed queries by name and refuse anything else.
- **Caching.** Response caching on Redis with an in-memory fallback and stale-while-revalidate. A change invalidates what it touched.
- **Sign-in.** JWT and OIDC from Auth0, Firebase, Okta, or any JWKS, across HTTP, WebSocket, SSE, and MCP.
- **Files and APIs as tables.** A bucket or an OpenAPI spec becomes something you can join.
- **Federation.** Every keyed table can be an Apollo Federation v2 subgraph.

{{< endgrid >}}

{{< endband >}}

{{< band label="The name" title="DeepORG was formerly GraphJin." >}}

The project grew from an engine into an agent built on that engine, and the name followed. The engine keeps its name, and so does everything you install: the `graphjin` CLI, the Go module, the npm package and the Docker images, for now.

```bash
curl -fsSL https://deeporg.io/install.sh | bash   # macOS and Linux
npm install -g graphjin                              # any platform
brew install dosco/graphjin/graphjin                 # macOS
docker pull dosco/graphjin
```

It is Apache 2.0. Read it, fork it, run it without the agent.

{{< endband >}}

{{< band label="In the docs" title="Go deeper." >}}

- [The compiler](/core/compiler/) and the [query language](/core/query-language/).
- [Subscriptions](/core/subscriptions/) and [caching](/configure/caching-redis/).
- [Saved queries](/start/saved-queries/) and [production](/configure/environment-production/).
- [Federation](/integrations/federation/).
- [Go examples](https://pkg.go.dev/github.com/dosco/graphjin/core#pkg-examples) and [the source](https://github.com/dosco/deeporg).

{{< endband >}}
