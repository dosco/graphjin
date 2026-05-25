SELECT COUNT(*)
FROM information_schema.tables t
WHERE UPPER(t.table_schema) = UPPER(CURRENT_SCHEMA())
	AND UPPER(t.table_name) = '_GJ_FK_METADATA';
