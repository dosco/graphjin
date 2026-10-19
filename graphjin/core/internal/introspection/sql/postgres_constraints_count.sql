SELECT CASE WHEN EXISTS (
	SELECT 1
	FROM pg_constraint co
	JOIN pg_class c ON c.oid = co.conrelid
	JOIN pg_namespace n ON n.oid = c.relnamespace
	WHERE co.contype IN ('p', 'u', 'f')
		AND n.nspname NOT IN ('information_schema', 'pg_catalog')
		AND n.nspname NOT LIKE 'pg_toast%'
		AND n.nspname NOT LIKE 'pg_temp_%'
		AND n.nspname NOT LIKE 'pg_toast_temp_%'
		AND c.relname != 'schema_version'
) THEN 1 ELSE 0 END;
