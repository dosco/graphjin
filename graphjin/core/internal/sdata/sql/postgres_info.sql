SELECT CAST(current_setting('server_version_num') AS integer) as db_version,
	COALESCE(current_schema(), '') as db_schema,
	COALESCE(current_database(), '') as db_name;