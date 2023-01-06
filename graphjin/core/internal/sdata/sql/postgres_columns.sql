SELECT n.nspname as "schema",
	c.relname as "table",
	f.attname AS "column",
	pg_catalog.format_type(f.atttypid, f.atttypmod) AS "type",
	f.attnotnull AS not_null,
	(
		CASE
			WHEN co.contype = ('p'::char) THEN true
			ELSE false
		END
	) AS primary_key,
	(
		CASE
			WHEN co.contype = ('u'::char) THEN true
			ELSE false
		END
	) AS unique_key,
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
			WHEN pg_catalog.format_type(f.atttypid, f.atttypmod) = 'tsvector' THEN TRUE
			ELSE FALSE
		END
	) AS full_text,
	(
		CASE
			WHEN co.contype = ('f'::char) THEN (
				SELECT n.nspname
				FROM pg_class c
					JOIN pg_namespace n ON n.oid = c.relnamespace
				WHERE c.oid = co.confrelid
			)
			ELSE ''::text
		END
	) AS foreignkey_schema,
	(
		CASE
			WHEN co.contype = ('f'::char) THEN (
				SELECT relname
				FROM pg_class
				WHERE oid = co.confrelid
			)
			ELSE ''::text
		END
	) AS foreignkey_table,
	(
		CASE
			WHEN co.contype = ('f'::char) THEN (
				SELECT f.attname
				FROM pg_attribute f
				WHERE f.attnum = co.confkey [1]
					and f.attrelid = co.confrelid
			)
			ELSE ''::text
		END
	) AS foreignkey_column
FROM pg_attribute f
	JOIN pg_class c ON c.oid = f.attrelid
	LEFT JOIN pg_attrdef d ON d.adrelid = c.oid
	AND d.adnum = f.attnum
	LEFT JOIN pg_namespace n ON n.oid = c.relnamespace
	LEFT JOIN pg_constraint co ON co.conrelid = c.oid
	AND f.attnum = ANY (co.conkey)
WHERE c.relkind IN ('r', 'v', 'm', 'f', 'p')
	AND n.nspname NOT IN ('_graphjin', 'information_schema', 'pg_catalog')
	AND c.relname != 'schema_version'
	AND f.attnum > 0
	AND f.attisdropped = false
ORDER BY f.attrelid,
	f.attnum ASC;