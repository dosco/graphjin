SELECT LOWER(table_schema) AS schema_name,
	LOWER(table_name) AS table_name,
	clustering_key
FROM information_schema.tables
WHERE table_schema NOT IN ('INFORMATION_SCHEMA')
	AND clustering_key IS NOT NULL
	AND clustering_key != '';
