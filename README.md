# GraphJin - A Compiler to Connect AI to Your Databases

[![Apache 2.0](https://img.shields.io/github/license/dosco/graphjin.svg?style=for-the-badge)](https://github.com/dosco/graphjin/blob/master/LICENSE)
[![NPM Package](https://img.shields.io/npm/v/graphjin?style=for-the-badge)](https://www.npmjs.com/package/graphjin)
[![Docker Pulls](https://img.shields.io/docker/pulls/dosco/graphjin?style=for-the-badge)](https://hub.docker.com/r/dosco/graphjin/tags)
[![Discord Chat](https://img.shields.io/discord/628796009539043348.svg?style=for-the-badge&logo=discord)](https://discord.gg/6pSWCTZ)
[![GoDoc](https://img.shields.io/badge/godoc-reference-5272B4.svg?style=for-the-badge&logo=go)](https://pkg.go.dev/github.com/dosco/graphjin/core/v3)
[![GoReport](https://goreportcard.com/badge/github.com/gojp/goreportcard?style=for-the-badge)](https://goreportcard.com/report/github.com/dosco/graphjin/core/v3)

Point GraphJin at any database and AI assistants can query it instantly. Auto-discovers your schema, understands relationships, compiles to optimized SQL. No configuration required.

Works with PostgreSQL, MySQL, MongoDB, SQLite, Oracle, MSSQL, Snowflake - and models from Claude/GPT-4 to local 7B models.

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
graphjin serve --demo --path examples/webshop
```

You'll see output like this:
```
GraphJin started
───────────────────────
  Web UI:      http://localhost:8080/
  GraphQL:     http://localhost:8080/api/v1/graphql
  REST API:    http://localhost:8080/api/v1/rest/
  Workflows:   http://localhost:8080/api/v1/workflows/<name>
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

## Authenticate the CLI / MCP

Before `graphjin cli` or `graphjin mcp` can talk to a server, point them at one. There are no `--server` or `--token` flags — both come from a single saved config file (`~/.config/graphjin/client.json`, mode `0600`):

```bash
graphjin cli setup http://localhost:8080            # local dev, no auth needed
graphjin cli setup https://graphjin.example.com     # signs in via the server's OIDC IdP
```

What `setup` does, depending on the server:

- **No built-in login** (the server has `auth_login.enabled: false`): saves only the URL. CLI calls send no `Authorization` header.
- **Built-in login enabled**: kicks off an [RFC 8628 device-code flow](https://www.rfc-editor.org/rfc/rfc8628). The CLI prints a verification URL + short code, opens your browser, you sign in with the configured identity provider (Google, Okta, Keycloak, Auth0-as-IdP, Azure AD — anything OIDC), and the server mints a 30-day JWT. Both URL and JWT are saved to `client.json`.

After setup every `graphjin cli ...` command just works:

```bash
graphjin cli health
graphjin cli query list
graphjin cli schema tables
graphjin cli setup show       # print the saved config (token redacted)
graphjin cli setup logout     # delete client.json
graphjin cli setup            # re-run sign-in against the same server (refresh token)
```

To enable built-in login, set this on the server:

```yaml
auth:
  jwt:
    secret: "long-random-shared-secret"   # used to sign and verify local JWTs

auth_login:
  enabled: true
  audience_graphjin: true                 # shorthand for audience: "graphjin-cli"
  oidc:
    issuer_url: "https://accounts.google.com"
    client_id: "..."
    client_secret: "..."                  # or $GJ_AUTH_LOGIN_OIDC_CLIENT_SECRET
    allowed_domains: ["example.com"]      # optional allow-list
```

Successful authentication is recorded in structured logs with the verified `email` and `name` claims (when present), giving you a clean audit trail of who called every endpoint.

### MCP install for OpenAI Codex + Claude Code

GraphJin includes a guided installer that configures MCP for OpenAI Codex, Claude Code, or both. Run `graphjin mcp setup <server-url>` first — `mcp install` reads the server URL from `client.json`, and the MCP-client config it writes is credential-free, so rotating tokens (re-running `mcp setup`) needs no edits to Claude / Codex.

```bash
graphjin mcp setup https://graphjin.example.com
graphjin mcp install                                # guided: target client + scope
```

#### OpenAI Codex

<img src="website/public/logos/openai-codex.svg" alt="OpenAI Codex logo" width="280">

```bash
graphjin mcp install --client codex --scope global --yes
```

#### Claude Code

<img src="website/public/logos/claude-code.svg" alt="Claude Code logo" width="280">

```bash
graphjin mcp install --client claude --scope global --yes
```

#### Troubleshooting

- `mcp install` requires a saved server URL — run `graphjin mcp setup <server-url>` first if you see "no GraphJin server configured".
- The generated MCP-client config is `args: ["mcp"]`. `graphjin mcp` reads server + token from `client.json` on its own, in proxy mode when a server is saved or local mode when it's not.
- If Codex CLI does not support `codex mcp add --scope` (older versions), GraphJin automatically falls back to updating:
  - global scope: `~/.codex/config.toml`
  - local scope: `.codex/config.toml`

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
4. **Runs JS workflows** - Chain multiple GraphJin MCP tools in one reusable workflow
5. **Compiles to SQL** - Every request becomes a single optimized query

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

Subscribe over **WebSockets** (`graphql-ws` / `graphql-transport-ws` subprotocols) or **Server-Sent Events** — set `Accept: text/event-stream` on a `POST /api/v1/graphql` request and GraphJin streams `event: next` frames for each result, terminated by `event: complete`. Works from Node.js, Go, or any browser `EventSource` / WebSocket client.

## HTTP API Routes

`graphjin serve` exposes everything under a single host/port. All routes go through the configured auth handler unless noted.

| Route | Methods | Purpose |
|---|---|---|
| `/api/v1/graphql` | `GET`, `POST` | GraphQL queries and mutations. Subscriptions if the request is a WebSocket upgrade or carries `Accept: text/event-stream` (SSE). |
| `/api/v1/rest/<name>` | `GET`, `POST` | Run a saved/persisted query by name. Variables go in `?variables=…` (GET) or the JSON body (POST). |
| `/api/v1/workflows/<name>` | `POST` | Execute the JS workflow at `./workflows/<name>.js` with the JSON body as `input`. |
| `/api/v1/openapi.json` | `GET` | OpenAPI 3 spec generated from your saved REST queries. |
| `/api/v1/mcp` | `POST` | MCP (Model Context Protocol) HTTP transport — Streamable HTTP, stateless. |
| `/api/v1/mcp/message` | `POST` | MCP HTTP transport for stateless message integrations. |
| `/api/v1/discovery` | `GET` | Discovery document (tables, insights, database overview) for the default database. |
| `/api/v1/discovery/<section>` | `GET` | Drill into a discovery sub-section (e.g. `tables`, `insights`). |
| `/api/v1/admin/tables` | `GET` | Admin: list known tables (Web UI). |
| `/api/v1/admin/tables/<name>` | `GET` | Admin: schema for a single table. |
| `/api/v1/admin/queries` | `GET` | Admin: list saved queries. |
| `/api/v1/admin/queries/<name>` | `GET` | Admin: details for a saved query. |
| `/api/v1/admin/fragments` | `GET` | Admin: list GraphQL fragments. |
| `/api/v1/admin/config` | `GET` | Admin: effective runtime config. |
| `/api/v1/admin/database` / `/api/v1/admin/databases` | `GET` | Admin: connected database info. |
| `/api/v1/auth/device` | `POST` | OIDC device-flow start (only if `auth_login.enabled`). |
| `/api/v1/auth/device/token` | `POST` | OIDC device-flow poll. |
| `/api/v1/auth/login` | `GET` | OIDC login redirect. |
| `/api/v1/auth/callback` | `GET` | OIDC callback. |
| `/health` | `GET` | Liveness probe. **No auth.** |
| `/` | `GET` | Built-in Web UI (only when `webui: true`). |

**Mode flags that change which routes are live:**
- `mcp.disable: true` — removes `/api/v1/mcp` and `/api/v1/mcp/message`.
- `mcp.only: true` — keeps only `/health`, `/api/v1/mcp*`, `/api/v1/workflows/*`, and `/api/v1/discovery*`. The GraphQL/REST/OpenAPI/Web UI routes are not registered.
- `webui: false` — drops `/` and the `/api/v1/admin/*` routes.

## MCP Tools

GraphJin exposes several tools that guide AI models to write valid queries. Key tools: `list_tables` and `describe_table` for schema discovery, `get_query_syntax` for learning the DSL, `execute_graphql` for running queries, and `execute_saved_query` for production-approved queries.

For JS orchestration, use:
- `get_js_runtime_api` to discover exactly which globals/functions are available inside workflow scripts
- `execute_workflow` to run `./workflows/<name>.js` with input variables

Prompts like `write_query` and `fix_query_error` help models construct and debug queries.

## JS Workflows (MCP + REST)

Workflows let an LLM run multi-step logic in JavaScript while still using GraphJin MCP tools for DB-aware operations.

Create a file in `./workflows`, for example `./workflows/customer_insights.js`:

```js
function main(input) {
  const tables = gj.tools.listTables({});
  const top = gj.tools.executeSavedQuery({
    name: "top_customers",
    variables: { limit: input.limit || 5 }
  });
  return { tables, top };
}
```

### Run via MCP

Call:
- `get_js_runtime_api` first (for exact runtime schema)
- `execute_workflow` with:
  - `name`: workflow file name (with or without `.js`)
  - `variables`: input payload passed to global `input` and `main(input)`

### Run via REST

- `POST /api/v1/workflows/<name>` with JSON body
- `GET /api/v1/workflows/<name>?variables={...json...}`

Both map variables to the same workflow input object.

## Chat Walkthroughs

Real conversations showing how an AI assistant uses GraphJin MCP tools to help you go from zero to running queries.

### "I have databases running but don't know the details"

> **You:** I have some databases running locally but I'm not sure about the ports or connection details. Can you help me set up GraphJin?

> **Claude:** Let me scan your system to find running databases.
>
> *calls `discover_databases`*
>
> I found 2 databases on your system:
> - **PostgreSQL** on localhost:5432 (TCP) — listening, auth: ok (user: postgres), databases: `myapp`, `myapp_test`
> - **MySQL** on localhost:3306 (TCP) — listening, auth: auth_failed — default credentials failed, provide username and password
>
> The PostgreSQL instance has databases `myapp` and `myapp_test`. Which one would you like to configure GraphJin with?

> **You:** Let's go with the "myapp" database on Postgres.

> **Claude:** I'll configure GraphJin to connect to your PostgreSQL database.
>
> *calls `update_current_config` with databases: {"main": {type: "postgres", host: "localhost", port: 5432, user: "postgres", dbname: "myapp"}}*
>
> Connected successfully! Let me see what tables you have.
>
> *calls `list_tables`*
>
> Your database has these tables: `users`, `orders`, `products`, `reviews`. Want me to write a query?

> **You:** Show me users with their orders.

> **Claude:** *calls `get_query_syntax` then `execute_graphql`*
> ```graphql
> { users { id name email orders { id total status } } }
> ```
> Here are the results — 24 users returned, each with their associated orders.

### "I have a Docker Compose project with MySQL"

> **You:** I'm running a Docker Compose stack with MySQL. Help me connect GraphJin to it.

> **Claude:** Let me check what database containers you have running.
>
> *calls `discover_databases`*
>
> Found a MySQL Docker container:
> - **MySQL** via Docker — container `myapp-db-1`, image `mysql:8.0`, port mapped to localhost:13306
>
> I'll configure GraphJin with the discovered connection details.
>
> *calls `update_current_config` with databases: {"main": {type: "mysql", host: "localhost", port: 13306, user: "root", dbname: ""}}*
>
> What's the database name and password for your MySQL instance?

> **You:** Database is "shop_db", password is "secret123".

> **Claude:** *calls `update_current_config` with the full credentials*
>
> Connected! Let me explore the schema.
>
> *calls `list_tables`*
>
> Tables found: `customers`, `products`, `orders`, `order_items`, `categories`. Let me check how they relate.
>
> *calls `explore_relationships` with table: "orders"*
>
> `orders` connects to `customers` (via customer_id) and `order_items` (via order_id), and `order_items` links to `products`. Here's a nested query:
>
> ```graphql
> { orders { id total customer { name email } order_items { quantity product { name price } } } }
> ```

### "I want to query my SQLite app database"

> **You:** I have a SQLite database file in my project. Can GraphJin work with it?

> **Claude:** Absolutely! Let me scan your project directory for SQLite files.
>
> *calls `discover_databases` with scan_dir: "."*
>
> Found 1 SQLite database:
> - **data/app.sqlite3** — file found
>
> I'll configure GraphJin to use it.
>
> *calls `update_current_config` with databases: {"main": {type: "sqlite", path: "data/app.sqlite3"}}*
>
> Connected! Let me see your tables.
>
> *calls `list_tables`*
>
> Tables: `tasks`, `projects`, `tags`, `task_tags`. Want to query something?

> **You:** Show me all tasks with their project name, sorted by due date.

> **Claude:** *calls `execute_graphql`*
> ```graphql
> { tasks(order_by: {due_date: asc}) { id title due_date completed project { name } } }
> ```
> Here are your 12 tasks sorted by due date, each showing the parent project name.

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
| Snowflake | Yes | Yes | No | No | No |
| CockroachDB | Yes | Yes | Yes | Yes | No |

Also works with AWS Aurora/RDS, Google Cloud SQL, and YugabyteDB. Snowflake supports key pair (JWT) authentication.

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
