-- oracle_functions.sql
--
-- Uses user_* dictionary views (user_objects, user_arguments, user_procedures,
-- user_coll_types, user_type_attrs) instead of the all_* equivalents.
-- The all_* views scan every object the session can see across all schemas and
-- require Oracle to evaluate security predicates row-by-row; the user_* views
-- are implicitly scoped to the current session owner with no security overhead.
-- The long NOT IN exclusion list is also unnecessary with user_* views.
SELECT "id", "schema", "name", "type", "pid", "pname", "ptype", "pkind" FROM (
    SELECT
        USER || '.' || o.object_name AS "id",
        LOWER(USER) AS "schema",
        LOWER(o.object_name) AS "name",
        CASE
            WHEN EXISTS (
                SELECT 1 FROM user_arguments ret
                WHERE ret.object_name  = o.object_name
                  AND ret.position     = 0
                  AND ret.data_level   = 0
                  AND ret.type_name    IS NOT NULL
                  AND ret.package_name IS NULL
            ) THEN 'record'
            WHEN EXISTS (
                SELECT 1 FROM user_arguments a2
                WHERE a2.object_name  = o.object_name
                  AND a2.in_out       = 'OUT'
                  AND a2.position     > 0
                  AND a2.data_level   = 0
                  AND a2.package_name IS NULL
            ) THEN 'record'
            ELSE 'scalar'
        END AS "type",
        NVL(a.position, 0) AS "pid",
        NVL(LOWER(a.argument_name), '') AS "pname",
        NVL(LOWER(a.data_type), '') AS "ptype",
        NVL(a.in_out, '') AS "pkind"
    FROM user_objects o
    LEFT JOIN user_arguments a
           ON o.object_name  = a.object_name
          AND a.data_level   = 0
          AND a.position     > 0
          AND a.package_name IS NULL
    WHERE o.object_type IN ('FUNCTION', 'PROCEDURE')
    UNION ALL
    SELECT
        USER || '.' || p.object_name AS "id",
        LOWER(USER) AS "schema",
        LOWER(p.object_name) AS "name",
        'record' AS "type",
        ta.attr_no AS "pid",
        LOWER(ta.attr_name) AS "pname",
        LOWER(ta.attr_type_name) AS "ptype",
        'OUT' AS "pkind"
    FROM user_arguments ret
    JOIN user_procedures p  ON ret.object_name  = p.object_name
                            AND p.object_type    = 'FUNCTION'
                            AND p.procedure_name IS NULL  -- standalone only; packaged functions have object_name = package
    JOIN user_coll_types ct ON ret.type_name    = ct.type_name
    JOIN user_type_attrs ta ON ct.elem_type_name = ta.type_name
    WHERE ret.position     = 0
      AND ret.data_level   = 0
      AND ret.type_name    IS NOT NULL
      AND ret.package_name IS NULL
)
ORDER BY "schema", "name", "pid"
