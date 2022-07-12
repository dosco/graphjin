SELECT 
	r.routine_name as func_name, 
	p.specific_name as func_id,
	p.data_type as func_type, 
	p.parameter_name as param_name,
	p.ordinal_position	as param_id
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