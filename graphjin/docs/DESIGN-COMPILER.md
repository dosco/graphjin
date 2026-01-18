# GraphJin Core Design & Architecture

GraphJin is an engine that automatically compiles GraphQL requests into highly optimized SQL queries. It avoids the N+1 problem by generating a single SQL query for complex, nested GraphQL requests, while respecting permissions and relationships.

## High-Level Architecture

The core logic resides in the `core` package, which orchestrates the entire lifecycle of a request:

1.  **API Layer (`core/api.go`)**: Entry point for the library.
    -   `NewGraphJin(conf *Config, db *sql.DB)`: Initializes the engine, triggers schema discovery, and pre-calculates the allow list if in production.
    -   `GraphJin.GraphQL(ctx, query, vars, rc)`: The main execution entry point. Handles APQ caching, parses the query, checks the allow-list, and delegates to the internal compilers.
    -   `GraphJin.Reload()`: Re-runs schema discovery (useful in dev mode or after schema changes).

2.  **Engine (`core/core.go`)**: Manages the state, schema discovery, and compiler initialization. It holds the `graphjinEngine` struct which contains:
    -   `dbinfo`: Raw database metadata.
    -   `schema`: The graph-based representation of the application data model.
    -   `qcodeCompiler`: The configured GraphQL-to-IR compiler.
    -   `psqlCompiler`: The configured IR-to-SQL compiler.

3.  **Schema Discovery (`internal/sdata`)**: Inspects the database (Postgres, etc.) to build an in-memory graph.
    -   **`DBSchema`**: The top-level container for the graph.
    -   **`DBTable`**: Represents a database table, including columns and primary/foreign keys.
    -   **`DBRel`**: Defines edges between tables.
        -   `RelOneToOne`, `RelOneToMany`: Standard foreign key relationships.
        -   `RelPolymorphic`: For joining across multiple tables based on a type discriminator.
        -   `RelRecursive`: Use for recursive queries (CTEs).
        -   `RelRemote`: For joining data from remote APIs (requires config).

4.  **Query Compilation (`internal/qcode`)**: Parses GraphQL into an intermediate representation.
    -   **`QCode`**: The root object representing a compiled query. Contains the operation type (`QTQuery`, `QTMutation`) and a list of `Select`s (roots).
    -   **`Select`**: Represents a node in the query tree. Maps to a database table or a derived table.
    -   **`Exp`**: Represents filters (`WHERE` clauses) and expressions.
    -   **Responsibilities**:
        -   Validates field existence against `sdata`.
        -   Calculates relationship paths (`Rel` within `Select`).
        -    interpolates variables.

5.  **SQL Generation (`internal/psql`)**: Takes the `QCode` and generates a single SQL statement.
    -   **Compiler**: Iterates over the `QCode` tree.
    -   **`LATERAL JOIN`**: Used extensively to fetch child data for each parent row efficiently.
    -   **JSON Functions**: Uses `jsonb_build_object` (Postgres) or `json_object` (MySQL) to construct the result shape.
    -   **Cursor Pagination**: Implements efficient cursor-based paging using window functions or CTEs depending on the dialect.

## Key Components

### 1. Core API (`core/api.go`)
-   **GraphJin Struct**: The main handle for the library. Thread-safe and designed to be initialized once.
-   **NewGraphJin**: Factory function that triggers schema discovery (`sdata`) and prepares the compilers.
-   **GraphQL**: The main execution method. It handles caching (APQ), allow-listing, and delegates the actual processing to the internal pipeline.

### 2. Schema Metadata (`internal/sdata`)
-   **DBInfo**: Raw metadata including table names, columns, and foreign keys.
-   **DBSchema**: Enriched schema that builds a graph of relationships (`RelOneToOne`, `RelOneToMany`, `RelPolymorphic`, etc.).
-   **Relationship Inference**: GraphJin automatically infers relationships based on foreign key constraints and naming conventions (e.g., standardizing on `_id` suffixes).

### 3. QCode Compiler (`internal/qcode`)
-   **Purpose**: Converts GraphQL AST into a flatter, database-centric intermediate representation (`QCode`).
-   **Responsibilities**:
    -   Parsing GraphQL.
    -   Validating fields against the `sdata` schema.
    -   Resolving arguments (filters, pagination, ordering).
    -   Handling fragments and variables.
    -   Constructing the "Select" tree which mirrors the GraphQL selection set but with database metadata attached.

