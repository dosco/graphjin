# GraphJin Discovery Mechanism

This note visualizes how GraphJin discovers database structure, enriches it with config, turns it into a relationship graph, and exposes higher-level discovery APIs.

## 1. Core Initialization Flow

```mermaid
flowchart TD
    A["NewGraphJin(conf, db)"] --> B["newGraphJin"]
    B --> C["initConfig"]
    B --> D["Create database contexts"]
    D --> E["discoverAllDatabases"]
    E --> F["discoverDatabase(ctx)"]

    F --> G{"Schema source?"}
    G -->|MockDB or prod EnableSchema| H["Read db.graphql"]
    H --> I["qcode.ParseSchema"]
    I --> J["sdata.NewDBInfo"]

    G -->|Live DB connection| K["sdata.GetDBInfo"]
    K --> J

    J --> L["ctx.dbinfo"]
    L --> M["initResolvers"]
    M --> N["finalizeAllDatabases"]
    N --> O["finalizeDatabaseSchema"]

    O --> P["Apply config tables, columns, FKs, functions"]
    P --> Q["sdata.NewDBSchema"]
    Q --> R["Build relationship graph"]
    R --> S["Create qcode.Compiler"]
    R --> T["Create psql.Compiler"]
    S --> U["GraphQL queries can compile"]
    T --> U
```

Key files:

- `core/api.go`
- `core/init_multidb.go`
- `core/init.go`
- `core/internal/sdata/tables.go`
- `core/internal/sdata/schema.go`

## 2. Raw Catalog Discovery

```mermaid
flowchart TD
    A["sdata.GetDBInfo(ctx, db, dbtype, blocklist)"] --> B["Retry wrapper"]
    B --> C["getDBInfoOnce"]

    C --> D["DB info query"]
    C --> E["DiscoverColumns"]
    C --> F["DiscoverFunctions"]

    D --> D1["Version, database name, default schema"]

    E --> E1{"Dialect"}
    E1 --> E2["Postgres/MySQL/SQLite/Oracle/MSSQL/Snowflake SQL"]
    E1 --> E3["MongoDB JSON introspection DSL"]

    E2 --> E4["Scan columns"]
    E3 --> E4
    E4 --> E5["Normalize identifiers"]
    E5 --> E6["Merge duplicate catalog rows"]
    E6 --> E7["Detect PK, unique, arrays, full text, FKs"]
    E7 --> E8["Skip internal and blocklisted columns"]
    E8 --> E9["View primary-key enrichment"]

    E9 --> G{"Composite FK candidates?"}
    G -->|Yes| H["DiscoverCompositeFKs"]
    G -->|No| I["Skip expensive composite FK query"]
    H --> J["Attach composite FK metadata"]
    I --> J

    F --> F1["Scan database functions"]
    F1 --> K["NewDBInfo"]
    J --> K
    D1 --> K
    K --> L["Tables, columns, functions, hash"]
```

Key files:

- `core/internal/sdata/tables.go`
- `core/internal/sdata/sql.go`
- `core/internal/sdata/sql/*_columns.sql`
- `core/internal/sdata/sql/*_info.sql`
- `core/internal/sdata/sql/mongodb_*.json`

## 3. Relationship Graph Build

```mermaid
flowchart TD
    A["DBInfo from catalog or db.graphql"] --> B["finalizeDatabaseSchema"]
    B --> C["Normalize configured table names"]
    C --> D["ensureDiscoveredTablesInConfig"]
    D --> E["addTables"]
    D --> F["addForeignKeys"]
    D --> G["addFullTextColumns"]
    D --> H["addFunctions"]

    E --> E1["Regular tables"]
    E --> E2["JSON virtual tables"]
    E --> E3["Polymorphic virtual tables"]
    F --> F1["Declared same-DB FKs"]
    F --> F2["Declared cross-DB FKs"]

    E1 --> I["sdata.NewDBSchema"]
    E2 --> I
    E3 --> I
    F1 --> I
    F2 --> I
    G --> I
    H --> I

    I --> J["Add graph nodes"]
    J --> K["Add aliases"]
    K --> L["addRels for each table"]

    L --> M["Column FKs"]
    L --> N["JSON embedded relationships"]
    L --> O["Virtual table relationships"]
    L --> P["Remote/cross-DB relationships"]

    M --> Q{"Relationship type"}
    Q -->|Referenced col unique| R["One-to-one"]
    Q -->|Referenced col not unique| S["One-to-many"]
    Q -->|Self-reference| T["Recursive"]
    N --> U["Embedded"]
    O --> V["Polymorphic"]
    P --> W["Stored as crossDBRels"]

    R --> X["addToGraph"]
    S --> X
    T --> X
    U --> X
    V --> X
    X --> Y["Forward and reverse edges"]
    W --> Z["FindCrossDBPath fallback"]
    Y --> AA["QCode FindPath"]
    Z --> AA
```

Key files:

- `core/init.go`
- `core/internal/sdata/schema.go`
- `core/internal/sdata/dwg.go`
- `core/internal/qcode/qcode.go`

## 4. Runtime Schema Watcher

