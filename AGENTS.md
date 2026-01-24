# GraphJin Agent Guide

This document is a guide for AI agents working on the GraphJin codebase. It outlines the architectural patterns, coding conventions, and common tasks associated with maintaining and extending this library.

## Architectural Overview

GraphJin is a compiler that turns GraphQL into database queries. For SQL databases, it generates SQL. For MongoDB, it generates a JSON DSL that is translated to aggregation pipelines. It is NOT a typical ORM or resolver-based GraphQL server.

-   **Core Philosophy**: Push as much work as possible to the database.
-   **No Resolvers**: Data fetching is done via a single generated SQL query. Do not add resolvers for database fields.
-   **Schema Driven**: The database schema (`sdata`) is the source of truth.

## Directory Structure & Responsibilities

| Path | Component | Responsibility |
| :--- | :--- | :--- |
| `core/api.go` | **Public API** | The only entry point for users. Changes here are breaking. |
| `core/core.go` | **Engine** | Internal orchestration, initialization, and state management. |
| `core/internal/sdata` | **Schema** | Metadata about tables, columns, and relationships. Graph traversal logic. |
| `core/internal/qcode` | **IR Compiler** | Front-end compiler. Parses GraphQL -> `QCode` (Intermediate Representation). |
| `core/internal/psql` | **SQL Compiler** | Back-end compiler. `QCode` -> SQL. Handles dialect differences. |
| `core/internal/dialect` | **Dialect Interface** | Database-specific SQL generation methods. Each dialect implements this interface. |
| `core/internal/graph` | **GraphQL Parser** | Lexer and parser for GraphQL input. Builds AST. |
| `core/internal/jsn` | **JSON Processing** | High-performance JSON parsing and filtering utilities. |
| `serv/` | **HTTP Service** | Standalone server with REST, GraphQL, and WebSocket APIs. |
| `auth/` | **Authentication** | Auth providers: JWT, Auth0, Firebase, Rails session. |
| `cmd/` | **CLI Tool** | Command-line interface for migrations, deployment, and management. |
| `conf/` | **Configuration** | YAML-based config loading and validation. |
| `wasm/` | **WebAssembly** | WASM build for NodeJS integration. |
| `mongodriver/` | **MongoDB Driver** | Custom database/sql-compatible driver for MongoDB. Translates JSON DSL to aggregation pipelines. |

## Build Commands

```bash
make build    # Build for current platform
make test     # Run tests (requires Docker for database containers)
make lint     # Lint code
make gen      # Generate code (stringer, etc.)
```

## Coding Guidelines

### 1. Adding New SQL Features
To add support for a new SQL feature (e.g., a new aggregation or function):
1.  **Update `qcode`**: meaningful changes often start here. Ensure the new feature can be represented in the `QCode` struct (`core/internal/qcode/qcode.go`).
2.  **Update `psql`**: Implement the SQL generation logic in `core/internal/psql/query.go` (or `mutate.go` for writes).
3.  **Tests**: Add a test case in `core/internal/psql/tests`. These tests compare the generated SQL against an expected string.
4.  **Dialect Compatibility**: If the feature syntax varies by database (Postgres vs MySQL):
    -   Add a method to the `Dialect` interface in `core/internal/dialect`.
    -   Implement it in `postgres.go` and `mysql.go`.
    -   Return an error from the implementation if the feature is not supported by that dialect.
    -   **DO NOT** use `if dialect == ...` checks in the shared `psql` logic.

### 2. MCP Maintenance

When adding new features to GraphJin (operators, syntax, capabilities), remember to update the MCP syntax documentation:

-   **File**: `serv/mcp_syntax.go`
-   **What to update**:
    -   `FilterOperators` struct - add new filter operators
    -   `querySyntaxReference` - add operator lists and syntax descriptions
    -   `queryExamples` - add example queries demonstrating new features
-   **Why**: AI assistants using MCP call `get_query_syntax` to learn available operators. Undocumented operators won't be used by AI agents.

