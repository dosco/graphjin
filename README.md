# GraphJin — One Governed Graph for Your AI Agents

[![Apache 2.0](https://img.shields.io/github/license/dosco/graphjin.svg?style=for-the-badge)](https://github.com/dosco/graphjin/blob/master/LICENSE)
[![NPM Package](https://img.shields.io/npm/v/graphjin?style=for-the-badge)](https://www.npmjs.com/package/graphjin)
[![Docker Pulls](https://img.shields.io/docker/pulls/dosco/graphjin?style=for-the-badge)](https://hub.docker.com/r/dosco/graphjin/tags)
[![Discord Chat](https://img.shields.io/discord/628796009539043348.svg?style=for-the-badge&logo=discord)](https://discord.gg/6pSWCTZ)
[![GoDoc](https://img.shields.io/badge/godoc-reference-5272B4.svg?style=for-the-badge&logo=go)](https://pkg.go.dev/github.com/dosco/graphjin/core/v3)
[![GoReport](https://goreportcard.com/badge/github.com/gojp/goreportcard?style=for-the-badge)](https://goreportcard.com/report/github.com/dosco/graphjin/core/v3)

GraphJin is a compiler and runtime that gives AI agents one governed graph over the systems a real company already has: databases, warehouses, files, source code, workflows, metadata, and security policy. Instead of handing an agent raw credentials and hoping it guesses correctly, GraphJin exposes that graph through GraphQL + MCP: the agent discovers before acting, validates queries, runs approved work, observes runtime status — and every answer is checked against an execution ledger before it leaves the server.

It is not only for agents. GraphJin is still a high-performance GraphQL-to-database compiler, Go library, standalone API service, REST/OpenAPI gateway, and real-time subscription server. The agent use case is where everything comes together: the same compiler that serves your apps can also give AI a smart, auditable way to work across data, code, and operations.

Works with PostgreSQL, MySQL, MongoDB, SQLite, Oracle, MSSQL, Snowflake, Redshift, BigQuery, Apache Cassandra / Amazon Keyspaces, S3/GCS/local files, CodeSQL source indexes - and models from Claude/GPT-4 to local 7B models.

## Why GraphJin For Agents

- **A built-in agent, one call away** - POST one instruction to `/api/v1/agent` (or call the `ask_graphjin_agent` MCP tool) and GraphJin runs the discovery loop itself - as the caller, under the caller's permissions - and returns a typed, evidence-backed answer. See [Server-Side Agent](#server-side-agent).
- **One governed surface for many systems** - Query operational databases, warehouses, MongoDB, object stores, local files, CodeSQL source indexes, workflows, and GraphJin system roots through GraphQL and MCP.
- **Smart discovery before action** - Agents start with `query_catalog(search: "<user instruction>")`, `graphql_help`, relationship evidence, examples, config recipes, and safety notes before writing or running queries.
- **Guarded action, not raw access** - Source-mode access, query allow-lists, read-only boundaries, policy-aware MCP tools, local encrypted secrets, and `gj_config` preview/apply keep changes auditable.
- **Operational awareness** - `gj_security`, `gj_runtime`, and the built-in console expose policy and bounded runtime status so agents can check what is safe before they act.
- **Durable memory and standing questions** - Saved queries, fragments, and workflows live in the owner-scoped `gj_artifacts` store; cursor-backed watches (`gj_watch`) run standing questions under the owner's permissions, resume from persisted subscription cursors, and deliver fired events to a durable inbox (`gj_watch_event`), webhooks, or workflows. Normal watches are durable by default; explicit ephemeral watches use TTL leases.

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

One command, no clone, no Docker. The binary ships with a built-in demo — a
SaaS company ops app (accounts, subscriptions, invoices, support tickets) on
SQLite with seeded data, saved queries, and workflows:

```bash
graphjin serve --demo
```

The demo project is extracted to `./graphjin-demo`, and its state lives under
`./graphjin-demo/demo/`. Delete the `demo/` folder to reset the data, or
delete `./graphjin-demo` entirely for a fresh copy. You'll see output like
this:

```
GraphJin started
───────────────────────
  Web UI:      http://localhost:8083/
  GraphQL:     http://localhost:8083/api/v1/graphql
  REST API:    http://localhost:8083/api/v1/rest/
  Workflows:   http://localhost:8083/api/v1/workflows/<name>
  MCP:         http://localhost:8083/api/v1/mcp
```

**Ask the built-in agent**

With a model API key in `./.env` (`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, or `GOOGLE_APIKEY`), the same command switches into agentic mode and enables GraphJin's built-in agent - one instruction in, a typed, evidence-backed answer out:

```bash
curl -sS localhost:8083/api/v1/agent \
  -H 'content-type: application/json' \
  -d '{"instruction": "Which account is most at risk of churning?"}'
```

Answers come back as `{status, answer, data, evidence, actions, next}`, grounded by server-side protocol guards. You can also chat with it in the built-in web console at `localhost:8083/agent` — it streams every tool call live as it works. See [Server-Side Agent](#server-side-agent) below, [AGENTIC.md](AGENTIC.md#server-side-agent), and [graphjin.com/agentic/server-agent](https://graphjin.com/agentic/server-agent/).

**More demo verticals**

The bigger demos live in the repo — clone it and pass `--path`:

```bash
git clone https://github.com/dosco/graphjin
cd graphjin
graphjin serve --demo --path examples/webshop          # classic GraphQL starter (Postgres)
graphjin serve --demo --path examples/coffee-roastery  # flagship agentic demo (Postgres + BigQuery-emu + CodeSQL)
```

Each demo keeps its state under `<path>/demo/` and reuses it on later starts.
A MySQL corrugated-box plant with JWT roles and a PCB fab spanning Postgres +
Snowflake-emu + MongoDB + a file source + an OpenAPI supplier API live in
[examples/](examples/README.md), each with an end-to-end smoke suite
(`make smoke-all` runs them all). The full tour is at
<https://graphjin.com/start/demos/>.

## Add GraphJin To Your AI Client

### Local / Dev

Use GraphJin's helper when you want one command that normalizes the URL, probes
auth, and installs the right Codex or Claude config:

```bash
graphjin mcp add codex
graphjin mcp add claude
graphjin mcp add all http://localhost:8080
```

Defaults are `client=codex`, `server=http://localhost:8080`, and project scope.
The command normalizes the server to `http://localhost:8080/api/v1/mcp`. Local
non-TLS HTTP is correct for loopback development; hosted servers should use
HTTPS.

If you prefer native client commands, add GraphJin's Streamable HTTP endpoint
directly:

```bash
codex mcp add graphjin --url http://localhost:8080/api/v1/mcp
claude mcp add --transport http graphjin http://localhost:8080/api/v1/mcp
```

GraphJin's `/api/v1/mcp` endpoint is Streamable HTTP, so Claude should use
`--transport http` for GraphJin. SSE is only for older/custom MCP servers.

Use `--global` when you want the MCP connection available outside the current project:

```bash
graphjin mcp add codex --global
```

Codex can also add non-URL stdio MCP servers with the generic command shape:

```bash
codex mcp add <server-name> -- <command> [args...]
```

### Hosted GraphJin With OAuth

When `mcp.oauth.enabled: true` is configured on a hosted GraphJin server, modern
MCP clients can add it by URL and handle OAuth login themselves:

```bash
codex mcp add graphjin --url https://graphjin.example.com/api/v1/mcp
claude mcp add --transport http graphjin https://graphjin.example.com/api/v1/mcp
```

This is the native remote-MCP path. GraphJin serves OAuth protected-resource
metadata, authorization-server metadata, DCR/CIMD discovery, and MCP 401
challenges so the client can discover login automatically. See the official
[OpenAI Docs MCP quickstart](https://platform.openai.com/docs/docs-mcp) for the
Codex `mcp add --url` flow and the
[Claude Code MCP docs](https://docs.claude.com/en/docs/claude-code/mcp) for
Claude's HTTP transport and authentication flow.

For legacy/custom SSE servers, use Claude's SSE transport explicitly:

```bash
claude mcp add --transport sse <name> <url>
claude mcp add --transport sse private-api https://api.company.com/sse \
  --header "X-API-Key: your-key-here"
```

### Legacy / Current `auth_login` Fallback

If a server still uses GraphJin's current `auth_login` device-code flow instead of standards OAuth, `graphjin mcp add` detects that automatically:

```bash
graphjin mcp add codex https://graphjin.example.com
```

The command opens the device-code login, saves `~/.config/graphjin/client.json`, and installs a credential-free local proxy config for the AI client. Re-run `graphjin mcp setup https://graphjin.example.com` later only when you want to refresh or rotate that saved CLI/proxy token.

The deprecated aliases still work for scripts:

```bash
graphjin mcp install codex https://graphjin.example.com
graphjin mcp plugin install https://graphjin.example.com   # deprecated Claude alias
```

## Authenticate The CLI

Before `graphjin cli` can talk to a server, point it at one. There are no `--server` or `--token` flags — both come from a single saved config file (`~/.config/graphjin/client.json`, mode `0600`):

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
  type: jwt
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

mcp:
  oauth:
    enabled: true
    mode: builtin                         # reuses auth_login identity
    scopes: ["mcp"]
```

Successful authentication is recorded in structured logs with the verified `email` and `name` claims (when present), giving you a clean audit trail of who called every endpoint.

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

**Step 3: Add GraphJin to an AI client**

```bash
graphjin mcp add claude http://localhost:8080
```

**Step 4: Ask Claude questions like:**
- "What tables are in the database?"
- "Show me all products under $50"
- "List customers and their purchases"
- "What's the total revenue by product?"
- "Find products with 'wireless' in the name"
- "Add a new product called 'USB-C Cable' for $19.99"

## How It Works

1. **Connects to database** - Reads your schema automatically
2. **Discovers relationships** - Foreign keys become navigable joins
3. **Exposes metadata** - `gj_*` tables make discovered databases, tables, columns, relationships, functions, and indexes queryable when the GraphJin source is enabled
4. **Indexes source code** - CodeSQL turns tree-sitter syntax trees and database references into a managed SQLite database
5. **Exposes MCP tools** - Teach any LLM the query syntax
6. **Runs JS workflows** - Chain multiple GraphJin MCP tools in one reusable workflow
7. **Compiles to SQL** - Every request becomes a single optimized query

No resolvers. No ORM. No N+1 queries. Just point and query.

## CodeSQL: Query Source Code Like a Database

CodeSQL is a managed source kind for source trees. Configure a source folder and GraphJin creates a SQLite cache under `config/codesql/`, indexes it with tree-sitter, and updates it on restart. In development it also watches for changes while the service runs; in production live watching is disabled.

```yaml
sources:
  - name: app
    kind: sql
    type: postgres
    connection_string: postgres://app:secret@db/app
    default: true

  - name: code
    kind: codesql
    path: /srv/app
    infer_db_refs: true

  - name: graphjin
    kind: graphjin
    metadata: true

tables:
  - name: users
    source: app

  - name: gj_code
    source: code
    read_only: true
```

GraphJin exposes CodeSQL through one ordinary GraphQL root, `gj_code`. Use `kind` to select files, symbols, references, imports, database references, docs, parse errors, change sets, and locks:

```graphql
query {
  gj_code(where: { kind: { eq: "symbol" }, name: { iregex: "handler|resolver" } }, limit: 20) {
    name
    symbol_kind
    language
    start_row
    path
    hash
  }
}
```

With a `kind: graphjin` source, GraphJin creates a read-only system graph named `graphjin` by default. Schema, catalog, entrypoint, capability, workflow, and system metadata are catalog items in `gj_catalog`; table and column metadata are selected by `kind`. When one CodeSQL source is active, GraphJin links catalog items to code references automatically:

```graphql
query {
  gj_catalog(where: { kind: { eq: "column" }, table_name: { eq: "users" }, column_name: { eq: "email" } }) {
    database_name
    table_name
    column_name
    gj_code {
      kind
      ref_kind
      path
      symbol_id
    }
  }
}
```

This is where the model gets genuinely powerful: the same agent can inspect production data systems and the code that operates them. It can ask, "which handlers touch customer invoices?", "what tables do these workflows depend on?", or "show me the imports and call sites near this data path" without switching tools or inventing a new backend.

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

**Analytics directives:**
```graphql
{
  orders {
    account_id
    month
    total
    running_total: total @running(aggregate: sum, by: "account_id", orderBy: { month: asc })
    moving_avg_total: total @moving(aggregate: avg, rows: 6, by: "account_id", orderBy: { month: asc })
    previous_total: total @previous(by: "account_id", orderBy: { month: asc })
    rank_by_total: total @rank(by: "account_id", order: desc)
  }
}
```
Use analytics directives when each original row should remain visible while adding report metrics such as running totals, moving averages, previous/next values, first/last values, and rank within a group. Ordinary one-row-per-group summaries still use `distinct` plus aggregate fields. Supported SQL databases validate analytics support at compile time; MongoDB and known-old database versions return clear errors.

**Mutations:**
```graphql
mutation {
  products(insert: { name: "New Product", price: 29.99 }) { id }
}
```

PostgreSQL and SQLite also support an update-free insert conflict policy for a single row. GraphJin infers one supplied unique key and returns the stored row unchanged on conflict:

```graphql
mutation {
  users(insert: { email: "ada@example.com", full_name: "Ada" }, on_conflict: get) {
    id email full_name
  }
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

Get live updates when your data changes. For databases with batching support, GraphJin handles thousands of concurrent subscribers with a single database query - not one per subscriber.

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

Dialects that do not implement GraphJin's batched polling path are listed as unsupported in the database support matrix.

Subscribe over **WebSockets** (`graphql-ws` / `graphql-transport-ws` subprotocols) or **Server-Sent Events** — set `Accept: text/event-stream` on a `POST /api/v1/graphql` request and GraphJin streams `event: next` frames for each result, terminated by `event: complete`. Works from Node.js, Go, or any browser `EventSource` / WebSocket client.

## Filesystem Tables (Local, S3, GCS)

Object stores show up as ordinary tables in your GraphQL schema. Declare them in config and they get the same query surface as a database table — no per-storage GraphQL plumbing on your side.

```yaml
sources:
  - name: avatars
    kind: filesystem
    backend: s3
    bucket: my-bucket
    prefix: avatars/
    region: us-east-1
    presign_ttl: 15m

  - name: invoices
    kind: filesystem
    backend: gcs
    bucket: invoices
    prefix: 2026/

  - name: uploads_local
    kind: filesystem
    backend: local
    root: /var/lib/graphjin/uploads

tables:
  - name: avatars
    source: avatars
    read_only: true

  - name: invoices
    source: invoices
    read_only: true

  - name: uploads_local
    source: uploads_local
```

Every filesystem table exposes the same columns regardless of backend:

```graphql
{ avatars(
    where: { key: { like: "users/%" } }
    order_by: { key: asc }
    limit: 50
  ) {
    key size content_type modified_at url
  }
}

{ avatars(id: "users/42.png") {
    key size url data    # data is base64 because the field was selected
  }
}
```

The legacy `prefix`, `key`, and `inline_data` arguments remain accepted, but new callers should use the normal GraphJin read surface: `id`, `where`, `order_by`, `limit`, `offset`, `first`, `last`, `after`, and `before`.
For cursor pagination, request the standard root cursor field, e.g. `avatars_cursor`, and pass it back through `after: $cursor`.

`url` is a presigned GET URL by default (15 min, configurable per table). Auth follows the standard credential chain: AWS env / `~/.aws` / IRSA / EC2 IMDS for S3, Application Default Credentials for GCS — never embedded in GraphJin config.

Slim builds drop SDK weight: `-tags no_s3` or `-tags no_gcs` excludes either backend. Custom backends register through `core.OptionSetFilesystemBackend(name, factory)` — same SDK GraphJin uses for the built-ins.

## File Uploads

The GraphQL endpoint accepts multipart bodies per the [graphql-multipart-request-spec](https://github.com/jaydenseric/graphql-multipart-request-spec). Files can be inlined as base64 (default) or streamed straight to a filesystem table:

```yaml
uploads:
  enabled: true
  storage: avatars               # name of a filesystems[] entry; omit to inline as base64
  storage_key_prefix: "{date}/"  # {date} → YYYY/MM/DD
  max_size: 25_000_000
  allowed_mime: ["image/*", "application/pdf"]
```

When `storage` is set, the file body is written to the backend and the GraphQL variable becomes a stable reference — mutations persist this directly into a JSONB column:

```json
{ "key": "2026/05/08/abc123.png",
  "url":  "https://s3.../...?presigned",
  "size": 12345,
  "content_type": "image/png" }
```

When `storage` is empty the variable carries the bytes inline as `{filename, content_type, size, data}` (base64) — useful for small uploads going straight into `bytea`.

## Apollo Federation v2

GraphJin can register as a federation subgraph so it composes with other services behind Apollo Router / Cosmo / Hive Gateway:

```yaml
federation:
  enabled: true
  version: "v2.5"
  keys:
    users: ["id"]                  # auto-derived from PKs by default
    orders: ["id", "tenant_id"]    # composite keys via override
  shareable: ["Tag.name"]          # field-level @shareable
  inaccessible: ["Users.encrypted_password"]
```

`_service { sdl }` returns a federation-flavoured SDL with `@link`, `@key`, `@shareable`, `@inaccessible`, `@tag`, `_Service`, and `_Entity`. Composition succeeds out of the box; `_entities` resolution is on the roadmap (the engine returns a clear error today, so gateways see the gap rather than silent failures).

## HTTP API Routes

`graphjin serve` exposes everything under a single host/port. All routes go through the configured auth handler unless noted.

| Route | Methods | Purpose |
|---|---|---|
| `/api/v1/graphql` | `GET`, `POST` | GraphQL queries and mutations. Subscriptions if the request is a WebSocket upgrade or carries `Accept: text/event-stream` (SSE). |
| `/api/v1/rest/<name>` | `GET`, `POST` | Run a saved/persisted query by name. Variables go in `?variables=…` (GET) or the JSON body (POST). |
| `/api/v1/workflows/<name>` | `GET`, `POST` | Legacy workflow execution endpoint. In source mode it is registered only when `mcp.legacy_discovery: true`; use `gj_workflow_execution(insert)` through GraphQL otherwise. |
| `/api/v1/openapi.json` | `GET` | OpenAPI 3 spec generated from your saved REST queries. |
| `/api/v1/mcp` | `POST` | MCP (Model Context Protocol) HTTP transport — Streamable HTTP, stateless. |
| `/api/v1/mcp/message` | `POST` | Legacy MCP message route for older local proxy/client integrations. New clients should use `/api/v1/mcp`. |
| `/api/v1/agent` | `POST` | Server-side agent: send one instruction, get a typed evidence-backed answer (only when `agent.enabled`). See [Server-Side Agent](#server-side-agent). |
| `/.well-known/oauth-protected-resource[/api/v1/mcp]` | `GET` | MCP OAuth protected-resource metadata (only when `mcp.oauth.enabled`). |
| `/.well-known/oauth-authorization-server` | `GET` | Built-in MCP OAuth authorization-server metadata (only when `mcp.oauth.enabled`). |
| `/api/v1/oauth/register` | `POST` | Built-in MCP OAuth dynamic client registration (only in builtin mode). |
| `/api/v1/oauth/authorize` | `GET` | Built-in MCP OAuth authorization-code + PKCE start (only in builtin mode). |
| `/api/v1/oauth/token` | `POST` | Built-in MCP OAuth token exchange/refresh (only in builtin mode). |
| `/api/v1/discovery` | `GET` | Legacy discovery document. In source mode it is registered only when `mcp.legacy_discovery: true`; use catalog GraphQL roots otherwise. |
| `/api/v1/discovery/<section>` | `GET` | Legacy discovery drill-down (e.g. `tables`, `insights`), gated the same way as `/api/v1/discovery`. |
| `/api/v1/auth/device` | `POST` | OIDC device-flow start (only if `auth_login.enabled`). |
| `/api/v1/auth/device/token` | `POST` | OIDC device-flow poll. |
| `/api/v1/auth/login` | `GET` | OIDC login redirect. |
| `/api/v1/auth/callback` | `GET` | OIDC callback. |
| `/health` | `GET` | Liveness probe. **No auth.** |
| `/` | `GET` | Built-in Web UI. Defaults on in `dev` and `agentic` modes, off in `prod`; set `web_ui` explicitly to override. The UI uses `/api/v1/graphql` and GraphJin system roots such as `gj_catalog`, `gj_security`, `gj_runtime`, `gj_code`, and `gj_config`; there is no separate admin REST API. |

**Mode flags that change which routes are live:**
- `mcp.disable: true` — removes `/api/v1/mcp` and `/api/v1/mcp/message`.
- `mcp.only: true` — keeps only `/health` and `/api/v1/mcp*`. Legacy `/api/v1/workflows/*` and `/api/v1/discovery*` remain only when `mcp.legacy_discovery: true`.
- Source mode (`sources:` present) disables legacy `/api/v1/workflows/*` and `/api/v1/discovery*` unless `mcp.legacy_discovery: true`.
- `web_ui: false` — drops `/` even in dev or agentic mode.

## MCP Tools

GraphJin exposes a catalog-first MCP surface that guides AI models to discover before acting. The surface is caller-aware: `tools/list`, `graphql_help`, and `query_catalog` reflect the caller's visible tools, `gj_*` roots, catalog capabilities, and blocked/admin-only actions.

- Start goal-driven work with `query_catalog(search: "<user instruction>")` when it is listed.
- Inspect the best row with `query_catalog(id: "...")` before writing queries, choosing relationships, or using GraphJin-specific syntax.
- Use `config_recipe` rows for operator work; they spell out preflight, preview/apply, unsupported apply, verification, stop conditions, and forbidden patterns.
- Act through governed GraphQL roots such as `gj_workflow_execution(insert)`, `gj_workflow(insert/update/delete)`, and `gj_config(id: "current", update: ...)` only when policy exposes them.
- In source mode, `gj_config` writes must run `mode: "preview"` with `expected_catalog_revision`, then resend the exact same payload with `mode: "apply"` and `preview_id`.
- Source access and GraphJin root changes should use `source_patches` by exact source name instead of rewriting the full `sources` array.
- Watches are managed through `gj_watch` / `gj_watch_event` and REST wrappers (`/api/v1/watches`, `/api/v1/watch-events/unseen`). MCP clients can subscribe to `graphjin://watch-events/unseen` for caller-scoped unseen-event notifications; unsubscribing does not pause or delete durable watches.
- Use `/api/v1/watches/cleanup-preview` before `/api/v1/watches/cleanup-apply`. Expired ephemeral watches can be expired automatically, but durable watch deletion is always explicit.
- Legacy discovery tools are migration shims and are disabled unless `mcp.legacy_discovery: true`.

Schema reloads, schema changes, where-clause validation, and query repair remain MCP action tools.

For teams building MCP agents, internal copilots, workflow agents, or enterprise automation, see [AGENTIC.md](AGENTIC.md). It explains the catalog-first agent loop in detail: discover, inspect, validate, act, observe, and refine.

Key discovery tools, when listed for the caller:
- `graphql_help` to choose a catalog-backed topic route when the task is broad or unclear
- `query_catalog` to search schema, relationship, workflow, language, `config_recipe`, config, policy, capability, and query-pattern items. Use `search` for ranked text discovery and `where` for exact filters.
- `query_catalog(id: "...")` to inspect evidence, examples, details, safety notes, and graph edges for one row
- `validate_where_clause` to validate filters before execution

For JS orchestration, use:
- `query_catalog` with `where: { kind: { eq: "workflow" } }` to discover reusable workflows
- `graphql_help(for: "workflow_runtime")` and workflow catalog rows to discover exactly which globals/functions are available inside workflow scripts
- `gj_workflow_execution(insert: { workflow_name: "...", variables: {...} })` to run `./workflows/<name>.js` through GraphQL. This is mutation-only and returns an ephemeral result row; it does not store run history. Mark the workflows source or `gj_workflow_execution` table `read_only` to block it. The `execute_workflow` MCP compatibility tool is available only when `mcp.legacy_discovery: true` and `mcp.allow_workflow_execution: true`.

Legacy prompts such as `write_query` and `fix_query_error` are available only when the caller's MCP surface lists them; in source mode their guidance lives in `graphql_help`, `query_catalog`, and GraphJin repair hints.

## Server-Side Agent

Optional: instead of your client chaining `query_catalog` → `validate_where_clause` → `execute_saved_query`, let GraphJin run the catalog-first discovery loop for you. When enabled, GraphJin exposes one MCP tool `ask_graphjin_agent` and one REST endpoint `POST /api/v1/agent` that take a single instruction and return a typed, evidence-backed answer (`status`, `answer`, `data`, `evidence`, `actions`, `next`).

- **Enable** by loading `agentic.yml` (`GO_ENV=agentic`) with `agent.enabled: true` plus a provider, model, and `api_key_env`.
- **Execution control:** there are no per-request agent modes. `agent.read_only: true` forces the server-side agent to reject mutations, including saved-query mutations; otherwise core roles, row-level security, source/table `read_only`, and Go protocol guards decide what can run.
- **Caller-scoped:** the agent runs as the caller, so core roles + row-level security enforce access. The caller's role only changes which *guidance* the agent follows, never what it can read or write.
- **Grounded:** Go protocol guards keep every answer backed by real catalog/validation/execution evidence and downgrade to `blocked` (with evidence) when a step is skipped.
- **Machine-actionable refusals:** blocked responses carry a structured `refusal` (code, reasons, unblock steps, `policy_final`/`retryable`) so a calling agent can course-correct in one step instead of guessing.
- **Model sampling:** with `agent.sampling: auto` (or `require`), the agent borrows the calling MCP client's model via MCP sampling — no server-side model key needed; caller identity and permissions are unchanged.

It is an RLM loop — the model writes JavaScript that calls the discovery tools, and the typed result is parsed from `key: value` output. So it needs a model that is good at **code generation**, not provider tool-calling or structured-output modes; any OpenAI-compatible endpoint works via `agent.base_url`. See [AGENTIC.md](AGENTIC.md#server-side-agent) and [CONFIG.md](CONFIG.md#agent-configuration).

## JS Workflows (GraphQL + REST)

Workflows let an LLM run multi-step logic in JavaScript while still using GraphJin MCP tools for DB-aware operations.

Create a file in `./workflows`, for example `./workflows/customer_insights.js`:

```js
function main(input) {
  const tables = gj.tools.queryCatalog({
    where: { kind: { eq: "table" } }
  }).cards;
  const top = gj.tools.executeSavedQuery({
    name: "top_customers",
    variables: { limit: input.limit || 5 }
  });
  return { tables, top };
}
```

### Run via GraphQL

```graphql
mutation {
  gj_workflow_execution(insert: {
    workflow_name: "customer_insights"
    variables: { limit: 5 }
  }) {
    status
    result_json
    error
  }
}
```

Legacy MCP clients can call `execute_workflow` only when `mcp.legacy_discovery: true`.

### Run via REST

This legacy HTTP endpoint is available in legacy database mode, or in source mode only when `mcp.legacy_discovery: true`.

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
> Connected successfully! Let me discover the catalog surface.
>
> *calls `query_catalog` with `where: {kind: {eq: "table"}}`*
>
> Your database has these tables: `users`, `orders`, `products`, `reviews`. Want me to write a query?

> **You:** Show me users with their orders.

> **Claude:** *calls `query_catalog` with `search: "join users orders", where: {kind: {eq: "relationship"}}`, then `query_catalog(id: "<relationship_id>")`, then `execute_graphql` if raw execution is listed*
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
> Connected! Let me explore the catalog.
>
> *calls `query_catalog` with `where: {kind: {eq: "table"}}`*
>
> Tables found: `customers`, `products`, `orders`, `order_items`, `categories`. Let me check how they relate.
>
> *calls `query_catalog` with `search: "join orders customers order_items products", where: {kind: {eq: "relationship"}}`, then `query_catalog(id: "<relationship_id>")` for the relevant relationship items*
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
> *calls `query_catalog` with `where: {kind: {eq: "table"}}`*
>
> Tables: `tasks`, `projects`, `tags`, `task_tags`. Want to query something?

> **You:** Show me all tasks with their project name, sorted by due date.

> **Claude:** *calls `query_catalog` with `search: "join tasks projects", where: {kind: {eq: "relationship"}}`, then `query_catalog(id: "<relationship_id>")`, then `execute_graphql` if raw execution is listed*
> ```graphql
> { tasks(order_by: {due_date: asc}) { id title due_date completed project { name } } }
> ```
> Here are your 12 tasks sorted by due date, each showing the parent project name.

## Database Support

| Database | Queries | Mutations | Subscriptions | Full-Text | GIS | Schema DDL |
|----------|---------|-----------|---------------|-----------|-----|------------|
| PostgreSQL | Yes | Yes | Yes | Yes | PostGIS | Yes |
| MySQL | Yes | Yes | Yes | Yes | 8.0+ | Yes |
| MariaDB | Yes | Yes | Yes | Yes | Yes | Yes |
| MSSQL | Yes | Yes | Yes | No | Yes | Yes |
| Oracle | Yes | Yes | Yes | No | Yes | Yes |
| SQLite | Yes | Yes | Yes | FTS5 | SpatiaLite | Yes |
| MongoDB | Yes | Yes | Yes | Yes | Yes | No |
| Cassandra / Keyspaces | CQL-native | Single-table PK | Partition-bound polling | No | No | No |
| Snowflake | Yes | Yes | No | No | No | Yes |
| Redshift | Experimental queries | Experimental PK writes | Experimental batched polling | Limited search | Limited | Experimental basic DDL |
| BigQuery | Yes (experimental) | No | No | No | No | No |
| CockroachDB | Yes | Yes | Yes | Yes | No | No |

Also works with AWS Aurora/RDS, Google Cloud SQL, and YugabyteDB. Snowflake supports key pair (JWT) authentication and SHOW-based catalog discovery/paging. Redshift support is experimental: queries/discovery, single-table primary-key writes, batched polling subscriptions, limited `ILIKE` search over configured `full_text` columns, limited spatial filters, and basic generated DDL. Redshift subscriptions are polling-based warehouse queries, not native change streams. BigQuery support is experimental and query-focused. Cassandra / Keyspaces support CQL-native queries, partition-bound polling subscriptions, and single-table primary-key writes; aggregates, full scans, OR-style cross-partition filters, full-text, and GIS remain outside the generic surface.

## Production Security

**Query allow-lists** - In production, only saved queries can run. AI models call `execute_saved_query` with pre-approved queries. No arbitrary SQL injection possible.

**Source-mode access** - New multi-user deployments should use `sources:` with request-wide `identity` and source-level access defaults. GraphJin compiles those defaults into the existing qcode/SQL enforcement path, so account filters and trusted mutation presets are enforced by the generated database query.

```yaml
identity:
  user_id_claim: sub
  role_claims: [role, roles]
  namespace_claim: account_id

sources:
  - name: app
    kind: database
    access:
      read: account
      write: blocked
      delete: blocked
      namespace_column: account_id
```

See [SECURITY.md](SECURITY.md) for the security model and [Source Mode Migration](docs/SOURCE-MODE-MIGRATION.md) for legacy `roles[].tables` migration steps. In source mode, user-written `roles[].tables` rules are rejected intentionally.

**JWT authentication** - Supports Auth0, Firebase, JWKS endpoints.

**Response caching** - Redis with in-memory fallback. Automatic cache invalidation on mutations. **Stale-while-revalidate** support: serve cached responses immediately while a background worker refreshes the entry — concurrent refreshes for the same key are deduplicated via singleflight, and the worker pool is bounded so a thundering herd can't spawn unbounded goroutines.

```yaml
caching:
  ttl: 3600          # hard expiry in seconds
  fresh_ttl: 300     # soft expiry — entries past this trigger SWR refresh
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
- [Feature Reference](FEATURES.md)
- [Go Examples](https://pkg.go.dev/github.com/dosco/graphjin/core#pkg-examples)

## Get in Touch

[Twitter @dosco](https://twitter.com/dosco) | [Discord](https://discord.gg/6pSWCTZ)

## License

[Apache Public License 2.0](https://opensource.org/licenses/Apache-2.0)
