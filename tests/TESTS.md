# GraphJin Test Suite Documentation

## Overview

GraphJin's integration tests run against real database containers via [Testcontainers](https://testcontainers.com/). Each database gets its own Docker container spun up at test startup, loaded with a shared schema (`tests/<db>.sql`), and torn down after tests complete.

### Supported Databases

| Database | Container Image | Driver | Make Target |
|----------|----------------|--------|-------------|
| PostgreSQL | `postgis/postgis:12-3.3` | `postgres` | `make test-postgres` |
| MySQL 8.0 | `mysql:8.0` | `mysql` | `make test-mysql` |
| MariaDB 10.11 | `mariadb:10.11` | `mysql` | `make test-mariadb` |
| SQLite | (in-process) | `sqlite3_regexp` | `make test-sqlite` |
| Oracle XE 21 | `gvenzl/oracle-xe:21-slim-faststart` | `oracle` | `make test-oracle` |
| MSSQL 2022 | `mcr.microsoft.com/mssql/server:2022-latest` | `mssql` | `make test-mssql` |
| Snowflake | (emulator) | `snowflake` | — |
| MongoDB | `mongo:7` | `mongodb` | `make test-mongodb` |
| AdventureWorks | `postgres:15` (70 tables, 121K+ rows) | `postgres` | `make test-adventureworks` |

## Running Tests

### All databases in parallel (default)
```bash
make test
```

### All databases sequentially
```bash
make test-sequential
```

### Single database
```bash
# Via make
make test-postgres
make test-mysql

# Via go test directly
cd tests && go test -v -timeout 30m -db=postgres .
cd tests && go test -v -timeout 30m -db=mysql .
cd tests && go test -v -timeout 30m -db=oracle .
```

### Run specific tests
```bash
# Composite FK tests on all DBs
cd tests && go test -v -timeout 5m -db=postgres -run TestCompositeFK .

# Composite PK tests on MySQL
cd tests && go test -v -timeout 5m -db=mysql -run TestCompositePK .
```

### AdventureWorks (large-scale)
```bash
make test-adventureworks
# or
cd tests && go test -v -timeout 60m -db=adventureworks -run TestAdventureWorks .
```
See `tests-large/ADVENTUREWORKS_TESTS.md` for the full 24-test matrix.

## Test File Organization

| File | What it tests | DBs |
|------|--------------|-----|
| `dbint_test.go` | Test infrastructure: container setup, schema loading, `TestMain` | All |
| `query_test.go` | GraphQL query examples (`Example_query*`) | All (skips vary) |
| `insert_test.go` | Mutation insert examples (`Example_insert*`) | All (skips vary) |
| `update_test.go` | Mutation update examples (`Example_update*`) | All (skips vary) |
| `core_test.go` | Core functionality (config, roles, security) | All |
| `composite_fk_test.go` | Composite foreign key joins (multi-column FK) | PG, MySQL, MariaDB, SQLite, MSSQL, Oracle |
| `composite_pk_test.go` | Composite primary key lookups and queries | PG, MySQL, MariaDB, MSSQL, Oracle |
| `query_pg_test.go` | PG-specific: array columns, functions, multi-schema | PostgreSQL |
| `array_test.go` | Array column joins, insert+connect with arrays | PostgreSQL, MySQL |
| `subs_test.go` | Real-time subscriptions (LISTEN/NOTIFY) | PostgreSQL |
| `intro_test.go` | GraphQL introspection queries | PostgreSQL, MySQL |
| `geo_test.go` | PostGIS geometry queries | PostgreSQL |
| `discovery_test.go` | Schema discovery / MCP resources | All |
| `adventureworks_test.go` | 24 business-scenario tests against AdventureWorks | PostgreSQL (adventureworks) |
| `snowflake_test.go` | Snowflake-specific tests | Snowflake |
| `multidb_test.go` | Multi-database / cross-database queries | PostgreSQL |
| `workflows_test.go` | Multi-step mutation workflows | All |
| `readonly_test.go` | Read-only mode enforcement | All |
| `schema_diff_test.go` | Schema change detection | All |
| `mock_test.go` | Mock database driver tests | N/A |

## Test Patterns

### Ground Truth Verification
Integration tests query the database directly via `db.QueryRow`/`db.Query` first, then compare GraphJin's GraphQL results against those values. This ensures correctness, not just "doesn't crash."

```go
// 1. Get ground truth from SQL
var gtName string
err = db.QueryRow(`SELECT name FROM products WHERE id = 1`).Scan(&gtName)

// 2. Run GraphQL
res, err := gj.GraphQL(ctx, `query { products(id: 1) { name } }`, nil, nil)

// 3. Compare
assert.Equal(t, gtName, result.Products.Name)
```

