-- Snowflake column discovery.
--
-- Snowflake's INFORMATION_SCHEMA does NOT expose KEY_COLUMN_USAGE (it's a
-- standard-SQL view present in Postgres/MySQL but omitted by Snowflake).
-- TABLE_CONSTRAINTS and REFERENTIAL_CONSTRAINTS exist but do not carry
-- column-level info — they only list constraint names and types. The only
-- reliable Snowflake-native source of column-level PK/UK/FK information is
-- the SHOW PRIMARY KEYS / SHOW UNIQUE KEYS / SHOW IMPORTED KEYS commands,
-- which the Go layer runs separately and merges on top of this result set.
--
-- This query therefore returns columns only (no constraint info). Identifier
-- casing is preserved — unquoted identifiers are stored UPPERCASE in
-- Snowflake; quoted objects keep their stored case. GraphJin's case-
-- insensitive lookup layer (dwg.go) resolves GraphQL identifiers against the
-- stored case at query time.
SELECT col.table_schema AS schema_name,
	col.table_name AS table_name,
	col.column_name AS column_name,
	LOWER(col.data_type) AS col_type,
	(
		CASE
			WHEN col.is_nullable = 'NO' THEN TRUE
			ELSE FALSE
		END
	) AS not_null,
	FALSE AS primary_key,
	FALSE AS unique_key,
	(
		CASE
			WHEN UPPER(col.data_type) = 'ARRAY'
			OR col.data_type LIKE '%[]' THEN TRUE
			ELSE FALSE
		END
	) AS is_array,
	FALSE AS full_text,
	'' AS foreignkey_schema,
	'' AS foreignkey_table,
	'' AS foreignkey_column
FROM information_schema.columns col
WHERE col.table_schema NOT IN ('INFORMATION_SCHEMA')
  -- Scope discovery to the connection's current schema so orphan
  -- schemas from prior test runs (or unrelated app data in the same
  -- database) don't leak in as duplicate table entries — GraphJin's
  -- compiler would then pin one table copy to schema A and another
  -- copy to schema B in the same query, producing invalid cross-schema
  -- SQL at mutation/insert time.
  AND col.table_schema = CURRENT_SCHEMA();
