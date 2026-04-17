-- snowflake_columns_basic.sql
--
-- Last-resort column-discovery query for Snowflake accounts where
-- information_schema.KEY_COLUMN_USAGE does not exist. This is the case
-- for many production Snowflake deployments — KEY_COLUMN_USAGE is not a
-- standard Snowflake INFORMATION_SCHEMA view.
--
-- This fallback returns column metadata without primary key, unique key,
-- or foreign key information. GraphJin will still work but relationship
-- discovery won't be automatic — users can declare relationships in
-- config or via the _gj_fk_metadata table.
--
-- Activated only when both snowflake_columns.sql and
-- snowflake_columns_no_overrides.sql fail.
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
			WHEN col.data_type LIKE '%[]' THEN TRUE
			ELSE FALSE
		END
	) AS is_array,
	FALSE AS full_text,
	'' AS foreignkey_schema,
	'' AS foreignkey_table,
	'' AS foreignkey_column
FROM information_schema.columns col
WHERE col.table_schema NOT IN ('INFORMATION_SCHEMA');
