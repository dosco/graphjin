SELECT COUNT(*)
FROM information_schema.table_constraints tc
WHERE UPPER(tc.table_schema) = UPPER(?)
	AND tc.constraint_type IN ('PRIMARY KEY', 'UNIQUE', 'FOREIGN KEY');
