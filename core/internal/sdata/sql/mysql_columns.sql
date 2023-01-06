SELECT col.table_schema as "schema",
	col.table_name as "table",
	col.column_name as "column",
	col.data_type as "type",
	(
		CASE
			WHEN col.is_nullable = 'YES' THEN TRUE
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
WHERE col.table_schema NOT IN (
		'_graphjin',
		'information_schema',
		'performance_schema',
		'mysql',
		'sys'
	)
UNION
SELECT kcu.table_schema as "schema",
	kcu.table_name as "table",
	kcu.column_name as "column",
	'' as "type",
	false AS not_null,
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
	false AS is_array,
	false AS full_text,
	(
		CASE
			WHEN tc.constraint_type = 'FOREIGN KEY' THEN kcu.referenced_table_schema
			ELSE ''
		END
	) AS foreignkey_schema,
	(
		CASE
			WHEN tc.constraint_type = 'FOREIGN KEY' THEN kcu.referenced_table_name
			ELSE ''
		END
	) AS foreignkey_table,
	(
		CASE
			WHEN tc.constraint_type = 'FOREIGN KEY' THEN kcu.referenced_column_name
			ELSE ''
		END
	) AS foreignkey_column
FROM information_schema.key_column_usage kcu
	JOIN information_schema.table_constraints tc ON kcu.table_schema = tc.table_schema
	AND kcu.table_name = tc.table_name
	AND kcu.constraint_name = tc.constraint_name
WHERE kcu.constraint_schema NOT IN (
		'_graphjin',
		'information_schema',
		'performance_schema',
		'mysql',
		'sys'
	);