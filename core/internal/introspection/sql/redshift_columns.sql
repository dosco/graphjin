SELECT
	schema_name,
	table_name,
	column_name,
	data_type AS col_type,
	COALESCE(not_null, is_nullable = 'NO', FALSE) AS not_null,
	COALESCE(primary_key, FALSE) AS primary_key,
	COALESCE(unique_key, FALSE) AS unique_key,
	FALSE AS is_array,
	FALSE AS full_text,
	COALESCE(foreignkey_schema, '') AS foreignkey_schema,
	COALESCE(foreignkey_table, '') AS foreignkey_table,
	COALESCE(foreignkey_column, '') AS foreignkey_column
FROM svv_all_columns
WHERE table_name NOT LIKE '\_gj\_%' ESCAPE '\'
ORDER BY schema_name, table_name, ordinal_position;
