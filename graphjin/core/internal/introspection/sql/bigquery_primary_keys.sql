SELECT kcu.table_schema AS schema_name,
	kcu.table_name AS table_name,
	kcu.column_name AS column_name,
	'' AS col_type,
	FALSE AS not_null,
	(
		CASE
			WHEN tc.constraint_type = 'PRIMARY KEY' THEN TRUE
			ELSE FALSE
		END
	) AS primary_key,
	FALSE AS unique_key,
	FALSE AS is_array,
	FALSE AS full_text,
	'' AS foreignkey_schema,
	'' AS foreignkey_table,
	'' AS foreignkey_column
FROM information_schema.key_column_usage kcu
	JOIN information_schema.table_constraints tc ON (
		kcu.constraint_catalog = tc.constraint_catalog
		AND kcu.constraint_schema = tc.constraint_schema
		AND kcu.constraint_name = tc.constraint_name
	)
WHERE kcu.table_schema NOT IN ('INFORMATION_SCHEMA')
	AND (
		COALESCE(@@dataset_id, '') = ''
		OR LOWER(kcu.table_schema) = LOWER(COALESCE(@@dataset_id, ''))
	)
	AND tc.constraint_type = 'PRIMARY KEY';
