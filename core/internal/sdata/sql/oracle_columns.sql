-- oracle_columns.sql
--
-- Uses user_* dictionary views throughout (user_tab_columns, user_constraints,
-- user_cons_columns, user_indexes, user_ind_columns). These views are
-- implicitly scoped to the current session owner and carry no Oracle
-- security-predicate overhead, unlike the all_* equivalents which must
-- evaluate privileges row-by-row across every visible schema.
--
-- PK, UK, full-text and FK metadata are pre-computed as inline views and
-- joined once rather than executed as N correlated subqueries (one per
-- column row). The only remaining correlated EXISTS is for JSON CLOB
-- detection, which must embed tc.column_name in a text search of the check
-- constraint condition and cannot be pre-aggregated.
--
-- FK referenced-table lookup uses all_constraints / all_cons_columns on the
-- *referenced* side only (a single indexed key-lookup on r_constraint_name),
-- so cross-schema FK targets resolve correctly without a full scan.
--
-- ROW_NUMBER() in the FK inline view defends against ORA-01427 for the rare
-- case where one column participates in more than one FK constraint.
--
-- DR$% tables are Oracle Text internal shadow tables; excluded to avoid noise.
SELECT
    USER AS "schema",
    tc.table_name  AS "table",
    tc.column_name AS "column",
    -- JSON type: correlated EXISTS is required because the heuristic must
    -- match tc.column_name inside the check-constraint condition text.
    CASE
        WHEN tc.data_type = 'CLOB' AND EXISTS (
            SELECT 1 FROM user_constraints uc
            WHERE uc.table_name      = tc.table_name
              AND uc.constraint_type = 'C'
              AND UPPER(uc.search_condition_vc)
                      LIKE '%' || tc.column_name || '%IS%JSON%'
        ) THEN 'json'
        ELSE tc.data_type
    END AS "type",
    CASE WHEN tc.nullable = 'N' THEN 1 ELSE 0 END AS not_null,
    CASE WHEN pk.column_name  IS NOT NULL THEN 1 ELSE 0 END AS primary_key,
    CASE WHEN uk.column_name  IS NOT NULL THEN 1 ELSE 0 END AS unique_key,
    CASE
        WHEN tc.data_type = 'CLOB' AND (
            LOWER(tc.column_name) LIKE '%_ids' OR
            LOWER(tc.column_name) = 'tags'
        ) THEN 1
        ELSE 0
    END AS is_array,
    CASE WHEN ctx.column_name IS NOT NULL THEN 1 ELSE 0 END AS full_text,
    NVL(fk.fkey_schema, ' ') AS foreignkey_schema,
    NVL(fk.fkey_table,  ' ') AS foreignkey_table,
    NVL(fk.fkey_col,    ' ') AS foreignkey_column
FROM user_tab_columns tc
-- Primary key: at most one PK per table, so no row fanout.
LEFT JOIN (
    SELECT ucc.table_name, ucc.column_name
    FROM   user_constraints  uc
    JOIN   user_cons_columns ucc ON uc.constraint_name = ucc.constraint_name
    WHERE  uc.constraint_type = 'P'
) pk  ON pk.table_name  = tc.table_name  AND pk.column_name  = tc.column_name
-- Unique keys: DISTINCT collapses multiple UK constraints on the same column.
LEFT JOIN (
    SELECT DISTINCT ucc.table_name, ucc.column_name
    FROM   user_constraints  uc
    JOIN   user_cons_columns ucc ON uc.constraint_name = ucc.constraint_name
    WHERE  uc.constraint_type = 'U'
) uk  ON uk.table_name  = tc.table_name  AND uk.column_name  = tc.column_name
-- Full-text CONTEXT indexes: DISTINCT collapses multiple indexes per column.
LEFT JOIN (
    SELECT DISTINCT uic.table_name, uic.column_name
    FROM   user_indexes     ui
    JOIN   user_ind_columns uic ON ui.index_name = uic.index_name
    WHERE  ui.ityp_name = 'CONTEXT'
) ctx ON ctx.table_name = tc.table_name AND ctx.column_name = tc.column_name
-- Foreign keys: the three previous scalar subqueries (schema, table, col)
-- ran the same join three times. Here the join runs once; ROW_NUMBER picks
-- one FK per column when the column is in more than one FK constraint.
-- Referenced side uses all_constraints / all_cons_columns for cross-schema
-- correctness (indexed key-lookup on r_constraint_name, not a full scan).
LEFT JOIN (
    SELECT ucc.table_name,
           ucc.column_name,
           LOWER(r_ac.owner)      AS fkey_schema,
           LOWER(r_ac.table_name) AS fkey_table,
           r_acc.column_name      AS fkey_col,
           ROW_NUMBER() OVER (
               PARTITION BY ucc.table_name, ucc.column_name
               ORDER BY     uc.constraint_name
           ) AS rn
    FROM   user_constraints  uc
    JOIN   user_cons_columns ucc  ON uc.constraint_name   = ucc.constraint_name
    JOIN   all_constraints   r_ac ON uc.r_constraint_name = r_ac.constraint_name
    JOIN   all_cons_columns  r_acc ON  r_ac.constraint_name = r_acc.constraint_name
                                   AND r_ac.owner           = r_acc.owner
                                   AND ucc.position         = r_acc.position
    WHERE  uc.constraint_type = 'R'
) fk  ON fk.table_name  = tc.table_name  AND fk.column_name  = tc.column_name
     AND fk.rn = 1
WHERE tc.table_name NOT LIKE 'DR$%'
ORDER BY tc.table_name, tc.column_id
