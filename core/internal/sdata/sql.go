package sdata

const functionsStmt = `
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
	p.specific_schema NOT IN ('information_schema', 'performance_schema', 'pg_catalog', 'mysql', 'sys')
ORDER BY 
	r.routine_name, p.ordinal_position;
`

const postgresInfo = `
SELECT 
	CAST(current_setting('server_version_num') AS integer) as db_version,
	current_schema() as db_schema,
	current_database() as db_name;
`

const postgresColumnsStmt = `
SELECT  
	n.nspname as "schema",
	c.relname as "table",
	f.attname AS "column",  
	pg_catalog.format_type(f.atttypid,f.atttypmod) AS "type",  
	f.attnotnull AS not_null,  
	(CASE  
		WHEN co.contype = ('p'::char) THEN true  
		ELSE false 
	END) AS primary_key,  
	(CASE  
		WHEN co.contype = ('u'::char) THEN true  
		ELSE false
	END) AS unique_key,
	(CASE
		WHEN f.attndims != 0 THEN true
		WHEN right(pg_catalog.format_type(f.atttypid,f.atttypmod), 2) = '[]' THEN true
		ELSE false
	END) AS is_array,
	(CASE
		WHEN pg_catalog.format_type(f.atttypid,f.atttypmod) = 'tsvector' THEN TRUE  
		ELSE FALSE
	END) AS full_text,
	(CASE
		WHEN co.contype = ('f'::char) 
		THEN (SELECT n.nspname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE c.oid = co.confrelid)
		ELSE ''::text
	END) AS foreignkey_schema,
	(CASE
		WHEN co.contype = ('f'::char) 
		THEN (SELECT relname FROM pg_class WHERE oid = co.confrelid)
		ELSE ''::text
	END) AS foreignkey_table,
	(CASE
		WHEN co.contype = ('f'::char) 
		THEN (SELECT f.attname FROM pg_attribute f WHERE f.attnum = co.confkey[1] and f.attrelid = co.confrelid)
		ELSE ''::text
	END) AS foreignkey_column
FROM 
	pg_attribute f
	JOIN pg_class c ON c.oid = f.attrelid  
	LEFT JOIN pg_attrdef d ON d.adrelid = c.oid AND d.adnum = f.attnum  
	LEFT JOIN pg_namespace n ON n.oid = c.relnamespace  
	LEFT JOIN pg_constraint co ON co.conrelid = c.oid AND f.attnum = ANY (co.conkey) 
WHERE 
	c.relkind IN ('r', 'v', 'm', 'f')
	AND n.nspname NOT IN ('information_schema', 'pg_catalog') 
	AND c.relname != 'schema_version'
	AND f.attnum > 0
	AND f.attisdropped = false;
`

const mysqlInfo = `
SELECT 
		a.c as db_version, 
    b.c as db_schema, 
    b.c as db_name 
FROM 
	(SELECT CONVERT(REPLACE(VERSION(), '.', ''), SIGNED INTEGER) as c) as a, 
    (SELECT DATABASE() as c) as b;
`

const mysqlColumnsStmt = `
SELECT 
	col.table_schema as "schema",
	col.table_name as "table",
	col.column_name as "column",
	col.data_type as "type",
  (CASE
		WHEN col.is_nullable = 'YES' THEN TRUE  
		ELSE FALSE
	END) AS not_null,
	false AS primary_key,
	false AS unique_key,
	(CASE
		WHEN col.data_type = 'ARRAY' THEN TRUE  
		ELSE FALSE
	END) AS is_array,
	(CASE
		WHEN stat.index_type = 'FULLTEXT' THEN TRUE  
		ELSE FALSE
	END) AS full_text,
	'' AS foreignkey_schema,
	'' AS foreignkey_table,
	'' AS foreignkey_column
FROM 
	information_schema.columns col
LEFT JOIN information_schema.statistics stat ON col.table_schema = stat.table_schema
	AND col.table_name = stat.table_name
  AND col.column_name = stat.column_name
  AND stat.index_type = 'FULLTEXT'
WHERE
	col.table_schema NOT IN ('information_schema', 'performance_schema', 'mysql', 'sys')
UNION 
SELECT
	kcu.table_schema as "schema",
	kcu.table_name as "table",
	kcu.column_name as "column",
	'' as "type",
	false AS not_null,
	(CASE
		WHEN tc.constraint_type = 'PRIMARY KEY' THEN TRUE  
		ELSE FALSE
	END) AS primary_key,
	(CASE  
		WHEN tc.constraint_type = 'UNIQUE' THEN TRUE
		ELSE FALSE
	END) AS unique_key,
	false AS is_array,
	false AS full_text,
	(CASE
		WHEN tc.constraint_type = 'FOREIGN KEY' THEN kcu.referenced_table_schema
		ELSE ''
	END) AS foreignkey_schema,
	(CASE
		WHEN tc.constraint_type = 'FOREIGN KEY' THEN kcu.referenced_table_name
		ELSE ''
	END) AS foreignkey_table,
	(CASE
		WHEN tc.constraint_type = 'FOREIGN KEY' THEN kcu.referenced_column_name
		ELSE ''
	END) AS foreignkey_column
FROM 
	information_schema.key_column_usage kcu
JOIN
	information_schema.table_constraints tc ON kcu.table_schema = tc.table_schema
	AND kcu.table_name = tc.table_name
  	AND kcu.constraint_name = tc.constraint_name
WHERE
	kcu.constraint_schema NOT IN ('information_schema', 'performance_schema', 'mysql', 'sys');
`
