-- Detect primary key columns for Oracle views by tracing view columns
-- back to their source base tables via ALL_DEPENDENCIES.
--
-- Logic: for each view column, find base tables the view depends on,
-- match by column name, and check if that base column is part of a PK.
SELECT DISTINCT
    v.owner AS "schema",
    v.view_name AS "table",
    vc.column_name AS "column"
FROM all_views v
JOIN all_tab_columns vc ON vc.owner = v.owner AND vc.table_name = v.view_name
JOIN all_dependencies dep ON dep.owner = v.owner
    AND dep.name = v.view_name
    AND dep.type = 'VIEW'
    AND dep.referenced_type = 'TABLE'
JOIN all_cons_columns acc ON acc.owner = dep.referenced_owner
    AND acc.table_name = dep.referenced_name
    AND acc.column_name = vc.column_name
JOIN all_constraints ac ON ac.owner = acc.owner
    AND ac.constraint_name = acc.constraint_name
    AND ac.constraint_type = 'P'
WHERE v.owner NOT IN ('SYS', 'SYSTEM', 'OUTLN', 'DBSNMP', 'APPQOSSYS',
    'XDB', 'WMSYS', 'CTXSYS', 'MDSYS', 'ORDSYS', 'ORDDATA',
    'ORDPLUGINS', 'SI_INFORMTN_SCHEMA', 'OLAPSYS', 'MDDATA',
    'AUDSYS', 'GSMADMIN_INTERNAL', 'ANONYMOUS', 'XS$NULL', 'OJVMSYS')
