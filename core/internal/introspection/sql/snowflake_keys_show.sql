-- PK/UK/FK flags from RESULT_SCAN of SHOW {PRIMARY,UNIQUE,IMPORTED} KEYS (query
-- ids in that order). Emitted as flag-only column rows that merge onto the
-- columns discovered by SHOW COLUMNS. Snowflake restricts INFORMATION_SCHEMA, so
-- SHOW is the only key-metadata source.
WITH pks AS (
	SELECT "schema_name" AS sch, "table_name" AS tbl, "column_name" AS col
	FROM TABLE(RESULT_SCAN(?))
),
uks AS (
	SELECT "schema_name" AS sch, "table_name" AS tbl, "column_name" AS col
	FROM TABLE(RESULT_SCAN(?))
),
fks AS (
	SELECT "fk_schema_name" AS sch, "fk_table_name" AS tbl, "fk_column_name" AS col,
		"pk_schema_name" AS pk_sch, "pk_table_name" AS pk_tbl, "pk_column_name" AS pk_col
	FROM TABLE(RESULT_SCAN(?))
)
SELECT sch AS schema_name, tbl AS table_name, col AS column_name,
	'' AS col_type, FALSE AS not_null, TRUE AS primary_key, FALSE AS unique_key,
	FALSE AS is_array, FALSE AS full_text,
	'' AS foreignkey_schema, '' AS foreignkey_table, '' AS foreignkey_column
FROM pks
UNION ALL
SELECT sch, tbl, col, '', FALSE, FALSE, TRUE, FALSE, FALSE, '', '', '' FROM uks
UNION ALL
SELECT sch, tbl, col, '', FALSE, FALSE, FALSE, FALSE, FALSE, pk_sch, pk_tbl, pk_col FROM fks;
