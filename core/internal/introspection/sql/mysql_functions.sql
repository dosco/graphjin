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
	AND p.specific_schema NOT IN (
		'_graphjin',
		'information_schema',
		'performance_schema',
		'mysql',
		'sys'
	);