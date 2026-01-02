SELECT
    CAST(o.object_id AS VARCHAR(50)) AS func_id,
    SCHEMA_NAME(o.schema_id) AS func_schema,
    o.name AS func_name,
    CASE
        WHEN o.type = 'TF' THEN 'record'
        WHEN o.type = 'IF' THEN 'record'
        ELSE LOWER(ISNULL(TYPE_NAME(ret.user_type_id), 'void'))
    END AS data_type,
    ISNULL(p.parameter_id, 0) AS param_id,
    ISNULL(REPLACE(p.name, '@', ''), '') AS param_name,
    LOWER(ISNULL(TYPE_NAME(p.user_type_id), '')) AS param_type,
    CASE WHEN p.is_output = 1 THEN 'OUT' ELSE 'IN' END AS param_kind
FROM sys.objects o
LEFT JOIN sys.parameters p ON o.object_id = p.object_id AND p.parameter_id > 0
LEFT JOIN sys.parameters ret ON o.object_id = ret.object_id AND ret.parameter_id = 0
WHERE o.type IN ('FN', 'IF', 'TF', 'AF')
    AND SCHEMA_NAME(o.schema_id) NOT IN (
        'sys',
        'INFORMATION_SCHEMA'
    )
ORDER BY o.name, ISNULL(p.parameter_id, 0);
