# GraphJin Configuration Reference

This document provides a comprehensive reference for all GraphJin configuration options. GraphJin uses YAML or JSON configuration files that can be customized for different environments.

## How configuration works

This file is the exhaustive field reference. If you want the mental model first — what the config file is, how values are resolved across layers, and every way to change it without memorizing keys — read [How Configuration Works](https://graphjin.com/configure/how-it-works/). In short:

- **One file, two halves.** The file configures both the query **engine** (`sources`, `tables`, `roles`, …) and the **server** around it (`auth`, `rate_limiter`, `redis`, `caching`, `agent`, …). Engine changes reload in place; most server changes need a restart.
- **Layered overrides** (later wins): built-in defaults → inherited parent (`inherits:`) → your file → `GJ_*`/`SJ_*` environment variables → dev-mode auto-enables.
- **Many ways to change it**, not just editing YAML:
  - Editor autocomplete via a `# yaml-language-server: $schema=./config.schema.json` modeline (scaffolded automatically).
  - The `graphjin config` CLI — `get`, `set`, `unset`, `explain`, `validate`, `schema`, `docs` — offline and comment-preserving.
  - The built-in agent (natural-language config recipes) and, in dev, the `get_current_config` / `validate_config` / `update_current_config` MCP tools.
  - The `gj_config` GraphQL control plane (reflects both halves, secrets redacted).

Run `graphjin config docs` (or `graphjin serve new`) for the annotated example templates that document every option inline.

## Table of Contents

- [Introduction](#introduction)
- [What Changed: Database vs Sources](#what-changed-database-vs-sources)
- [Quick Start](#quick-start)
- [Sources Mode](#sources-mode)
- [Service Configuration](#service-configuration)
- [Database Configuration](#database-configuration)
- [Metadata Graph Configuration](#metadata-graph-configuration)
- [Authentication Configuration](#authentication-configuration)
- [Core Compiler Configuration](#core-compiler-configuration)
- [Security & Admin Configuration](#security--admin-configuration)
- [Rate Limiting](#rate-limiting)
- [MCP Configuration](#mcp-configuration)
- [Agent Configuration](#agent-configuration)
- [Task Configuration](#task-configuration)
- [Redis Configuration](#redis-configuration)
- [Discovery Cache and Semantic Catalog Search](#discovery-cache-and-semantic-catalog-search)
- [Caching Configuration](#caching-configuration)
- [Uploads Configuration](#uploads-configuration)
- [Filesystems Configuration](#filesystems-configuration)
- [Federation Configuration](#federation-configuration)
- [Schema Configuration](#schema-configuration)
- [OpenAPI Integration](#openapi-integration)
- [Role-Based Access Control](#role-based-access-control)
- [Multi-Database Configuration](#multi-database-configuration)
- [Environment Variables Reference](#environment-variables-reference)
- [Complete Examples](#complete-examples)

---

## Introduction

### Config File Formats

GraphJin supports both YAML and JSON configuration files:
- `config/dev.yml` - Development configuration
- `config/prod.yml` - Production configuration
- `config/agentic.yml` - Agentic deployment configuration
- `config/dev.json` - JSON format alternative

### Environment-Based Config Selection

GraphJin automatically selects the configuration file based on the `GO_ENV` environment variable:

| GO_ENV Value | Config File |
|-------------|-------------|
| `development`, `dev`, or empty | `dev.yml` |
| `production`, `prod` | `prod.yml` |
| `agentic`, `agent` | `agentic.yml` |
| `staging`, `stage` | `stage.yml` |
| `testing`, `test` | `test.yml` |
| Other values | `{value}.yml` |

The selected config name also supplies the default `mode`: `dev.yml` defaults to `mode: dev`, `prod.yml` defaults to `mode: prod`, and `agentic.yml` defaults to `mode: agentic`. `agentic.yml` is required for `GO_ENV=agentic`; use `inherits: prod` inside `agentic.yml` when an agentic deployment should reuse production settings.

### Config Inheritance

Use the `inherits` field to inherit configuration from another file, allowing you to override only specific values:

```yaml
# prod.yml - inherits all settings from dev.yml and overrides specific ones
inherits: dev

mode: prod
log_level: "warn"
web_ui: false
```

### Environment Variable Overrides

Configuration values can be overridden using environment variables with the `GJ_` or `SJ_` prefix:

```bash
# Override database host
export GJ_DATABASE_HOST=mydb.example.com

# Override database port
export GJ_DATABASE_PORT=5433
```

---

## What Changed: Database vs Sources

Use the legacy `database:` / `databases:` shape only for database-only configs that do not define `sources:`. Once `sources:` is present, every graph provider is declared as a source and tables point at `tables[].source` instead of `tables[].database`.

**Old / legacy database-only config:**

```yaml
databases:
  app:
    type: postgres
    connection_string: ${APP_DATABASE_URL}

  analytics:
    type: snowflake
    connection_string: ${ANALYTICS_DSN}

tables:
  - name: users
    database: app

  - name: events
    database: analytics
```

**New / sources config, shown as `agentic.yml`:**

```yaml
sources:
  - name: app
    kind: database
    type: postgres
    connection_string: ${APP_DATABASE_URL}
    default: true

  - name: analytics
    kind: database
    type: snowflake
    connection_string: ${ANALYTICS_DSN}

tables:
  - name: users
    source: app

  - name: events
    source: analytics
```

Canonical source kinds are `database`, `code`, `file`, and `api`. System roots and workflows are built-in features, not sources. Old implementation names such as `sql`, `codesql`, `filesystem`, `openapi`, `graphjin`, `workflow`, and `workflows` are rejected with errors that name the replacement.

Use `mode: agentic` or `GO_ENV=agentic` for deployments where authenticated company users interact with GraphJin through an approved agent. Agentic mode applies production-oriented source and control-plane defaults, enables safe discovery and approved workflow execution by default, and keeps detailed security/config/workflow-code surfaces blocked unless explicitly enabled with feature capabilities. Existing `production: true` configs still work and imply `mode: prod` when `mode` is omitted.

---

## Quick Start

### Minimal Development Configuration

```yaml
app_name: "My App Development"
host_port: 0.0.0.0:8080
web_ui: true
production: false

database:
  type: postgres
  host: localhost
  port: 5432
  dbname: myapp_development
  user: postgres
  password: ""
```

### Minimal Production Configuration

```yaml
inherits: dev

app_name: "My App Production"
mode: prod
web_ui: false
log_level: "warn"
log_format: "json"
auth_fail_block: true

database:
  dbname: myapp_production
```

### MCP Client Connection Quick Reference

For local development, the GraphJin helper is the easiest way to add GraphJin to
Codex or Claude. It normalizes the URL to `/api/v1/mcp`, probes the server, and
uses the right native URL or local proxy config:

```bash
graphjin mcp add codex
graphjin mcp add claude
```

Native MCP clients can also connect directly to GraphJin's Streamable HTTP
endpoint:

```bash
codex mcp add graphjin --url http://localhost:8080/api/v1/mcp
claude mcp add --transport http graphjin http://localhost:8080/api/v1/mcp
```

Hosted GraphJin should use HTTPS and can advertise MCP OAuth metadata when
`mcp.oauth.enabled: true`:

```bash
codex mcp add graphjin --url https://graphjin.example.com/api/v1/mcp
claude mcp add --transport http graphjin https://graphjin.example.com/api/v1/mcp
```

```yaml
auth:
  type: jwt
  jwt:
    secret: ${GRAPHJIN_JWT_SECRET}

auth_login:
  enabled: true
  oidc:
    issuer_url: https://accounts.google.com
    client_id: ${GRAPHJIN_OIDC_CLIENT_ID}
    client_secret: ${GRAPHJIN_OIDC_CLIENT_SECRET}

mcp:
  oauth:
    enabled: true
    mode: builtin
    scopes: ["mcp"]
```

Claude's SSE transport is for legacy/custom MCP servers, not GraphJin's
`/api/v1/mcp` endpoint:

```bash
claude mcp add --transport sse <name> <url>
claude mcp add --transport sse private-api https://api.company.com/sse \
  --header "X-API-Key: your-key-here"
```

---

## Sources Mode

`sources:` is the canonical config for external providers only: databases, code indexes, file/object-store tables, and OpenAPI-backed APIs.

If `sources:` is absent, GraphJin runs in legacy database-only mode. Existing `database`, `databases`, and `tables[].database` SQL configs continue to work there. Legacy mode rejects CodeSQL, top-level `filesystems`, and top-level `openapi` / `openapi_specs_dir`; move external providers to `sources`. Replace old metadata/catalog overrides with `system.capabilities.catalog.read`.

If `sources:` is present, source mode is active. In source mode:

- Configure databases and code indexes through `sources`; do not use legacy `database` or `databases`.
- Every `tables[]` entry must set `source`, not `database`.
- System roots and workflows use mode defaults and require no source entries.
- Use optional top-level `system:` and `workflows:` sections only to override those defaults.
- Use `sources[].capabilities` to enable or block source surfaces; valid capability keys depend on the source kind.
- `tables[].read_only` blocks normal and managed mutations for that root.
- `relationships: [{ from, to, as? }]` adds graph edges; cardinality and reverse traversal are inferred.
- Legacy MCP discovery/action tools and legacy HTTP helper endpoints (`/api/v1/discovery*`, `/api/v1/workflows/*`) are disabled by default; set `mcp.legacy_discovery: true` as an escape hatch.

Direct migration:

```yaml
# Old (rejected)
sources:
  - name: graphjin
    kind: graphjin
    capabilities:
      catalog.read: true
      config.write: false
    access:
      roots:
        gj_catalog: public
        gj_config: admin
  - name: workflows
    kind: workflow
    path: ./workflows
    runtime: javascript
    capabilities:
      workflow.execute: true

# New
system:
  capabilities:
    catalog.read: true
    config.write: false
  root_access:
    gj_catalog: public
    gj_config: admin
workflows:
  path: ./workflows
  capabilities:
    execute: true
```

Names are not reserved. An external application source may be named `graphjin`, `workflows`, or `__graphjin_artifacts` as long as its `kind` is one of the four external kinds.

```yaml
sources:
  - name: app
    kind: database
    type: postgres
    connection_string: ${APP_DATABASE_URL}
    default: true

  - name: code
    kind: code
    path: /srv/app
    infer_db_refs: true

  - name: avatars
    kind: file
    backend: s3
    bucket: app-assets
    prefix: avatars/

  - name: upstream
    kind: api
    specs_dir: ./config/specs
    specs:
      interaction_studio:
        base_url: https://${IS_ACCOUNT}.example.com/api
        auth: {}

tables:
  - name: users
    source: app

  - name: gj_code
    source: code
    read_only: true

  - name: avatars
    source: avatars

relationships:
  - from: gj_catalog.code_refs_id
    to: code:gj_code.db_object_id
    as: gj_code

system:
  root_access:
    gj_catalog: public
    gj_config: admin

workflows:
  path: ./workflows
```

### Source Capabilities

Each external source can define a `capabilities` map. A value of `true` enables the capability for authenticated default users, `false` disables it, and an omitted key uses the secure default for the active mode. Anonymous access is never granted by source capabilities. `read_only: true` still hard-blocks writes, deletes, and watches where applicable.

Deployment modes:

| Mode | Intended audience | Default posture |
|------|-------------------|-----------------|
| `dev` | Developers | Broad catalog, config, security, workflow-code, raw GraphQL, schema, and dev-tool access for local development |
| `prod` | Production applications | Production protections and allow-list discipline; the agentic system surface does not mount |
| `agentic` | Authenticated company end users using an approved agent | Production protections plus `catalog.read` and approved `workflow.execute`; detailed `security.read`, `config.read`, `workflow.read`, writes, raw GraphQL, schema writes, dev tools, and legacy discovery stay blocked unless explicitly enabled |

| Source kind | Capability keys |
|-------------|-----------------|
| `database` | `data.read`, `data.write`, `schema.read`, `schema.write` |
| `code` | `code.search`, `code.read`, `code.write`, `code.watch`, `code.infer_db_refs` |
| `file` | `files.list`, `files.read`, `files.write`, `files.delete`, `files.watch` |
| `api` | `api.read`, `api.write` |
System capabilities live under `system.capabilities`: `catalog.read`, `security.read`, `config.read`, `config.write`, `runtime.read`, `raw_graphql.query`, `raw_graphql.mutate`, `schema.reload`, `schema.write`, `dev_tools.read`, and `legacy_discovery.read`.

Workflow capabilities live under `workflows.capabilities`: `execute`, `read`, and `write`.

```yaml
system:
  capabilities:
    catalog.read: true
    security.read: true
    config.read: false

workflows:
  path: ./workflows
  capabilities:
    execute: true
    read: false
    write: false
```

---

## Service Configuration

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `app_name` | string | - | Application name used in logs and debug messages |
| `host_port` | string | `0.0.0.0:8080` | Host and port the service runs on |
| `host` | string | - | Host to run the service on (alternative to host_port) |
| `port` | string | - | Port to run the service on (alternative to host_port) |
| `production` | boolean | `false` | Legacy production switch; prefer `mode: prod` or `mode: agentic` in new configs |
| `web_ui` | boolean | `false` | Enable the GraphJin web UI |
| `log_level` | string | `info` | Logging level: `debug`, `error`, `warn`, `info` |
| `log_format` | string | `auto` | Log format: `auto`, `json`, `simple` |
| `http_compress` | boolean | `true` | Enable HTTP gzip compression |
| `server_timing` | boolean | `true` | Enable Server-Timing HTTP header |
| `enable_tracing` | boolean | `false` | Enable OpenTrace request tracing |
| `auth_fail_block` | boolean | `false` | Return HTTP 401 on auth failure |
| `reload_on_config_change` | boolean | - | Reload service on config file changes |
| `cors_allowed_origins` | []string | - | CORS allowed origins (use `["*"]` for all) |
| `cors_allowed_headers` | []string | - | CORS allowed headers |
| `cors_debug` | boolean | `false` | Enable CORS debug logging |
| `cache_control` | string | - | HTTP Cache-Control header value |

### Log Format Behavior

| log_format | Development Mode | Production Mode |
|------------|------------------|-----------------|
| `auto` | Colored console | JSON |
| `json` | JSON | JSON |
| `simple` | Colored console | Colored console |

### Example

```yaml
app_name: "My GraphQL API"
host_port: 0.0.0.0:8080
production: false
web_ui: true
log_level: "debug"
log_format: "auto"
http_compress: true
server_timing: true
enable_tracing: true
auth_fail_block: false
reload_on_config_change: true

cors_allowed_origins: ["https://myapp.com", "https://*.myapp.com"]
cors_allowed_headers: ["Authorization", "Content-Type"]
cors_debug: false

cache_control: "public, max-age=300, s-maxage=600"
```

---

## Database Configuration

### Connection Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `type` | string | `postgres` | Database type |
| `connection_string` | string | - | Full connection string (alternative to individual params) |
| `host` | string | `localhost` | Database host |
| `port` | integer | `5432` | Database port |
| `dbname` | string | - | Database name |
| `user` | string | `postgres` | Database user |
| `password` | string | - | Database password |
| `schema` | string | `public` | Database schema (PostgreSQL) |

### Connection Pool Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `pool_size` | integer | `10` | Size of the connection pool |
| `max_connections` | integer | - | Maximum number of active connections |
| `max_connection_idle_time` | duration | - | Max idle time before closing connection |
| `max_connection_life_time` | duration | - | Max lifetime of a connection |
| `ping_timeout` | duration | - | Timeout for health check pings |

### TLS Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enable_tls` | boolean | `false` | Enable TLS encrypted connection |
| `server_name` | string | - | TLS server name (e.g., GCP project:instance) |
| `server_cert` | string | - | Server certificate (file path or PEM content) |
| `client_cert` | string | - | Client certificate (file path or PEM content) |
| `client_key` | string | - | Client key (file path or PEM content) |

### Supported Database Types

| Type | Single DB | Multi-DB | Notes |
|------|-----------|----------|-------|
| `postgres` | Yes | Yes | Default database type |
| `mysql` | Yes | Yes | Use with MariaDB as well |
| `mariadb` | Yes | Yes | Alias for MySQL driver |
| `sqlite` | Yes | Yes | Set `host` to file path |
| `oracle` | Yes | Yes | Oracle Database |
| `mssql` | No | Yes | Microsoft SQL Server |
| `mongodb` | No | Yes | MongoDB (multi-db only) |
| `snowflake` | Yes | Yes | Requires `connection_string` |
| `clickhouse` | Yes | Yes | Columnar OLAP, reads-first. No foreign keys (declare relationships in config), async/best-effort mutations, single-table writes only |

CodeSQL is not a `database.type`; configure repository/code indexes as `sources[].kind: code`.

### Database Configuration Examples

#### PostgreSQL

```yaml
database:
  type: postgres
  host: localhost
  port: 5432
  dbname: myapp
  user: postgres
  password: secret
  schema: "public"
  pool_size: 15

  # Or use connection string
  # connection_string: postgres://user:password@localhost:5432/myapp?sslmode=disable
```

#### MySQL / MariaDB

```yaml
database:
  type: mysql
  host: localhost
  port: 3306
  dbname: myapp
  user: root
  password: secret

  # Or use connection string (recommended params included)
  # connection_string: user:password@tcp(localhost:3306)/myapp?multiStatements=true&parseTime=true&interpolateParams=true
```

#### SQLite

```yaml
database:
  type: sqlite
  # File-based database
  host: /path/to/database.db

  # Or in-memory with shared cache
  # connection_string: file:memdb?mode=memory&cache=shared&_busy_timeout=5000
```

#### Code Source (CodeSQL)

Code indexes are configured as `sources[].kind: code` and backed by CodeSQL internally. Set `path`; GraphJin creates a managed SQLite cache in `config/codesql/<source-name>-<source-root-hash>.sqlite` and reconciles new/changed/deleted files on startup. In development, it also watches the source tree while running; in production, live watching is disabled and the cache updates on restart.

```yaml
sources:
  - name: code
    kind: code
    path: /path/to/source
```

For mixed application-data and code graphs, declare both sources and attach tables to sources:

```yaml
sources:
  - name: app
    kind: database
    type: postgres
    connection_string: postgres://app:secret@db/app
    default: true

  - name: code
    kind: code
    path: /path/to/source
    infer_db_refs: true

tables:
  - name: users
    source: app

  - name: gj_code
    source: code
    read_only: true
```

At runtime GraphJin treats CodeSQL as `sqlite` with `read_only: true` and `analytics_mode: true`. The public graph exposes one root, `gj_code`, where `kind` selects files, symbols, references, imports, edges, database references, injections, docs, text chunks, parse errors, AST nodes, captures, index status, change sets, and locks. Raw CodeSQL storage tables stay internal by default.

`infer_db_refs` is CodeSQL-only and defaults to `true`. It records best-effort references from SQL files and strings, GraphQL files and strings, GraphJin config/allowlist files, migration references, and common struct/model tags.

#### Oracle

```yaml
database:
  type: oracle
  host: localhost
  port: 1521
  dbname: FREEPDB1
  user: myuser
  password: secret

  # Or use connection string
  # connection_string: oracle://user:password@localhost:1521/FREEPDB1
```

#### MS SQL Server

```yaml
database:
  type: mssql
  host: localhost
  port: 1433
  dbname: myapp
  user: sa
  password: YourStrong!Passw0rd

  # Or use connection string
  # connection_string: sqlserver://sa:YourStrong!Passw0rd@localhost:1433?database=myapp
```

#### MongoDB

```yaml
database:
  type: mongodb
  host: localhost
  port: 27017
  dbname: myapp
  # Note: MongoDB has no foreign keys; relationships must be configured explicitly
```

#### Snowflake

```yaml
database:
  type: snowflake
  # Snowflake requires a connection string (host/port fields are not used)
  connection_string: user@myaccount/mydb/public?warehouse=compute_wh&account=myaccount
```

##### Key Pair (JWT) Authentication

Snowflake supports key pair authentication using RSA-2048 private keys in PKCS#8 PEM format. The gosnowflake driver handles JWT generation, signing, and the 60-second token expiry internally.

```yaml
database:
  type: snowflake
  connection_string: user@myaccount/mydb/public?warehouse=compute_wh&account=myaccount

  # Path to PKCS#8 PEM-encoded RSA private key
  private_key_path: /path/to/rsa_key.p8
  # Or provide the PEM content inline
  # private_key_pem: "-----BEGIN PRIVATE KEY-----\n..."

  # Optional: passphrase if the key is encrypted
  # key_passphrase: my-passphrase
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `private_key_path` | string | - | File path to PKCS#8 PEM private key |
| `private_key_pem` | string | - | Inline PEM content (alternative to file path) |
| `key_passphrase` | string | - | Passphrase for encrypted private keys |

**Setup steps:**

1. Generate an RSA-2048 key pair in PKCS#8 format:
   ```bash
   openssl genrsa 2048 | openssl pkcs8 -topk8 -inform PEM -out rsa_key.p8 -nocrypt
   ```
2. Extract the public key:
   ```bash
   openssl rsa -in rsa_key.p8 -pubout -out rsa_key.pub
   ```
3. Register the public key on your Snowflake user:
   ```sql
   ALTER USER my_user SET RSA_PUBLIC_KEY='MIIBIjANBg...';
   ```

#### Redshift

```yaml
database:
  type: redshift
  # Redshift uses the PostgreSQL wire protocol. Use the standard connection
  # string form accepted by the configured SQL driver.
  connection_string: postgres://user:password@cluster.example.us-east-1.redshift.amazonaws.com:5439/dev?sslmode=require
```

Redshift support is experimental and Redshift-owned, not plain PostgreSQL compatibility. GraphJin uses Redshift-native catalog discovery, preferring `SHOW TABLES` and `SHOW COLUMNS` compatible metadata.

Supported Redshift v1 surfaces:
- Queries and schema discovery.
- Experimental single-table primary-key writes: insert, update, and delete.
- Experimental batched polling subscriptions.
- Experimental basic schema-diff DDL: create/drop table, add/drop column, informational PK/FK/unique constraints, `DISTSTYLE AUTO`, `ENCODE AUTO`, and sort keys.
- Limited search over configured `full_text` columns using `ILIKE` predicates.
- Limited GIS filters: `st_dwithin`, `st_within`, `st_contains`, and `st_intersects`.

Redshift limits:
- Subscriptions use GraphJin batched polling. They are not native Redshift change streams and can be expensive for large warehouse queries.
- Insert result selection requires a client-supplied primary key in v1; identity-only inserts are rejected.
- Upsert, nested mutations, connect/disconnect, writable CTEs, generated identity return, user-managed indexes, and search indexes are not supported.
- Redshift PK/FK/unique constraints are informational planner hints except for enforced `NOT NULL`.
- Limited search is not indexed full-text ranking/headline support.
- Limited GIS does not claim full PostGIS or complete Redshift spatial coverage.

#### ClickHouse

ClickHouse is a columnar OLAP engine. GraphJin treats it as a reads-first source: it
compiles GraphQL to a batched read plan executed over the native protocol (port 9000),
assembling nested JSON in Go (it does not use SQL `JOIN`/`LATERAL`).

```yaml
database:
  type: clickhouse
  host: localhost
  port: 9000
  dbname: mydb
  user: default
  password: secret

  # Or use a DSN
  # connection_string: clickhouse://user:password@localhost:9000/mydb
```

ClickHouse specifics to be aware of:

- **No foreign keys.** Relationships must be declared in config (a column's `foreign_key`),
  e.g. `tables[].columns[]: { name: owner_id, foreign_key: users.id }` — GraphJin cannot
  infer them as it does for SQL databases.
- **Reads.** Filters (incl. `or`/`not`/`like`), ordering, `limit`/`offset`, keyset cursor
  pagination, and analytics (aggregates, `distinct:` group-by) are supported.
- **Mutations are best-effort and single-table.** `insert` is first-class (pass the row as a
  JSON variable: `insert: $data`); `update`/`delete` run as synchronous `ALTER … UPDATE` /
  lightweight `DELETE` and require a `where`. No upsert, no nested/related writes, no
  `RETURNING`-style read-after-write guarantees for async paths.

#### TLS Connection Example

```yaml
database:
  type: postgres
  host: mydb.example.com
  port: 5432
  dbname: myapp
  user: dbuser
  password: secret

  enable_tls: true
  server_name: "myproject:cloud-sql-instance"
  server_cert: ./certs/server-ca.pem
  client_cert: ./certs/client-cert.pem
  client_key: ./certs/client-key.pem
```

---

## Catalog Graph Configuration

The catalog graph exposes GraphJin's discovered schema, workflow read data, and system guidance as ordinary read-only GraphQL tables backed by an internal nanoDB. It follows the deployment-mode default for `system.capabilities.catalog.read`; no source entry is required.

GraphJin-managed databases are intentionally excluded from catalog rows: the system graph itself and CodeSQL SQLite caches do not appear as application metadata, which prevents recursive self-description. CodeSQL can still be linked through `gj_catalog` to `gj_code` relationships.

```yaml
system:
  capabilities:
    catalog.read: true
  root_access:
    gj_catalog: public
```

| Option | Default | Description |
|--------|---------|-------------|
| `system.capabilities.catalog.read` | mode default | Exposes catalog metadata through `gj_catalog` |
| `system.root_access.gj_catalog` | mode default | Controls which caller class may query `gj_catalog` |

Catalog item kinds include:

```text
database
table
column
relationship
workflow
directive
operator_set
query_pattern
mutation_pattern
config
entrypoint
capability
system_capability
```

With CodeSQL enabled, you can query database catalog metadata and code usage in the same graph:

```graphql
query {
  gj_catalog(where: { kind: { eq: "column" }, table_name: { eq: "users" }, column_name: { eq: "email" } }) {
    type
    gj_code {
      kind
      ref_kind
      path
      symbol_id
    }
  }
}
```

---

## Authentication Configuration

### Auth Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `auth.type` | string | `none` | Auth type: `none`, `jwt`, `header` |
| `auth.cookie` | string | - | Name of the cookie holding the auth token |
| `auth.development` | boolean | `false` | Enable development mode (use headers for testing) |

### JWT Configuration

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `auth.jwt.provider` | string | - | JWT provider: `auth0`, `firebase`, `jwks`, `other` |
| `auth.jwt.secret` | string | - | Secret key for HMAC signing |
| `auth.jwt.public_key` | string | - | Public key for RSA/ECDSA verification |
| `auth.jwt.public_key_type` | string | `ecdsa` | Public key type: `ecdsa`, `rsa` |
| `auth.jwt.audience` | string | - | Expected audience claim value |
| `auth.jwt.issuer` | string | - | Expected issuer claim value |
| `auth.jwt.jwks_url` | string | - | JWKS endpoint URL |
| `auth.jwt.jwks_refresh` | integer | - | JWKS refresh interval in minutes |
| `auth.jwt.jwks_min_refresh` | integer | `60` | JWKS minimum refresh interval in minutes |

### Header Authentication

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `auth.header.name` | string | - | HTTP header name to check |
| `auth.header.value` | string | - | Expected header value (optional) |
| `auth.header.exists` | boolean | `false` | Only check if header exists |

### Authentication Examples

#### No Authentication

```yaml
auth:
  type: none
```

#### JWT with Auth0

```yaml
auth:
  type: jwt
  cookie: _myapp_session

  jwt:
    provider: auth0
    secret: your-secret-key-here
    audience: https://api.myapp.com
    issuer: https://myapp.auth0.com/
```

#### JWT with Public Key

```yaml
auth:
  type: jwt

  jwt:
    provider: other
    public_key_type: rsa
    public_key: |
      -----BEGIN PUBLIC KEY-----
      MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA...
      -----END PUBLIC KEY-----
    audience: myapp
    issuer: https://auth.myapp.com
```

#### JWT with JWKS

```yaml
auth:
  type: jwt

  jwt:
    provider: jwks
    jwks_url: https://myapp.auth0.com/.well-known/jwks.json
    audience: https://api.myapp.com
    issuer: https://myapp.auth0.com/
    jwks_refresh: 60  # Refresh every 60 minutes
```

#### Header Authentication

```yaml
auth:
  type: header

  header:
    name: X-API-Key
    value: my-secret-api-key
```

#### Header Exists Check

```yaml
auth:
  type: header

  header:
    name: X-Authenticated
    exists: true
```

#### Development Mode (Testing)

```yaml
auth:
  type: jwt
  development: true  # Allows X-User-ID, X-User-Role headers for testing

  jwt:
    provider: auth0
    secret: dev-secret
```

When `development: true`, you can set user context via headers:
- `X-User-ID` - Sets the user ID
- `X-User-Role` - Sets the user role
- `X-User-ID-Provider` - Sets the user ID provider

---

## Core Compiler Configuration

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `secret_key` | string | auto | Secret for encrypting cursors and opaque values |
| `disable_allow_list` | boolean | `false` | Disable the allow list workflow |
| `enable_schema` | boolean | `false` | Generate/use database schema file |
| `enable_introspection` | boolean | `false` | Generate introspection JSON file |
| `set_user_id` | boolean | `false` | Set database session variable `user.id` |
| `default_block` | boolean | `true` | Block all tables for anonymous users |
| `default_limit` | integer | `20` | Default row limit for queries |
| `analytics_mode` | boolean | `false` | OLAP mode: skip implicit row limits and require partition filters on partitioned tables |
| `mode` | string | derived from `production` | Deployment mode: `dev`, `prod`, or `agentic` |
| `subs_poll_duration` | duration | `5s` | Subscription polling interval |
| `db_schema_poll_duration` | duration | `10s` | Schema change detection interval |
| `disable_agg_functions` | boolean | `false` | Disable aggregation functions |
| `disable_functions` | boolean | `false` | Disable all SQL functions |
| `enable_camelcase` | boolean | `false` | Convert camelCase to snake_case |
| `mock_db` | boolean | `false` | Return mock data without database |
| `debug` | boolean | `false` | Enable debug logging |
| `log_vars` | boolean | `false` | Log SQL query variable values |

### Example

```yaml
secret_key: "your-32-char-secret-key-here!!"
disable_allow_list: false
enable_schema: false
enable_introspection: true
set_user_id: true
default_block: true
default_limit: 50
analytics_mode: false
subs_poll_duration: 2s
db_schema_poll_duration: 20s
disable_agg_functions: false
disable_functions: false
enable_camelcase: true
debug: false
log_vars: false
```

### Analytics Mode

When `analytics_mode: true`, GraphJin compiles queries for OLAP / data-warehouse
workloads:

- **No implicit row limit.** `default_limit` is ignored — queries return all
  matching rows unless `limit` is set on the query or in the role's table
  config. Use this for analytics dashboards over Snowflake, BigQuery-style
  workloads, or any aggregate query where capping rows would corrupt results.
- **Partition filters required.** On tables that declare a partition key,
  queries must filter on that key. Unfiltered scans are rejected at compile
  time to prevent unbounded warehouse scans.

In multi-database deployments you can set `analytics_mode` globally and override
it per database — typically `true` for an analytics warehouse and `false` for an
OLTP application database. See [Multi-Database Configuration](#multi-database-configuration).

Analytics directives such as `@running`, `@moving`, `@previous`, and `@rank`
are query syntax, not a separate config flag. They work well with analytics
mode for business-reporting queries such as running totals, moving averages,
row ranking, previous-period values, and first/last values in an ordered group.
GraphJin validates support from the selected dialect and detected database
version: MongoDB and known-old MySQL/MariaDB/SQLite/MSSQL versions fail at
compile time with clear errors.

---

## Security & Admin Configuration

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `disable_production_security` | boolean | `false` | Disable production security features |

---

## Rate Limiting

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `rate_limiter.rate` | float | - | Number of events per second |
| `rate_limiter.bucket` | integer | - | Maximum burst size |
| `rate_limiter.ip_header` | string | - | Header containing client IP |

Rate limiting uses the [token bucket algorithm](https://en.wikipedia.org/wiki/Token_bucket).

### Example

```yaml
rate_limiter:
  rate: 100      # 100 requests per second
  bucket: 20     # Allow bursts of up to 20 requests
  ip_header: X-Forwarded-For
```

---

## MCP Configuration

Model Context Protocol (MCP) enables AI assistants to interact with GraphJin.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `mcp.disable` | boolean | `false` | Disable the MCP server |
| `mcp.include_tools_with_agent` | boolean | dev/agentic: `true`; prod: `false` | Expose primitive tools alongside `ask_graphjin_agent`; set false for an agent-only surface |
| `mcp.allow_mutations` | boolean | `true` | Allow mutation operations |
| `mcp.allow_raw_queries` | boolean | `true` | Allow arbitrary GraphQL queries |
| `mcp.legacy_discovery` | boolean | `false` | Re-enable legacy MCP discovery/action tools and legacy HTTP helper endpoints in source mode |
| `mcp.stdio_user_id` | string | - | Default user ID for stdio transport |
| `mcp.stdio_user_role` | string | - | Default user role for stdio transport |
| `mcp.only` | boolean | `false` | MCP-only mode; legacy HTTP helpers remain only when `mcp.legacy_discovery` is true |
| `mcp.cursor_cache_ttl` | integer | `1800` | Cursor cache TTL in seconds (30 min) |
| `mcp.cursor_cache_size` | integer | `10000` | Max in-memory cursor cache entries |
| `mcp.allow_config_updates` | boolean | `false` | Allow LLMs to modify config (dangerous) |
| `mcp.allow_schema_reload` | boolean | `false` | Allow schema reload via MCP (auto-enabled in dev mode) |
| `mcp.allow_workflow_execution` | boolean | `false` | Allow legacy `execute_workflow` MCP tool; GraphQL `gj_workflow_execution` is controlled by `read_only` table/source config |
| `mcp.oauth.enabled` | boolean | `false` | Enable OAuth metadata and challenges for hosted MCP clients |
| `mcp.oauth.mode` | string | `external` | `builtin` reuses `auth_login`; `external` advertises configured authorization servers |
| `mcp.oauth.resource` | string | request URL | Expected MCP resource/audience; defaults to the public `/api/v1/mcp` URL |
| `mcp.oauth.issuer` | string | request origin | Authorization server issuer in builtin mode |
| `mcp.oauth.authorization_servers` | []string | issuer | Authorization servers advertised in protected-resource metadata |
| `mcp.oauth.scopes` | []string | `["mcp"]` | OAuth scopes advertised and accepted for MCP |
| `mcp.oauth.dynamic_client_registration` | boolean | `true` | Enable dynamic client registration in builtin mode |
| `mcp.oauth.client_id_metadata_documents` | boolean | `true` | Enable CIMD client metadata discovery in builtin mode |

### Example

```yaml
mcp:
  disable: false
  allow_mutations: true
  allow_raw_queries: false  # Only allow saved queries
  stdio_user_id: "system"
  stdio_user_role: "admin"
  cursor_cache_ttl: 3600
  cursor_cache_size: 5000
  allow_config_updates: false  # Keep disabled for security
  allow_schema_reload: true  # Enabled by default in dev mode
  allow_workflow_execution: false
  oauth:
    enabled: false
    mode: external
    scopes: ["mcp"]
```

---

## Agent Configuration

The server-side GraphJin agent runs the catalog-first discovery loop
inside GraphJin and returns a typed, evidence-backed answer. When enabled it is
available at `POST /api/v1/agent` and as the MCP tool `ask_graphjin_agent`. The
Dev and agentic modes enable it automatically. See
[AGENTIC.md](AGENTIC.md#server-side-agent) for behavior.

It is an RLM (reasoning-with-code) loop built on Ax: the model writes JavaScript
that calls the catalog tools and the typed result is parsed from `key: value`
output. The model does not need provider tool-calling, but the Ax client requests
structured JSON for each typed stage. Deployments differ in which
structured-output mechanisms they support, so Ax selects one from the named
deployment profile and its model rules. `agent.structured_output_mode: auto`
(the default) accepts that selection; the explicit modes are an override.

Ax 24 separates the official `openai` profile from the conservative
`openai-compatible` one. Point third-party OpenAI-compatible endpoints
(vLLM, OpenRouter, Together, LM Studio, …) at `provider: openai-compatible` so
they inherit that deployment's capabilities rather than OpenAI's.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `agent.enabled` | boolean | dev/agentic: `true`; prod: `false` | Enable the REST endpoint and MCP tool |
| `agent.provider` | string | `openai` | Named Ax deployment profile. The profile owns the endpoint, authentication, capabilities, and model rules — any profile name Ax ships is valid (`openai`, `openai-compatible`, `google-gemini`, `vertex-ai`, `anthropic`, `groq`, `amazon-bedrock`, …) |
| `agent.model` | string | - | Model within the selected deployment profile. Profiles carry per-model rules, so the pairing decides capabilities |
| `agent.api_key_env` | string | `OPENAI_API_KEY` | Env var holding the provider API key; must be non-empty (use a dummy value for keyless local endpoints) |
| `agent.base_url` | string | - | OpenAI-compatible provider base URL (e.g. a local or self-hosted endpoint) |
| `agent.structured_output_mode` | string | `auto` | How Ax requests structured output: `auto` lets the profile and model rule pick the verified mechanism; `native` (JSON Schema), `function`, and `json_object` force one. An explicit mode the deployment cannot serve fails before any request is sent |
| `agent.response_format` | string | - | **Deprecated** alias of `agent.structured_output_mode`: `json_schema` maps to `native`, `json_object` to `json_object`. Set only one; the canonical key wins |
| `agent.max_steps` | integer | `8` | Maximum agent actor steps per request |
| `agent.timeout_seconds` | integer | `50` | Request timeout for agent runs; values below 50 are raised to the 50-second minimum |
| `agent.read_only` | boolean | `false` | Force the server-side agent to reject mutations, including saved-query mutations |
| `agent.return_trace` | boolean | `false` | Include agent action/trace data in responses |
| `agent.seed_limit` | integer | `40` | Initial `query_catalog(search: instruction)` seed row cap |
| `agent.catalog_default_limit` | integer | `20` | Default row limit for model-issued catalog queries |

There are no per-request agent modes. The server-side agent's internal runtime
always includes discovery, validation, saved-query execution, and guarded raw
GraphQL execution. `agent.read_only: true` is the operator kill-switch that
rejects mutations before execution, including saved-query mutations. Otherwise
the agent always runs as the caller, so core roles, row-level security,
source/table `read_only`, and protocol evidence gates decide what can run. The
public MCP `execute_graphql` tool is separate and is listed only when
`mcp.allow_raw_queries: true`.

For each request, GraphJin uses the configured server provider. If the
environment variable named by `agent.api_key_env` is empty, MCP and REST fail
closed with `model_credentials_required`. The removed `agent.sampling` and
`mcp.http_stateful` settings are rejected with migration guidance; modern
stateless and legacy stateful MCP routing is automatic.

Requests may include `task_id`, the retained ID of an active (`open` or
`verifying`) `gj_task`. GraphJin
loads that owner's declared goal and recent trail into the untrusted `history`
channel, then appends one `agent_run` entry after the run. The ID is only a
correlation label: it never grants access and never satisfies a discovery or
mutation evidence guard.

---

## Task Configuration

Tasks are explicit, owner-scoped declared goals with a provenance-labeled trail.
They use the artifact SQL store and are exposed as `gj_task` and
`gj_task_entry`. Parsed `dev` and `agentic` configs enable tasks whenever
artifacts are enabled; `prod` and directly constructed Go configs retain the
literal disabled default.

```yaml
tasks:
  enabled: true
  max_per_owner: 20
  max_entries_per_task: 500
  entry_retention_hours: 168
  snapshot_max_bytes: 32768
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `tasks.enabled` | boolean | dev/agentic: artifact setting; prod: `false` | Enable `gj_task`, `gj_task_entry`, agent warm-start/journaling, and optional watch linkage |
| `tasks.max_per_owner` | integer | `20` | Maximum active (`open` or `verifying`) tasks per owner; closed tasks do not count |
| `tasks.max_entries_per_task` | integer | `500` | Maximum retained trail entries per task; oldest entries are pruned on append |
| `tasks.entry_retention_hours` | integer | `168` | Trail retention window in hours, enforced on append and in the nanoDB projection |
| `tasks.snapshot_max_bytes` | integer | `32768` | Byte cap applied before and after JSON normalization to task `snapshot_json`, task `verify_json`, and entry `detail_json` |

Create tasks explicitly; GraphJin never infers a task from a session or tool
call. Caller journal entries accept only `task_id`, `body`, and optional
`detail_json`; provenance fields are server-managed. Closing preserves the
trail and is preferred to deleting it. Deleting a task removes its entries and
clears `task_id` from linked watches.

For a verified close, `verify_json` names a saved query plus a typed expectation
and may include one bounded, delayed `recheck`. Verification runs under the
stored owner identity and has no configuration knobs in v1: the window clamps
to 1 minute through 30 days and the durable sweep runs every 30 seconds. A
passing check closes the task; a failing check leaves it open and records a
server-managed `verification` trail entry. Inline GraphQL and JavaScript
conditions are not accepted.

---

## Redis Configuration

`redis.url` is optional for a single GraphJin process. In a horizontal
deployment it provides shared response-cache state and coordinates discovery
and semantic-index builders. Discovery snapshots and vectors remain on the
filesystem; Redis stores only leases, fencing tokens, active generation IDs,
builder status, dirty counters, and PubSub notifications for those builders.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `redis.url` | string | - | Redis connection URL |

### Example

```yaml
redis:
  url: redis://localhost:6379/0
```

---

## Discovery Cache and Semantic Catalog Search

GraphJin Service enables cache-first schema discovery by default. It writes
full-fidelity database metadata to immutable filesystem generations, validates
their checksums and catalog revisions, and starts from the newest compatible
activated generation on the next warm start. Direct users of the `core` Go
package keep the existing live-first behavior.

Optional semantic search uses Ax embeddings as a recall layer over the catalog.
It can find business concepts and likely table endpoints, but it never invents
relationships: GraphJin follows only real, caller-visible metadata edges when
adding a join path.

```yaml
discovery_cache:
  enabled: true
  path: .graphjin/discovery
  refresh_interval: 5m
  startup_wait: 2m
  retain_generations: 2

catalog_search:
  semantic:
    enabled: false
    provider: openai
    embedding_model: text-embedding-3-small
    api_key_env: OPENAI_API_KEY
    base_url: ""
    dimensions: tiny
```

### Discovery cache options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `discovery_cache.enabled` | boolean | `true` | Enable service-owned cache-first discovery and coordinated refresh. Set `false` for the previous service live-first path. |
| `discovery_cache.path` | string | `.graphjin/discovery` | Filesystem root for immutable schema and semantic generations. |
| `discovery_cache.refresh_interval` | duration | `5m` | Interval between background live-schema checks. |
| `discovery_cache.startup_wait` | duration | `2m` | Maximum cold-start or explicit-refresh wait for a coordinated activation. |
| `discovery_cache.retain_generations` | integer | `2` | Number of valid activated generations retained when the filesystem supports deletion. |

Each discovery generation contains per-source full schema snapshots, optional
DDL, and a manifest with a secret-free config/source fingerprint, source and
catalog revisions, file sizes, and SHA-256 checksums. Data files are written
before the manifest, and an activation receipt is written only after successful
activation. Partial, corrupt, or incompatible generations are ignored.

On a warm start GraphJin serves the filesystem generation immediately while a
background refresh checks the live sources. If source revisions are unchanged,
no new generation is activated. Explicit schema reloads and schema-affecting
configuration changes use the same generation path and wait for activation.
The running `GraphJin` pointer remains stable while its engine is rebuilt from
the new snapshots.

For one GraphJin process, Redis is not required: discovery and semantic builds
use in-process coordination. For multiple replicas, configure `redis.url` and
mount `discovery_cache.path` as the same shared read-after-write filesystem on
every replica. Redis coordinates one builder but never stores the schema or
vectors.

When Redis is configured but unavailable:

- A valid filesystem generation starts stale/degraded and rebuilding is
  suspended.
- With no valid filesystem generation, startup fails after `startup_wait` so
  every replica does not run discovery independently.
- A replica that cannot read a Redis-activated shared generation keeps its
  previous generation and logs degraded status.

### Semantic catalog search options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `catalog_search.semantic.enabled` | boolean | `false` | Enable hybrid lexical and semantic catalog retrieval. |
| `catalog_search.semantic.provider` | string | `openai` | Ax embedding provider name. |
| `catalog_search.semantic.embedding_model` | string | - | Embedding model identifier. Required when semantic search is enabled. |
| `catalog_search.semantic.api_key_env` | string | `OPENAI_API_KEY` | Environment variable containing the embedding provider key. |
| `catalog_search.semantic.base_url` | string | - | Optional provider-compatible endpoint override. |
| `catalog_search.semantic.dimensions` | string | `tiny` | `tiny` (128), `small` (256), `medium` (512), or `default` (provider-native). |

Named dimensions are strict. A response with a different dimension is rejected
and catalog queries fall back to lexical results. Provider, model, base URL,
dimension preset, and semantic document format form the embedding-space
fingerprint. Changing any of them causes a full rebuild; compatible catalog
changes reuse vectors by canonical document hash and embed only added or
changed documents.

The index is deliberately bounded. Each table produces one identity document,
column facets grouped by meaning and chunked at 64 columns, and one real
relationship-neighborhood document. Safe shared catalog concepts can also be
embedded. Caller-owned artifacts, query/workflow bodies, evidence payloads,
configuration values, examples containing values, and secrets remain
lexical-only.

A 1,000-table catalog with 300 columns per table therefore produces roughly
12,000 vectors instead of 300,000. At `tiny`, raw vectors occupy about 6 MB.
Document embedding uses batches of 64 with two concurrent requests. Warm
startup performs no document-embedding calls.

Catalog queries remain lexical-first. Exact identifiers avoid a query
embedding call and stay pinned above fused results. Other queries exact-scan
the compact semantic index, discard low-confidence matches, map hits back to
catalog cards, and optionally add at most two real relationship paths of three
edges or fewer between up to four table endpoints. Original filters,
visibility, ordering, limit, and offset are applied to the combined candidates.
`explain: true` reports lexical, semantic table/facet, or deterministic
relationship-path provenance. `search_rank` is the fused relevance rank;
explicit non-relevance `order_by` sorts the hybrid union by the requested
fields instead.

Query embeddings are never persisted. Each serving replica keeps a 1,024-entry
in-memory LRU for ten minutes. Embedding errors and the two-second query timeout
return lexical results instead of failing `query_catalog`.

See [Discovery Cache And Semantic Search](/configure/discovery-semantic-search/)
for the complete startup matrix, storage layout, safety boundaries, and
operational checklist.

---

## Caching Configuration

Response caching with automatic invalidation on mutations and stale-while-revalidate (SWR) for cheap stale serving.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `caching.disable` | boolean | `false` | Disable response caching entirely |
| `caching.ttl` | integer | `3600` | Hard TTL (seconds). Entries past this are dropped. |
| `caching.fresh_ttl` | integer | `300` | Soft TTL (seconds). Entries past this trigger background SWR refresh; set to `0` to disable SWR. |
| `caching.exclude_tables` | []string | - | Tables whose row changes won't invalidate cached queries (and queries touching them are not cached) |

### Backend selection

Caching is enabled by default with an in-memory LRU cache. Configure `redis.url` to switch to Redis for shared cache across instances:

```yaml
redis:
  url: redis://localhost:6379/0
caching:
  ttl: 3600
  fresh_ttl: 300
```

### Stale-while-revalidate semantics

When `fresh_ttl < ttl` and an entry is past `fresh_ttl` but inside `ttl`:

1. The stale response is returned to the caller immediately
2. A background worker re-runs the query under the original role and overwrites the cache entry
3. Concurrent refreshes for the same key are deduplicated via singleflight
4. The worker pool is bounded so a thundering herd of stale hits cannot spawn unbounded goroutines

Set `fresh_ttl: 0` (or equal to `ttl`) to disable SWR — entries are then served fresh until the hard expiry.

### Invalidation

Mutations that touch a row drop every cached query that depended on that row. Tracking is row-level for small results (≤500 rows) and falls back to table-level for larger results, so analytic queries don't pay row-tracking overhead.

The cache key is derived from operation name, query text, variables, role, and (for ABAC) user ID. Anonymous queries (no operation name, no APQ key) bypass the cache entirely.

### Example

```yaml
caching:
  disable: false
  ttl: 3600        # 1 hour hard expiry
  fresh_ttl: 300   # 5 minute fresh window; stale-but-valid for the next 55 min
  exclude_tables:
    - audit_logs
    - sessions
```

---

## Uploads Configuration

GraphQL endpoint accepts `multipart/form-data` POSTs per the [graphql-multipart-request-spec](https://github.com/jaydenseric/graphql-multipart-request-spec). See the [File Uploads](FEATURES.md#file-uploads) feature reference for the on-the-wire shape.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `uploads.enabled` | boolean | `false` | Accept multipart/form-data POSTs on `/api/v1/graphql` |
| `uploads.max_size` | integer | `26214400` (25 MB) | Per-request body limit, in bytes |
| `uploads.allowed_mime` | []string | - | When non-empty, restricts content types. Glob-aware (`image/*`, `application/pdf`) |
| `uploads.storage` | string | - | Filesystem table name to stream into. When empty, files are inlined as base64 in the variable. |
| `uploads.storage_key_prefix` | string | - | Prefix for streamed-file keys. `{date}` is substituted with `YYYY/MM/DD` at request time. |

### Two modes

**Inline base64** (`storage` empty) — the multipart file becomes a JSON object inside the variable: `{filename, content_type, size, data}`. Useful for small uploads going into a `bytea` column.

**Streamed to a filesystem table** (`storage: avatars`) — the body is written to the named [filesystem source](#filesystems-configuration) and the variable becomes `{key, content_type, size, url, etag, modified_at}`. Keeps large bodies out of the request/response.

### Example

```yaml
uploads:
  enabled: true
  max_size: 25_000_000
  allowed_mime: ["image/*", "application/pdf"]
  storage: avatars                 # name of a filesystem source/table
  storage_key_prefix: "{date}/"    # → 2026/05/08/<8-byte-hex>.png
```

---

## Filesystems Configuration

Object stores (local directories, S3, GCS) exposed as virtual GraphQL tables. They use the normal read surface (`id`, `where`, `order_by`, `limit`, `offset`, cursor-style page args) over portable object metadata; see the [Filesystem Tables](FEATURES.md#filesystem-tables) feature reference for examples.

Declare each object store as `sources[].kind: file`, then attach the table to that source:

```yaml
sources:
  - name: avatars
    kind: file
    backend: s3
    bucket: app-assets
    prefix: avatars/

tables:
  - name: avatars
    source: avatars
    read_only: true
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `name` | string | required | Table name surfaced in GraphQL |
| `backend` | string | required | `local`, `s3`, or `gcs` |
| `bucket` | string | - | S3/GCS bucket (required for those backends) |
| `region` | string | - | S3 region |
| `endpoint` | string | - | S3 endpoint override (MinIO, localstack, R2, etc.) — forces path-style addressing |
| `prefix` | string | - | Prepended to every key (effective root for the table) |
| `root` | string | - | Local filesystem root directory (required for `local`) |
| `presign_ttl` | duration | `15m` | TTL for generated presigned URLs |
| `public_base_url` | string | - | When set, replaces presigned URLs with `<base>/<key>` — useful for CDN fronting |
| `max_list_page_size` | integer | `1000` | Caps `limit:` for list queries |

### Authentication

Each backend uses its platform's standard credential chain — never embedded in GraphJin config:

- **S3**: env vars (`AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY`), `~/.aws/credentials`, IRSA on EKS, EC2 IMDS
- **GCS**: Application Default Credentials (`GOOGLE_APPLICATION_CREDENTIALS` env, GCE/GKE metadata server, `gcloud auth application-default login`)
- **Local**: filesystem permissions of the GraphJin process

### Build-tag gating

Slim builds drop SDK weight:

- `-tags no_s3` — excludes the S3 backend (drops AWS SDK from the binary)
- `-tags no_gcs` — excludes the GCS backend (drops Google Cloud SDK from the binary)
- `local` is always built in

Configuring an excluded backend produces a clear startup error.

### Custom backends

Plug in Azure Blob, Cloudflare R2, or anything implementing `fstable.Backend` via `core.OptionSetFilesystemBackend(name, factory)` from your own setup code.

### Examples

```yaml
sources:
  - name: avatars
    kind: file
    backend: s3
    bucket: my-bucket
    prefix: avatars/
    region: us-east-1
    presign_ttl: 15m
    public_base_url: https://cdn.example.com

  - name: invoices
    kind: file
    backend: gcs
    bucket: invoices
    prefix: 2026/

  - name: uploads_local
    kind: file
    backend: local
    root: /var/lib/graphjin/uploads

  # MinIO / localstack via S3-compatible endpoint:
  - name: dev_blob
    kind: file
    backend: s3
    bucket: dev
    region: us-east-1
    endpoint: http://localhost:9000

tables:
  - name: avatars
    source: avatars
    read_only: true

  - name: invoices
    source: invoices
    read_only: true

  - name: uploads_local
    source: uploads_local

  - name: dev_blob
    source: dev_blob
```

---

## Federation Configuration

Register GraphJin as an Apollo Federation v2 subgraph so it composes alongside other services behind Apollo Router / Cosmo / Hive Gateway. See the [Apollo Federation v2](FEATURES.md#apollo-federation-v2) feature reference for the SDL surface.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `federation.enabled` | boolean | `false` | Emit federation SDL and answer `_service { sdl }` queries |
| `federation.version` | string | `v2.5` | Federation spec version (the value used in the `@link` directive) |
| `federation.keys` | map | - | Per-table override for `@key(fields: ...)`. Auto-derived from primary keys when absent. |
| `federation.shareable` | []string | - | `Type.field` identifiers tagged `@shareable` |
| `federation.inaccessible` | []string | - | `Type.field` identifiers tagged `@inaccessible` |
| `federation.tags` | map | - | `Type.field` → list of `@tag(name:...)` annotations |

When `enabled: true`:

- `_service { sdl }` returns a federation-flavoured SDL covering every non-blocked, primary-keyed table
- `_entities(representations: [_Any!]!)` is recognised but currently returns a clear "not yet implemented" error — composition still succeeds in the gateway
- All other queries flow through the regular pipeline unchanged. Detection is a token-bounded substring scan over the raw query.

### Example

```yaml
federation:
  enabled: true
  version: "v2.5"

  keys:
    users:  ["id"]                   # default — auto-derived
    orders: ["id", "tenant_id"]      # composite key override

  shareable:
    - Tag.name

  inaccessible:
    - Users.encrypted_password
    - Users.reset_password_token

  tags:
    Users.full_name: ["pii"]
    Users.email:     ["pii", "exportable"]
```

---

## Schema Configuration

### Variables

Define variables for use in queries and filters.

```yaml
# Static variables
variables:
  admin_account_id: "5"
  default_status: "active"

# SQL-based variables (prefixed with "sql:")
variables:
  admin_id: "sql:select id from users where admin = true limit 1"
```

### Header Variables

Map HTTP headers to variables.

```yaml
header_variables:
  remote_ip: "X-Forwarded-For"
  tenant_id: "X-Tenant-ID"
```

### Blocklist

Block specific tables or columns from all queries.

```yaml
blocklist:
  - ar_internal_metadata
  - schema_migrations
  - secret
  - password
  - encrypted
  - token
```

### Tables Configuration

Configure table aliases, relationships, and column metadata.

| Option | Type | Description |
|--------|------|-------------|
| `name` | string | Virtual table name (used in queries) |
| `table` | string | Actual database table name |
| `schema` | string | Database schema |
| `type` | string | Table type: `polymorphic`, `jsonb` |
| `database` | string | Database name (for multi-db) |
| `blocklist` | []string | Columns to block for this table |
| `order_by` | map | Named order-by presets |
| `columns` | []Column | Column configurations |

#### Column Configuration

| Option | Type | Description |
|--------|------|-------------|
| `name` | string | Column name |
| `type` | string | Column type (e.g., `integer`, `text`, `bigint`) |
| `primary` | boolean | Mark as primary key |
| `array` | boolean | Column is an array type |
| `full_text` | boolean | Enable full-text search |
| `related_to` | string | Foreign key relationship (e.g., `users.id`) |

### Tables Examples

```yaml
tables:
  # Table alias - query "me" maps to "users" table
  - name: me
    table: users

  # Custom order_by presets
  - name: users
    order_by:
      new_users: ["created_at desc", "id asc"]
      by_id: ["id asc"]

  # Column relationships (for arrays without foreign keys)
  - name: products
    columns:
      - name: category_ids
        related_to: categories.id
    order_by:
      price_and_id: ["price desc", "id asc"]

  # Polymorphic table
  - name: subject
    type: polymorphic
    columns:
      - name: subject_id
        related_to: subject_type.id

  # Self-referential relationship
  - name: chats
    columns:
      - name: reply_to_id
        related_to: chats.id

  # JSONB column type
  - name: category_counts
    table: users
    type: jsonb
    columns:
      - name: category_id
        related_to: categories.id
        type: bigint
      - name: count
        type: integer
```

### Functions Configuration

Configure custom database functions.

```yaml
functions:
  - name: calculate_total
    schema: public
    return_type: numeric

  - name: get_user_permissions
    return_type: record
```

### Resolvers Configuration

Configure remote API resolvers to join external data into queries.

```yaml
resolvers:
  - name: payments
    type: remote_api
    table: customers
    column: stripe_id
    json_path: data
    debug: false
    url: http://payments-service/payments/$id
    pass_headers:
      - cookie
      - authorization
    set_headers:
      - name: Host
        value: payments-service
      - name: X-API-Key
        value: ${PAYMENTS_API_KEY}
```

---

## OpenAPI Integration

GraphJin can join data from any HTTP API that publishes an **OpenAPI 3** specification. Drop the spec into `config/specs/`, declare credentials and join wiring in your environment config (`dev.yml`/`prod.yml`), and the API's GET endpoints become joinable from your existing tables — without writing a custom resolver per integration.

The OpenAPI integration is the spec-driven counterpart to `remote_api` (above). Use `remote_api` for ad-hoc URL joins; use OpenAPI when the upstream publishes a spec and you want auth, parameter wiring, and response shape derived automatically.

### Quick Start

1. Drop an OpenAPI 3 spec into `config/specs/<name>.yaml`:

   ```
   config/
     dev.yml
     specs/
       interaction_studio.yaml    # vendor-supplied, untouched
   ```

2. Add an API source for OpenAPI specs to your environment config (`dev.yml`/`prod.yml`):

   ```yaml
   sources:
     - name: app
       kind: database
       type: postgres
       connection_string: ${APP_DATABASE_URL}
       default: true

     - name: upstream
       kind: api
       specs_dir: ./config/specs
       specs:
         interaction_studio:
           base_url: https://${IS_ACCOUNT}.personalization.salesforce.com/api
           auth:
             scheme: token_exchange
             token_url: https://${IS_ACCOUNT}.personalization.salesforce.com/api/token
             request:
               body:
                 apiKeyId: ${IS_API_KEY}
                 apiKeySecret: ${IS_API_SECRET}
             response:
               token_field: access_token
               expires_field: expires_in

           joins:
             getUserById:
               parent_table: users
               parent_column: email
               param: userId
               expose_as: is_profile

   tables:
     - name: users
       source: app
   ```

3. Restart GraphJin. Boot logs report what was loaded:

   ```
   openapi: loaded interaction_studio.yaml (5 active, 12 skipped)
   openapi: GET /exports/{jobId} — skipped: non-JSON response (application/octet-stream)
   ```

4. Query as a child field on the parent table:

   ```graphql
   query {
     users(where: { id: { eq: 42 } }) {
       id
       email
       is_profile {
         lastSeenAt
         segments { id name }
       }
     }
   }
   ```

### Operation Classification

Every GET in the spec is classified into one of:

| Mode | Path shape | Behaviour |
|---|---|---|
| **Row-join** | `GET /resource/{id}` with a matching `joins:` entry | Exposed as a child field on the parent DB table. The parent column's value populates the path parameter at query time. |
| **Top-level (single)** | `GET /resource/{id}` without a `joins:` entry | Exposed as a top-level GraphQL field. The path parameter becomes a required field argument. |
| **Top-level (list)** | `GET /resources` with optional query filters | Exposed as a top-level GraphQL field. Each query parameter becomes an optional field argument. |
| **Skipped** | Async (Location header), binary response, mutating verb (POST/PUT/PATCH/DELETE), multi-segment path params, or single non-trailing path param without an explicit opt-in | Reason logged at boot. Single non-trailing path params can be opted in with `expose_top_level: true` (see Operation Overrides). |

#### Top-level virtual tables

Operations classified as top-level are queryable directly, with no parent DB row required:

```graphql
# Single-record by ID — path param becomes a required arg
query {
  is_get_user_by_id(userId: "u-42") {
    id
    email
    lastSeenAt
  }
}

# Collection — query params become args
query {
  is_audit_logs(actorId: "u-42") {
    id
    action
  }
}

# Mixed — DB tables and OpenAPI virtuals in one query
query {
  users(where: { id: { eq: 1 } }) { id email }
  is_audit_logs(actorId: "u-1") { id action }
}
```

Default GraphQL field name is `<spec_key>_<operation_id_snake>`; override per-operation under `operations:`:

```yaml
sources:
  - name: upstream
    kind: api
    specs:
      is:
        operations:
          listAuditLogs:
            expose_as: audit_logs
```

### Authentication

Configure auth per spec under the `auth:` block. All credential-bearing fields support `${VAR}` env-var expansion at load time.

#### Bearer (static or pass-through)

```yaml
auth:
  scheme: bearer
  token: ${API_TOKEN}
  # Or, for multi-tenant deployments, forward the token from an inbound header:
  # token_from_request:
  #   header: X-User-Token
```

#### Basic

```yaml
auth:
  scheme: basic
  username: ${API_USER}
  password: ${API_PASS}
```

#### API Key (header or query)

```yaml
auth:
  scheme: api_key
  key_name: X-API-Key
  key_value: ${API_KEY}
  key_in: header   # or "query"
```

#### OAuth2 client_credentials

GraphJin fetches an access token at startup and refreshes it before expiry.

```yaml
auth:
  scheme: oauth2_client_credentials
  token_url: https://auth.example.com/oauth/token
  client_id: ${CLIENT_ID}
  client_secret: ${CLIENT_SECRET}
  scopes: [read]
```

#### token_exchange (vendor-specific flows)

For vendors that don't follow OAuth2 — including Salesforce Marketing Cloud Personalization (formerly Interaction Studio) — `token_exchange` lets you describe the request body shape and response field names directly:

```yaml
auth:
  scheme: token_exchange
  token_url: https://auth.example.com/api/token
  request:
    method: POST           # default
    body_format: json      # or "form"
    body:
      apiKeyId: ${API_KEY_ID}
      apiKeySecret: ${API_SECRET}
    headers:
      X-Custom-Header: foo
  response:
    token_field: access_token
    expires_field: expires_in
  cache_ttl: 3500s         # override when response omits expires_in
```

### Per-Spec Concurrency

Each spec gets its own concurrency budget, applied collectively across every operation against that spec:

```yaml
sources:
  - name: upstream
    kind: api
    specs:
      interaction_studio:
        concurrency:
          max_concurrent: 8           # in-flight requests cap (default: 8)
          rate_limit_per_second: 50   # token-bucket RPS cap (default: 50)
```

Without these caps, a 1000-row parent select would spawn 1000 parallel requests upstream — a reliable way to get rate-limited.

### Operation Overrides

Tweak per-operation presentation:

```yaml
sources:
  - name: upstream
    kind: api
    specs:
      interaction_studio:
        operations:
          listAuditLogs:
            expose_as: audit_logs       # rename auto-derived field
            result_path: data.records   # override auto-detected wrapper path
            disabled: false             # set true to hide an operation entirely
          exportUsers:
            expose_top_level: true      # opt-in classification (see below)
            expose_as: is_users
```

#### `expose_top_level`

Operations whose path has a single non-trailing path parameter — for example `GET /api/dataset/{datasetId}/users.json` — are skipped by default because the semantics ("nested resource" vs "list scoped by an arg") aren't safe to auto-infer. Set `expose_top_level: true` to opt the operation in:

- The path parameter becomes a required GraphQL field argument.
- Mode is chosen from the response shape (array → list, object → single record).
- Result-path stripping and field filtering work exactly as for auto-classified top-level ops.

```graphql
{ is_users(datasetId: "abc", pageSize: "200") { items { id email } } }
```

`expose_top_level` does not opt-in operations that are skipped for other reasons (mutating verb, async, non-JSON, multi-segment path params).

### Result Path Auto-Detection

When the upstream wraps responses in `{data: [...]}`, `{items: [...]}`, `{results: [...]}`, or `{records: [...]}`, GraphJin strips the wrapper automatically. Other shapes need an explicit `result_path:` under `operations:`.

### Naming Defaults

Auto-derived field names use `<spec_key>_<operationId_snake>`:

- Spec file `interaction_studio.yaml` + operation `getUserById` → `interaction_studio_get_user_by_id`

Override per-operation via `expose_as:` (under `joins:` for row-joins, under `operations:` for everything else).

### Collision Defence

GraphJin validates synthesised remote tables at boot so an OpenAPI op cannot silently shadow a real table.

| Collision | Behaviour |
|---|---|
| `expose_as` matches a real (non-remote) table in the primary schema | **Hard fail at boot.** Error names the operation and the YAML path to fix. |
| Two operations resolve to the same `expose_as` | **Hard fail at boot.** Error names both operations. |
| `expose_as` matches a configured table alias | **Warn.** OpenAPI remote takes precedence; alias still queryable under its other name. |
| `expose_as` matches a user-declared `resolvers:` entry | **Warn.** Registration order decides; review and rename if needed. |

Default `<spec_key>_<operationId_snake>` namespacing makes these rare. Overrides via `expose_as` are the usual cause.

Example error:

```
openapi: operation interaction_studio/exportUsers exposes as "users" which collides with a real table in schema public; set 'expose_as' under openapi.interaction_studio.joins.exportUsers to a unique name
```

### Boot Log Output

GraphJin reports the OpenAPI integration state at startup so you can verify what's loaded before queries arrive:

```
openapi: loaded interaction_studio.yaml (5 active, 12 skipped)
openapi: GET /exports/{jobId} — skipped: non-JSON response (application/octet-stream)
openapi: GET /jobs/{jobId}/status — skipped: async pattern (Location header on success response)
openapi: POST /users — skipped: mutating verb (write-side not yet supported)
openapi: GET /api/dataset/{datasetId}/users.json — skipped: path parameter not in trailing position (set expose_top_level: true to opt in)
```

### Limitations

- **Read-only** — only GET operations are processed. Mutations (POST/PUT/PATCH/DELETE) are logged and skipped. Direct write support is planned.
- **Async/export endpoints** — out of scope. GraphJin is a query engine, not a data-replication system. Job-based or file-download endpoints are skipped at classification.
- **Header pass-through** — multi-tenant header forwarding (`token_from_request`) is wired through the auth provider but the bridge layer to inbound HTTP headers isn't yet plumbed. Static `${ENV}` tokens work today.
- **Top-level args are scalar literals only** — quote numbers and booleans (`limit: "50"`). Nested object/array args are not supported in v1.

---

## Role-Based Access Control

### Roles Query

Dynamically assign roles based on user attributes.

```yaml
# The query receives $user_id as a parameter
# SQL mode: column names become context values for role matching
roles_query: "SELECT role, org_id FROM user_roles WHERE user_id = $user_id:bigint"
```

`roles_query` can also be written as a GraphQL query. GraphQL mode is selected when
the value starts with `{`, `query`, or `fragment`. The query must return zero rows
or one object from a single root field; returning more than one row is treated as a
configuration error. In GraphQL mode, `match` is evaluated against the returned
object in role configuration order:

```yaml
roles_query: |
  query {
    users(id: $user_id) {
      role
      userid: id
      account {
        tier
      }
    }
  }

roles:
  - name: admin
    match: role = 'the_admin_dude' or userid < 10 or account.tier = 'enterprise'
```

GraphQL `match` supports a portable predicate subset: identifiers and dotted
paths, string/number/boolean/null literals, `=`, `!=`, `<>`, `<`, `<=`, `>`,
`>=`, `and`, `or`, `not`, parentheses, `is null`, and `is not null`. SQL
functions and casts are available only with SQL `roles_query`.

### Role Configuration

| Option | Type | Description |
|--------|------|-------------|
| `name` | string | Role name (e.g., `user`, `admin`, `anon`) |
| `match` | string | Role predicate. SQL condition in SQL roles_query mode; portable predicate over returned fields in GraphQL roles_query mode. |
| `comment` | string | Description of the role |
| `tables` | []RoleTable | Per-table configurations |

### Default Roles

- `anon` - Anonymous users (no authentication)
- `user` - Authenticated users (user ID present)

### Per-Table Role Configuration

| Operation | Options |
|-----------|---------|
| `query` | `limit`, `filters`, `columns`, `disable_functions`, `block` |
| `insert` | `filters`, `columns`, `presets`, `block` |
| `update` | `filters`, `columns`, `presets`, `block` |
| `upsert` | `filters`, `columns`, `presets`, `block` |
| `delete` | `filters`, `columns`, `block` |

### Role Configuration Examples

```yaml
roles:
  # Anonymous users - restricted access
  - name: anon
    tables:
      - name: products
        query:
          limit: 10
          columns: [id, name, description, price]

      - name: categories
        query:
          limit: 50

  # Authenticated users
  - name: user
    tables:
      # Users can only query their own data
      - name: me
        query:
          filters: ["{ id: { _eq: $user_id } }"]

      - name: products
        query:
          limit: 50
          filters: ["{ published: { _eq: true } }"]

        insert:
          columns: [name, description, price, category_id]
          presets:
            - user_id: "$user_id"
            - created_at: "now"
            - updated_at: "now"

        update:
          filters: ["{ user_id: { _eq: $user_id } }"]
          columns: [name, description, price]
          presets:
            - updated_at: "now"

        delete:
          filters: ["{ user_id: { _eq: $user_id } }"]

  # Admin role (matched via roles_query)
  - name: admin
    match: role = 'admin'
    tables:
      - name: users
        query:
          # No filters - admins can see all users
          limit: 100

        update:
          columns: [name, email, role, active]

        delete:
          # Admins can delete any user
          block: false

  # Organization-specific role
  - name: org_member
    match: org_id IS NOT NULL
    tables:
      - name: projects
        query:
          filters: ["{ org_id: { _eq: $org_id } }"]

        insert:
          presets:
            - org_id: "$org_id"
            - created_by: "$user_id"
```

### Blocking Operations

Use `block: true` to completely disable an operation for a role:

```yaml
roles:
  - name: user
    tables:
      - name: audit_logs
        query:
          block: true  # Users cannot query audit logs
        insert:
          block: true  # Users cannot insert audit logs
        update:
          block: true
        delete:
          block: true
```

---

## Multi-Database Configuration

GraphJin supports querying across multiple SQL databases in a single GraphQL request. `databases:` is the legacy database-only spelling and remains supported when `sources:` is absent. In source mode, declare these same databases as `sources[].kind: database`.

### Database Map Structure

```yaml
databases:
  primary:
    type: postgres
    host: localhost
    port: 5432
    dbname: myapp
    user: postgres
    password: secret
    schema: public
    max_open_conns: 25
    max_idle_conns: 5

  analytics:
    type: postgres
    host: analytics-db.example.com
    port: 5432
    dbname: analytics
    user: readonly
    password: secret
    read_only: true  # Blocks all mutations and DDL against this database
    tables:
      - events
      - metrics

  legacy:
    type: mysql
    host: legacy-db.example.com
    port: 3306
    dbname: legacy_app
    user: app_user
    password: secret
```

CodeSQL is no longer configured under `databases:`. Move source-code indexes to `sources[].kind: code`.

### Per-Database Read-Only Mode

Set `read_only: true` on a database to block all mutations and DDL (schema changes) against it. This is useful for production/reporting databases that should never be modified by an LLM or application code.

```yaml
databases:
  production_replica:
    type: postgres
    host: replica.example.com
    dbname: myapp
    read_only: true  # No mutations or DDL allowed
```

**Tamper protection:** Once `read_only: true` is set in the config file, MCP tools (including `update_current_config`) cannot change it to `false` at runtime. The value is snapshotted at startup and enforced immutably.

When a database is read-only:
- `apply_schema_changes` targeting that database returns an error
- All tables in the database inherit `read_only: true` for role-level enforcement
- `update_current_config` preserves the `read_only: true` flag even if the LLM tries to change it

### Per-Database Analytics Mode

`analytics_mode` can be set globally on the top-level config and overridden per
database. This is the typical setup when GraphJin fronts both an OLTP
application DB and an analytics warehouse:

```yaml
analytics_mode: false  # default for app DBs

databases:
  app:
    type: postgres
    host: app-db.example.com
    dbname: myapp
    # inherits analytics_mode: false

  warehouse:
    type: snowflake
    host: xy12345.snowflakecomputing.com
    dbname: ANALYTICS
    analytics_mode: true  # OLAP rules for this DB only
```

The per-database value, when set, fully overrides the top-level value. See
[Analytics Mode](#analytics-mode) for what the flag does.

### Assigning Tables to Databases

You can assign tables to databases in two ways:

#### 1. In the database config

```yaml
databases:
  analytics:
    type: postgres
    host: analytics-db.example.com
    # ...
    tables:
      - events
      - metrics
      - user_activity
```

#### 2. In the table config

```yaml
tables:
  - name: events
    database: analytics

  - name: legacy_users
    table: users
    database: legacy
```

### Cross-Database Relationships

Tables in different databases can have foreign key relationships. GraphJin automatically detects cross-database relationships and handles them via result stitching (similar to remote API joins but for in-process database calls).

Configure a cross-database foreign key using `related_to` in the table config:

```yaml
databases:
  primary:
    type: postgres
  analytics:
    type: sqlite
    host: /data/analytics.db
    tables:
      - user_events

tables:
  - name: user_events
    database: analytics
    columns:
      - name: user_id
        related_to: users.id   # 'users' is in the primary database
```

Now queries that join across these databases work transparently:

```graphql
query {
  users {
    name
    user_events {   # Fetched from analytics DB automatically
      event_type
      created_at
    }
  }
}
```

**How it works:** GraphJin executes the parent query, extracts foreign key values from the result, builds a filtered query for the child table in the target database, executes it, and merges the child data into the parent response. Null foreign keys produce `null` child results gracefully.

### Environment Variables for Multiple Databases

Environment variables can override any nested config key using the `GJ_` prefix. Underscores are progressively converted to dots to match config paths.

**Important:** The key must already exist in your config file for the environment variable to take effect.

#### Example: Override multi-database settings

Config file with placeholders:

```yaml
databases:
  primary:
    type: postgres
    host: ""        # Overridden by GJ_DATABASES_PRIMARY_HOST
    port: 5432      # Overridden by GJ_DATABASES_PRIMARY_PORT
    dbname: myapp
    user: ""        # Overridden by GJ_DATABASES_PRIMARY_USER
    password: ""    # Overridden by GJ_DATABASES_PRIMARY_PASSWORD

  analytics:
    type: postgres
    host: ""        # Overridden by GJ_DATABASES_ANALYTICS_HOST
    dbname: analytics
    user: ""        # Overridden by GJ_DATABASES_ANALYTICS_USER
    password: ""    # Overridden by GJ_DATABASES_ANALYTICS_PASSWORD
```

Environment variables:

```bash
export GJ_DATABASES_PRIMARY_HOST=primary-db.example.com
export GJ_DATABASES_PRIMARY_PORT=5432
export GJ_DATABASES_PRIMARY_USER=app_user
export GJ_DATABASES_PRIMARY_PASSWORD=secret123

export GJ_DATABASES_ANALYTICS_HOST=analytics-db.example.com
export GJ_DATABASES_ANALYTICS_USER=readonly_user
export GJ_DATABASES_ANALYTICS_PASSWORD=analytics_secret
```

#### How the mapping works

The `GJ_` prefix is stripped, then underscores are converted to dots until a matching config key is found:

| Environment Variable | Config Path |
|---------------------|-------------|
| `GJ_DATABASES_PRIMARY_HOST` | `databases.primary.host` |
| `GJ_DATABASES_PRIMARY_PORT` | `databases.primary.port` |
| `GJ_DATABASES_ANALYTICS_PASSWORD` | `databases.analytics.password` |

---

## Environment Variables Reference

### Database Variables

| Variable | Maps To |
|----------|---------|
| `GJ_DATABASE_HOST` | `database.host` |
| `GJ_DATABASE_PORT` | `database.port` |
| `GJ_DATABASE_USER` | `database.user` |
| `GJ_DATABASE_PASSWORD` | `database.password` |
| `GJ_DATABASE_NAME` | `database.dbname` |
| `GJ_DATABASE_SCHEMA` | `database.schema` |

### Authentication Variables

| Variable | Maps To |
|----------|---------|
| `GJ_AUTH_JWT_SECRET` | `auth.jwt.secret` |
| `GJ_AUTH_JWT_PUBLIC_KEY_FILE` | `auth.jwt.public_key` (file path) |

### Service Variables

| Variable | Maps To |
|----------|---------|
| `GO_ENV` | Config file and default mode selection (`agentic` loads required `agentic.yml` with `mode: agentic`) |
| `HOST` | `host` |
| `PORT` | `port` |

---

## Complete Examples

### Development Configuration

```yaml
app_name: "My App Development"
host_port: 0.0.0.0:8080
web_ui: true
production: false
log_level: "debug"
log_format: "plain"
http_compress: true
server_timing: true
enable_tracing: true
auth_fail_block: false
reload_on_config_change: true
debug: true

secret_key: dev-secret-key-change-in-prod

cors_allowed_origins: ["*"]
cors_debug: false

subs_poll_duration: 2s
default_limit: 20
default_block: false

hot_deploy: false
admin_secret_key: dev-admin-key

auth:
  type: none
  development: true

database:
  type: postgres
  host: localhost
  port: 5432
  dbname: myapp_development
  user: postgres
  password: postgres
  pool_size: 10
  ping_timeout: 1m

variables:
  admin_id: "sql:select id from users where admin = true limit 1"

header_variables:
  remote_ip: "X-Forwarded-For"

blocklist:
  - ar_internal_metadata
  - schema_migrations
  - password
  - secret

tables:
  - name: me
    table: users

roles:
  - name: user
    tables:
      - name: me
        query:
          filters: ["{ id: { _eq: $user_id } }"]
```

### Production Configuration

```yaml
inherits: dev

app_name: "My App Production"
host_port: 0.0.0.0:8080
web_ui: false
mode: prod
log_level: "warn"
log_format: "json"
http_compress: true
enable_tracing: false
auth_fail_block: true
reload_on_config_change: false

hot_deploy: true
# Use environment variable for admin secret
# admin_secret_key: ${GJ_ADMIN_SECRET_KEY}

auth:
  type: jwt
  cookie: _myapp_session
  development: false

  jwt:
    provider: jwks
    jwks_url: https://myapp.auth0.com/.well-known/jwks.json
    audience: https://api.myapp.com
    issuer: https://myapp.auth0.com/

database:
  type: postgres
  host: ${GJ_DATABASE_HOST}
  port: 5432
  dbname: myapp_production
  user: ${GJ_DATABASE_USER}
  password: ${GJ_DATABASE_PASSWORD}
  pool_size: 25
  max_connections: 50
  ping_timeout: 5m
  enable_tls: true
  server_cert: /etc/ssl/certs/db-ca.pem
```

### Multi-Database Configuration

```yaml
app_name: "Multi-DB App"
host_port: 0.0.0.0:8080
production: false

database:
  type: postgres
  host: localhost
  port: 5432
  dbname: primary_db
  user: postgres
  password: postgres

databases:
  primary:
    type: postgres
    host: localhost
    port: 5432
    dbname: primary_db
    user: postgres
    password: postgres
    max_open_conns: 25

  analytics:
    type: postgres
    host: analytics.example.com
    port: 5432
    dbname: analytics_db
    user: readonly
    password: secret
    tables:
      - events
      - page_views
      - user_sessions

  legacy:
    type: mysql
    host: legacy.example.com
    port: 3306
    dbname: legacy_app
    user: app
    password: secret

tables:
  - name: legacy_customers
    table: customers
    database: legacy
    columns:
      - name: user_id
        related_to: users.id

auth:
  type: jwt
  jwt:
    provider: other
    secret: your-secret-key
```

---

## Dev vs Production Recommendations

| Setting | Development | Production |
|---------|-------------|------------|
| `mode` | `dev` | `prod` |
| `web_ui` | `true` | `false` |
| `log_level` | `debug` | `warn` or `info` |
| `log_format` | `plain` | `json` |
| `enable_tracing` | `true` | `false` |
| `auth_fail_block` | `false` | `true` |
| `reload_on_config_change` | `true` | `false` |
| `debug` | `true` | `false` |
| `hot_deploy` | `false` | `true` |
| `auth.development` | `true` | `false` |
| `cors_allowed_origins` | `["*"]` | Specific origins |
| `disable_allow_list` | `true` | `false` |