```mermaid
flowchart TD
    A["initDBWatcher"] --> B{"Production mode?"}
    B -->|Yes| C["Watcher disabled"]
    B -->|No| D{"Poll duration valid?"}
    D -->|Too small| E["Disable or coerce poll interval"]
    D -->|Valid| F["startDBWatcher"]

    F --> G["Poll each database"]
    G --> H["sdata.GetDBInfo"]
    H --> I{"ctx.schema is nil?"}
    I -->|Yes and tables discovered| J["Reload schema"]
    I -->|No| K{"latestDi.Hash differs?"}
    K -->|Yes| J
    K -->|No| L["No-op"]

    J --> M["Re-finalize database schema"]
    M --> N["Rebuild compilers"]
    N --> O["Fire schema callbacks"]
```

Key file:

- `core/watcher.go`

Note: while reading this path, I noticed `NewDBInfo` appears to assign `di.hash = h.Size()` rather than the digest value. Since `h.Size()` is constant for the hash algorithm, normal watcher change detection may not notice catalog changes.

## 5. Service Discovery Layer

```mermaid
flowchart TD
    A["GraphJin core schema APIs"] --> B["serv.DiscoveryManager"]
    B --> C["Per-database lazy caches"]
    C --> D["tablesCache"]
    C --> E["fullCache"]
    C --> F["insightsCache"]
    C --> G["profileCache"]

    D --> D1["GetTablesForDatabase"]
    D1 --> D2["Cheap table index"]
    D2 --> D3["name, schema, database, type, comment, column_count"]

    E --> E1["getSchemas"]
    E1 --> E2["GetTableSchemaForDatabaseSchema"]
    E2 --> E3["Schema-only table details"]

    F --> F1["getSchemas"]
    F1 --> F2["Graph/schema insights"]
    F2 --> F3["Hub tables, paths, duplicates, query templates"]

    G --> G1["get_table_sample"]
    G1 --> G2["Single table live queries"]
    G2 --> G3["Approx row count, date/numeric/enum stats, sample rows"]

    B --> H["singleflight per cache key"]
    B --> I["Invalidate all caches on schema change"]
    B --> J["Subscribe"]
    J --> K["Send payload lazily"]
```

Key files:

- `serv/discovery_cache.go`
- `serv/discovery_gen.go`
- `serv/discovery_sample.go`
- `serv/discovery_schema.go`
- `serv/discovery_rowcount.go`
- `serv/discovery_types.go`

The important performance boundary is that `NewDiscoveryManager` only registers schema-change invalidation. It does not build table indexes, full schemas, insights, or profiles at GraphJin startup.

Current lazy discovery contract:

| Tool or endpoint | Cost | Purpose |
| :--- | :--- | :--- |
| `list_tables(search, schema, limit, cursor, database)` | Cheap, in-memory | Paginated table index. No row counts or data profiling. |
| `describe_table(table, schema, database)` | Cheap, in-memory | One table schema only. Does not profile data. |
| `find_path(from, to, database)` | Cheap, graph-only | Relationship path between tables. |
| `get_schema_insights(database)` | Cheap, schema/graph-only | Hub tables, duplicate warnings, templates, overview/functions. |
| `explore_relationships` | Legacy, graph-only | Relationship graph exploration. |
| `get_table_sample(table, schema, database, mode)` | Expensive, live data | The only discovery tool allowed to run profiling/sample queries. |
| `discover_databases` | Separate onboarding | Finds database connection candidates, not schema details. |
| `reload_schema` | Separate mutating tool | Reloads GraphJin schema state. |

MCP uses the protocol-defined `outputSchema` field for tool metadata. The app-level discovery schema contract is available at `/api/v1/discovery/schema`.

## 6. MCP Database Discovery

This is separate from schema introspection. It discovers database endpoints and candidate connection configs.

```mermaid
flowchart TD
    A["MCP discover_databases"] --> B["runDiscovery"]
    B --> C["Parse options"]
    C --> D["TCP port probes"]
    C --> E["Unix socket checks"]
    C --> F["SQLite file scan"]
    C --> G["Explicit targets"]
    C --> H["Docker detection"]

    D --> I["Deduplicate candidates"]
    E --> I
    F --> I
    G --> I
    H --> I

    I --> J["Connection probing"]
    J --> K["Try default/user credentials"]
    J --> L["SQLite open"]
    J --> M["MongoDB probe"]
    J --> N["Snowflake connection string"]

    K --> O["Filter system DBs"]
    L --> O
    M --> O
    N --> O

    O --> P["Enrich, rank, sort"]
    P --> Q["Return candidates and config snippets"]
```

Key file:

- `serv/mcp_discover.go`

## Mental Model

```mermaid
flowchart LR
    A["Database catalog or db.graphql"] --> B["DBInfo"]
    B --> C["Config enrichment"]
    C --> D["DBSchema"]
    D --> E["Relationship graph"]
    E --> F["QCode path finding"]
    F --> G["SQL or MongoDB DSL compiler"]
    D --> H["Service discovery payloads"]
    D --> I["Schema watcher reloads"]
```

In short: catalog introspection creates `DBInfo`; config and resolvers enrich it; `NewDBSchema` creates the relationship graph; QCode uses that graph to plan joins; and the service layer packages the same discovered structure for assistant/MCP-facing discovery features.