### 3. Modifying Schema Discovery
If you need to change how GraphJin discovers tables or relationships:
-   Focus on `core/internal/sdata/schema.go` and `tables.go`.
-   Modifications here affect the graph used for query planning.

### 4. Error Handling
-   Use standard Go error wrapping (`fmt.Errorf("%w", err)`).
-   Fail fast during initialization (`NewGraphJin`).
-   During query execution, return meaningful error messages that help the user debug their GraphQL query.

### 5. Performance
-   **Zero Allocation**: Strive for zero-allocation in the hot path (`GraphQL` execution).
-   **Pre-computation**: Do heavy lifting (schema analysis, allow-list preparation) at initialization time, not request time.

### 6. Configuration
-   **YAML Config**: Use `dev.yml` for development, `prod.yml` for production.
-   **Production Mode**: In production, all queries must be pre-saved (no dynamic client queries). This is a security feature.
-   **Environment Variables**: Secrets and connection strings should come from environment variables, not config files.

## Testing Guidelines

### 1. Running Tests
-   **Requirement**: Docker must be running.
-   **Command**: `make test`
-   **Note**: This command runs integration tests that require a real database connection.
-   **Mechanism**: The tests will automatically spin up a Postgres container.
-   **Time**: First run may take a moment to pull images; subsequent runs are faster but still involve container startup overhead.

### 2. Running Specific Tests
To avoid running the entire suite (which can be slow):
-   **Single Integration Test**: Use the `-run` flag with the test function name.
    ```bash
    go test -v -run Example_queryWithJsonColumn ./tests
    ```
-   **Package Level Unit Tests**: Run tests for a specific package (e.g., SQL generation).
    ```bash
    go test -v ./core/internal/psql
    ```

### 3. Adding New Tests
-   **Regression/Feature Tests**: Add a new `Example_` function in `tests/query_test.go` or a new file in `tests/`.
-   **Database Changes**: If your test requires new schema elements, update `tests/postgres.sql`.
-   **Output Verification**: Use the `// Output:` comment at the end of your example function. The test runner checks stdout against this comment.

### 4. Fuzz Testing
-   **Security-critical components**: Use Go's built-in fuzz testing (`go test -fuzz=FuzzName`) for parsers and input validation.
-   **Focus areas**: GraphQL lexer/parser, JSON processing, SQL generation edge cases.

## Key constraints
-   **Do not use ORMs** internally.
-   **Do not use reflection** in the hot path.
-   **Keep `core/api.go` stable**.
### 4. Shared Code Stability
-   **Critical**: When modifying shared code (e.g., `query.go`, `columns.go`), you **MUST** verify that existing dialects (Postgres, MySQL, SQLite) are not broken.
-   **Regression Testing**: Always run the full test suite or relevant dialect-specific tests before committing changes to shared logic.
-   **Isolation**: If a new feature or dialect requires different behavior, prefer using `if dialect == ...` blocks or interface methods over changing the common logic that other dialects rely on.

## Adding New Database Dialects

When adding support for a new database (e.g., Oracle, SQL Server), follow these guidelines to ensure consistency and correctness.

### 1. SQL Standards & Undefined Behavior

**Result ordering is undefined without ORDER BY in all SQL databases.**

-   **Do NOT add implicit ordering**: Never add automatic `ORDER BY` to queries that don't specify one, even if it would make behavior "match" another database.
-   **Why**: Adding implicit ordering causes performance overhead (unnecessary sorts), violates SQL standards, and creates unexpected behavior for users.
-   **PostgreSQL's "consistent" ordering is a myth**: Tests that pass on PostgreSQL without explicit ordering are relying on undefined implementation behavior, not guaranteed semantics. These tests are buggy.

### 2. Test Determinism

When tests fail on a new database due to different row ordering:

-   **Fix the test, not the database layer**: Add explicit `order_by: { id: asc }` (or appropriate column) to the GraphQL query.
-   **Tests must be deterministic**: Any test that checks specific result ordering must specify that ordering explicitly.
-   **Pattern for ordering fix**:
    ```go
    // BAD - relies on undefined ordering
    gql := `query { products(limit: 2) { id name } }`

    // GOOD - explicit ordering
    gql := `query { products(limit: 2, order_by: { id: asc }) { id name } }`
    ```

