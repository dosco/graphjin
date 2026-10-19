SELECT col.table_schema as "schema",
	col.table_name as "table",
	col.column_name as "column",
	-- MariaDB: JSON columns are stored as LONGTEXT with json_valid check constraint.
	(
		CASE
			WHEN col.data_type = 'longtext' AND EXISTS (
				SELECT 1 FROM information_schema.check_constraints chk
				WHERE chk.constraint_schema = col.table_schema
					AND REPLACE(LOWER(chk.check_clause), '`', '') LIKE CONCAT('%json_valid%', LOWER(col.column_name), '%')
			) THEN 'json'
			ELSE LOWER(col.data_type)
		END
	) as "type",
	(
		CASE
			WHEN col.is_nullable = 'NO' THEN TRUE
			ELSE FALSE
		END
	) AS not_null,
	false AS primary_key,
	false AS unique_key,
	(
		CASE
			WHEN col.data_type = 'ARRAY' THEN TRUE
			ELSE FALSE
		END
	) AS is_array,
	(
		CASE
			WHEN stat.index_type = 'FULLTEXT' THEN TRUE
			ELSE FALSE
		END
	) AS full_text,
	'' AS foreignkey_schema,
	'' AS foreignkey_table,
	'' AS foreignkey_column
FROM information_schema.columns col
	LEFT JOIN information_schema.statistics stat ON col.table_schema = stat.table_schema
	AND col.table_name = stat.table_name
	AND col.column_name = stat.column_name
	AND stat.index_type = 'FULLTEXT'
WHERE col.table_schema = DATABASE()
	AND col.table_schema NOT IN (
		'_graphjin',
		'information_schema',
		'performance_schema',
		'mysql',
		'sys'
	);
