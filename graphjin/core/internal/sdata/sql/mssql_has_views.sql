-- mssql_has_views.sql
--
-- Preflight check for mssql_view_pks.sql. The view-PK discovery query uses
-- sys.dm_exec_describe_first_result_set, which parses and compiles every
-- view to trace columns back to their source base table PKs — expensive on
-- schema-heavy production databases. This preflight lets the caller skip
-- the expensive query entirely when the database has no user-visible views.
SELECT TOP 1 1
FROM sys.views v
JOIN sys.schemas s ON v.schema_id = s.schema_id
WHERE s.name NOT IN (
    'sys', 'INFORMATION_SCHEMA', 'guest', 'cdc',
    'db_owner', 'db_accessadmin', 'db_securityadmin', 'db_ddladmin',
    'db_backupoperator', 'db_datareader', 'db_datawriter',
    'db_denydatareader', 'db_denydatawriter'
)
