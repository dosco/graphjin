# Multi-Database Support Design

This document describes the design for supporting multiple databases within a single GraphJin instance, enabling polyglot data architectures where data can be fetched from PostgreSQL, MySQL, SQLite, MongoDB, and other supported databases in a single GraphQL query.

> **Implementation Note**: The recommended approach extends the existing remote join infrastructure for both root-level parallel queries and hierarchical cross-DB relationships. See [Critical Architecture Insights](#critical-architecture-insights) for details.

## 1. Motivation

Modern applications often have data distributed across multiple databases:

- **Domain separation**: Users in PostgreSQL, analytics events in MongoDB, audit logs in SQLite
- **Legacy integration**: New services querying existing databases that cannot be consolidated
- **Specialized storage**: Relational data in PostgreSQL, document data in MongoDB, time-series in TimescaleDB
- **Read replicas**: Routing read queries to replicas for horizontal scaling

Currently, GraphJin is architected for a single database per engine instance. To query multiple databases, users must:
1. Run separate GraphJin instances per database
2. Implement their own federation/stitching layer
3. Make multiple round-trips from the client

This design enables querying multiple databases within a single GraphQL request, with automatic parallel execution and result merging.

## 2. Use Cases

### 2.1 Root-Level Parallel Queries

Independent queries to different databases executed concurrently:

```graphql
query DashboardData {
  users {                    # PostgreSQL
    id
    name
  }
  recent_events {            # MongoDB
    type
    timestamp
  }
  audit_logs(limit: 10) {    # SQLite
    action
    created_at
  }
}
```

**Execution**: All three queries run in parallel, results merged into a single response.

### 2.2 Hierarchical Cross-Database Queries

Nested queries where child data depends on parent results:

```graphql
query UserWithOrders($id: ID!) {
  user(id: $id) {            # PostgreSQL
    id
    name
    orders {                 # MongoDB - fetched using user.id
      id
      total
      items {
        product_name
        quantity
      }
    }
  }
}
```

**Execution**:
1. Fetch user from PostgreSQL
2. Extract user ID
3. Query MongoDB for orders with matching user_id
4. Merge orders into user response

### 2.3 Table Name Disambiguation

When multiple databases have tables with the same name:

```graphql
query {
  users @source("main") {      # PostgreSQL users table
    id
    name
  }
  users @source("analytics") { # MongoDB users collection
    id
    activity_score
  }
}
```

## 3. Current Architecture

### 3.1 Single-Database Design

The current `graphjinEngine` is tightly coupled to a single database:

```go
// core/core.go - Current implementation
type graphjinEngine struct {
    conf           *Config
    db             *sql.DB           // Single database connection
    dbtype         string            // Single database type
    dbinfo         *sdata.DBInfo     // Single schema metadata
    schema         *sdata.DBSchema   // Single processed schema
    qcodeCompiler  *qcode.Compiler   // Single query compiler
    psqlCompiler   *psql.Compiler    // Single SQL compiler
    // ...
}
```

### 3.2 Query Flow

```
GraphQL Request
       │
       ▼
┌─────────────────┐
│  Fast Parse     │  Determine operation type/name
└─────────────────┘
       │
       ▼
┌─────────────────┐
│  QCode Compile  │  GraphQL → Intermediate Representation
└─────────────────┘
       │
       ▼
┌─────────────────┐
│  SQL Compile    │  IR → SQL (dialect-specific)
└─────────────────┘
       │
       ▼
┌─────────────────┐
│  Execute SQL    │  Single database execution
└─────────────────┘
       │
       ▼
     JSON Response
```

### 3.3 Existing Remote Join Infrastructure

GraphJin already supports "remote joins" via HTTP (`core/remote_join.go`):

```go
// Simplified flow
func (s *gstate) execRemoteJoin(c context.Context) error {
    // 1. Extract parent IDs from result
    fids, sfmap, err := s.parentFieldIds()
    from := jsn.Get(s.data, fids)

    // 2. Fetch data from remote HTTP endpoints
    to, err := s.resolveRemotes(c, from, sfmap)

    // 3. Merge remote data into response
    jsn.Replace(&ob, s.data, from, to)
}
```

This pattern can be extended for cross-database joins.

---

## Critical Architecture Insights

After deep analysis of the GraphJin codebase, several architectural constraints significantly impact the multi-database design:

### Insight 1: SQL Compilation Produces Single Nested Statement

The `psqlCompiler.Compile()` produces a **single SQL statement with nested JSON functions**:

```sql
SELECT json_object(
  'users', (SELECT json_agg(...) FROM users WHERE ...),
  'orders', (SELECT json_agg(...) FROM orders WHERE orders.user_id = users.id)
)
```

**Impact**: You **cannot split this SQL** after compilation because child queries reference parent columns via LATERAL JOIN. Query splitting must happen at **QCode level BEFORE SQL generation**.

### Insight 2: Each Database Needs Its Own Compiler

The `psqlCompiler` is initialized with a single dialect and schema. Compilers are tightly coupled to their database context.

**Impact**: Each `dbContext` needs both its own `qcodeCompiler` (for schema validation) and `psqlCompiler` (for dialect-specific SQL).

### Insight 3: Mixed Same-DB and Cross-DB Children

A parent might have children in BOTH the same DB and different DBs:

```graphql
users {           # PostgreSQL
  posts { ... }   # PostgreSQL (same DB - use SQL JOIN)
  orders { ... }  # MongoDB (different DB - need database join)
}
```

**Impact**: During QCode analysis, children must be categorized:
- **Same-DB children**: Keep in QCode, compile into single SQL with JOINs
- **Cross-DB children**: Extract, mark with `SkipRender`, handle like remote joins

### Insight 4: Remote Join Pattern is the Key

The existing remote join infrastructure (`core/remote_join.go`) already implements:
1. Execute parent query with placeholders
2. Extract IDs using `jsn.Get()`
3. Fetch child data (currently via HTTP)
4. Merge using `jsn.Replace()`

**Impact**: Cross-database joins can reuse this entire pattern, replacing HTTP calls with in-process database queries.

### Insight 5: Relationship Graph Assumes Single Database

The `relationshipGraph` in `DBSchema` uses weighted path-finding for table relationships. Cross-DB relationships cannot be SQL JOINed.

**Impact**: Cross-DB edges need special weight (e.g., 1000) to trigger "database join" instead of SQL join.

### Insight 6: Root-Level Merge is Different from Nested Replace

The `jsn.Replace()` function is designed for **nested replacements** - it matches on key + value hash and replaces specific nested fields. However, **root-level parallel queries** produce separate JSON objects that need to be **concatenated**, not replaced:

```json
// From DB1: {"users": [...]}
// From DB2: {"events": [...]}
// From DB3: {"logs": [...]}
// Need: {"users": [...], "events": [...], "logs": [...]}
```

**Impact**: Root-level results require a different merging approach - either JSON object concatenation or parsing as `map[string]json.RawMessage` and combining. The `jsn.Replace()` pattern only applies to hierarchical cross-DB queries.

### Insight 7: QCode Already Has Remote Infrastructure

The codebase already contains infrastructure for cross-database-like operations:

```go
// qcode/qcode.go
type SkipType int
const (
    SkipTypeNone SkipType = iota
    SkipTypeRemote  // Already exists for remote joins!
)

type QCode struct {
    Selects []Select
    Roots   []int32
    Remotes int32    // Counter of remote selections already exists!
}
```

The `core/remote_join.go` already implements:
- `parentFieldIds()` - finds fields marked as remote
- `resolveRemotes()` - parallel HTTP calls using `sync.WaitGroup`
- Pattern: `jsn.Get()` extracts IDs → remote fetch → `jsn.Replace()` merges

**Impact**: Cross-DB joins can reuse this entire infrastructure! Add `SkipTypeDatabaseJoin` alongside `SkipTypeRemote`, and extend `resolveRemotes()` pattern for in-process DB calls.

### Insight 8: Child QCode Extraction Complexity

To execute a cross-DB child query, we need to build a **new QCode** for the target database. Two approaches:

**Option A: Extract and Remap IDs (Complex)**
- Copy Select to new QCode
- Remap all ID references (ParentID, Children, expression IDs)
- Handle nested expression references

**Option B: Build Fresh QCode (Simpler - Recommended)**
- Treat cross-DB child as a new root query
- Use child Select's table/columns to construct query parameters
- Compile fresh with target DB's compiler

Option B is recommended because it:
1. Leverages existing compilation pipeline
2. Avoids complex ID remapping
3. Is easier to test and maintain

### Insight 9: CRITICAL - Query Cache Key Missing Database Identifier

The compiled query cache key is currently `namespace + name + role` only:

```go
func (s *gstate) key() string {
    return s.r.namespace + s.r.name + s.role  // NO database identifier!
}
```

**Impact**: If you have `db1` and `db2`, a query named `getUsers` with role `user` will share the same cached compiled SQL across databases - **completely incorrect** if they have different schemas or dialects. This will cause silent data corruption or query failures.

**Solution**: Include database identifier(s) in cache key:
```go
func (s *gstate) key() string {
    dbs := s.involvedDatabases()
    sort.Strings(dbs)
    return s.r.namespace + s.r.name + s.role + strings.Join(dbs, ",")
}
```

### Insight 10: Subscription Cache Has Same Problem

Subscriptions use a similar cache pattern in `gj.subs` sync.Map with the same key format. Multi-database subscriptions will cache incorrectly, potentially returning wrong data or using wrong SQL.

**Solution**: Apply same fix - include database identifier in subscription cache key.

### Insight 11: Tracing and Logging Need Database Context

Current span attributes and logging have no database identifier:

```go
// Spans - no database info
span.SetAttributesString(
    StringAttr{"query.namespace", s.r.namespace},
    StringAttr{"query.name", cs.st.qc.Name},
    StringAttr{"query.role", cs.st.role})  // Missing: query.database

// Logging - no database info
gj.log.Printf(errSubs, "query", err)  // Which database failed?
```

**Impact**: With multi-DB, impossible to debug which database an operation ran against or which database failed.

**Solution**:
1. Add `query.database` attribute to all spans
2. Include database name in all log messages
3. Consider structured logging with database context

### Insight 12: Mutation Execution Strategy Complexity

Mutations use TWO different execution paradigms based on dialect:

1. **CTE-Based (PostgreSQL, MariaDB 10.5+)**: Single atomic WITH statement
2. **Linear Execution (MySQL, SQLite, Oracle, MSSQL)**: Multi-statement scripts with:
   - Temp tables (`_gj_ids` for SQLite)
   - Session variables (`@var` for MySQL)
   - Statement-by-statement execution with `-- @gj_ids=table_0` hints
   - Parameter slicing across statements

**Impact**: Multi-DB mutations targeting different dialect databases simultaneously would require orchestrating BOTH strategies in one request - extremely complex.

**Recommendation**: The design decision to keep mutations single-database is correct and should be enforced. Cross-database mutations should error explicitly.

---

## Recommended Implementation

Based on these insights, extend the existing remote join infrastructure for **both** query patterns in a single implementation:

### Why This Approach

1. **Minimal changes**: Reuses proven patterns (`jsn.Get`, `jsn.Replace`, parallel execution)
2. **Lower risk**: Existing remote join code is battle-tested
3. **Backward compatible**: Single-DB configs work unchanged
4. **Same infrastructure**: Both patterns need the same `dbContext` setup

### How It Works

**Root-Level Parallel Queries:**
```
Query Analysis → Group roots by database → Execute in parallel (goroutines) → Merge results
```

**Hierarchical Cross-DB Queries:**
```
Parent Query (DB1) → Extract IDs → In-process query to DB2 → Merge via jsn.Replace()
```

Both use the same infrastructure and leverage existing patterns from `remote_join.go`.

### Implementation Steps

1. **Configuration**: Add `databases` map to config, table-to-database mapping
2. **Database Contexts**: Each DB gets its own `dbContext` with schema + compilers
3. **Schema Merging**: Add `Database` field to `DBTable`, merge into unified schema
4. **QCode Marking**: Tag each Select with database, mark cross-DB children with `SkipTypeDatabaseJoin`
5. **Parallel Root Execution**: New `executeMultiDBRoots()` using goroutines
6. **Database Join Execution**: New `execDatabaseJoins()` extending remote join pattern

### Key Code Additions

**Parallel Root Execution:**
```go
// core/gstate.go - for root-level queries to different DBs
func (s *gstate) executeMultiDBRoots(c context.Context) error {
    byDB := s.groupRootsByDatabase()

    var wg sync.WaitGroup
    results := make([]dbResult, len(byDB))

    for i, group := range byDB {
        wg.Add(1)
        go func(idx int, g dbGroup) {
            defer wg.Done()
            ctx := s.gj.databases[g.database]
            results[idx].data, results[idx].err = ctx.execute(c, g.selects)
        }(i, group)
    }

    wg.Wait()
    return s.mergeRootResults(results)
}
```

**Hierarchical Cross-DB Joins:**
```go
// core/database_join.go (new file)
func (s *gstate) execDatabaseJoins(c context.Context) error {
    // Reuse remote join pattern
    fids, sfmap := s.parentFieldIds()  // Find cross-DB fields
    from := jsn.Get(s.data, fids)      // Extract parent IDs

    // For each cross-DB relationship
    for i, id := range from {
        sel := sfmap[string(id.Key)]
        targetDB := s.gj.databases[sel.Rel.TargetDB]

        // Build and execute child query
        childData := targetDB.executeChildQuery(c, sel, id.Value)

        to[i] = jsn.Field{Key: sel.FieldName, Value: childData}
    }

    // Merge child data into parent result
    jsn.Replace(&ob, s.data, from, to)
    s.data = ob.Bytes()
    return nil
}
```

### Files to Modify

| File | Change |
|------|--------|
| `core/config.go` | Add `Databases` map config |
| `core/core.go` | Add `dbContext` struct |
| `core/api.go` | Initialize multiple DB contexts |
| `core/internal/sdata/schema.go` | Add `Database` to `DBTable`, `RelDatabaseJoin` |
| `core/internal/qcode/qcode.go` | Add `SkipTypeDatabaseJoin`, `Database` to Select |
| `core/internal/qcode/parse.go` | Tag selects with database, detect cross-DB |
| `core/database_join.go` | **NEW**: Cross-DB join + parallel root execution |
| `core/gstate.go` | Add `executeMultiDBRoots()` + `execDatabaseJoins()` |
| `serv/db.go` | Support multiple connections |

---

## 4. Proposed Architecture

### 4.1 Multi-Database Engine

Refactor `graphjinEngine` to manage multiple database contexts:

```go
// core/core.go - Proposed changes
type graphjinEngine struct {
    conf       *Config
    databases  map[string]*dbContext  // Named database contexts
    defaultDB  string                  // Default database name
    schema     *sdata.DBSchema         // Merged schema (all databases)
    // ...
}

// New: Database context encapsulates per-database state
type dbContext struct {
    name       string
    db         *sql.DB                 // Connection pool (nil for MongoDB)
    mongoDb    *mongo.Database         // MongoDB client (nil for SQL)
    dbtype     string                  // postgres, mysql, mongodb, sqlite, etc.
    dbinfo     *sdata.DBInfo           // Raw schema for this database
    schema     *sdata.DBSchema         // Processed schema for this database
    compiler   *psql.Compiler          // Dialect-aware compiler
}
```

### 4.2 Configuration Schema

New multi-database configuration format:

```yaml
# config.yml
databases:
  main:                        # Database name (referenced in table mapping)
    type: postgres
    host: localhost
    port: 5432
    dbname: myapp
    user: postgres
    password: secret
    pool_size: 10
    default: true              # Queries route here if no explicit mapping

  analytics:
    type: mongodb
    host: localhost
    port: 27017
    dbname: analytics
    # MongoDB-specific options
    replica_set: rs0

  audit:
    type: sqlite
    path: /var/data/audit.db
    # SQLite-specific options
    busy_timeout: 5000

# Explicit table-to-database mapping
tables:
  - name: users
    database: main

  - name: products
    database: main

  - name: events
    database: analytics

  - name: user_sessions
    database: analytics

  - name: audit_logs
    database: audit

# Relationships can span databases
tables:
  - name: orders
    database: analytics
    columns:
      - name: user
        related_to: main.users.id   # Cross-database relationship
```

### 4.3 Backward Compatibility

Existing single-database configurations continue to work:

```yaml
# Legacy format - still supported
db_type: postgres
host: localhost
port: 5432
dbname: myapp
```

When legacy format is detected:
1. Create a single database context named "default"
2. All tables route to this database
3. Behavior is identical to current implementation

## 5. Schema Management

### 5.1 Per-Database Schema Discovery

Each database context runs independent schema discovery:

```go
func (gj *graphjinEngine) initDatabases() error {
    for name, conf := range gj.conf.Databases {
        ctx := &dbContext{name: name}

        // Initialize connection based on type
        switch conf.Type {
        case "mongodb":
            ctx.mongoDb = initMongoDB(conf)
        default:
            ctx.db = initSQLDB(conf)
        }

        // Discover schema for this database
        ctx.dbinfo, _ = sdata.GetDBInfo(ctx.db, conf.Type, ...)

        // Create dialect-specific compiler
        ctx.compiler = psql.NewCompiler(psql.Config{
            DBType: conf.Type,
            Schema: ctx.schema,
        })

        gj.databases[name] = ctx
    }
    return nil
}
```

### 5.2 Schema Merging

Tables from all databases are merged into a unified schema:

```go
// core/internal/sdata/schema.go - Additions
type DBTable struct {
    Name       string
    Database   string    // NEW: Source database name
    Schema     string
    Type       string
    Columns    []DBColumn
    PrimaryKey []string
    // ...
}

func MergeSchemas(contexts map[string]*dbContext) (*DBSchema, error) {
    merged := &DBSchema{
        Tables: make(map[string]*DBTable),
    }

    for dbName, ctx := range contexts {
        for tableName, table := range ctx.schema.Tables {
            // Check for conflicts
            if existing, ok := merged.Tables[tableName]; ok {
                // Table exists in multiple databases
                // Mark both as requiring @source directive
                existing.RequiresSource = true
                table.RequiresSource = true
            }

            // Copy table with database annotation
            tableCopy := *table
            tableCopy.Database = dbName
            merged.Tables[tableName] = &tableCopy
        }
    }

    return merged, nil
}
```

### 5.3 Table Name Conflicts

When the same table name exists in multiple databases:

1. **Startup validation**: Log warning about conflict
2. **Query validation**: Require `@source("db_name")` directive
3. **Error on ambiguity**: Return clear error if directive is missing

```go
// core/internal/qcode/parse.go
func (c *Compiler) resolveTable(name string, source string) (*sdata.DBTable, error) {
    tables := c.schema.FindTables(name)

    if len(tables) == 0 {
        return nil, fmt.Errorf("table not found: %s", name)
    }

    if len(tables) == 1 {
        return tables[0], nil
    }

    // Multiple tables with same name
    if source == "" {
        return nil, fmt.Errorf(
            "table '%s' exists in multiple databases (%v), use @source directive",
            name, tableDBNames(tables),
        )
    }

    // Find table in specified database
    for _, t := range tables {
        if t.Database == source {
            return t, nil
        }
    }

    return nil, fmt.Errorf("table '%s' not found in database '%s'", name, source)
}
```

## 6. Query Compilation

### 6.1 Database Annotation in QCode

Extend `Select` to track target database:

```go
// core/internal/qcode/qcode.go - Additions
type Select struct {
    ID        int32
    Type      SelType
    Table     string
    Database  string    // NEW: Target database for this select
    Cols      []Column
    Where     *Exp
    Children  []int32
    // ...
}
```

### 6.2 Compilation Strategy

During QCode compilation, each `Select` is annotated with its target database:

```go
// core/internal/qcode/parse.go
func (c *Compiler) compileSelect(node *graphql.Field) (*Select, error) {
    table, err := c.resolveTable(node.Name, getSourceDirective(node))
    if err != nil {
        return nil, err
    }

    sel := &Select{
        Table:    table.Name,
        Database: table.Database,  // Annotate with database
    }

    // Compile children
    for _, child := range node.SelectionSet {
        childSel, _ := c.compileSelect(child)
        sel.Children = append(sel.Children, childSel.ID)
    }

    return sel, nil
}
```

### 6.3 Query Splitting

After QCode compilation, queries are analyzed and split by database:

```go
// core/query_splitter.go (new file)
type QueryPlan struct {
    // Queries that can run in parallel (same depth level, different DBs)
    ParallelGroups []QueryGroup

    // Queries that must run sequentially (child depends on parent)
    DependentChains []DependentChain
}

type QueryGroup struct {
    Database string
    Selects  []*qcode.Select
}

type DependentChain struct {
    Parent   *qcode.Select
    Children []*qcode.Select  // Children from different database
    JoinKey  string           // Field to use for joining
}

func AnalyzeQuery(qc *qcode.QCode) *QueryPlan {
    plan := &QueryPlan{}

    // Group root-level selects by database
    rootsByDB := make(map[string][]*qcode.Select)
    for _, sel := range qc.Roots {
        rootsByDB[sel.Database] = append(rootsByDB[sel.Database], sel)
    }

    // All root groups can run in parallel
    for db, selects := range rootsByDB {
        plan.ParallelGroups = append(plan.ParallelGroups, QueryGroup{
            Database: db,
            Selects:  selects,
        })
    }

    // Identify cross-database dependencies
    for _, sel := range qc.AllSelects() {
        for _, childID := range sel.Children {
            child := qc.Select(childID)
            if child.Database != sel.Database {
                // Cross-database relationship
                plan.DependentChains = append(plan.DependentChains, DependentChain{
                    Parent:   sel,
                    Children: []*qcode.Select{child},
                    JoinKey:  findJoinKey(sel, child),
                })
            }
        }
    }

    return plan
}
```

## 7. Query Execution

### 7.1 Parallel Root Query Execution

Root-level queries to different databases execute concurrently:

```go
// core/gstate.go - Additions
func (s *gstate) executeMultiDB(c context.Context) error {
    plan := AnalyzeQuery(s.cs.st.qc)

    // Execute parallel groups concurrently
    results := make(chan dbResult, len(plan.ParallelGroups))
    var wg sync.WaitGroup

    for _, group := range plan.ParallelGroups {
        wg.Add(1)
        go func(g QueryGroup) {
            defer wg.Done()

            // Get database context
            dbCtx := s.gj.databases[g.Database]

            // Compile SQL for this database's dialect
            sql, md, _ := dbCtx.compiler.Compile(g.Selects)

            // Execute against this database
            var data []byte
            if dbCtx.mongoDb != nil {
                data, _ = executeMongoQuery(c, dbCtx.mongoDb, sql)
            } else {
                data, _ = executeSQLQuery(c, dbCtx.db, sql, md)
            }

            results <- dbResult{
                database: g.Database,
                data:     data,
                selects:  g.Selects,
            }
        }(group)
    }

    // Wait and collect results
    go func() {
        wg.Wait()
        close(results)
    }()

    // Merge results
    return s.mergeParallelResults(results)
}
```

### 7.2 Result Merging

There are **two distinct merging operations** required:

#### 7.2.1 Root-Level Merge (JSON Object Concatenation)

For parallel root queries, results need to be concatenated at the top level. This is **NOT** a replacement - it's combining separate JSON objects:

```go
// core/result_merger.go (new file)
func (s *gstate) mergeRootResults(results []dbResult) error {
    // Approach 1: Parse and combine (safer, handles edge cases)
    merged := make(map[string]json.RawMessage)
    for _, r := range results {
        if r.err != nil {
            return r.err
        }
        var resultObj map[string]json.RawMessage
        json.Unmarshal(r.data, &resultObj)
        for key, value := range resultObj {
            merged[key] = value
        }
    }
    s.data, _ = json.Marshal(merged)
    return nil

    // Approach 2: String concatenation (faster, more fragile)
    // Strip outer {} from each result, join with commas, wrap in {}
}
```

#### 7.2.2 Hierarchical Merge (jsn.Replace Pattern)

For cross-DB child queries, use the existing `jsn.Replace()` pattern from remote joins. This replaces placeholder values within nested JSON:

```go
// Already implemented pattern in core/remote_join.go
func (s *gstate) mergeCrossDBChild(from, to []jsn.Field) error {
    var ob bytes.Buffer
    if err := jsn.Replace(&ob, s.data, from, to); err != nil {
        return err
    }
    s.data = ob.Bytes()
    return nil
}
```

**Key distinction:**
- **Root-level**: Combine `{"users":[...]}` + `{"events":[...]}` → `{"users":[...],"events":[...]}`
- **Hierarchical**: Replace `{"users":[{"orders": null}]}` → `{"users":[{"orders":[...]}]}`

### 7.3 Cross-Database Joins (Database Joins)

Extend the remote join infrastructure for in-process database joins:

```go
// core/database_join.go (new file)
type databaseJoin struct {
    parent     *qcode.Select
    child      *qcode.Select
    parentDB   string
    childDB    string
    joinColumn string
}

func (s *gstate) executeDatabaseJoins(c context.Context, chains []DependentChain) error {
    for _, chain := range chains {
        // 1. Extract join keys from parent result
        parentIDs := extractJoinKeys(s.data, chain.Parent, chain.JoinKey)

        // 2. Build child query with IN clause
        childQuery := buildChildQuery(chain.Children, parentIDs)

        // 3. Execute against child database
        childDB := s.gj.databases[chain.Children[0].Database]
        childSQL, md, _ := childDB.compiler.Compile(childQuery)
        childData, _ := executeSQLQuery(c, childDB.db, childSQL, md)

        // 4. Merge child data into parent result
        s.data = mergeChildIntoParent(s.data, childData, chain)
    }

    return nil
}

func buildChildQuery(children []*qcode.Select, parentIDs []interface{}) *qcode.QCode {
    // Add WHERE child.foreign_key IN ($parentIDs) filter
    for _, child := range children {
        child.Where = &qcode.Exp{
            Op:    qcode.OpIn,
            Col:   child.JoinColumn,
            Vals:  parentIDs,
        }
    }
    return &qcode.QCode{Roots: children}
}
```

### 7.4 Execution Flow Diagram

```
                    GraphQL Request
                           │
                           ▼
                  ┌─────────────────┐
                  │   QCode Parse   │
                  └─────────────────┘
                           │
                           ▼
                  ┌─────────────────┐
                  │  Query Analyze  │  Identify databases per Select
                  └─────────────────┘
                           │
              ┌────────────┴────────────┐
              │                         │
              ▼                         ▼
    ┌─────────────────┐       ┌─────────────────┐
    │ Same-DB Groups  │       │ Cross-DB Chains │
    └─────────────────┘       └─────────────────┘
              │                         │
              ▼                         │
    ┌─────────────────┐                 │
    │ Parallel Exec   │                 │
    │                 │                 │
    │  ┌───┐ ┌───┐    │                 │
    │  │PG │ │MDB│    │                 │
    │  └───┘ └───┘    │                 │
    └─────────────────┘                 │
              │                         │
              ▼                         │
    ┌─────────────────┐                 │
    │  Merge Results  │◀────────────────┘
    └─────────────────┘    (inject dependent data)
              │
              ▼
        JSON Response
```

## 8. Directive Syntax

### 8.1 @source Directive

Used to disambiguate tables that exist in multiple databases:

```graphql
directive @source(name: String!) on FIELD

query {
  users @source(name: "main") {
    id
    name
  }
  users @source(name: "analytics") {
    id
    activity_score
  }
}
```

### 8.2 Directive Parsing

```go
// core/internal/graph/directive.go (new file)
type SourceDirective struct {
    Name string
}

func ParseSourceDirective(field *graphql.Field) *SourceDirective {
    for _, dir := range field.Directives {
        if dir.Name == "source" {
            return &SourceDirective{
                Name: dir.Arguments.GetString("name"),
            }
        }
    }
    return nil
}
```

## 9. Mutations

### 9.1 Single-Database Mutations

Mutations target a single database (no distributed transactions):

```graphql
mutation CreateOrder {
  orders(insert: { user_id: 1, total: 99.99 }) @source(name: "analytics") {
    id
    created_at
  }
}
```

### 9.2 Database Determination

For mutations:
1. Use `@source` directive if provided
2. Use table-to-database mapping from config
3. Error if ambiguous

```go
func (s *gstate) executeMutation(c context.Context) error {
    // Validate all mutations target same database
    databases := s.getMutationDatabases()
    if len(databases) > 1 {
        return fmt.Errorf(
            "mutations cannot span multiple databases: %v",
            databases,
        )
    }

    db := s.gj.databases[databases[0]]
    return s.executeSingleDBMutation(c, db)
}
```

### 9.3 Future: Saga Pattern

For users requiring cross-database mutations, a future enhancement could implement the saga pattern:

```yaml
# Future feature - not in initial implementation
sagas:
  create_user_with_profile:
    steps:
      - database: main
        mutation: insert_user
        compensate: delete_user
      - database: analytics
        mutation: insert_profile
        compensate: delete_profile
```

## 10. Subscriptions

### 10.1 Initial Scope

Subscriptions remain single-database only in the initial implementation:

```graphql
subscription UserUpdates {
  users(id: 1) @source(name: "main") {  # Only polls one database
    name
    updated_at
  }
}
```

### 10.2 Future: Multi-Database Subscriptions

Potential future enhancement:

```graphql
# Future feature
subscription DashboardUpdates {
  users @source(name: "main") { ... }
  events @source(name: "analytics") { ... }
}
```

Would require:
- Multiple polling loops (one per database)
- Merged notification when any database changes
- Careful handling of different polling intervals

## 11. Error Handling

### 11.1 Partial Failures

When one database query fails but others succeed:

```json
{
  "data": {
    "users": [{"id": 1, "name": "Alice"}],
    "events": null
  },
  "errors": [
    {
      "message": "MongoDB connection failed",
      "path": ["events"],
      "extensions": {
        "database": "analytics",
        "code": "DATABASE_ERROR"
      }
    }
  ]
}
```

### 11.2 Connection Failures

Each database connection is health-checked independently:

```go
func (gj *graphjinEngine) HealthCheck(c context.Context) map[string]error {
    results := make(map[string]error)

    for name, db := range gj.databases {
        if db.db != nil {
            results[name] = db.db.PingContext(c)
        } else if db.mongoDb != nil {
            results[name] = db.mongoDb.Client().Ping(c, nil)
        }
    }

    return results
}
```

## 12. Performance Considerations

### 12.1 Connection Pooling

Each database maintains its own connection pool:

```yaml
databases:
  main:
    type: postgres
    pool_size: 20          # Per-database pool
    max_connections: 50

  analytics:
    type: mongodb
    pool_size: 10
```

### 12.2 Query Caching

**CRITICAL**: Query plans are cached per (query, role, database-combination). The cache key **MUST** include database identifiers:

```go
// Cache key MUST include all involved databases
func (s *gstate) key() string {
    dbs := s.involvedDatabases()
    sort.Strings(dbs)
    return s.r.namespace + s.r.name + s.role + strings.Join(dbs, ",")
}
```

**Why this matters**: Without database in the cache key, a query named `getUsers` would share cached SQL across all databases, causing incorrect results or errors when databases have different schemas.

**Subscription caching** must also include database identifier using the same pattern.

### 12.3 Parallel Execution Benefits

For a query hitting 3 databases:
- **Sequential**: 100ms + 80ms + 60ms = 240ms
- **Parallel**: max(100ms, 80ms, 60ms) = 100ms

### 12.4 Cross-Database Join Overhead

Database joins add latency for dependent queries:
- Parent query: 100ms
- ID extraction: ~1ms
- Child query: 80ms
- Result merge: ~1ms
- **Total**: ~182ms

Optimization: Batch child queries when multiple parents exist.

## 13. Observability

### 13.1 Tracing Requirements

All spans must include database context for multi-DB debugging:

```go
// Updated span attributes
span.SetAttributesString(
    StringAttr{"query.namespace", s.r.namespace},
    StringAttr{"query.operation", cs.st.qc.Type.String()},
    StringAttr{"query.name", cs.st.qc.Name},
    StringAttr{"query.role", cs.st.role},
    StringAttr{"query.database", s.database},           // NEW: Required
    StringAttr{"query.databases", strings.Join(dbs, ",")}) // NEW: For multi-DB queries
```

### 13.2 Span Hierarchy for Multi-DB

For parallel queries, create proper parent-child span relationships:

```
Parent Span: "GraphJin Query" (spans entire request)
├── Child Span: "Execute Query (main)"
│   └── database: main, dialect: postgres
├── Child Span: "Execute Query (analytics)"
│   └── database: analytics, dialect: mongodb
└── Child Span: "Merge Results"
```

### 13.3 Logging Requirements

All log statements must include database context:

```go
// Before (ambiguous)
gj.log.Printf("query error: %s", err)

// After (clear)
gj.log.Printf("[db=%s] query error: %s", dbName, err)
```

Consider structured logging for production:

```go
type LogContext struct {
    Database  string
    Operation string
    QueryName string
}
```

### 13.4 Health Checks

Extend health check to report per-database status:

```go
func (gj *graphjinEngine) HealthCheck(c context.Context) map[string]HealthStatus {
    results := make(map[string]HealthStatus)
    for name, db := range gj.databases {
        results[name] = HealthStatus{
            Status:    db.Ping(c),
            Latency:   measureLatency(db),
            PoolStats: db.Stats(),
        }
    }
    return results
}
```

## 14. Files to Modify

| File | Changes |
|------|---------|
| `core/config.go` | Add `Databases` map, `DatabaseConfig` struct |
| `core/core.go` | Add `dbContext` type, modify `graphjinEngine` |
| `core/api.go` | Multi-DB initialization in `NewGraphJin()` |
| `core/gstate.go` | **CRITICAL**: Update `key()` to include database, parallel execution, result merging |
| `core/subs.go` | **CRITICAL**: Update subscription cache key to include database |
| `core/database_join.go` | New file for cross-DB joins |
| `core/result_merger.go` | New file for JSON merging |
| `core/query_splitter.go` | New file for query analysis |
| `core/trace.go` | Add database context to span attributes |
| `core/internal/qcode/qcode.go` | Add `Database` to `Select` |
| `core/internal/qcode/parse.go` | Table resolution with database routing |
| `core/internal/sdata/schema.go` | Add `Database` to `DBTable`, schema merging |
| `core/internal/graph/directive.go` | New file for `@source` directive parsing |
| `serv/db.go` | Multiple connection initialization |
| `serv/config.go` | Service-level multi-DB config |
| `plugin/otel/trace.go` | Update OpenTelemetry tracer with database attributes |

## 15. Testing Strategy

### 15.1 Unit Tests

- Database context initialization
- Schema merging with conflicts
- Query splitting algorithm
- Result merging
- **Cache key includes database identifier**

### 15.2 Integration Tests

```go
func TestMultiDB_ParallelRootQueries(t *testing.T) {
    // Setup: Postgres + SQLite test containers
    // Query: Fetch from both in single request
    // Assert: Both results present, execution was parallel
}

func TestMultiDB_CrossDatabaseJoin(t *testing.T) {
    // Setup: Postgres (users) + MongoDB (orders)
    // Query: users { orders { ... } }
    // Assert: Orders correctly joined to users
}

func TestMultiDB_TableConflict(t *testing.T) {
    // Setup: users table in both Postgres and MongoDB
    // Query without @source: Should error
    // Query with @source: Should succeed
}
```

### 15.3 Backward Compatibility Tests

```go
func TestMultiDB_LegacyConfigWorks(t *testing.T) {
    // Use old single-DB config format
    // Verify behavior is unchanged
}
```

### 15.4 Cache Isolation Tests

```go
func TestMultiDB_CacheKeyIncludesDatabase(t *testing.T) {
    // Setup: Same query name across two databases
    // Execute: Query db1, then query db2
    // Assert: Each gets correctly compiled SQL for its schema
}

func TestMultiDB_SubscriptionCacheIsolation(t *testing.T) {
    // Setup: Same subscription name across two databases
    // Assert: Each subscription polls correct database
}
```

## 16. Migration Path

### 16.1 Opt-In Activation

Multi-database is activated only when `databases` config is present:

```go
func (gj *graphjinEngine) isMultiDB() bool {
    return len(gj.conf.Databases) > 0
}
```

### 16.2 Gradual Adoption

1. Start with existing single-DB config
2. Add second database with explicit table mapping
3. Queries continue working unchanged
4. New tables can use new databases

## 17. Future Enhancements

### 17.1 Connection Routing Rules

```yaml
databases:
  main:
    type: postgres
    routing:
      - pattern: "read_*"     # Tables starting with read_
        replica: read_replica
```

### 17.2 Cross-Database Transactions (Saga)

```yaml
sagas:
  transfer_funds:
    steps:
      - db: accounts
        action: debit
        compensate: credit
      - db: ledger
        action: record
```

### 17.3 Database Sharding

```yaml
databases:
  users_shard_1:
    type: postgres
    shard_key: user_id
    shard_range: [0, 1000000]
  users_shard_2:
    type: postgres
    shard_key: user_id
    shard_range: [1000001, 2000000]
```

## 18. Summary

This design enables GraphJin to query multiple databases in a single GraphQL request through:

1. **Database contexts** with isolated connections, schemas, and compilers per database
2. **Config-based table routing** keeping GraphQL queries clean
3. **Parallel root execution** for independent queries to different databases
4. **Database joins** reusing the `jsn.Get()` + `jsn.Replace()` pattern from remote joins
5. **`@source` directive** for explicit disambiguation when tables conflict
6. **Observability** with database context in all spans and logs

### Critical Architecture Insights (12 Total)

| # | Insight | Severity | Impact |
|---|---------|----------|--------|
| 1 | SQL compilation produces single nested statement | HIGH | Must split at QCode level |
| 2 | Each database needs its own compiler | HIGH | Per-DB qcode + psql compilers |
| 3 | Mixed same-DB and cross-DB children | MEDIUM | Categorize during QCode analysis |
| 4 | Remote join pattern is the key | - | Reuse existing infrastructure |
| 5 | Relationship graph assumes single database | MEDIUM | Cross-DB edges need special weight |
| 6 | Root-level merge differs from nested replace | MEDIUM | Two distinct merging operations |
| 7 | QCode already has remote infrastructure | - | Reuse `SkipTypeRemote` pattern |
| 8 | Child QCode extraction complexity | MEDIUM | Build fresh QCode (recommended) |
| 9 | **Query cache key missing database** | **CRITICAL** | **MUST include database in key** |
| 10 | **Subscription cache has same problem** | **CRITICAL** | **MUST include database in key** |
| 11 | Tracing/logging need database context | MEDIUM | Add `query.database` attribute |
| 12 | Mutation execution strategy complexity | HIGH | Keep mutations single-DB only |

### Key Implementation Requirements

1. **Cache key MUST include database**: `namespace + name + role + databases`
2. **Subscription cache MUST include database**: Same pattern as query cache
3. **All spans MUST include `query.database`**: For observability
4. **All logs MUST include database context**: For debugging
5. **Mutations MUST be single-database**: Different dialects use incompatible strategies

### Benefits

- **Leverages proven patterns**: Remote join code is battle-tested
- **Backward compatible**: Single-DB configs work unchanged
- **Complete solution**: Both query patterns handled in one implementation
- **Observable**: Full tracing and logging with database context

The implementation is modular and leverages existing infrastructure (dialects, remote joins, JSON utilities) while enabling powerful polyglot data access patterns.
