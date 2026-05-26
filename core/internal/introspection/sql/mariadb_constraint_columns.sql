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
	AND kcu.constraint_schema = tc.constraint_schema
	AND kcu.constraint_name = tc.constraint_name
WHERE kcu.constraint_schema = DATABASE()
	AND tc.constraint_type IN ('PRIMARY KEY', 'UNIQUE', 'FOREIGN KEY');