### Database-Specific Skips
Tests skip gracefully for unsupported databases:
```go
if dbType == "mongodb" || dbType == "snowflake" {
    t.Skipf("skipping for %s", dbType)
}
```

### Example Tests
Go's `Example` pattern — output is verified by the test framework:
```go
func Example_query() {
    gql := `query { users { id email } }`
    // ... test code ...
    // Output: {"users":[{"id":1,"email":"user@test.com"}]}
}
```

## Composite FK Tests (`composite_fk_test.go`)

Tests multi-column foreign key joins using `product_variants` + `order_items` tables with composite FK `(product_id, variant_id)`.

| Test | What it verifies |
|------|-----------------|
| `TestCompositeFKJoinOrderItemToVariant` | Forward join returns correct variant (not just first match) |
| `TestCompositeFKAllRowsMatch` | All rows join to correct variant via both FK columns |
| `TestCompositeFKReverseJoin` | Reverse join (variant → order_items) works |

**Runs on:** PostgreSQL, MySQL, MariaDB, SQLite, MSSQL, Oracle (6 DBs)

## Composite PK Tests (`composite_pk_test.go`)

Tests composite primary key support using `product_variants` table with PK `(product_id, variant_id)`.

| Test | What it verifies |
|------|-----------------|
| `TestCompositePKLookup` | `id: {product_id: 1, variant_id: 2}` object argument works |
| `TestCompositePKOrderBy` | Queries on composite PK tables return all rows with correct ordering |
| `TestCompositePKFilterWhere` | WHERE filters work correctly on composite PK tables |

**Runs on:** PostgreSQL, MySQL, MariaDB, MSSQL, Oracle (5 DBs)

## Database Compatibility Matrix

| Feature | PG | MySQL | MariaDB | SQLite | Oracle | MSSQL | Snowflake | MongoDB |
|---------|:--:|:-----:|:-------:|:------:|:------:|:-----:|:---------:|:-------:|
| Basic queries | Y | Y | Y | Y | Y | Y | Y | Y |
| Mutations (insert/update/delete) | Y | Y | Y | Y | Y | Y | Y | Y |
| Composite FK joins | Y | Y | Y | Y | Y | Y | - | - |
| Composite PK lookup | Y | Y | Y | - | Y | Y | - | - |
| Array columns | Y | - | - | - | - | - | - | - |
| Full-text search | Y | Y | - | Limited | - | - | - | - |
| Subscriptions | Y | - | - | - | - | - | - | - |
| PostGIS geo queries | Y | - | - | - | - | - | - | - |
| Recursive queries | Y | Y | Y | - | Y | Y | - | - |
| Cross-schema joins | Y | - | - | - | Y | Y | - | - |
| Multi-database | Y | - | - | - | - | - | - | - |

## Schema Files

Each database has its own schema SQL file loaded at container startup:

| File | Database |
|------|----------|
| `tests/postgres.sql` | PostgreSQL |
| `tests/mysql.sql` | MySQL |
| `tests/mariadb.sql` | MariaDB |
| `tests/sqlite.sql` | SQLite |
| `tests/oracle.sql` | Oracle |
| `tests/mssql.sql` | MSSQL |
| `tests/snowflake.sql` | Snowflake |
| `tests-large/01_adventureworks_schema.sql` | AdventureWorks (PostgreSQL) |
| `tests-large/02_adventureworks_data.sql` | AdventureWorks data (121K+ rows, git LFS) |

All schema files include the shared test tables (`users`, `products`, `categories`, `product_variants`, `order_items`, etc.) plus database-specific syntax.

## Known Issues

### SQLite
- `current_setting()` function not available — tests using `$user_id` context fail
- FTS5 module required (`-tags "sqlite fts5"`)

### MariaDB
- Array column connect mutations return empty results (connect filter does not execute correctly)

### MongoDB
- Composite FK/PK tests skipped (document model, no FK constraints)

### Snowflake
- Composite FK/PK tests skipped (emulator limitations)
- FK introspection relies on custom `_gj_fk_metadata` table

## Contributing Tests

1. **Determine database compatibility** — will it work on all databases?
2. **Add runtime skips** for known incompatibilities:
   ```go
   if dbType == "mongodb" || dbType == "snowflake" {
       t.Skipf("skipping for %s", dbType)
   }
   ```
3. **Use ground truth verification** — query the DB directly, compare results
4. **Add test tables to all relevant schema files** (`postgres.sql`, `mysql.sql`, etc.)
5. **Test on multiple databases** before submitting:
   ```bash
   make test-postgres
   make test-mysql
   make test-mssql
   ```
