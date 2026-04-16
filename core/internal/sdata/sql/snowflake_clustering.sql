-- Aliased `t` because the SQL rewriter in some Snowflake test emulators
-- back-tick quotes `tables` as a reserved word otherwise. The alias keeps
-- the unaliased reference out of the rewriter's pattern. Non-fatal either
-- way (clustering data is an optimization, not a correctness requirement).
-- Identifiers are returned in their original case.
SELECT t.table_schema AS schema_name,
	t.table_name AS table_name,
	t.clustering_key
FROM information_schema.tables t
WHERE t.table_schema NOT IN ('INFORMATION_SCHEMA')
	AND t.clustering_key IS NOT NULL
	AND t.clustering_key != '';
