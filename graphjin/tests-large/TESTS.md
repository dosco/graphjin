# Large-Scale Integration Tests

## Overview

The `tests-large/` directory contains large-scale integration test fixtures that exercise GraphJin against realistic, production-scale databases. These are separated from `tests/` because:

1. **Data size** — fixtures contain 121K+ rows and are stored via git LFS
2. **Runtime** — tests take 10-20 minutes due to schema complexity (70+ tables, 10 schemas)
3. **Scope** — they test cross-schema joins, composite FKs, deep nesting, and fan-out queries that smaller test schemas can't cover

## AdventureWorks

Microsoft's [AdventureWorks](https://learn.microsoft.com/en-us/sql/samples/adventureworks-install-configure) sample database, ported to PostgreSQL. It models a bicycle manufacturer with sales, production, purchasing, and HR data.

### Database Stats

| Metric | Value |
|--------|-------|
| Tables | 70 across 10 schemas |
| Schemas | `person`, `production`, `sales`, `purchasing`, `humanresources` (+ 5 system) |
| Largest table | `sales.salesorderdetail` — 121,317 rows |
| Total rows | ~275K |

### Files

| File | Description | Size |
|------|-------------|------|
| `01_adventureworks_schema.sql` | DDL: all tables, constraints, indexes | ~50KB |
| `02_adventureworks_data.sql` | INSERT statements (git LFS) | ~45MB |
| `ADVENTUREWORKS_TESTS.md` | Full 24-test matrix with ground truth SQL | — |

### Running

```bash
make test-adventureworks
# or
cd tests && go test -v -timeout 60m -db=adventureworks -run TestAdventureWorks .
```

Each test takes ~16s due to schema discovery (91 tables). The full suite runs in ~8 minutes.

### Test Structure

All 24 tests are in `tests/adventureworks_test.go`. Every test:

1. Runs the equivalent SQL query directly via `db.QueryRow`/`db.Query` (ground truth)
2. Runs the GraphQL query through GraphJin
3. Compares results field-by-field

### What It Exercises

- **Cross-schema joins** (7 tests): Joins across `sales`, `person`, `production`, `humanresources`, `purchasing`
- **Composite foreign keys** (2 tests): Multi-column FK `(productid, specialofferid)` on `salesorderdetail → specialofferproduct`
- **Deep nesting** (3 tests): Up to 7-table / 6-level joins
- **Fan-out queries** (4 tests): Multiple child tables from one parent
- **Large dataset filtering** (1 test): ORDER BY + LIMIT on 121K rows
- **Many-to-many** (1 test): Junction table traversal
- **Manufacturing BOM** (1 test): Self-referencing product assembly hierarchy

### Test Matrix

See `ADVENTUREWORKS_TESTS.md` for the full 24-row matrix with user queries, ground truth SQL, test names, and what each test exercises.

### Bugs Found

These tests uncovered 3 bugs that were fixed:

1. **`renderJoin` schema qualification** — intermediate JOIN tables rendered without schema prefix
2. **Composite FK columns missing from subquery SELECT** — extra pair columns not in parent SELECT list
3. **FK column disambiguation** — FK columns misinterpreted as relationship joins in WHERE filters

## Adding New Large-Scale Tests

1. Place schema and data SQL files in `tests-large/` with numeric prefixes (`01_`, `02_`) for load ordering
2. Large data files (>1MB) should be tracked with git LFS: `git lfs track "tests-large/*.sql"`
3. Add the database entry in `tests/dbint_test.go` `dbinfoList` with `disable: true` (opt-in via `-db=` flag)
4. Write tests in `tests/` — they share the same package and test infrastructure
5. Use ground truth verification for every test
6. Document the test matrix in a `*_TESTS.md` file alongside the fixtures
