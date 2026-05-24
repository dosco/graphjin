SELECT COUNT(*) AS constraint_count
FROM information_schema.table_constraints tc
WHERE tc.table_schema NOT IN ('INFORMATION_SCHEMA')
	AND (
		COALESCE(@@dataset_id, '') = ''
		OR LOWER(tc.table_schema) = LOWER(COALESCE(@@dataset_id, ''))
	)
	AND tc.constraint_type IN ('PRIMARY KEY', 'FOREIGN KEY');
