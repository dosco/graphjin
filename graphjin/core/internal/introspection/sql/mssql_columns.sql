-- mssql_columns.sql
--
-- JSON detection: the check-constraint text search uses CHARINDEX rather
-- than LIKE. SQL Server LIKE treats `[abc]` as a character class (match one
-- character from the set), so a naive pattern like `%isjson%[' + col + ']%`
-- does NOT match the literal bracketed identifier `[col]` — it matches
-- "isjson...one character from the column-name letter set..." which causes
-- false positives whenever another ISJSON-constrained column in the same
-- table happens to share letters with `col`. CHARINDEX is a literal
-- substring search with no metacharacter pitfalls. We OR both bracketed
-- and bare forms so `ISJSON([col])` and `ISJSON(col)` both match.
SELECT
    s.name AS [schema],
    t.name AS [table],
    c.name AS [column],
    CASE
        WHEN ty.name IN ('nvarchar', 'varchar') AND c.max_length = -1 AND EXISTS (
            SELECT 1 FROM sys.check_constraints chk
            WHERE chk.parent_object_id = c.object_id
                AND (
                    CHARINDEX('isjson([' + LOWER(c.name) + '])', LOWER(chk.definition)) > 0
                 OR CHARINDEX('isjson(' + LOWER(c.name) + ')',   LOWER(chk.definition)) > 0
                )
        ) THEN 'json'
        ELSE LOWER(ty.name)
    END AS [type],
    CASE WHEN c.is_nullable = 0 THEN 1 ELSE 0 END AS not_null,
    CASE WHEN pk.column_id IS NOT NULL THEN 1 ELSE 0 END AS primary_key,
    CASE WHEN uq.column_id IS NOT NULL THEN 1 ELSE 0 END AS unique_key,
    CASE WHEN LOWER(c.name) = 'tags' OR LOWER(c.name) LIKE '%[_]ids' THEN 1 ELSE 0 END AS is_array,
    CASE WHEN fti.column_id IS NOT NULL THEN 1 ELSE 0 END AS full_text,
    ISNULL(fk.ref_schema, '') AS foreignkey_schema,
    ISNULL(fk.ref_table, '') AS foreignkey_table,
    ISNULL(fk.ref_column, '') AS foreignkey_column
FROM sys.columns c
JOIN sys.objects t ON c.object_id = t.object_id AND t.type IN ('U', 'V')
JOIN sys.schemas s ON t.schema_id = s.schema_id
JOIN sys.types ty ON c.user_type_id = ty.user_type_id
LEFT JOIN (
    SELECT ic.object_id, ic.column_id
    FROM sys.index_columns ic
    JOIN sys.indexes i ON ic.object_id = i.object_id AND ic.index_id = i.index_id
    WHERE i.is_primary_key = 1
) pk ON c.object_id = pk.object_id AND c.column_id = pk.column_id
LEFT JOIN (
    SELECT ic.object_id, ic.column_id
    FROM sys.index_columns ic
    JOIN sys.indexes i ON ic.object_id = i.object_id AND ic.index_id = i.index_id
    WHERE i.is_unique = 1 AND i.is_primary_key = 0
) uq ON c.object_id = uq.object_id AND c.column_id = uq.column_id
LEFT JOIN (
    SELECT ic.object_id, ic.column_id
    FROM sys.fulltext_index_columns ic
) fti ON c.object_id = fti.object_id AND c.column_id = fti.column_id
LEFT JOIN (
    SELECT
        fkc.parent_object_id,
        fkc.parent_column_id,
        rs.name AS ref_schema,
        rt.name AS ref_table,
        rc.name AS ref_column
    FROM sys.foreign_key_columns fkc
    JOIN sys.tables rt ON fkc.referenced_object_id = rt.object_id
    JOIN sys.schemas rs ON rt.schema_id = rs.schema_id
    JOIN sys.columns rc ON fkc.referenced_object_id = rc.object_id
        AND fkc.referenced_column_id = rc.column_id
) fk ON c.object_id = fk.parent_object_id AND c.column_id = fk.parent_column_id
WHERE s.name NOT IN (
    'sys',
    'INFORMATION_SCHEMA',
    'guest',
    'cdc',              -- Change Data Capture shadow tables
    'db_owner',
    'db_accessadmin',
    'db_securityadmin',
    'db_ddladmin',
    'db_backupoperator',
    'db_datareader',
    'db_datawriter',
    'db_denydatareader',
    'db_denydatawriter'
);
