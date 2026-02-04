# GraphJin - A Compiler to Connect AI to Your Databases

[![Apache 2.0](https://img.shields.io/github/license/dosco/graphjin.svg?style=for-the-badge)](https://github.com/dosco/graphjin/blob/master/LICENSE)
[![NPM Package](https://img.shields.io/npm/v/graphjin?style=for-the-badge)](https://www.npmjs.com/package/graphjin)
[![Docker Pulls](https://img.shields.io/docker/pulls/dosco/graphjin?style=for-the-badge)](https://hub.docker.com/r/dosco/graphjin/builds)
[![Discord Chat](https://img.shields.io/discord/628796009539043348.svg?style=for-the-badge&logo=discord)](https://discord.gg/6pSWCTZ)
[![GoDoc](https://img.shields.io/badge/godoc-reference-5272B4.svg?style=for-the-badge&logo=go)](https://pkg.go.dev/github.com/dosco/graphjin/core/v3)
[![GoReport](https://goreportcard.com/badge/github.com/gojp/goreportcard?style=for-the-badge)](https://goreportcard.com/report/github.com/dosco/graphjin/core/v3)

Point GraphJin at any database and AI assistants can query it instantly. Auto-discovers your schema, understands relationships, compiles to optimized SQL. No configuration required.

Works with PostgreSQL, MySQL, MongoDB, SQLite, Oracle, MSSQL - and models from Claude/GPT-4 to local 7B models.

## Installation

**npm (all platforms)**
```bash
npm install -g graphjin
```

**macOS (Homebrew)**
```bash
brew install dosco/graphjin/graphjin
```

**Windows (Scoop)**
```bash
scoop bucket add graphjin https://github.com/dosco/graphjin-scoop
scoop install graphjin
```

**Linux**

Download .deb/.rpm from [releases](https://github.com/dosco/graphjin/releases)

**Docker**
```bash
docker pull dosco/graphjin
```

## Try It Now

This is a quick way to try out GraphJin we'll use the `--demo` command which automatically
starts a database using docker and loads it with demo data.

Download the source which contains the `webshop` demo
```
git clone https://github.com/dosco/graphjin
cd graphjin
```

Now launch the Graphjin service that you installed using the install options above
```bash
graphjin serve --demo --path examples/webshop/config
```

You'll see output like this:
```
GraphJin started
───────────────────────
  Web UI:      http://localhost:8080/
  GraphQL:     http://localhost:8080/api/v1/graphql
  REST API:    http://localhost:8080/api/v1/rest/
  MCP:         http://localhost:8080/api/v1/mcp

Claude Desktop Configuration
────────────────────────────
Add to claude_desktop_config.json:

  {
    "mcpServers": {
      "Webshop Development": {
        "command": "/path/to/graphjin",
        "args": ["mcp", "--server", "http://localhost:8080"]
      }
    }
  }
```

Copy the JSON config shown and add it to your Claude Desktop config file (see below for file location). You can also click `File > Settings > Developer` to get to it in Claude Desktop. You will also need to **Restart Claude Desktop**

| OS | Possible config file locations |
|----|---------------------|
| macOS | `~/Library/Application Support/Claude/claude_desktop_config.json` |
| Windows | `%APPDATA%\Claude\claude_desktop_config.json` |

## Getting started

To use GraphJin with your own databases you have to first create a new GraphJin app, then configure it using its config files and then launch GraphJin.

**Step 1: Create New GraphJin App** 
```bash
graphjin new my-app
```

**Step 2: Start the GraphJin Service**
```bash
graphjin serve --path ./my-app
```

**Step 3: Add to Claude Desktop config file**

Copy paste the Claude Desktop Config provided by `graphjin serve` into the Claude Desktop MCP config file. How to do this has been defined clearly above in the `Try it Now` section.

**Step 4: Restart Claude Desktop**

**Step 5: Ask Claude questions like:**
- "What tables are in the database?"
- "Show me all products under $50"
- "List customers and their purchases"
- "What's the total revenue by product?"
- "Find products with 'wireless' in the name"
- "Add a new product called 'USB-C Cable' for $19.99"

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

GraphJin exposes several tools that guide AI models to write valid queries. Key tools: `list_tables` and `describe_table` for schema discovery, `get_query_syntax` for learning the DSL, `execute_graphql` for running queries, and `execute_saved_query` for production-approved queries. Prompts like `write_query` and `fix_query_error` help models construct and debug queries.

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

**Claude Desktop config** (`claude_desktop_config.json`):
```json
{
  "mcpServers": {
    "my-database": {
      "command": "graphjin",
      "args": ["mcp", "--path", "/path/to/config"],
    }
  }
}
```

## Also a GraphQL API

GraphJin works as a traditional API too - use it from Go or as a standalone service.

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



- [Configuration Reference](CONFIG.md)
- [Feature Reference](docs/FEATURES.md)
- [Go Examples](https://pkg.go.dev/github.com/dosco/graphjin/core#pkg-examples)

## Get in Touch

[Twitter @dosco](https://twitter.com/dosco) | [Discord](https://discord.gg/6pSWCTZ)

## License

[Apache Public License 2.0](https://opensource.org/licenses/Apache-2.0)
