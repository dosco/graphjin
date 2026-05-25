WITH cols AS (
    SELECT
        "schema_name"  AS sch_name,
        "table_name"   AS tbl_name,
        "column_name"  AS col_name,
        TRY_PARSE_JSON("data_type"):type::TEXT AS show_type,
        COALESCE(NOT TRY_PARSE_JSON("data_type"):nullable::BOOLEAN, FALSE) AS is_not_null
    FROM TABLE(RESULT_SCAN(?))
)
SELECT
    c.sch_name AS schema_name,
    c.tbl_name AS table_name,
    c.col_name AS column_name,
    LOWER(CASE c.show_type
        WHEN 'FIXED' THEN 'number'
        WHEN 'REAL'  THEN 'float'
        ELSE c.show_type
    END) AS col_type,
    c.is_not_null AS not_null,
    FALSE AS primary_key,
    FALSE AS unique_key,
    FALSE AS is_array,
    FALSE AS full_text,
    '' AS foreignkey_schema,
    '' AS foreignkey_table,
    '' AS foreignkey_column
FROM cols c
WHERE UPPER(c.sch_name) = UPPER(CURRENT_SCHEMA())
ORDER BY c.sch_name, c.tbl_name, c.col_name
