WITH row_type_routines AS (
	-- Functions whose result is a row type: RETURNS [SETOF] tbl, RETURNS [SETOF]
	-- composite_type and RETURNS TABLE (LIKE tbl).
	SELECT r.specific_name,
		t.typrelid
	FROM information_schema.routines r
		JOIN pg_catalog.pg_namespace n ON n.nspname = r.type_udt_schema
		JOIN pg_catalog.pg_type t ON t.typnamespace = n.oid
		AND t.typname = r.type_udt_name
	WHERE r.routine_type = 'FUNCTION'
		AND r.data_type = 'USER-DEFINED'
		AND t.typrelid != 0
)
SELECT r.specific_name as func_id,
	r.routine_schema as func_schema,
	r.routine_name as func_name,
	(
		CASE
			WHEN r.data_type = 'USER-DEFINED' THEN 'record'
			ELSE r.data_type
		END
	) as data_type,
	p.ordinal_position as param_id,
	COALESCE(p.parameter_name, '') as param_name,
	p.data_type as param_type,
	COALESCE(p.parameter_mode, '') as param_kind
FROM information_schema.routines r
	RIGHT JOIN information_schema.parameters p ON (
		r.specific_name = p.specific_name
		AND r.specific_name = p.specific_name
	)
WHERE r.routine_type = 'FUNCTION'
	AND r.data_type != 'void'
	-- The single OUT parameter of a row type function holds the whole row.
	-- Postgres expands it into the row's columns, returned by the SELECT below.
	AND NOT (
		p.parameter_mode = 'OUT'
		AND r.specific_name IN (
			SELECT specific_name
			FROM row_type_routines
		)
	)
	AND r.specific_schema NOT IN (
		'_graphjin',
		'information_schema',
		'performance_schema',
		'pg_catalog',
		'mysql',
		'sys'
	)
UNION
SELECT r.specific_name as func_id,
	r.routine_schema as func_schema,
	r.routine_name as func_name,
	'record' as data_type,
	f.attnum as param_id,
	f.attname as param_name,
	pg_catalog.format_type(f.atttypid, f.atttypmod) as param_type,
	'OUT' as param_kind
FROM information_schema.routines r
	JOIN row_type_routines rt ON rt.specific_name = r.specific_name
	JOIN pg_catalog.pg_attribute f ON f.attrelid = rt.typrelid
WHERE f.attnum > 0
	AND f.attisdropped = false
	AND r.specific_schema NOT IN (
		'_graphjin',
		'information_schema',
		'performance_schema',
		'pg_catalog',
		'mysql',
		'sys'
	);
