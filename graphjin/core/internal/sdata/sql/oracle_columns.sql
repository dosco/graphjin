SELECT
    owner AS "schema",
    table_name AS "table",
    column_name AS "column",
    CASE
        WHEN data_type = 'CLOB' AND EXISTS (
            SELECT 1 FROM all_constraints ac
            WHERE ac.owner = tc.owner
              AND ac.table_name = tc.table_name
              AND ac.constraint_type = 'C'
              AND UPPER(ac.search_condition_vc) LIKE '%' || tc.column_name || '%IS%JSON%'
        ) THEN 'json'
        ELSE data_type
    END AS "type",
    CASE WHEN nullable = 'N' THEN 1 ELSE 0 END AS not_null,
    CASE WHEN EXISTS (
        SELECT 1 FROM all_constraints ac
        JOIN all_cons_columns acc ON ac.constraint_name = acc.constraint_name AND ac.owner = acc.owner
        WHERE ac.owner = tc.owner AND ac.table_name = tc.table_name AND acc.column_name = tc.column_name AND ac.constraint_type = 'P'
    ) THEN 1 ELSE 0 END AS primary_key,
    CASE WHEN EXISTS (
        SELECT 1 FROM all_constraints ac
        JOIN all_cons_columns acc ON ac.constraint_name = acc.constraint_name AND ac.owner = acc.owner
        WHERE ac.owner = tc.owner AND ac.table_name = tc.table_name AND acc.column_name = tc.column_name AND ac.constraint_type = 'U'
    ) THEN 1 ELSE 0 END AS unique_key,
    CASE
        WHEN data_type = 'CLOB' AND (
            LOWER(column_name) LIKE '%_ids' OR
            LOWER(column_name) = 'tags'
        ) THEN 1
        ELSE 0
    END AS is_array,
    0 AS full_text,
    NVL((
        SELECT r_ac.owner FROM all_constraints ac
        JOIN all_cons_columns acc ON ac.constraint_name = acc.constraint_name AND ac.owner = acc.owner
        JOIN all_constraints r_ac ON ac.r_constraint_name = r_ac.constraint_name AND ac.r_owner = r_ac.owner
        WHERE ac.owner = tc.owner AND ac.table_name = tc.table_name AND acc.column_name = tc.column_name AND ac.constraint_type = 'R'
    ), ' ') AS foreignkey_schema,
    NVL((
        SELECT r_ac.table_name FROM all_constraints ac
        JOIN all_cons_columns acc ON ac.constraint_name = acc.constraint_name AND ac.owner = acc.owner
        JOIN all_constraints r_ac ON ac.r_constraint_name = r_ac.constraint_name AND ac.r_owner = r_ac.owner
        WHERE ac.owner = tc.owner AND ac.table_name = tc.table_name AND acc.column_name = tc.column_name AND ac.constraint_type = 'R'
    ), ' ') AS foreignkey_table,
    NVL((
        SELECT r_acc.column_name FROM all_constraints ac
        JOIN all_cons_columns acc ON ac.constraint_name = acc.constraint_name AND ac.owner = acc.owner
        JOIN all_constraints r_ac ON ac.r_constraint_name = r_ac.constraint_name AND ac.r_owner = r_ac.owner
        JOIN all_cons_columns r_acc ON r_ac.constraint_name = r_acc.constraint_name AND r_ac.owner = r_acc.owner
        WHERE ac.owner = tc.owner AND ac.table_name = tc.table_name AND acc.column_name = tc.column_name AND ac.constraint_type = 'R'
        AND acc.position = r_acc.position
    ), ' ') AS foreignkey_column
FROM all_tab_columns tc
WHERE owner NOT IN ('SYS', 'SYSTEM', 'OUTLN', 'DBSNMP', 'APPQOSSYS', 'XDB', 'WMSYS', 'CTXSYS', 'MDSYS', 'ORDSYS', 'ORDDATA', 'ORDPLUGINS', 'SI_INFORMTN_SCHEMA', 'OLAPSYS', 'MDDATA', 'SPATIAL_WFS_ADMIN_USR', 'SPATIAL_CSW_ADMIN_USR', 'SYSMAN', 'FLOWS_FILES', 'APEX_040200', 'APEX_PUBLIC_USER', 'LBACSYS', 'DVF', 'DVSYS', 'AUDSYS', 'GSMADMIN_INTERNAL', 'GSMCATUSER', 'GSMUSER', 'REMOTE_SCHEDULER_AGENT', 'GGSYS', 'DBSFWUSER', 'ANONYMOUS', 'XS$NULL', 'OJVMSYS', 'ORACLE_OCM', 'ORDPLUGINS')
ORDER BY owner, table_name, column_id
