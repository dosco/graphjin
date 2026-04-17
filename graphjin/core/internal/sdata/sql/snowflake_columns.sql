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
WHERE col.table_schema NOT IN ('INFORMATION_SCHEMA')
UNION
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
	(
		CASE
			WHEN tc.constraint_type = 'UNIQUE' THEN TRUE
			ELSE FALSE
		END
	) AS unique_key,
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
UNION
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
WHERE fk_kcu.table_schema NOT IN ('INFORMATION_SCHEMA')
UNION
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
FROM _gj_fk_metadata m;
