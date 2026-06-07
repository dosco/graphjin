SELECT 0 AS db_version,
	LOWER(COALESCE(CURRENT_SCHEMA(), 'public')) AS db_schema,
	COALESCE(CURRENT_DATABASE(), 'dev') AS db_name;
