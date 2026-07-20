# Database Addition Checklist

## Simulator

- Create `tests/hostedemu/<db>` with an adapter implementing parse, setup translation, discovery query translation, direct/runtime translation, type mapping, identifier normalization, and phase classification.
- Create `tests/<db>emu` as the public test wrapper over `hostedemu.NewConnector`.
- Add `tests/<db>.sql` with target-native DDL and seed data. Include features that discovery must surface, such as constraints, distribution, clustering, partitioning, encodings, comments, complex types, and unsupported constructs.
- Add tests for:
  - DDL parsing and target-specific syntax;
  - conservative type mapping to DuckDB;
  - metadata rows for tables, columns, keys, and target-specific physical layout;
  - docs-native discovery commands/views;
  - unsupported syntax that looks valid in a neighboring database;
  - large catalogs proving discovery is batched and not per-table.

## Public Runtime

- Add the database to `SupportedDBTypes` and `SupportedMultiDBTypes` only when public support is intended.
- Register a target-owned dialect in `core/internal/psql/query.go` and subscription lookup in `core/subs.go`.
- Implement introspection in `core/internal/introspection/tables.go` using the database's recommended discovery path first.
- Add schema DDL/diff support only if public docs will claim migrations or DDL generation.
- Add test harness wiring in `tests/dbint_test.go`, a `scripts/test-<db>.sh` script, and `scripts/test-parallel.sh` inclusion.
- Add focused skip helpers for unimplemented features. Skips must say what is unsupported and must not hide syntax bugs for claimed features.
- Verify application-database selection ignores the reserved `__graphjin_artifacts` managed SQLite store while the internal compiler/runtime graph can still compile its control-plane tables.
- Verify `graphjin serve new <app> --db-url <url>` renders parseable production and agentic sources for the database, including separate host and port values and URL-decoded credentials.

## Public Surfaces

- README: installation/support matrix and one concise note about support level.
- CONFIG: connection example, auth/env vars, feature limitations, and simulator/live test knobs if useful.
- FEATURES: support table and any feature-specific limitations.
- Website: `DatabaseLogos.astro`, `DatabaseMatrix.astro`, frontpage copy if it names database families, and a logo asset or existing label pattern.
- Keep `dev` / `agentic` configuration examples minimal: managed artifacts, watches, agent, stateful MCP HTTP, and primitive tools come from mode defaults. Only clustered examples should select a shared artifact SQL source explicitly.

## Redshift Notes

- Use the Amazon Redshift guide as authority. Redshift is based on PostgreSQL, but it has materially different SQL, types, and unsupported PostgreSQL features.
- Prefer SHOW-compatible discovery for public support: `SHOW DATABASES`, `SHOW SCHEMAS`, `SHOW TABLES`, and `SHOW COLUMNS`.
- Simulate Redshift catalog views needed by GraphJin: `SVV_ALL_COLUMNS`, `SVV_REDSHIFT_COLUMNS`, `SVV_ALL_TABLES`, `SVV_TABLES`, and `PG_TABLE_DEF`.
- Treat `SUPER`, `HLLSKETCH`, `GEOMETRY`, and `GEOGRAPHY` as inert values unless tests prove richer semantics.
- Do not claim mutations, subscriptions, full-text, GIS, or full PostgreSQL compatibility until Redshift-specific tests pass.
