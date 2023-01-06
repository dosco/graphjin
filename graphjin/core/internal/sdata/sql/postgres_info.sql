SELECT CAST(current_setting('server_version_num') AS integer) as db_version,
	current_schema() as db_schema,
	current_database() as db_name;