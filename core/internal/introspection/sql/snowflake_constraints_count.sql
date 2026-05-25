SELECT COUNT(*)
FROM information_schema.table_constraints tc
WHERE UPPER(tc.table_schema) = UPPER(CURRENT_SCHEMA())
	AND tc.constraint_type IN ('PRIMARY KEY', 'UNIQUE', 'FOREIGN KEY');