### 4. SQL Compiler (`internal/psql`)
-   **Purpose**: Converts `QCode` into a specific dialect of SQL (primarily PostgreSQL, with some MySQL support).
-   **Strategy**:
    -   Uses `LATERAL LEFT JOIN` (or equivalent) to fetch nested data without N+1 queries.
    -   Uses JSON generation functions (`jsonb_build_object`, `json_agg`) within the database to structure the output.
    -   Implements cursor-based pagination and efficient filtering.
-   **Output**: A single SQL string and a set of argument values.

### 5. Dialect Abstraction
-   **Principle**: `core/internal/psql` acts as the orchestrator but delegates all dialect-specific SQL generation to the `dialect` package.
-   **Strict Separation**: Core logic must never contain `if dialect == "mysql"` checks. All variations must be handled via the `Dialect` interface.
-   **Error Handling**: If a dialect cannot support a feature (e.g., MySQL missing `SIMILAR TO`), it must return an explicit error rather than silently generating invalid SQL.

### 6. Multi-Database Support
GraphJin supports querying multiple databases (PostgreSQL, MySQL, SQLite, MongoDB, etc.) within a single instance. Each database has its own `dbContext` with isolated schema, compilers, and connection pool. Query routing is determined by table-to-database configuration. See `docs/DESIGN-MULTIDB.md` for detailed architecture.

## 7. Mutation Strategies

GraphJin employs two distinct strategies for handling mutations, automatically selected based on the database's capabilities.

### 1. Atomic CTE Chain (PostgreSQL)
Used for databases that support **Writable CTEs** (Common Table Expressions).
- **Single Statement**: The entire mutation tree (root inserts and nested relationship inserts) is compiled into a single, atomic SQL statement.
- **Data Flow**: `RETURNING` clauses from parent CTEs pass generated IDs to child CTEs.
- **Atomicity**: Guaranteed by the database transaction for the single statement.

### 2. Linear Execution Strategy (MySQL, SQLite)
Used for databases that do *not* support Writable CTEs.
- **Flattened Script**: The mutation implementation flattens the dependency graph into a topological sort of individual SQL statements.
- **Variable Injection**:
    - **MySQL**: Uses session variables (e.g., `SET @user_id = LAST_INSERT_ID()`) to capture IDs and pass them to subsequent statements.
    - **SQLite**: Uses a temporary table (`_gj_ids`) to store and retrieve captured IDs (created via `RenderSetup` and dropped via `RenderTeardown`).
- **Result Selection**: A final `SELECT` statement is generated with injected `WHERE` clauses (e.g., `WHERE id = @user_id`) to retrieve the full result shape.

## Request Flow

1.  **User calls `gj.GraphQL(query)`**.
2.  **Fast Parse**: The query is quickly scanned to determine the operation name and type.
3.  **Allow List Check**: In production, the query is checked against a pre-compiled allow list.
4.  **QCode Compilation**:
    -   The query is parsed and validated against the `sdata` schema.
    -   A `QCode` object is produced, representing the query plan.
5.  **SQL Generation**:
    -   The `QCode` is fed into the `psql` compiler.
    -   SQL is generated to match the requested structure.
6.  **Execution**:
    -   The SQL is executed against the database.
    -   The database returns a single row containing the constructed JSON result.
7.  **Response**: The raw JSON bytes from the database are returned to the user, wrapping them in the standard GraphQL envelope (`{"data": ...}`).

-   `core/internal/jsn`: JSON helpers.
-   `core/internal/migrate`: Logic for diffing schema structures and generating SQL migrations.

## Schema Management

GraphJin offers a hybrid approach to database schema management:

### 1. Runtime Discovery (Default)
By default, GraphJin inspects the database at startup (`sdata.GetDBInfo`) to build its internal graph. This is zero-config but requires a database connection at init time.

### 2. Schema Bootstrapping (`EnableSchema`)
To improve startup time and remove the DB dependency during initialization (e.g., for serverless or testing):
-   **Generation (Dev)**: In development (`Production: false`), if `EnableSchema` is set, GraphJin discovers the schema and saves it to a `db.graphql` file.
-   **Bootstrapping (Prod)**: In production, it reads `db.graphql` (parsed by `qcode`) to hydrate the `DBInfo` struct, bypassing the expensive DB inspection.

