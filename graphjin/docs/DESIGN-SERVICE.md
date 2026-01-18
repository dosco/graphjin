# GraphJin Service Layer Design & Architecture

The `serv` package provides a production-ready HTTP service wrapper around the GraphJin core compiler. It exposes GraphQL, REST, and WebSocket endpoints while adding enterprise features like hot deployment, rate limiting, authentication, and observability.

## High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                      HTTP Request                                │
└───────────────────────────┬─────────────────────────────────────┘
                            ▼
┌─────────────────────────────────────────────────────────────────┐
│                   HttpService (atomic.Value)                     │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │                   Middleware Stack                          ││
│  │  ┌─────────┐ ┌──────┐ ┌──────┐ ┌───────────┐ ┌──────────┐  ││
│  │  │ Server  │→│ Auth │→│ CORS │→│ RateLimit │→│   GZip   │  ││
│  │  │ Header  │ │      │ │      │ │           │ │          │  ││
│  │  └─────────┘ └──────┘ └──────┘ └───────────┘ └──────────┘  ││
│  └─────────────────────────────────────────────────────────────┘│
│  ┌─────────────────────────────────────────────────────────────┐│
│  │                    graphjinService                          ││
│  │  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌────────────────┐ ││
│  │  │  Config  │ │  Logger  │ │ Database │ │ core.GraphJin  │ ││
│  │  └──────────┘ └──────────┘ └──────────┘ └────────────────┘ ││
│  └─────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────┘
                            │
        ┌───────────────────┼───────────────────┐
        ▼                   ▼                   ▼
