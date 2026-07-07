SELECT n.nspname as "schema",
	c.relname as "table",
	f.attname AS "column",
	pg_catalog.format_type(f.atttypid, f.atttypmod) AS "type",
	f.attnotnull AS not_null,
	false AS primary_key,
	false AS unique_key,
	(
		CASE
			WHEN f.attndims != 0 THEN true
			WHEN right(
				pg_catalog.format_type(f.atttypid, f.atttypmod),
				2
			) = '[]' THEN true
			ELSE false
		END
	) AS is_array,
	(
		CASE
			WHEN pg_catalog.format_type(f.atttypid, f.atttypmod) = 'tsvector' THEN true
			ELSE false
		END
	) AS full_text,
	''::text AS foreignkey_schema,
	''::text AS foreignkey_table,
	''::text AS foreignkey_column
FROM pg_attribute f
	JOIN pg_class c ON c.oid = f.attrelid
	JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE c.relkind IN ('r', 'v', 'm', 'f', 'p')
	AND n.nspname NOT IN ('information_schema', 'pg_catalog')
	AND n.nspname NOT LIKE 'pg_toast%'
	AND n.nspname NOT LIKE 'pg_temp_%'
	AND n.nspname NOT LIKE 'pg_toast_temp_%'
	AND c.relname != 'schema_version'
	AND f.attnum > 0
	AND f.attisdropped = false
ORDER BY f.attrelid,
	f.attnum ASC;
