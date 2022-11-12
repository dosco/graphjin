SELECT 
	r.specific_name 	as func_id,
	r.routine_schema 	as func_schema,
	r.routine_name 		as func_name, 
	r.data_type 		as data_type,
	p.ordinal_position	as param_id,
	COALESCE(p.parameter_name, CAST(p.ordinal_position as CHAR(3))) as param_name,
	p.data_type 		as param_type,
	p.parameter_mode 	as param_kind
FROM
	information_schema.routines r
RIGHT JOIN 
	information_schema.parameters p
	ON (r.specific_name = p.specific_name and p.ordinal_position IS NOT NULL)	
WHERE 
	p.specific_schema NOT IN ('_graphjin', 'information_schema', 'performance_schema', 'pg_catalog', 'mysql', 'sys')
	AND r.external_language NOT IN ('C')
ORDER BY 
	r.routine_name, p.ordinal_position;