┌───────────────┐  ┌───────────────┐  ┌───────────────┐
│   GraphQL     │  │     REST      │  │  WebSocket    │
│  /api/v1/     │  │  /api/v1/     │  │  Subscription │
│   graphql     │  │   rest/*      │  │               │
└───────────────┘  └───────────────┘  └───────────────┘
```

## Core Components

### 1. Service Structure (`api.go`)

**HttpService** - Thread-safe wrapper using `atomic.Value` for zero-downtime config reloading:
```go
type HttpService struct {
    atomic.Value              // Stores *graphjinService
    opt         []Option
    confPath    string
}
```

**graphjinService** - Internal runtime state:
```go
type graphjinService struct {
    conf         *Config           // Configuration
    db           *sql.DB           // Database connection
    gj           *core.GraphJin    // Core compiler instance
    log          *zap.SugaredLogger
    fs           core.FS           // Virtual filesystem
    tracer       trace.Tracer      // OpenTelemetry tracer
    asec         [32]byte          // SHA256 admin secret
    srv          *http.Server
}
```

### 2. Configuration System (`config.go`)

Three-layer configuration structure:
- **Core**: Embedded `core.Config` for compiler settings
- **Serv**: Service-specific settings (networking, logging, auth)
- **Admin**: Hot deployment settings

Key configuration fields:
| Section | Field | Purpose |
|---------|-------|---------|
| Serv | `HostPort` | Server bind address (default: `0.0.0.0:8080`) |
| Serv | `Production` | Enable production mode |
| Serv | `WebUI` | Serve embedded React UI |
| Serv | `WatchAndReload` | File watching in development |
| Serv | `RateLimiter` | IP-based rate limiting config |
| Admin | `HotDeploy` | Enable database-backed deployment |
| Admin | `AdminSecretKey` | Secret for admin API access |

Configuration loading via Viper supports YAML/TOML/JSON with environment variable overrides (`GJ_` prefix).

### 3. HTTP Server (`serv.go`, `routes.go`)

**Initialization** (`startHTTP`):
- Creates chi router with middleware
- Configures timeouts (10s read/write)
- Sets up graceful shutdown via signal handling
- Registers cleanup callbacks for database and services

**Route Registration** (`routesHandler`):
| Route | Handler | Purpose |
|-------|---------|---------|
| `/health` | `healthCheckHandler` | Database connectivity check |
| `/api/v1/graphql` | `apiV1GraphQL` | GraphQL endpoint |
| `/api/v1/rest/*` | `apiV1Rest` | REST endpoint (operation in path) |
| `/api/v1/openapi.json` | `openAPIHandler` | OpenAPI 3.0 specification |
| `/api/v1/deploy` | `adminDeployHandler` | Hot deployment (admin) |
| `/api/v1/deploy/rollback` | `adminRollbackHandler` | Rollback config (admin) |
| `/*` | `webuiHandler` | Embedded React UI |

### 4. Middleware Stack (`http.go`)

Request processing pipeline (innermost to outermost):
1. **Base Handler** - GraphQL/REST request processing
2. **Authentication** - Pluggable auth handler with `AuthFailBlock` option
3. **CORS** - Configurable allowed origins/headers
4. **ETag** - HTTP caching with `If-None-Match` support
5. **Rate Limiting** - Per-IP token bucket algorithm
6. **GZip Compression** - Optional response compression (level 6)

### 5. GraphQL Handler (`http.go:apiV1GraphQL`)

Request flow:
1. Check for WebSocket upgrade → delegate to `apiV1Ws`
2. Parse request body (POST) or query params (GET)
3. Extract APQ (Automatic Persisted Query) key if present
4. Inject header variables into context
5. Call `s.gj.GraphQL(ctx, query, vars, &rc)`
6. Process response with caching headers and logging

Subscription operations over HTTP return 400 error (must use WebSocket).

### 6. REST Handler (`http.go:apiV1Rest`)

Converts REST-style requests to GraphQL:
- Extracts operation name from URL path (`/api/v1/rest/{operation}`)
- Maps URL query parameters to GraphQL variables
- Calls `s.gj.GraphQLByName(ctx, operationName, vars, &rc)`

### 7. WebSocket Subscriptions (`ws.go`)

Implements `graphql-ws` and `graphql-transport-ws` protocols:

**Connection Lifecycle**:
1. HTTP upgrade with gorilla/websocket
2. `connection_init` message with optional auth payload
3. `start`/`subscribe` messages create subscriptions
4. Server streams `data`/`next` messages with results
5. `complete`/`stop` messages end subscriptions

**wsConn Structure**:
```go
type wsConn struct {
    sessions  map[string]wsState  // Active subscriptions by ID
    conn      *websocket.Conn
    connMutex sync.Mutex          // Thread-safe writes
    done      chan bool
}
```

**Message Types**:
| Client → Server | Server → Client |
|-----------------|-----------------|
| `connection_init` | `connection_ack` |
| `start` / `subscribe` | `data` / `next` |
| `complete` / `stop` | `error` |

### 8. Web UI (`webui.go`, `web/`)

Embedded React application using Go's `embed` package:
- Static files served from `web/build`
- Root path redirects to include GraphQL endpoint parameter
- Production-ready build with source maps

### 9. Hot Deployment System (`deploy.go`, `admin.go`)

Database-backed configuration management for zero-downtime updates:

**Storage Schema** (`_graphjin` schema):
```sql
CREATE TABLE _graphjin.configs (
    id          SERIAL PRIMARY KEY,
    previous_id INTEGER,          -- Rollback reference
    name        TEXT UNIQUE,      -- Config name
    hash        TEXT UNIQUE,      -- SHA256 of bundle
    active      BOOLEAN,          -- Currently active
    bundle      TEXT              -- Base64 zip archive
);
```

**Deployment Flow**:
1. Admin client bundles config directory as base64 zip
2. POST to `/api/v1/deploy` with admin secret
3. Server validates, deactivates previous config
4. Stores new bundle, marks as active
5. Hot-deploy watcher detects change (10s polling)
6. Atomically swaps service instance via `atomic.Value`

**Rollback**: Reverts to `previous_id` configuration atomically.

**Admin Security** (`isAdminSecret`):
- SHA256 hash comparison
- Random 2-4 second delay (timing attack mitigation)
- Maximum 2 concurrent admin requests

### 10. File Watching (`filewatch.go`)

Development hot-reload via fsnotify:
- Watches `./config` directory for `.yaml`/`.toml`/`.json` changes
- Validates new config before applying
- Uses `syscall.Exec()` for graceful process replacement
- Disabled in production mode

### 11. Database Layer (`db.go`)

**Supported Databases**:
- PostgreSQL (via pgx/v5)
- MySQL

**Connection Features**:
- Connection string parsing with auto-detection
- Connection pooling (max idle, max open, lifetime)
- Retry logic with exponential backoff
- TLS support with client certificates (PostgreSQL)
- Schema selection via `search_path`

### 12. Rate Limiting (`iplimiter.go`)

Per-IP rate limiting using token bucket algorithm:
- Configurable rate (requests/second) and bucket (burst size)
- IP extraction: Custom header → `X-Forwarded-For` → `RemoteAddr`
- 5-minute TTL cache for rate limiters
- Returns 429 Too Many Requests when exceeded

### 13. Telemetry (`telemetry.go`)

OpenTelemetry integration:
- Trace context propagation (W3C TraceContext + Baggage)
- Span creation with service metadata
- `AlwaysSample` strategy for development

### 14. Admin Client (`client.go`)

Programmatic deployment client:
```go
client := serv.NewAdminClient(endpoint, secret)
client.Deploy(configName, configPath)  // Deploy new config
client.Rollback()                       // Revert to previous
```

Bundle creation excludes `seed.js` and `migrations/` directory.

### 15. Filesystem Abstraction (`afero.go`)

Virtual filesystem interface for hot deployment:
- Wraps `afero.Fs` for storage abstraction
- Supports in-memory filesystems (from zip bundles)
- Methods: `Get`, `Put`, `Exists`, `List`

## Request Flow

```
HTTP Request
    │
    ▼
┌─────────────────────┐
│   setServerHeader   │  ← Adds "Server: GraphJin"
└─────────────────────┘
    │
    ▼
┌─────────────────────┐
│    Chi Router       │  ← Route matching
└─────────────────────┘
    │
    ├─── /health ────────────► healthCheckHandler (DB ping)
    │
    ├─── /api/v1/graphql ────► apiV1Handler middleware chain
    │                              │
    │                              ▼
    │                         apiV1GraphQL
    │                              │
    │                              ├─── WebSocket? ──► apiV1Ws
    │                              │
    │                              ▼
    │                         s.gj.GraphQL()
    │                              │
    │                              ▼
    │                         responseHandler
    │                              │
    │                              ▼
    │                         JSON Response
    │
    ├─── /api/v1/rest/* ─────► apiV1Rest
    │                              │
    │                              ▼
    │                         s.gj.GraphQLByName()
    │
    └─── /* ─────────────────► webuiHandler (static files)
```

## Key Design Patterns

1. **Atomic Service Reloading**: `atomic.Value` enables zero-downtime config updates
2. **Middleware Composition**: Each concern (auth, CORS, rate limiting) is isolated
3. **Options Pattern**: Service customization via functional options
4. **Graceful Shutdown**: Signal handling with cleanup callbacks
5. **Database Transactions**: Serializable isolation for admin operations

## Configuration Reference

### Service Configuration (Serv)
```yaml
app_name: "MyApp"
host_port: "0.0.0.0:8080"
production: false
log_level: "info"          # debug, info, warn, error
log_format: "json"         # json, simple
web_ui: true
enable_tracing: true
watch_and_reload: true     # Dev only
http_gzip: true
server_timing: false
auth_fail_block: false

allowed_origins: ["*"]
allowed_headers: []
debug_cors: false

rate_limiter:
  rate: 100.0              # Requests per second
  bucket: 20               # Burst size
  ip_header: ""            # Custom IP header

auth:
  type: "jwt"
  cookie: "session"
  development: false

database:
  type: "postgres"
  host: "localhost"
  port: 5432
  dbname: "mydb"
  user: "postgres"
  password: ""
  pool_size: 10
  max_connections: 50
  ping_timeout: "5s"
```

### Admin Configuration
```yaml
admin:
  hot_deploy: true
  admin_secret_key: "your-secret-key"
```

## File Reference

| File | Purpose |
|------|---------|
| `serv.go` | HTTP server initialization, graceful shutdown |
| `api.go` | Service structure, NewGraphJinService, options |
| `config.go` | Configuration loading (Viper), defaults |
| `routes.go` | Route registration |
| `http.go` | Request handlers, middleware, GraphQL/REST |
| `ws.go` | WebSocket subscription handling |
| `webui.go` | Embedded React UI serving |
| `db.go` | Database connection, pooling, TLS |
| `deploy.go` | Hot deployment, bundle management |
| `admin.go` | Admin API handlers, secret validation |
| `filewatch.go` | Config file watching, dev hot reload |
| `health.go` | Health check endpoint |
| `iplimiter.go` | IP-based rate limiting |
| `telemetry.go` | OpenTelemetry initialization |
| `client.go` | Admin deployment client |
| `afero.go` | Virtual filesystem abstraction |
| `migrate.go` | Admin schema migrations |
| `internal/etags/` | HTTP ETag caching middleware |
| `internal/util/` | Logger and Viper utilities |
| `web/` | React Web UI source and build |
