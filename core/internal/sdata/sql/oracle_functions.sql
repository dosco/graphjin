SELECT "id", "schema", "name", "type", "pid", "pname", "ptype", "pkind" FROM (
    SELECT
        o.owner || '.' || o.object_name AS "id",
        LOWER(o.owner) AS "schema",
        LOWER(o.object_name) AS "name",
        CASE
            WHEN EXISTS (
                SELECT 1 FROM all_arguments ret
                WHERE ret.owner = o.owner AND ret.object_name = o.object_name
                AND ret.position = 0 AND ret.data_level = 0 AND ret.type_name IS NOT NULL
            ) THEN 'record'
            WHEN EXISTS (
                SELECT 1 FROM all_arguments a2
                WHERE a2.owner = o.owner AND a2.object_name = o.object_name
                AND a2.in_out = 'OUT' AND a2.position > 0 AND a2.data_level = 0
            ) THEN 'record'
            ELSE 'scalar'
        END AS "type",
        NVL(a.position, 0) AS "pid",
        NVL(LOWER(a.argument_name), '') AS "pname",
        NVL(LOWER(a.data_type), '') AS "ptype",
        NVL(a.in_out, '') AS "pkind"
    FROM all_objects o
    LEFT JOIN all_arguments a ON o.owner = a.owner AND o.object_name = a.object_name
        AND a.data_level = 0 AND a.position > 0 AND a.package_name IS NULL
    WHERE o.object_type IN ('FUNCTION', 'PROCEDURE')
    AND o.owner NOT IN ('SYS', 'SYSTEM', 'OUTLN', 'DBSNMP', 'APPQOSSYS', 'XDB', 'WMSYS', 'CTXSYS', 'MDSYS', 'ORDSYS', 'ORDDATA', 'ORDPLUGINS', 'SI_INFORMTN_SCHEMA', 'OLAPSYS', 'MDDATA', 'SPATIAL_WFS_ADMIN_USR', 'SPATIAL_CSW_ADMIN_USR', 'SYSMAN', 'FLOWS_FILES', 'APEX_040200', 'APEX_PUBLIC_USER', 'LBACSYS', 'DVF', 'DVSYS', 'AUDSYS', 'GSMADMIN_INTERNAL', 'GSMCATUSER', 'GSMUSER', 'REMOTE_SCHEDULER_AGENT', 'GGSYS', 'DBSFWUSER', 'ANONYMOUS', 'XS$NULL', 'OJVMSYS', 'ORACLE_OCM', 'ORDPLUGINS')
    UNION ALL
    SELECT
        ret.owner || '.' || p.object_name AS "id",
        LOWER(ret.owner) AS "schema",
        LOWER(p.object_name) AS "name",
        'record' AS "type",
        ta.attr_no AS "pid",
        LOWER(ta.attr_name) AS "pname",
        LOWER(ta.attr_type_name) AS "ptype",
        'OUT' AS "pkind"
    FROM all_arguments ret
    JOIN all_procedures p ON ret.owner = p.owner AND ret.object_name = p.object_name AND p.object_type = 'FUNCTION'
    JOIN all_coll_types ct ON ret.type_owner = ct.owner AND ret.type_name = ct.type_name
    JOIN all_type_attrs ta ON ct.elem_type_owner = ta.owner AND ct.elem_type_name = ta.type_name
    WHERE ret.position = 0 AND ret.data_level = 0 AND ret.type_name IS NOT NULL AND ret.package_name IS NULL
    AND ret.owner NOT IN ('SYS', 'SYSTEM', 'OUTLN', 'DBSNMP', 'APPQOSSYS', 'XDB', 'WMSYS', 'CTXSYS', 'MDSYS', 'ORDSYS', 'ORDDATA', 'ORDPLUGINS', 'SI_INFORMTN_SCHEMA', 'OLAPSYS', 'MDDATA', 'SPATIAL_WFS_ADMIN_USR', 'SPATIAL_CSW_ADMIN_USR', 'SYSMAN', 'FLOWS_FILES', 'APEX_040200', 'APEX_PUBLIC_USER', 'LBACSYS', 'DVF', 'DVSYS', 'AUDSYS', 'GSMADMIN_INTERNAL', 'GSMCATUSER', 'GSMUSER', 'REMOTE_SCHEDULER_AGENT', 'GGSYS', 'DBSFWUSER', 'ANONYMOUS', 'XS$NULL', 'OJVMSYS', 'ORACLE_OCM', 'ORDPLUGINS')
)
ORDER BY "schema", "name", "pid"
