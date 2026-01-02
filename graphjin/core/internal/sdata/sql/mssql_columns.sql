SELECT
    s.name AS [schema],
    t.name AS [table],
    c.name AS [column],
    LOWER(ty.name) AS [type],
    CASE WHEN c.is_nullable = 0 THEN 1 ELSE 0 END AS not_null,
    CASE WHEN pk.column_id IS NOT NULL THEN 1 ELSE 0 END AS primary_key,
    CASE WHEN uq.column_id IS NOT NULL THEN 1 ELSE 0 END AS unique_key,
    0 AS is_array,
    CASE WHEN fti.column_id IS NOT NULL THEN 1 ELSE 0 END AS full_text,
    ISNULL(fk.ref_schema, '') AS foreignkey_schema,
    ISNULL(fk.ref_table, '') AS foreignkey_table,
    ISNULL(fk.ref_column, '') AS foreignkey_column
FROM sys.columns c
JOIN sys.tables t ON c.object_id = t.object_id
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
    'db_owner',
    'db_accessadmin',
    'db_securityadmin',
    'db_ddladmin',
    'db_backupoperator',
    'db_datareader',
    'db_datawriter',
    'db_denydatareader',
    'db_denydatawriter'
)
ORDER BY s.name, t.name, c.column_id;