### 3. Dialect Implementation Checklist

When implementing a new dialect, handle these common differences:

| Feature | PostgreSQL | MySQL | SQLite | Oracle | MSSQL | MongoDB |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| Row limiting | `LIMIT n` | `LIMIT n` | `LIMIT n` | `FETCH FIRST n ROWS ONLY` | `FETCH NEXT n ROWS ONLY` | `$limit` stage |
| Offset | `OFFSET n` | `OFFSET n` | `OFFSET n` | `OFFSET n ROWS` | `OFFSET n ROWS` | `$skip` stage |
| Boolean type | Native `boolean` | `TINYINT(1)` | `INTEGER` | `NUMBER(1)` - needs JSON conversion | `BIT` (0/1) | Native boolean |
| Recursive CTE | `WITH RECURSIVE` | `WITH RECURSIVE` | `WITH RECURSIVE` | `WITH` (no RECURSIVE keyword) | `WITH` (no RECURSIVE keyword) | `$graphLookup` |
| JSON aggregation | `json_agg()` | `JSON_ARRAYAGG()` | `json_group_array()` | `JSON_ARRAYAGG()` | `STRING_AGG()` + `FOR JSON PATH` | Native (documents) |
| Identifier quoting | `"name"` | `` `name` `` | `"name"` | `"NAME"` (case-sensitive) | `[name]` | N/A (JSON keys) |
| Relationships | SQL `JOIN` | SQL `JOIN` | SQL `JOIN` | SQL `JOIN` | SQL `JOIN` | `$lookup` stage |

### 4. Function Return Type Handling

Some databases don't have native types that map cleanly to JSON:

-   **Oracle booleans**: Oracle functions return `NUMBER` (0/1), not boolean. Configure function return types in the GraphJin config:
    ```go
    conf.Functions = []core.Function{{Name: "is_active", ReturnType: "boolean"}}
    ```
-   The SQL compiler will wrap these with appropriate CASE/FORMAT JSON logic.

### 5. Feature Skip Patterns

When a feature genuinely isn't supported by a database:

-   **Skip with clear documentation**:
    ```go
    // Skip for Oracle: recursive CTE identifier handling not yet supported
    if dbType == "oracle" {
        fmt.Println(`{"expected":"output"}`)
        return
    }
    ```
-   **Combine related skips**: If multiple databases share the same limitation, combine them:
    ```go
    // Skip for MySQL/SQLite: PostgreSQL array column syntax not supported
    if dbType == "mysql" || dbType == "sqlite" {
        fmt.Println(`{"expected":"output"}`)
        return
    }
    ```
-   **Prefer fixing over skipping**: Only skip when the feature truly cannot be supported. If it's just a syntax difference, implement it in the dialect.

### 6. Running Dialect-Specific Tests

Each dialect has its own test script:

```bash
./scripts/test-postgres.sh  # PostgreSQL tests
./scripts/test-mysql.sh     # MySQL tests
./scripts/test-sqlite.sh    # SQLite tests
./scripts/test-oracle.sh    # Oracle tests
./scripts/test-mssql.sh     # MSSQL tests
./scripts/test-mongo.sh     # MongoDB tests
```

**Always run all dialect tests** before merging changes to shared code.

### 7. Subscription Cursor Prefix (Common Pitfall)

Cursor prefixes must use `ctx.GetSecPrefix()` (the dynamic timestamp-based prefix like `gj-65a8b3c0:`). Hardcoding `"gj-"` will cause subscriptions to hang because `firstCursorValue()` in `core/crypt.go` won't recognize the cursor. Always test with `Example_subscriptionWithCursor`.

## MongoDB Implementation

MongoDB is fundamentally different from SQL databases. Instead of generating SQL, GraphJin generates a **JSON DSL** that the custom MongoDB driver (`mongodriver/`) translates into aggregation pipelines.

### Architecture Differences

