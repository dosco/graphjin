# GraphJin Agent Guide

This document is a guide for AI agents working on the GraphJin codebase. It outlines the architectural patterns, coding conventions, and common tasks associated with maintaining and extending this library.

## Architectural Overview

GraphJin is a compiler that turns GraphQL into SQL. It is NOT a typical ORM or resolver-based GraphQL server.

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

### 2. Modifying Schema Discovery
If you need to change how GraphJin discovers tables or relationships:
-   Focus on `core/internal/sdata/schema.go` and `tables.go`.
-   Modifications here affect the graph used for query planning.

### 3. Error Handling
-   Use standard Go error wrapping (`fmt.Errorf("%w", err)`).
-   Fail fast during initialization (`NewGraphJin`).
-   During query execution, return meaningful error messages that help the user debug their GraphQL query.

### 4. Performance
-   **Zero Allocation**: Strive for zero-allocation in the hot path (`GraphQL` execution).
-   **Pre-computation**: Do heavy lifting (schema analysis, allow-list preparation) at initialization time, not request time.

## Testing Guidelines

### 1. Running Tests
-   **Requirement**: Docker must be running.
-   **Command**: `make test`
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

## Key constraints
-   **Do not use ORMs** internally.
-   **Do not use reflection** in the hot path.
-   **Keep `core/api.go` stable**.
