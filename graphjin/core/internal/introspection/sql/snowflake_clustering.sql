-- Aliased `t` because the DuckDB-backed Snowflake test emulator's SQL
-- rewriter otherwise back-tick quotes `tables` as a reserved word, causing
-- a DuckDB parser error. The alias keeps the unaliased reference out of
-- the rewriter's pattern. Non-fatal either way (clustering data is an
-- optimization, not a correctness requirement).
SELECT LOWER(t.table_schema) AS schema_name,
	LOWER(t.table_name) AS table_name,
	t.clustering_key
FROM information_schema.tables t
WHERE UPPER(t.table_schema) = UPPER(CURRENT_SCHEMA())
	AND t.clustering_key IS NOT NULL
	AND t.clustering_key != '';