| Aspect | SQL Databases | MongoDB |
| :--- | :--- | :--- |
| Query output | SQL string | JSON DSL structure |
| Relationships | `JOIN` clauses | `$lookup` pipeline stages |
| Filtering | `WHERE` clause | `$match` pipeline stage |
| Ordering | `ORDER BY` | `$sort` pipeline stage |
| Field selection | `SELECT` columns | `$project` pipeline stage |
| Mutations | `INSERT`/`UPDATE`/`DELETE` | `insertOne`/`updateOne`/`deleteOne` |

### Key Files for MongoDB

| File | Purpose |
| :--- | :--- |
| `core/internal/dialect/mongodb.go` | Main dialect implementation. Generates JSON DSL from QCode. |
| `mongodriver/driver.go` | database/sql-compatible driver registration and connection handling. |
| `mongodriver/conn.go` | Connection implementation that executes JSON DSL against MongoDB. |
| `mongodriver/pipeline.go` | Translates JSON DSL to MongoDB aggregation pipeline and handles result transformation. |
| `mongodriver/query.go` | Parses JSON DSL and handles parameter substitution. |

### JSON DSL Format

The MongoDB dialect generates JSON structures like:

```json
{
  "operation": "aggregate",
  "collection": "users",
  "field_name": "users",
  "pipeline": [
    {"$match": {"_id": 1}},
    {"$project": {"_id": 0, "id": "$_id", "email": 1, "full_name": 1}}
  ]
}
```

For mutations:
```json
{
  "operation": "insertOne",
  "collection": "users",
  "raw_document": "$1",
  "return_pipeline": [...]
}
```

### MongoDB-Specific Considerations

1. **ID Translation**: GraphQL uses `id`, MongoDB uses `_id`. The dialect and driver handle this translation.

2. **Relationship Lookups**: Use `$lookup` with pipelines for field selection and filtering:
   ```json
   {"$lookup": {
     "from": "products",
     "let": {"userId": "$_id"},
     "pipeline": [{"$match": {"$expr": {"$eq": ["$owner_id", "$$userId"]}}}],
     "as": "products"
   }}
   ```

3. **Array Columns**: For array-based relationships (e.g., `category_ids: [1,2,3]`), use `$in` instead of `$eq`:
   ```json
   {"$match": {"$expr": {"$in": ["$_id", "$$categoryIds"]}}}
   ```

4. **JSON Virtual Tables (RelEmbedded)**: For embedded JSON arrays queried as virtual tables, use `$unwind` + `$lookup` + `$group` pattern instead of simple `$lookup`.

5. **Parameter Substitution**: Parameters are placeholders (`$1`, `$2`) in the JSON DSL. The driver's `SubstituteParams()` resolves them at runtime.

6. **Mutations with Allowlist**: Use `raw_document` with parameter placeholder for runtime substitution, not compile-time embedding. This ensures cached queries work correctly with different request data.

### MongoDB Feature Support Status

| Feature | Status | Notes |
| :--- | :--- | :--- |
| Basic queries | ✅ Supported | Field selection, filtering, ordering, limits |
| Relationships | ✅ Supported | Via `$lookup` with pipeline |
| Array column joins | ✅ Supported | Uses `$in` for matching |
| JSON virtual tables | ✅ Supported | Uses `$unwind`/`$lookup`/`$group` pattern |
| Mutations (insert/update/delete) | ✅ Supported | With `raw_document` for allowlist mode |
| Subscriptions | ✅ Supported | Uses polling (change streams possible future enhancement) |
| Aggregation functions | ❌ Not yet | `count`, `sum`, `avg`, etc. |
| `iregex` | ❌ Different | MongoDB uses `$regex`, needs mapping |
| Custom functions | ❌ Not yet | Database functions not supported |

### Testing MongoDB Changes

```bash
# Run all MongoDB tests
./scripts/test-mongo.sh

# Run specific test
./scripts/test-mongo.sh -run "TestQueryWithJsonColumn"
```

**Important**: MongoDB tests require Docker. The test script starts a MongoDB container automatically.
