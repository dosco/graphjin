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
FROM information_schema.key_column_usage fk_kcu
	JOIN information_schema.table_constraints tc ON (
		fk_kcu.constraint_catalog = tc.constraint_catalog
		AND fk_kcu.constraint_schema = tc.constraint_schema
		AND fk_kcu.constraint_name = tc.constraint_name
	)
	JOIN information_schema.constraint_column_usage ccu ON (
		ccu.constraint_catalog = fk_kcu.constraint_catalog
		AND ccu.constraint_schema = fk_kcu.constraint_schema
		AND ccu.constraint_name = fk_kcu.constraint_name
	)
	JOIN information_schema.key_column_usage pk_kcu ON (
		pk_kcu.table_schema = ccu.table_schema
		AND pk_kcu.table_name = ccu.table_name
		AND pk_kcu.ordinal_position = fk_kcu.position_in_unique_constraint
	)
	JOIN information_schema.table_constraints pk_tc ON (
		pk_tc.constraint_catalog = pk_kcu.constraint_catalog
		AND pk_tc.constraint_schema = pk_kcu.constraint_schema
		AND pk_tc.constraint_name = pk_kcu.constraint_name
		AND pk_tc.constraint_type = 'PRIMARY KEY'
	)
WHERE fk_kcu.table_schema NOT IN ('INFORMATION_SCHEMA')
	AND (
		COALESCE(@@dataset_id, '') = ''
		OR LOWER(fk_kcu.table_schema) = LOWER(COALESCE(@@dataset_id, ''))
	)
	AND tc.constraint_type = 'FOREIGN KEY';
