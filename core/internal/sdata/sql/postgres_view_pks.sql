-- Detect primary key columns for PostgreSQL views by tracing view columns
-- back to their source base tables via pg_rewrite + pg_depend.
-- This mirrors the MSSQL approach using sys.dm_exec_describe_first_result_set.
--
-- Logic: for each view column, find the base tables the view depends on,
-- match by column name, and check if that base column is part of a PK.
SELECT DISTINCT
    vn.nspname AS schema,
    vc.relname AS table,
    va.attname AS column
FROM pg_class vc
JOIN pg_namespace vn ON vc.relnamespace = vn.oid
JOIN pg_attribute va ON va.attrelid = vc.oid
    AND va.attnum > 0
    AND NOT va.attisdropped
JOIN pg_rewrite rw ON rw.ev_class = vc.oid
    AND rw.rulename = '_RETURN'
JOIN pg_depend d ON d.objid = rw.oid
    AND d.deptype = 'n'
    AND d.classid = 'pg_rewrite'::regclass
    AND d.refclassid = 'pg_class'::regclass
JOIN pg_class bc ON d.refobjid = bc.oid
    AND bc.relkind = 'r'
JOIN pg_attribute ba ON ba.attrelid = bc.oid
    AND ba.attname = va.attname
    AND ba.attnum > 0
    AND NOT ba.attisdropped
JOIN pg_constraint co ON co.conrelid = bc.oid
    AND ba.attnum = ANY(co.conkey)
    AND co.contype = 'p'
WHERE vc.relkind IN ('v', 'm')
  AND vn.nspname NOT IN ('pg_catalog', 'information_schema', '_graphjin')