### 3. Auto-Migration
The `core/internal/migrate` package provides capabilities to:
-   Compare the "current" schema (from DB) against an "expected" schema (from `db.graphql` or code).
-   Generate SQL operations (`CREATE TABLE`, `ALTER TABLE`, etc.) to align the database with the expected state.
-   This enables a "code-first" or "schema-first" workflow where changes in the GraphJin schema definition can propagate to the database.

## Testing Architecture

GraphJin primarily relies on **integration tests** that run against real database instances spun up via Docker.

### 1. Test Harness (`tests/dbint_test.go`)
-   **TestContainers**: Uses `github.com/testcontainers/testcontainers-go` to manage ephemeral Postgres and MySQL containers.
-   **Lifecycle**: The `TestMain` function initializes the container, applies the schema (e.g., `tests/postgres.sql`), runs all tests, and then tears down the container.
-   **Execution**: Tests are run sequentially against this shared database instance.

### 2. Test Data & Schema (`tests/postgres.sql`)
A complex schema is pre-loaded to verify various relationships and features:
-   **Tables**: `users`, `products`, `purchases`, `comments`, etc.
-   **Features Covered**: Arrays (`tags`), JSONB (`metadata`), polymorphic relationships (`notifications`), recursive relationships (`comments.reply_to_id`), and views.

### 3. Example-Based Tests (`tests/query_test.go`)
Many tests use Go's `Example` function pattern.
-   **Pattern**: Defines a GraphQL query, runs it through `GraphJin.GraphQL`, and prints the JSON output.
-   **Verification**: Go's testing tool automatically compares the printed output against the `// Output:` comment at the end of the function.
-   **Benefits**: These serve as both regression tests and live documentation for how to use the library.

### 4. Core Logic Tests
-   **`core_test.go`**: Tests configuration, allow-listing, APQ, and error handling.
-   **Internal Packages**: Some internal packages (like `qcode`) have their own unit tests, but the heavy lifting is done in the top-level integration tests.

## 8. Offline Mock Testing

The "Offline Mock Testing Driver" enables "Database-less Testing," allowing GraphJin to be initialized and queried without a running database instance. This is useful for CI/CD pipelines, instant unit tests, and client-side integration verification.

### 1. Configuration
A new boolean flag `MockDB` is added to the `Config` struct.
- **Enabled (`true`)**: Signals GraphJin to skip database discovery and connection establishment effectively running in an "air-gapped" mode regarding data storage.

### 2. Initialization
- **Schema Loading**: Instead of introspecting a live database, GraphJin expects a `db.graphql` file (the same format used by `EnableSchema` for bootstrapping). It parses this file to build the internal `sdata.DBInfo` and `sdata.DBSchema` structures.
- **Connection Bypass**: The validation logic permits the `sql.DB` handle to be `nil` when `MockDB` is set.

### 3. Execution Interception
The `compileAndExecute` method in `gstate.go` checks the `MockDB` flag.
- **Normal Flow**: Compile to SQL -> Get DB Conn -> Execute SQL -> Return JSON.
- **Mock Flow**: Compile to QCode -> **Call `executeMock`** -> Generate Mock Data -> Return JSON.

### 4. Mock Data Generation (`core/mock.go`)
The `executeMock` function requires no SQL generation. Instead, it performs a depth-first traversal of the compiled `QCode` tree:

1.  **Select Traversal**: Iterates through the selected fields and relationships defined in the query.
2.  **Type-Based Generation**: Generates deterministic or semi-random dummy data based on the column type defined in the schema:
    -   `Integer`/`Float`: Returns numeric sequences (e.g., based on array index).
    -   `String`: Returns pattern-based strings (e.g., `mock_<fieldname>_<index>`).
    -   `Boolean`: Returns alternating values.
    -   `Timestamp`: Returns the current time.
3.  **Recursion**: Recursively generates data for nested objects and lists, respecting the structure of the request (e.g., returning arrays for one-to-many relationships).

This approach ensures that the returned JSON strictly adheres to the requested shape and scalar types, allowing clients to validate their parsing logic without the overhead of a real database.
