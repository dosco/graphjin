SELECT fk_kcu.table_schema AS schema_name,
	fk_kcu.table_name AS table_name,
	fk_kcu.column_name AS column_name,
	'' AS col_type,
	FALSE AS not_null,
	FALSE AS primary_key,
	FALSE AS unique_key,
	FALSE AS is_array,
	FALSE AS full_text,
	pk_kcu.table_schema AS foreignkey_schema,
	pk_kcu.table_name AS foreignkey_table,
	pk_kcu.column_name AS foreignkey_column
FROM information_schema.referential_constraints rc
	JOIN information_schema.key_column_usage fk_kcu ON (
		rc.constraint_catalog = fk_kcu.constraint_catalog
		AND rc.constraint_schema = fk_kcu.constraint_schema
		AND rc.constraint_name = fk_kcu.constraint_name
	)
	JOIN information_schema.key_column_usage pk_kcu ON (
		rc.unique_constraint_catalog = pk_kcu.constraint_catalog
		AND rc.unique_constraint_schema = pk_kcu.constraint_schema
		AND rc.unique_constraint_name = pk_kcu.constraint_name
		AND fk_kcu.position_in_unique_constraint = pk_kcu.ordinal_position
	)
WHERE UPPER(fk_kcu.table_schema) = UPPER(CURRENT_SCHEMA());
