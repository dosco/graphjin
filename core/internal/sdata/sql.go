package sdata

const postgresColumnInfo = `
SELECT 
	col.table_name as table,
	col.column_name as name,
	col.data_type as "type",
	(CASE
		WHEN col.is_nullable = 'YES' THEN TRUE  
		ELSE FALSE
	END) AS notnull,
	(CASE
		WHEN col.data_type = 'ARRAY' THEN TRUE  
		ELSE FALSE
	END) AS isarray,
	(CASE
		WHEN tc.constraint_type = 'PRIMARY KEY' THEN TRUE  
		ELSE FALSE
	END) AS primarykey,
	(CASE  
		WHEN tc.constraint_type = 'UNIQUE' THEN TRUE
		ELSE FALSE
	END) AS uniquekey,
	(CASE
		WHEN tc.constraint_type = 'FOREIGN KEY' THEN ccu.table_schema
		ELSE ''
	END) AS foreignkey_schema,
	(CASE
		WHEN tc.constraint_type = 'FOREIGN KEY' THEN ccu.table_name
		ELSE ''
	END) AS foreignkey_table,
	(CASE
		WHEN tc.constraint_type = 'FOREIGN KEY' THEN ccu.column_name
		ELSE ''
	END) AS foreignkey_column
FROM 
	information_schema.columns col
LEFT JOIN 
	information_schema.key_column_usage kcu ON col.table_schema = kcu.table_schema
 	AND col.column_name = kcu.column_name
LEFT JOIN 
	information_schema.table_constraints tc ON kcu.table_schema = tc.table_schema
    AND kcu.constraint_name = tc.constraint_name
LEFT JOIN 
	information_schema.constraint_column_usage ccu ON tc.constraint_schema = ccu.constraint_schema
	AND ccu.constraint_name = tc.constraint_name
WHERE 
	col.table_schema NOT IN ('information_schema', 'pg_catalog')
ORDER BY 
	col.ordinal_position;
`

const mysqlColumnInfo = `
SELECT 
	col.table_name as "table",
	col.column_name as "column",
  col.data_type as "type",
  (CASE
		WHEN col.is_nullable = 'YES' THEN TRUE  
		ELSE FALSE
	END) AS notnull,
	(CASE
		WHEN col.data_type = 'ARRAY' THEN TRUE  
		ELSE FALSE
	END) AS isarray,
	(CASE
		WHEN tc.constraint_type = 'PRIMARY KEY' THEN TRUE  
		ELSE FALSE
	END) AS primarykey,
	(CASE  
		WHEN tc.constraint_type = 'UNIQUE' THEN TRUE
		ELSE FALSE
	END) AS uniquekey,
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
	information_schema.columns col
LEFT JOIN 
	information_schema.key_column_usage kcu ON col.table_schema = kcu.table_schema
 	AND col.column_name = kcu.column_name
LEFT JOIN 
	information_schema.table_constraints tc ON kcu.table_schema = tc.table_schema
    AND kcu.constraint_name = tc.constraint_name
WHERE 
	col.table_schema NOT IN ('information_schema', 'pg_catalog')
ORDER BY 
	col.ordinal_position;
`
