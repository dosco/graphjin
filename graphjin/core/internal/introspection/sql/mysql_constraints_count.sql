SELECT COUNT(*)
FROM information_schema.table_constraints tc
WHERE tc.constraint_schema = DATABASE()
	AND tc.constraint_type IN ('PRIMARY KEY', 'UNIQUE', 'FOREIGN KEY');
