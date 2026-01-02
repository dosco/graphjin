SELECT
    CAST(SERVERPROPERTY('ProductMajorVersion') AS INT) * 10000 +
    CAST(ISNULL(SERVERPROPERTY('ProductMinorVersion'), 0) AS INT) * 100 AS db_version,
    SCHEMA_NAME() AS db_schema,
    DB_NAME() AS db_name;
