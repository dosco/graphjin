# GraphJin Configuration Reference

This document provides a comprehensive reference for all GraphJin configuration options. GraphJin uses YAML or JSON configuration files that can be customized for different environments.

## Table of Contents

- [Introduction](#introduction)
- [Quick Start](#quick-start)
- [Service Configuration](#service-configuration)
- [Database Configuration](#database-configuration)
- [Authentication Configuration](#authentication-configuration)
- [Core Compiler Configuration](#core-compiler-configuration)
- [Security & Admin Configuration](#security--admin-configuration)
- [Rate Limiting](#rate-limiting)
- [MCP Configuration](#mcp-configuration)
- [Redis Configuration](#redis-configuration)
- [Caching Configuration](#caching-configuration)
- [Schema Configuration](#schema-configuration)
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
- `config/dev.json` - JSON format alternative

### Environment-Based Config Selection

GraphJin automatically selects the configuration file based on the `GO_ENV` environment variable:

| GO_ENV Value | Config File |
|-------------|-------------|
| `development`, `dev`, or empty | `dev.yml` |
| `production`, `prod` | `prod.yml` |
| `staging`, `stage` | `stage.yml` |
| `testing`, `test` | `test.yml` |
| Other values | `{value}.yml` |

### Config Inheritance

Use the `inherits` field to inherit configuration from another file, allowing you to override only specific values:

```yaml
# prod.yml - inherits all settings from dev.yml and overrides specific ones
inherits: dev

production: true
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
production: true
web_ui: false
log_level: "warn"
log_format: "json"
auth_fail_block: true

database:
  dbname: myapp_production
```

---

## Service Configuration

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `app_name` | string | - | Application name used in logs and debug messages |
| `host_port` | string | `0.0.0.0:8080` | Host and port the service runs on |
| `host` | string | - | Host to run the service on (alternative to host_port) |
| `port` | string | - | Port to run the service on (alternative to host_port) |
| `production` | boolean | `false` | Enable production mode with security defaults |
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
subs_poll_duration: 2s
db_schema_poll_duration: 20s
disable_agg_functions: false
disable_functions: false
enable_camelcase: true
debug: false
log_vars: false
```

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
| `mcp.enable_search` | boolean | `true` | Enable search for saved queries |
| `mcp.allow_mutations` | boolean | `true` | Allow mutation operations |
| `mcp.allow_raw_queries` | boolean | `true` | Allow arbitrary GraphQL queries |
| `mcp.stdio_user_id` | string | - | Default user ID for stdio transport |
| `mcp.stdio_user_role` | string | - | Default user role for stdio transport |
| `mcp.only` | boolean | `false` | MCP-only mode (disable other endpoints) |
| `mcp.cursor_cache_ttl` | integer | `1800` | Cursor cache TTL in seconds (30 min) |
| `mcp.cursor_cache_size` | integer | `10000` | Max in-memory cursor cache entries |
| `mcp.allow_config_updates` | boolean | `false` | Allow LLMs to modify config (dangerous) |
| `mcp.allow_schema_reload` | boolean | `false` | Allow schema reload via MCP (auto-enabled in dev mode) |

### Example

```yaml
mcp:
  disable: false
  enable_search: true
  allow_mutations: true
  allow_raw_queries: false  # Only allow saved queries
  stdio_user_id: "system"
  stdio_user_role: "admin"
  cursor_cache_ttl: 3600
  cursor_cache_size: 5000
  allow_config_updates: false  # Keep disabled for security
  allow_schema_reload: true  # Enabled by default in dev mode
```

---

## Redis Configuration

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `redis.url` | string | - | Redis connection URL |

### Example

```yaml
redis:
  url: redis://localhost:6379/0
```

---

## Caching Configuration

Response caching with automatic invalidation on mutations.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `caching.disable` | boolean | `false` | Disable response caching |
| `caching.ttl` | integer | `3600` | Cache TTL in seconds (hard TTL) |
| `caching.fresh_ttl` | integer | `300` | Soft TTL for stale-while-revalidate |
| `caching.exclude_tables` | []string | - | Tables to exclude from caching |

### Example

```yaml
caching:
  disable: false
  ttl: 3600        # 1 hour hard TTL
  fresh_ttl: 300   # 5 minute soft TTL
  exclude_tables:
    - audit_logs
    - sessions
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

## Role-Based Access Control

### Roles Query

Dynamically assign roles based on user attributes.

```yaml
# The query receives $user_id as a parameter
# Column names become context values for role matching
roles_query: "SELECT role, org_id FROM user_roles WHERE user_id = $user_id:bigint"
```

### Role Configuration

| Option | Type | Description |
|--------|------|-------------|
| `name` | string | Role name (e.g., `user`, `admin`, `anon`) |
| `match` | string | SQL condition to match role (uses roles_query columns) |
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

GraphJin supports querying across multiple databases in a single GraphQL request.

### Database Map Structure

```yaml
databases:
  primary:
    type: postgres
    default: true
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
    default: true
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
| `GO_ENV` | Config file selection |
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
production: true
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
    default: true
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
| `production` | `false` | `true` |
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
