# GraphJin - Connect AI to Your Database

[![Apache 2.0](https://img.shields.io/github/license/dosco/graphjin.svg?style=for-the-badge)](https://github.com/dosco/graphjin/blob/master/LICENSE)
[![NPM Package](https://img.shields.io/npm/v/graphjin?style=for-the-badge)](https://www.npmjs.com/package/graphjin)
[![Docker Pulls](https://img.shields.io/docker/pulls/dosco/graphjin?style=for-the-badge)](https://hub.docker.com/r/dosco/graphjin/builds)
[![Discord Chat](https://img.shields.io/discord/628796009539043348.svg?style=for-the-badge&logo=discord)](https://discord.gg/6pSWCTZ)
[![GoDoc](https://img.shields.io/badge/godoc-reference-5272B4.svg?style=for-the-badge&logo=go)](https://pkg.go.dev/github.com/dosco/graphjin/core/v3)
[![GoReport](https://goreportcard.com/badge/github.com/gojp/goreportcard?style=for-the-badge)](https://goreportcard.com/report/github.com/dosco/graphjin/core/v3)

Point GraphJin at any database and AI assistants can query it instantly. Auto-discovers your schema, understands relationships, compiles to optimized SQL. No configuration required.

Works with PostgreSQL, MySQL, MongoDB, SQLite, Oracle, MSSQL - and models from Claude/GPT-4 to local 7B models.

## Try It Now

```bash
# With Claude Desktop - run the demo
graphjin mcp demo --path examples/webshop/config
graphjin mcp demo info  # Copy output to Claude Desktop config

# With your own database
graphjin mcp --path /path/to/config
graphjin mcp info  # Copy output to Claude Desktop config
```

Within minutes, ask Claude: "What products do we have?" or "Show me orders from last week"

## How It Works

1. **Connects to database** - Reads your schema automatically
2. **Discovers relationships** - Foreign keys become navigable joins
3. **Exposes MCP tools** - Teach any LLM the query syntax
4. **Compiles to SQL** - Every request becomes a single optimized query

No resolvers. No ORM. No N+1 queries. Just point and query.

## What AI Can Do

**Simple queries with filters:**
```graphql
{ products(where: { price: { gt: 50 } }, limit: 10) { id name price } }
```

**Nested relationships:**
```graphql
{
  orders(limit: 5) {
    id total
    customer { name email }
    items { quantity product { name category { name } } }
  }
}
```

**Aggregations:**
```graphql
{ products { count_id sum_price avg_price } }
```

**Mutations:**
```graphql
mutation {
  products(insert: { name: "New Product", price: 29.99 }) { id }
}
```

**Spatial queries:**
```graphql
{
  stores(where: { location: { st_dwithin: { point: [-122.4, 37.7], distance: 1000 } } }) {
    name address
  }
}
```

## Real-time Subscriptions

Get live updates when your data changes. GraphJin handles thousands of concurrent subscribers with a single database query - not one per subscriber.

```graphql
subscription {
  orders(where: { user_id: { eq: $user_id } }) {
    id total status
    items { product { name } }
  }
}
```

**Why it's efficient:**
- Traditional approach: 1,000 subscribers = 1,000 database queries
- GraphJin: 1,000 subscribers = 1 optimized batch query
- Automatic change detection - updates only sent when data actually changes
- Built-in cursor pagination for feeds and infinite scroll

Works from Node.js, Go, or any WebSocket client.

## MCP Tools

GraphJin exposes 15 tools that guide AI models to write valid queries. Key tools: `list_tables` and `describe_table` for schema discovery, `get_query_syntax` for learning the DSL, `execute_graphql` for running queries, and `execute_saved_query` for production-approved queries. Prompts like `write_query` and `fix_query_error` help models construct and debug queries.

## Database Support

| Database | Queries | Mutations | Subscriptions | Full-Text | GIS |
|----------|---------|-----------|---------------|-----------|-----|
| PostgreSQL | Yes | Yes | Yes | Yes | PostGIS |
| MySQL | Yes | Yes | Yes | Yes | 8.0+ |
| MariaDB | Yes | Yes | Yes | Yes | Yes |
| MSSQL | Yes | Yes | Yes | No | Yes |
| Oracle | Yes | Yes | Yes | No | Yes |
| SQLite | Yes | Yes | Yes | FTS5 | SpatiaLite |
| MongoDB | Yes | Yes | Yes | Yes | Yes |
| CockroachDB | Yes | Yes | Yes | Yes | No |

Also works with AWS Aurora/RDS, Google Cloud SQL, and YugabyteDB.

## Production Security

**Query allow-lists** - In production, only saved queries can run. AI models call `execute_saved_query` with pre-approved queries. No arbitrary SQL injection possible.

**Role-based access** - Different roles see different data:
```yaml
roles:
  user:
    tables:
      - name: orders
        query:
          filters: ["{ user_id: { eq: $user_id } }"]
```

**JWT authentication** - Supports Auth0, Firebase, JWKS endpoints.

**Response caching** - Redis with in-memory fallback. Automatic cache invalidation.

## CLI Reference

```bash
# Run MCP server (stdio mode for Claude Desktop)
graphjin mcp --path /path/to/config

# Show Claude Desktop config JSON
graphjin mcp info

# Run with temporary database container
graphjin mcp demo --path examples/webshop/config

# Show demo mode config JSON
graphjin mcp demo info

# Keep data between restarts
graphjin mcp demo --persist

# Override database type
graphjin mcp demo --db mysql

# Set auth context
graphjin mcp --user-id admin --user-role admin
```

**Claude Desktop config** (`claude_desktop_config.json`):
```json
{
  "mcpServers": {
    "my-database": {
      "command": "graphjin",
      "args": ["mcp", "--path", "/path/to/config"],
      "env": {
        "GRAPHJIN_USER_ID": "admin",
        "GRAPHJIN_USER_ROLE": "admin"
      }
    }
  }
}
```

## Also a GraphQL API

GraphJin works as a traditional API too - use it from Node.js, Go, or as a standalone service.

### Node.js
```bash
npm install graphjin
```
```javascript
import graphjin from "graphjin";
import pg from "pg";

const db = new pg.Client({ host: "localhost", database: "myapp" });
await db.connect();

const gj = await graphjin("./config", "dev.yml", db);
const result = await gj.query("{ users(id: $id) { id email } }", { id: 1 });
console.log(result.data());
```

### Go
```bash
go get github.com/dosco/graphjin/core/v3
```
```go
db, _ := sql.Open("pgx", "postgres://localhost/myapp")
gj, _ := core.NewGraphJin(nil, db)
res, _ := gj.GraphQL(ctx, `{ users { id email } }`, nil, nil)
```

### Standalone Service
```bash
brew install dosco/graphjin/graphjin  # Mac
graphjin new myapp && cd myapp
graphjin serve
```

Built-in web UI at `http://localhost:8080` for query development.

## Documentation

- [Quick Start](https://graphjin.com/posts/start)
- [Full Documentation](https://graphjin.com)
- [Feature Reference](docs/FEATURES.md)
- [Go Examples](https://pkg.go.dev/github.com/dosco/graphjin/core#pkg-examples)

## Get in Touch

[Twitter @dosco](https://twitter.com/dosco) | [Discord](https://discord.gg/6pSWCTZ)

## License

[Apache Public License 2.0](https://opensource.org/licenses/Apache-2.0)
