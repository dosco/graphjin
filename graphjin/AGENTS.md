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

### 3. Source Capability Maintenance

When adding a new `sources[].capabilities` key, add it first to the central registry in `core/sourcecap`. Do not introduce ad hoc capability strings in catalog, security, MCP, or source-default code.

GraphJin-owned system and workflow capabilities are not source capabilities. Add those to `core/featurecap` and enforce them through the centralized system-root policy.

-   **What the registry owns**: canonical source kinds, capability keys, mode defaults, action, severity, enforcement type, read-only behavior, summaries, recommendations, and examples.
-   **What subsystems own**: actual runtime enforcement hooks for their surface (for example CodeSQL, filesystem read-only, control-plane tables, or MCP tools).
-   **Tests**: registry, catalog, security, config validation, and permission tests must pass. If a capability is not runtime-enforced yet, mark it as `config_audit`.

### 4. Modifying Schema Discovery
If you need to change how GraphJin discovers tables or relationships:
-   Focus on `core/internal/sdata/schema.go` and `tables.go`.
-   Modifications here affect the graph used for query planning.

### 5. Error Handling
-   Use standard Go error wrapping (`fmt.Errorf("%w", err)`).
-   Fail fast during initialization (`NewGraphJin`).
-   During query execution, return meaningful error messages that help the user debug their GraphQL query.

### 6. Performance
-   **Zero Allocation**: Strive for zero-allocation in the hot path (`GraphQL` execution).
-   **Pre-computation**: Do heavy lifting (schema analysis, allow-list preparation) at initialization time, not request time.

### 7. Configuration
-   **YAML Config**: Use `dev.yml` for development, `prod.yml` for production, and `agentic.yml` for agentic deployments; `GO_ENV=agentic` requires `agentic.yml`, and agentic configs can use `inherits: prod` to reuse production settings.
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

## Demo Smoke Suites (`examples/*/scripts/smoke.sh`)

Each vertical demo (coffee-roastery, saas-ops, pcb-fab, corrugated-plant, …) ships a black-box smoke suite that runs against a live demo server. These are contract checks over HTTP/MCP, not Go tests — start the server first:

```bash
graphjin serve --demo --path examples/coffee-roastery
examples/coffee-roastery/scripts/smoke.sh              # base checks
examples/coffee-roastery/scripts/smoke.sh --agent-eval # + open-ended agent evals
```

Flags: `--agent` (require agent checks), `--agent-eval` (stricter model-driven evals), `--no-agent`, `--deep` (waits for real background sweeps), `--model-resolution`, `--url URL`.

### 1. Where code goes

