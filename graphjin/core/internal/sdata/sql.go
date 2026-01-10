package sdata

import _ "embed"

//go:embed sql/postgres_functions.sql
var postgresFunctionsStmt string

//go:embed sql/mysql_functions.sql
var mysqlFunctionsStmt string

//go:embed sql/postgres_info.sql
var postgresInfo string

//go:embed sql/postgres_columns.sql
var postgresColumnsStmt string

//go:embed sql/mysql_info.sql
var mysqlInfo string

//go:embed sql/mysql_columns.sql
var mysqlColumnsStmt string

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

//go:embed sql/mssql_functions.sql
var mssqlFunctionsStmt string

//go:embed sql/mssql_info.sql
var mssqlInfo string

//go:embed sql/mssql_columns.sql
var mssqlColumnsStmt string

//go:embed sql/mongodb_info.json
var mongodbInfo string

//go:embed sql/mongodb_columns.json
var mongodbColumnsStmt string
