---
name: add-graphjin-database
description: Use when adding a new GraphJin database, warehouse, or CQL/NoSQL backend; building a simulator because no live service is available; wiring a dialect, discovery, tests, scripts, README/CONFIG/FEATURES, or website database support surfaces.
---

# Add GraphJin Database

Use this skill to add a database end to end without leaving half-public support behind. GraphJin is a compiler, not a resolver stack: push behavior into dialect, discovery, schema metadata, and tests.

## Workflow

1. Ground in docs and repo truth.
   - Read the database's official SQL, DDL, type, catalog, and limitations docs before coding.
   - Inspect existing GraphJin seams: config validation, dialect factory, introspection, `tests/dbint_test.go`, hosted emulators, scripts, README, CONFIG, FEATURES, and website components.
   - Decide and state the public support level: experimental queries, mutations, subscriptions, full support, or simulator-only.

2. Build a simulator first when live service access is unavailable or slow.
   - Add `tests/hostedemu/<db>` and `tests/<db>emu` around `hostedemu.NewConnector`.
   - Add `tests/<db>.sql` using database-native DDL, types, constraints, and metadata features.
   - Keep parser/translator code owned by the target database. Reuse hosted emulator patterns, not another database's grammar, unless docs prove the syntax is identical.
   - Translate setup and discovery into DuckDB, but expose the database's real discovery surfaces.
   - Add parser, type mapping, metadata, discovery, wrapper, default-fixture, and large-catalog tests.

3. Use the simulator to build public support.
   - Register the DB type in `core/config.go`, the psql dialect factory, subscription dialect lookup, introspection switches, and schema-DDL/diff code if public DDL support is claimed.
   - Add a DB-owned dialect. Inherit behavior only after target-specific tests prove it.
   - Add discovery using the docs-recommended catalog path first, with fallbacks only where needed.
   - Wire `tests/dbint_test.go`, focused skip helpers for unsupported features, and `scripts/test-<db>.sh`.
   - Keep GraphJin's reserved `__graphjin_artifacts` managed SQLite database internal. It belongs in the compiler/runtime graph but must never become an application source, primary database candidate, catalog source, or security-report database.

4. Update public surfaces only after the runtime is wired.
   - Update README, CONFIG, FEATURES, website database logos/frontpage copy, website database matrix, and test-parallel scripts.
   - If the database uses `graphjin serve new --db-url`, update the URL parser and the rendered `cmd/tmpl/prod.yml` and `cmd/tmpl/agentic.yml` sources. Verify a real generated app loads without test-only placeholder replacement.
   - Make feature claims match tests. Use "experimental" and "query/discovery only" when writes, subscriptions, GIS, full-text, or migrations are not verified.
   - Preserve unrelated dirty worktree edits. Patch on top; never revert user changes.
   - Do not add artifact, watch, agent, stateful-MCP, or primitive-tool enablement to new `dev.yml` / `agentic.yml` examples; parsed mode defaults already provide them. Configure an artifact source only when documenting a shared clustered store.

5. Verify and leave evidence.
   - Run simulator tests, dialect/introspection tests, the new `scripts/test-<db>.sh`, and affected shared suites.
   - If shared compiler or introspection code changed, run existing dialect scripts likely to be affected.
   - Before finalizing, recheck docs/website claims against the support level that actually passed.

## References

- Read `references/checklist.md` for the detailed implementation checklist and Redshift-specific notes.
