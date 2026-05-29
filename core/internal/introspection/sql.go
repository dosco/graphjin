package introspection

import _ "embed"

//go:embed sql/postgres_functions.sql
var postgresFunctionsStmt string

//go:embed sql/mysql_functions.sql
var mysqlFunctionsStmt string

//go:embed sql/postgres_info.sql
var postgresInfo string

//go:embed sql/postgres_columns_basic.sql
var postgresColumnsBasicStmt string

//go:embed sql/postgres_constraints_count.sql
var postgresConstraintsCountStmt string

//go:embed sql/postgres_constraint_columns.sql
var postgresConstraintColumnsStmt string

//go:embed sql/mysql_info.sql
var mysqlInfo string

//go:embed sql/mysql_columns.sql
var mysqlColumnsStmt string

//go:embed sql/mysql_columns_basic.sql
var mysqlColumnsBasicStmt string

//go:embed sql/mysql_constraints_count.sql
var mysqlConstraintsCountStmt string

//go:embed sql/mysql_constraint_columns.sql
var mysqlConstraintColumnsStmt string

//go:embed sql/sqlite_functions.sql
var sqliteFunctionsStmt string

//go:embed sql/sqlite_info.sql
var sqliteInfo string

//go:embed sql/sqlite_columns.sql
var sqliteColumnsStmt string

//go:embed sql/oracle_functions.sql
var oracleFunctionsStmt string

//go:embed sql/oracle_info.sql
var oracleInfo string

//go:embed sql/oracle_columns.sql
var oracleColumnsStmt string

//go:embed sql/mariadb_functions.sql
var mariadbFunctionsStmt string

//go:embed sql/mariadb_info.sql
var mariadbInfo string

//go:embed sql/mariadb_columns.sql
var mariadbColumnsStmt string

//go:embed sql/mariadb_columns_basic.sql
var mariadbColumnsBasicStmt string

//go:embed sql/mariadb_constraints_count.sql
var mariadbConstraintsCountStmt string

//go:embed sql/mariadb_constraint_columns.sql
var mariadbConstraintColumnsStmt string

//go:embed sql/mssql_functions.sql
var mssqlFunctionsStmt string

//go:embed sql/mssql_info.sql
var mssqlInfo string

//go:embed sql/mssql_columns.sql
var mssqlColumnsStmt string

//go:embed sql/mssql_view_pks.sql
var mssqlViewPKsStmt string

//go:embed sql/mssql_has_views.sql
var mssqlHasViewsStmt string

//go:embed sql/postgres_view_pks.sql
var postgresViewPKsStmt string

//go:embed sql/oracle_view_pks.sql
var oracleViewPKsStmt string

//go:embed sql/mysql_view_pks.sql
var mysqlViewPKsStmt string

//go:embed sql/snowflake_info.sql
var snowflakeInfo string

//go:embed sql/snowflake_columns.sql
var snowflakeColumnsStmt string

//go:embed sql/snowflake_fk_metadata_exists.sql
var snowflakeFKMetadataExistsStmt string

//go:embed sql/snowflake_fk_metadata.sql
var snowflakeFKMetadataStmt string

//go:embed sql/snowflake_clustering.sql
var snowflakeClusteringStmt string

//go:embed sql/bigquery_info.sql
var bigqueryInfo string

//go:embed sql/bigquery_columns.sql
var bigqueryColumnsStmt string

//go:embed sql/bigquery_constraints_count.sql
var bigqueryConstraintsCountStmt string

//go:embed sql/bigquery_primary_keys.sql
var bigqueryPrimaryKeysStmt string

//go:embed sql/bigquery_foreign_keys.sql
var bigqueryForeignKeysStmt string

//go:embed sql/mongodb_info.json
var mongodbInfo string

//go:embed sql/mongodb_columns.json
var mongodbColumnsStmt string
