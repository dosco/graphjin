SELECT 0 AS db_version,
	COALESCE(@@dataset_id, '') AS db_schema,
	COALESCE(@@project_id, '') AS db_name;
