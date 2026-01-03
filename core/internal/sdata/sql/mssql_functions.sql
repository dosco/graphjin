-- Input parameters for all functions (only rows with actual parameters)
SELECT
    CAST(o.object_id AS VARCHAR(50)) AS func_id,
    SCHEMA_NAME(o.schema_id) AS func_schema,
    o.name AS func_name,
    CASE
        WHEN o.type = 'TF' THEN 'record'
        WHEN o.type = 'IF' THEN 'record'
        ELSE LOWER(ISNULL(TYPE_NAME(ret.user_type_id), 'void'))
    END AS data_type,
    p.parameter_id AS param_id,
    REPLACE(p.name, '@', '') AS param_name,
    LOWER(TYPE_NAME(p.user_type_id)) AS param_type,
    CASE WHEN p.is_output = 1 THEN 'OUT' ELSE 'IN' END AS param_kind
FROM sys.objects o
JOIN sys.parameters p ON o.object_id = p.object_id AND p.parameter_id > 0
LEFT JOIN sys.parameters ret ON o.object_id = ret.object_id AND ret.parameter_id = 0
WHERE o.type IN ('FN', 'IF', 'TF', 'AF')
    AND SCHEMA_NAME(o.schema_id) NOT IN (
        'sys',
        'INFORMATION_SCHEMA'
    )

UNION ALL

-- Output columns for table-valued functions (IF and TF)
SELECT
    CAST(o.object_id AS VARCHAR(50)) AS func_id,
    SCHEMA_NAME(o.schema_id) AS func_schema,
    o.name AS func_name,
    'record' AS data_type,
    1000 + c.column_id AS param_id,
    c.name AS param_name,
    LOWER(TYPE_NAME(c.user_type_id)) AS param_type,
    'OUT' AS param_kind
FROM sys.objects o
JOIN sys.columns c ON o.object_id = c.object_id
WHERE o.type IN ('IF', 'TF')
    AND SCHEMA_NAME(o.schema_id) NOT IN (
        'sys',
        'INFORMATION_SCHEMA'
    )

ORDER BY func_name, param_id;
