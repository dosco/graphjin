package sdata

import _ "embed"

//go:embed sql/functions.sql
var functionsStmt string

//go:embed sql/postgres_info.sql
var postgresInfo string

//go:embed sql/postgres_columns.sql
var postgresColumnsStmt string

//go:embed sql/mysql_info.sql
var mysqlInfo string

//go:embed sql/mysql_columns.sql
var mysqlColumnsStmt string
