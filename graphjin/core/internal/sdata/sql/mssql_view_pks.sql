SELECT
    s.name AS [schema],
    v.name AS [table],
    r.name AS [column]
FROM sys.objects v
JOIN sys.schemas s ON v.schema_id = s.schema_id
CROSS APPLY sys.dm_exec_describe_first_result_set(
    N'SELECT * FROM [' + s.name + N'].[' + v.name + N']', NULL, 1
) r
WHERE v.type = 'V'
  AND r.source_table IS NOT NULL
  AND r.source_column IS NOT NULL
  AND r.is_part_of_unique_key = 1
  AND s.name NOT IN ('sys', 'INFORMATION_SCHEMA', 'guest',
      'db_owner', 'db_accessadmin', 'db_securityadmin',
      'db_ddladmin', 'db_backupoperator', 'db_datareader',
      'db_datawriter', 'db_denydatareader', 'db_denydatawriter')
