SELECT n.nspname as "schema",
	c.relname as "table",
	a.attname AS "column",
	''::text AS "type",
	false AS not_null,
	co.contype = 'p' AS primary_key,
	(co.contype = 'u' AND cardinality(co.conkey) = 1) AS unique_key,
	false AS is_array,
	false AS full_text,
	COALESCE(fn.nspname, '') AS foreignkey_schema,
	COALESCE(fc.relname, '') AS foreignkey_table,
	COALESCE(fa.attname, '') AS foreignkey_column
FROM pg_constraint co
	JOIN pg_class c ON c.oid = co.conrelid
	JOIN pg_namespace n ON n.oid = c.relnamespace
	JOIN unnest(co.conkey) WITH ORDINALITY ck(attnum, ord) ON true
	JOIN pg_attribute a ON a.attrelid = c.oid
		AND a.attnum = ck.attnum
		AND NOT a.attisdropped
	LEFT JOIN pg_class fc ON fc.oid = co.confrelid
		AND co.contype = 'f'
	LEFT JOIN pg_namespace fn ON fn.oid = fc.relnamespace
	LEFT JOIN unnest(co.confkey) WITH ORDINALITY fk(attnum, ord)
		ON co.contype = 'f'
		AND fk.ord = ck.ord
	LEFT JOIN pg_attribute fa ON fa.attrelid = co.confrelid
		AND fa.attnum = fk.attnum
		AND NOT fa.attisdropped
WHERE co.contype IN ('p', 'u', 'f')
	AND n.nspname NOT IN ('information_schema', 'pg_catalog')
	AND n.nspname NOT LIKE 'pg_toast%'
	AND n.nspname NOT LIKE 'pg_temp_%'
	AND n.nspname NOT LIKE 'pg_toast_temp_%'
	AND c.relname != 'schema_version'
ORDER BY c.oid,
	ck.ord ASC;