-   **Domain checks** (this demo's tables, saved queries, workflows) go in the demo's own `scripts/smoke.sh`.
-   **Reusable capability suites** (`run_watch_lifecycle_suite`, `run_refusal_suite`, …) go in `examples/lib/smoke-common.sh` and are parameterized by table/prompt so every demo can call them.
-   The shared lib provides: `graphql <label> <query>`, `mcp_tool <name> <json-args>`, `post_json`, `run_agent_rest_prompt`, `assert_jq <file> <jq-expr> <message>`, `assert_jq_args`, `log`/`pass`/`fail`, and identity helpers (`graphql_as_identity`, `build_auth_args_for_identity`) that send dev headers plus a minted HS256 JWT so the same test runs against header-trust and JWT-verified servers.

### 2. Capability contract comments

Every suite function carries a header comment naming what it proves:

```bash
# Capability: TASK-VERIFY-NOW
# Contract: Closing with a passing saved-query proof closes the task ...
# Deeper coverage: TestTaskImmediateVerificationPassAndFail.
run_task_control_plane_suite() { ... }
```

`scripts/check-capability-smokes.sh` enforces that these IDs exist and map to suites — add the ID there when introducing a new capability. `Deeper coverage:` names the Go tests that own the fine-grained cases; the smoke proves the wiring end to end.

### 3. State hygiene (non-negotiable)

Smoke suites run against a **persistent, reused** demo state (`<path>/demo`). Anything you leak is still there months later, and catalog pollution directly degrades the agent: one leaked-state instance carried 108 `smoke_route_*` subscription entries against 3 real saved queries, which broke agent discovery outright.

-   **Unique names**: every created resource uses a `smoke_` prefix plus a `$(date +%s)_$$` suffix.
-   **Track and clean**: append created IDs to suite-level arrays and delete them in `smoke_extra_cleanup` (invoked by the shared EXIT trap). Cleanup must be idempotent — suffix every delete with `|| true`.
-   **Known leak**: inserting a `gj_watch` registers its named subscription as a `gj_artifacts` row with `kind: "saved_query"` that currently outlives the watch. Until watch deletion cascades, cleanup must also delete that artifact by name.
-   **Owner scoping**: `gj_artifacts`, `gj_watch`, and `gj_task` rows are owner-scoped — delete them under the same identity that created them; an admin bulk-delete will silently skip other owners' rows.
-   **Reset**: interrupted runs leak by design. To reset a demo completely, stop the server and delete `<path>/demo`; a fresh provision also clears the `<path>/.graphjin` control-plane store (artifacts, watches, tasks) — on older builds delete that directory yourself, or stale saved queries survive the reset.

### 4. Agent evals (`--agent-eval`)

-   **Assert outcomes, not tool presence.** "The agent called `query_catalog`" is procedure; the eval must check the conclusion: real seeded values quoted in the answer, a verdict actually stated, and negative patterns excluding the known failure shapes (schema-blaming, "cannot determine", invented columns). Pin an exact verdict or figure only when one is unambiguously derivable from the seed — if the prompt's scope is genuinely open (orders alone vs orders plus subscriptions), several verdicts are defensible and pinning one makes the eval reject correct answers. Route the calculation through a workflow before pinning its result.
-   **Natural language first.** At least one eval per demo uses the PROMPTS.md wording verbatim with zero tool coaching — that is what users type, and tool-prescriptive prompts hide real failures.
-   **Absorb variance, not wrongness.** Model-driven evals may retry (2 attempts); an assertion that a wrong conclusion could pass on retry is a broken eval.
-   **Model floor**: these evals need roughly a gpt-4.1-class server model (`GJ_AGENT_MODEL=gpt-4.1`); provider defaults in the mini/flash tier stall in the discovery loop. Switch providers per run with `GJ_AGENT_PROVIDER` / `GJ_AGENT_API_KEY_ENV`.

### 5. Data-accuracy eval loop (`scripts/agent-data-eval.sh`)

The smoke `--agent-eval` suites are wiring checks; the data-accuracy loop is the scored harness for improving the agent's *answers*. It boots the saas-ops demo with `GJ_DEFAULT_LIMIT=10` (so truncated-page aggregation is reproducible), runs `agent/testdata/data_eval_cases.json` through `agent/cmd/skill-eval`, and scores every case on three dimensions:

1.  **Ground truth** — the answer must match a runtime oracle: a trusted DB-side-aggregate GraphQL query executed against the same server's `/api/v1/graphql`, so date-relative seeds and `data_anchor` shifting never desync ground truth.
2.  **Method** — the executed queries (read from the response action trail) must show the *database* computed the result (`sum_*`/`count_*`/`order_by` on aggregates). This catches right-number-wrong-method runs that sum a row page client-side on a table small enough to get away with it. The principle under test: **the model plans, the engine computes.**
3.  **Efficiency** — advisory per-case budgets on actor turns and tokens; exceeding one feeds the `runaway` failure bucket, never a hard gate.

Failed runs are auto-classified (`client_side_aggregation`, `stale_anchor`, `wrong_window`, `ranking_method`, `truncated_finalize`, `runaway`, …) so a report reads as a diagnosis. Per-case verdicts are majority-of-repeats with a separate consistency score, so single-sample flake is absorbed without hiding real wrongness.

The loop:

```sh
make agent-data-eval-baseline                      # before a change
# ...land the prompt/guardrail change...
make agent-data-eval BASELINE=.graphjin-evals/<report>.json   # exits 2 on gate failure
make agent-data-eval-trend                         # recall history across iterations
```

The candidate phase gates as a **ratchet**: ground-truth recall and method recall must not regress vs the baseline (method is the leading indicator — it can improve before answers do). The 0.90 ground-truth target is a warning until reached, so below-target candidates that improve still land during the climb. `--weak-arm gpt-4.1-mini` adds an advisory flash-tier run to measure guardrail robustness; it never gates.

Authoring rules for new cases (enforced by `agent/data_eval_corpus_test.go`): prompts are natural language with zero tool coaching; oracles are pure aggregates or `limit: 1` group-bys only (pure aggregate roots self-set no-limit, keeping oracles immune to the low boot limit); tolerance stays within 1–5% and only for genuinely fractional values; ranking cases must require `order_by` in their method rule. When a field report arrives (an external eval, a user complaint), convert each failure into a corpus case in the matching group — that is how feedback accumulates instead of evaporating.

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
