-- Detect primary key columns for MySQL views (8.0+) by tracing view columns
-- back to their source base tables via information_schema.VIEW_TABLE_USAGE.
--
-- Logic: for each view column, find base tables the view references,
-- match by column name, and check if that base column is part of a PK.
-- Note: VIEW_TABLE_USAGE requires MySQL 8.0.13+.
SELECT DISTINCT
    v.TABLE_SCHEMA AS `schema`,
    v.TABLE_NAME AS `table`,
    vc.COLUMN_NAME AS `column`
FROM information_schema.VIEWS v
JOIN information_schema.COLUMNS vc
    ON vc.TABLE_SCHEMA = v.TABLE_SCHEMA
    AND vc.TABLE_NAME = v.TABLE_NAME
JOIN information_schema.VIEW_TABLE_USAGE vtu
    ON vtu.VIEW_SCHEMA = v.TABLE_SCHEMA
    AND vtu.VIEW_NAME = v.TABLE_NAME
JOIN information_schema.TABLE_CONSTRAINTS tc
    ON tc.TABLE_SCHEMA = vtu.TABLE_SCHEMA
    AND tc.TABLE_NAME = vtu.TABLE_NAME
    AND tc.CONSTRAINT_TYPE = 'PRIMARY KEY'
JOIN information_schema.KEY_COLUMN_USAGE kcu
    ON kcu.CONSTRAINT_SCHEMA = tc.CONSTRAINT_SCHEMA
    AND kcu.CONSTRAINT_NAME = tc.CONSTRAINT_NAME
    AND kcu.TABLE_NAME = tc.TABLE_NAME
    AND kcu.COLUMN_NAME = vc.COLUMN_NAME
WHERE v.TABLE_SCHEMA NOT IN ('mysql', 'information_schema', 'performance_schema', 'sys')
