# GraphJin - A New Kind of JIT Compiler for Your Data

[![Apache 2.0](https://img.shields.io/github/license/dosco/graphjin.svg?style=for-the-badge)](https://github.com/dosco/graphjin/blob/master/LICENSE)
[![NPM Package](https://img.shields.io/npm/v/graphjin?style=for-the-badge)](https://www.npmjs.com/package/graphjin)
[![Docker Pulls](https://img.shields.io/docker/pulls/dosco/graphjin?style=for-the-badge)](https://hub.docker.com/r/dosco/graphjin/builds)
[![Discord Chat](https://img.shields.io/discord/628796009539043348.svg?style=for-the-badge&logo=discord)](https://discord.gg/6pSWCTZ)
[![GoDoc](https://img.shields.io/badge/godoc-reference-5272B4.svg?style=for-the-badge&logo=go)](https://pkg.go.dev/github.com/dosco/graphjin/core/v3)
[![GoReport](https://goreportcard.com/badge/github.com/gojp/goreportcard?style=for-the-badge)](https://goreportcard.com/report/github.com/dosco/graphjin/core/v3)

Point GraphJin at a database and start querying. It introspects your schema, discovers relationships from foreign keys, and understands your data model automatically. No models to define. No resolvers to write. No configuration required.

Write a GraphQL query and GraphJin compiles it to the native query language of each data source - SQL for Postgres/MySQL, aggregation pipelines for MongoDB, REST calls for remote APIs. One request can query multiple databases simultaneously. No N+1 queries.

## Query Multiple Databases in One Request

```graphql
query Dashboard {
  # Users from PostgreSQL
  users(limit: 5, order_by: { id: asc }) {
    id
    full_name
    email
  }

  # Events from MongoDB
  events(limit: 10, order_by: { id: desc }) {
    id
    type
    timestamp
  }

  # Audit logs from SQLite
  audit_logs(limit: 5) {
    id
    action
    created_at
  }
}
```

One request. Three databases. Each queried in its native language. Response merged automatically.

## Zero Configuration

```go
db, _ := sql.Open("pgx", "postgres://localhost/myapp")
gj, _ := core.NewGraphJin(nil, db)  // nil config - nothing to configure
```

That's the entire setup. GraphJin connects to your database and:
- Reads all table structures
- Discovers relationships from foreign keys
- Infers naming conventions (user_id → users table)
- Builds the full data graph

You're ready to query. No schema files. No model definitions. No relationship mapping.

## Complex Nested Queries, Single Optimized Query

GraphJin auto-discovers your schema and relationships. This GraphQL:

```graphql
query getProducts {
  products(
    limit: 20
    order_by: { price: desc }
    where: { price: { gte: 20, lt: 50 } }
  ) {
    id
    name
    price
    owner {
      full_name
      email
      category_counts(limit: 3) {
        count
        category { name }
      }
    }
    category(limit: 3) {
      id
      name
    }
  }
  products_cursor
}
```

Becomes one efficient SQL statement. No N+1. No multiple round trips. Just data.

## Why GraphJin Exists

Every product I worked on had the same problem: weeks spent building API backends that all do the same thing - query a database and reshape data for the frontend.

The pattern is always identical: figure out what the UI needs, write an endpoint, wrestle with an ORM, transform the response. Repeat for every feature. Every change.

I realized this is a compiler problem. GraphQL already describes what data you want. Why manually translate that to SQL?

So I built a compiler that does it automatically. GraphJin takes your GraphQL and generates optimized queries in the native language of each data source. The result is a single SQL statement (or MongoDB aggregation, or REST call) that fetches exactly what you asked for.

No backend code to write. No resolvers to maintain. Just GraphQL queries that work.

## Database Support

| Database | Queries | Mutations | Subscriptions | Arrays | Full-Text |
|----------|---------|-----------|---------------|--------|-----------|
| PostgreSQL | Yes | Yes | Yes | Yes | Yes |
| MySQL | Yes | Yes | Yes | No | Yes |
| MariaDB | Yes | Yes | Yes | No | Yes |
| MSSQL | Yes | Yes | Yes | No | No |
| Oracle | Yes | Yes | Yes | No | No |
| SQLite | Yes | Yes | Yes | No | FTS5 |
| MongoDB | Yes | Yes | Yes | Yes | Yes |
| CockroachDB | Yes | Yes | Yes | Yes | Yes |

Also works with AWS Aurora/RDS, Google Cloud SQL, and YugabyteDB.

## What You Get

**Query Power**
- Nested queries compiled to single optimized statements
- Full-text search, aggregations, cursor pagination
- Recursive relationships (parent/child trees)
- Polymorphic types
- Remote API joins (combine database + REST in one query)

**Mutations**
- Atomic nested inserts/updates across tables
- Connect/disconnect relationships in single mutation
- Validation with @constraint directives

**Real-time**
- GraphQL subscriptions with automatic change detection
- Cursor-based pagination for subscriptions

**Security**
- Role and attribute-based access control
- Row-level and column-level permissions
- Query allow-lists (clients can't modify queries in production)
- JWT support (Auth0, JWKS, Firebase, etc.)

**Developer Experience**
- Auto-discovers schema and relationships
- Works with Node.js and Go
- Built-in Web UI for query development
- Database migrations and seeding
- Tracing support (Zipkin, Prometheus, X-Ray, Stackdriver)
- Small Docker image, low memory footprint
- Hot-deploy and rollback

## Quick Start: Node.js

```console
npm install graphjin
```

```javascript
import graphjin from "graphjin";
import pg from "pg";

const { Client } = pg;
const db = new Client({
  host: "localhost",
  port: 5432,
  user: "postgres",
  password: "postgres",
  database: "myapp",
});
await db.connect();

const gj = await graphjin("./config", "dev.yml", db);

// Query
const result = await gj.query(
  "query { users(id: $id) { id email } }",
  { id: 1 },
  { userID: 1 }
);
console.log(result.data());

// Subscribe to changes
const sub = await gj.subscribe(
  "subscription { users(id: $id) { id email } }",
  null,
  { userID: 2 }
);
sub.data((res) => console.log(res.data()));
```

## Quick Start: Go

```console
go get github.com/dosco/graphjin/core/v3
```

```go
package main

import (
    "context"
    "database/sql"
    "log"
    "github.com/dosco/graphjin/core/v3"
    _ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
    db, err := sql.Open("pgx", "postgres://postgres:@localhost:5432/myapp")
    if err != nil {
        log.Fatal(err)
    }

    gj, err := core.NewGraphJin(nil, db)
    if err != nil {
        log.Fatal(err)
    }

    query := `query { posts { id title } }`

    ctx := context.WithValue(context.Background(), core.UserIDKey, 1)
    res, err := gj.GraphQL(ctx, query, nil, nil)
    if err != nil {
        log.Fatal(err)
    }

    log.Println(string(res.Data))
}
```

## Standalone Service

```bash
# Mac
brew install dosco/graphjin/graphjin

# Ubuntu
sudo snap install --classic graphjin

# Create new app
graphjin new myapp

# Deploy
graphjin deploy --host=https://your-server.com --secret="your-key"
```

Built-in web UI for developing queries:

![graphjin-screenshot-final](https://user-images.githubusercontent.com/832235/108806955-1c363180-7571-11eb-8bfa-488ece2e51ae.png)

## Production Security

In production, queries are read from saved files, not from client requests. Clients cannot modify queries. This makes GraphJin as secure as hand-written APIs - the "clients can send any query" concern with GraphQL doesn't apply here.

## AI Integration (MCP)

GraphJin includes native [Model Context Protocol (MCP)](https://modelcontextprotocol.io) support, allowing AI assistants like Claude to query your database directly. **MCP is enabled by default.**

**To disable MCP:**
```yaml
mcp:
  disable: true
```

**Claude Desktop integration** - add to `claude_desktop_config.json`:
```json
{
  "mcpServers": {
    "my-database": {
      "command": "graphjin",
      "args": ["mcp", "--config", "/path/to/config"],
      "env": {
        "GRAPHJIN_USER_ID": "admin",
        "GRAPHJIN_USER_ROLE": "admin"
      }
    }
  }
}
```

**HTTP endpoints** (when service is running):
- SSE: `GET /api/v1/mcp`
- HTTP: `POST /api/v1/mcp/message`

**16 MCP tools** including:
- Schema discovery (list tables, describe relationships)
- Query execution (GraphQL queries/mutations)
- Syntax reference (teaches LLMs GraphJin's DSL)
- Saved query search

See [MCP Documentation](docs/DESIGN-MCP.md) for full details.

## Documentation

- [Quick Start](https://graphjin.com/posts/start)
- [Full Documentation](https://graphjin.com)
- [Feature Reference](docs/FEATURES.md) - All 50+ features with examples
- [Go Examples](https://pkg.go.dev/github.com/dosco/graphjin/core#pkg-examples)

## Support

GraphJin is open source and saves teams months of development time. If your team uses it, consider becoming a sponsor.

## Get in Touch

[Twitter @dosco](https://twitter.com/dosco) | [Discord](https://discord.gg/6pSWCTZ)

## License

[Apache Public License 2.0](https://opensource.org/licenses/Apache-2.0)
