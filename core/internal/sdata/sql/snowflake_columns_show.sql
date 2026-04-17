WITH cols AS (
    SELECT
        "schema_name"  AS sch_name,
        "table_name"   AS tbl_name,
        "column_name"  AS col_name,
        TRY_PARSE_JSON("data_type"):type::TEXT AS show_type,
        COALESCE(NOT TRY_PARSE_JSON("data_type"):nullable::BOOLEAN, FALSE) AS is_not_null
    FROM TABLE(RESULT_SCAN(?))
),
pks AS (
    SELECT "schema_name" AS sch_name, "table_name" AS tbl_name, "column_name" AS col_name
    FROM TABLE(RESULT_SCAN(?))
),
uks AS (
    SELECT "schema_name" AS sch_name, "table_name" AS tbl_name, "column_name" AS col_name
    FROM TABLE(RESULT_SCAN(?))
),
fks AS (
    SELECT
        "fk_schema_name"  AS sch_name,
        "fk_table_name"   AS tbl_name,
        "fk_column_name"  AS col_name,
        LOWER("pk_schema_name") AS pk_sch,
        LOWER("pk_table_name")  AS pk_tbl,
        LOWER("pk_column_name") AS pk_col
    FROM TABLE(RESULT_SCAN(?))
)
SELECT
    LOWER(c.sch_name) AS schema_name,
    LOWER(c.tbl_name) AS table_name,
    LOWER(c.col_name) AS column_name,
    LOWER(CASE c.show_type
        WHEN 'FIXED' THEN 'number'
        WHEN 'REAL'  THEN 'float'
        ELSE c.show_type
    END) AS col_type,
    c.is_not_null AS not_null,
    (pks.col_name IS NOT NULL) AS primary_key,
    (uks.col_name IS NOT NULL) AS unique_key,
    FALSE AS is_array,
    FALSE AS full_text,
    COALESCE(fks.pk_sch, '') AS foreignkey_schema,
    COALESCE(fks.pk_tbl, '') AS foreignkey_table,
    COALESCE(fks.pk_col, '') AS foreignkey_column
FROM cols c
LEFT JOIN pks ON pks.sch_name = c.sch_name AND pks.tbl_name = c.tbl_name AND pks.col_name = c.col_name
LEFT JOIN uks ON uks.sch_name = c.sch_name AND uks.tbl_name = c.tbl_name AND uks.col_name = c.col_name
LEFT JOIN fks ON fks.sch_name = c.sch_name AND fks.tbl_name = c.tbl_name AND fks.col_name = c.col_name
WHERE UPPER(c.sch_name) <> 'INFORMATION_SCHEMA'
ORDER BY c.sch_name, c.tbl_name, c.col_name
