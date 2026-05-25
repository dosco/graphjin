SELECT m.table_schema AS schema_name,
	m.table_name AS table_name,
	m.column_name AS column_name,
	'' AS col_type,
	FALSE AS not_null,
	FALSE AS primary_key,
	FALSE AS unique_key,
	FALSE AS is_array,
	FALSE AS full_text,
	m.foreign_table_schema AS foreignkey_schema,
	m.foreign_table_name AS foreignkey_table,
	m.foreign_column_name AS foreignkey_column
FROM _gj_fk_metadata m
WHERE UPPER(m.table_schema) = UPPER(CURRENT_SCHEMA());
