-- Last-resort column discovery for Snowflake accounts where both scoped
-- INFORMATION_SCHEMA enrichment and SHOW-based discovery are unavailable.
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
WHERE UPPER(col.table_schema) NOT IN ('INFORMATION_SCHEMA');
