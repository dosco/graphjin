SELECT 0 AS db_version,
	LOWER(COALESCE(CURRENT_SCHEMA(), 'main')) AS db_schema,
	COALESCE(CURRENT_DATABASE(), '') AS db_name;
