package redshift

import (
	"database/sql/driver"
	"fmt"
	"regexp"
	"strings"
)

func TranslateSetup(seedSQL string, schema *Schema) ([]string, error) {
	if schema == nil {
		return nil, fmt.Errorf("nil schema")
	}
	var out []string
	for _, t := range schema.Tables {
		if t.IsView {
			continue
		}
		out = append(out, createTableSQL(t))
	}
	for _, stmt := range SplitStatements(StripSQLComments(seedSQL)) {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		upper := strings.ToUpper(stmt)
		switch {
		case strings.HasPrefix(upper, "INSERT "):
			out = append(out, translateSetupInsert(stmt))
		case strings.HasPrefix(upper, "CREATE VIEW "):
			out = append(out, lowerRedshift(stmt))
		}
	}
	out = append(out, metadataCreateSQL()...)
	out = append(out, TranslateMetadataRefresh(schema)...)
	return out, nil
}

func TranslateMetadataRefresh(schema *Schema) []string {
	if schema == nil {
		return nil
	}
	out := []string{
		"DELETE FROM _gj_rs_databases",
		"DELETE FROM _gj_rs_schemas",
		"DELETE FROM _gj_rs_tables",
		"DELETE FROM _gj_rs_columns",
		"DELETE FROM _gj_rs_pg_table_def",
		fmt.Sprintf("INSERT INTO _gj_rs_databases VALUES (%s)", sqlString(schema.DBName)),
		fmt.Sprintf("INSERT INTO _gj_rs_schemas VALUES (%s, %s, 'owner', 'local', '', '', '')", sqlString(schema.DBName), sqlString(schema.Schema)),
	}
	for _, t := range schema.Tables {
		out = append(out, tableMetadataSQL(t))
		for i, c := range t.Columns {
			out = append(out, columnMetadataSQL(t, c, i+1))
			out = append(out, pgTableDefSQL(t, c))
		}
	}
	return out
}

func TranslateDiscoveryQuery(sql string, args []driver.NamedValue, schema *Schema) (string, []driver.NamedValue, error) {
	norm := strings.TrimSpace(StripSQLComments(sql))
	upper := strings.ToUpper(hostedNormalize(norm))
	switch {
	case strings.HasPrefix(upper, "SHOW DATABASES"):
		return translateShowDatabases(norm), args, nil
	case strings.HasPrefix(upper, "SHOW SCHEMAS"):
		return translateShowSchemas(norm, schema), args, nil
	case strings.HasPrefix(upper, "SHOW TABLES"):
		return translateShowTables(norm, schema), args, nil
	case strings.HasPrefix(upper, "SHOW COLUMNS"):
		return translateShowColumns(norm, schema), args, nil
	case strings.Contains(upper, "CURRENT_DATABASE") || strings.Contains(upper, "CURRENT_SCHEMA"):
		return translateCurrentContext(norm), args, nil
	default:
		return rewriteDiscoveryTables(norm), args, nil
	}
}

func createTableSQL(t *Table) string {
	cols := make([]string, 0, len(t.Columns))
	for _, c := range t.Columns {
		col := NormalizeIdentifier(c.Name) + " " + MapType(c.DDLType)
		if c.Default != "" {
			col += " DEFAULT " + translateDefault(c.Default)
		}
		if c.NotNull {
			col += " NOT NULL"
		}
		cols = append(cols, col)
	}
	return "CREATE TABLE " + NormalizeIdentifier(t.Name) + " (" + strings.Join(cols, ", ") + ")"
}

func metadataCreateSQL() []string {
	return []string{
		"CREATE TABLE _gj_rs_databases (database_name VARCHAR)",
		"CREATE TABLE _gj_rs_schemas (database_name VARCHAR, schema_name VARCHAR, schema_owner VARCHAR, schema_type VARCHAR, schema_acl VARCHAR, source_database VARCHAR, schema_option VARCHAR)",
		`CREATE TABLE _gj_rs_tables (
database_name VARCHAR, schema_name VARCHAR, table_name VARCHAR, table_type VARCHAR,
table_acl VARCHAR, remarks VARCHAR, owner VARCHAR, last_altered_time TIMESTAMP,
last_modified_time TIMESTAMP, dist_style VARCHAR, table_subtype VARCHAR, row_count BIGINT
)`,
		`CREATE TABLE _gj_rs_columns (
database_name VARCHAR, schema_name VARCHAR, table_name VARCHAR, column_name VARCHAR,
ordinal_position BIGINT, column_default VARCHAR, is_nullable VARCHAR, data_type VARCHAR,
character_maximum_length BIGINT, numeric_precision BIGINT, numeric_scale BIGINT,
remarks VARCHAR, sort_key_type VARCHAR, sort_key BIGINT, dist_key BOOLEAN,
encoding VARCHAR, collation_name VARCHAR, not_null BOOLEAN,
primary_key BOOLEAN, unique_key BOOLEAN,
foreignkey_schema VARCHAR, foreignkey_table VARCHAR, foreignkey_column VARCHAR
)`,
		`CREATE TABLE _gj_rs_pg_table_def (
schemaname VARCHAR, tablename VARCHAR, "column" VARCHAR, "type" VARCHAR,
encoding VARCHAR, distkey BOOLEAN, sortkey BIGINT, not_null BOOLEAN
)`,
	}
}

func tableMetadataSQL(t *Table) string {
	return fmt.Sprintf(
		"INSERT INTO _gj_rs_tables VALUES (%s, %s, %s, %s, '', '', 'owner', NULL, NULL, %s, %s, %d)",
		sqlString(t.DBName),
		sqlString(t.Schema),
		sqlString(t.Name),
		sqlString(tableType(t)),
		sqlString(distStyle(t)),
		sqlString(tableSubtype(t)),
		rowCount(t),
	)
}

func columnMetadataSQL(t *Table, c *Column, ordinal int) string {
	precision, scale := numericPrecisionScale(c.DDLType, c.Type)
	return fmt.Sprintf(
		"INSERT INTO _gj_rs_columns VALUES (%s, %s, %s, %s, %d, %s, %s, %s, %s, %s, %s, '', %s, %d, %t, %s, %s, %t, %t, %t, %s, %s, %s)",
		sqlString(t.DBName),
		sqlString(t.Schema),
		sqlString(t.Name),
		sqlString(c.Name),
		ordinal,
		nullableSQLString(c.Default),
		sqlString(nullable(c)),
		sqlString(redshiftDataType(c)),
		sqlNullableInt(charMaxLength(c.DDLType)),
		sqlNullableInt(precision),
		sqlNullableInt(scale),
		sqlString(c.SortKeyType),
		c.SortKey,
		c.DistKey,
		sqlString(encoding(c)),
		sqlString(strings.ToLower(c.Collation)),
		c.NotNull,
		c.PrimaryKey,
		c.UniqueKey,
		sqlString(fkSchema(t, c)),
		sqlString(fkTable(c)),
		sqlString(c.FKeyColumn),
	)
}

func pgTableDefSQL(t *Table, c *Column) string {
	return fmt.Sprintf(
		"INSERT INTO _gj_rs_pg_table_def VALUES (%s, %s, %s, %s, %s, %t, %d, %t)",
		sqlString(t.Schema),
		sqlString(t.Name),
		sqlString(c.Name),
		sqlString(redshiftDDLType(c)),
		sqlString(encoding(c)),
		c.DistKey,
		c.SortKey,
		c.NotNull,
	)
}

func fkSchema(t *Table, c *Column) string {
	if c.FKeyTable == "" {
		return ""
	}
	parts := splitQualified(c.FKeyTable)
	if len(parts) >= 2 {
		return parts[len(parts)-2]
	}
	if t != nil {
		return t.Schema
	}
	return DefaultSchema
}

func fkTable(c *Column) string {
	if c.FKeyTable == "" {
		return ""
	}
	parts := splitQualified(c.FKeyTable)
	if len(parts) != 0 {
		return parts[len(parts)-1]
	}
	return c.FKeyTable
}

func translateShowDatabases(sql string) string {
	out := "SELECT database_name FROM _gj_rs_databases"
	out = appendLike(out, sql, "database_name")
	out = appendLimit(out, sql)
	return out
}

func translateShowSchemas(sql string, schema *Schema) string {
	out := "SELECT database_name, schema_name, schema_owner, schema_type, schema_acl, source_database, schema_option FROM _gj_rs_schemas"
	if db := parseShowObject(sql, "FROM DATABASE"); db != "" {
		out += " WHERE lower(database_name) = lower(" + sqlString(db) + ")"
	}
	out = appendLike(out, sql, "schema_name")
	out = appendLimit(out, sql)
	if !strings.Contains(strings.ToUpper(out), " WHERE ") && schema != nil && schema.DBName != "" {
		return out
	}
	return out
}

func translateShowTables(sql string, schema *Schema) string {
	out := "SELECT database_name, schema_name, table_name, table_type, table_acl, remarks, owner, last_altered_time, last_modified_time, dist_style, table_subtype FROM _gj_rs_tables"
	dbName, schemaName := showSchemaTarget(sql, schema)
	var filters []string
	if dbName != "" {
		filters = append(filters, "lower(database_name) = lower("+sqlString(dbName)+")")
	}
	if schemaName != "" {
		filters = append(filters, "lower(schema_name) = lower("+sqlString(schemaName)+")")
	}
	if len(filters) != 0 {
		out += " WHERE " + strings.Join(filters, " AND ")
	}
	out = appendLike(out, sql, "table_name")
	out += " ORDER BY database_name, schema_name, table_name"
	out = appendLimit(out, sql)
	return out
}

func translateShowColumns(sql string, schema *Schema) string {
	out := "SELECT database_name, schema_name, table_name, column_name, ordinal_position, column_default, is_nullable, data_type, character_maximum_length, numeric_precision, numeric_scale, remarks, sort_key_type, sort_key, dist_key, encoding, collation_name FROM _gj_rs_columns"
	dbName, schemaName, tableName := showTableTarget(sql, schema)
	var filters []string
	if dbName != "" {
		filters = append(filters, "lower(database_name) = lower("+sqlString(dbName)+")")
	}
	if schemaName != "" {
		filters = append(filters, "lower(schema_name) = lower("+sqlString(schemaName)+")")
	}
	if tableName != "" {
		filters = append(filters, "lower(table_name) = lower("+sqlString(tableName)+")")
	}
	if len(filters) != 0 {
		out += " WHERE " + strings.Join(filters, " AND ")
	}
	out = appendLike(out, sql, "column_name")
	out += " ORDER BY database_name, schema_name, table_name, ordinal_position"
	out = appendLimit(out, sql)
	return out
}

func translateCurrentContext(sql string) string {
	out := replaceFunction(sql, "CURRENT_DATABASE", func(string) string {
		return "(SELECT database_name FROM _gj_rs_databases LIMIT 1)"
	})
	out = replaceFunction(out, "CURRENT_SCHEMA", func(string) string {
		return "(SELECT schema_name FROM _gj_rs_schemas LIMIT 1)"
	})
	return out
}

func rewriteDiscoveryTables(sql string) string {
	repls := []struct{ from, to string }{
		{"svv_all_columns", "_gj_rs_columns"},
		{"svv_redshift_columns", "_gj_rs_columns"},
		{"svv_columns", "_gj_rs_columns"},
		{"svv_all_tables", "_gj_rs_tables"},
		{"svv_redshift_tables", "_gj_rs_tables"},
		{"svv_tables", "_gj_rs_tables"},
		{"pg_table_def", "_gj_rs_pg_table_def"},
	}
	out := sql
	for _, repl := range repls {
		out = replaceFoldAll(out, repl.from, repl.to)
	}
	return lowerRedshift(out)
}

func lowerRedshift(sql string) string {
	out := strings.TrimSpace(sql)
	if subSQL, ok := translateSubscriptionUnbox(out); ok {
		return subSQL
	}
	out = replaceFoldAll(out, "public.", "")
	out = replaceFoldAll(out, `"public".`, "")
	out = replaceFoldAll(out, `"PUBLIC".`, "")
	out = strings.ReplaceAll(out, `"json"FROM`, `"json" FROM`)
	out = replaceFunctionName(out, "OBJECT_CONSTRUCT_KEEP_NULL", "json_object")
	out = replaceFunctionName(out, "OBJECT_CONSTRUCT", "json_object")
	out = replaceFunction(out, "GET", translateGet)
	out = replaceFunctionName(out, "ARRAY_AGG", "json_group_array")
	out = replaceFunctionName(out, "ANY_VALUE", "any_value")
	for _, fn := range []string{"ST_DWITHIN", "ST_WITHIN", "ST_CONTAINS", "ST_INTERSECTS"} {
		out = replaceFunction(out, fn, func(string) string {
			return "TRUE"
		})
	}
	out = replaceFunction(out, "ST_GEOMFROMTEXT", func(arg string) string {
		parts := SplitTopLevel(arg, ',')
		if len(parts) == 0 {
			return "NULL"
		}
		return strings.TrimSpace(parts[0])
	})
	out = replaceFunction(out, "ST_GEOMFROMGEOJSON", func(arg string) string {
		return strings.TrimSpace(arg)
	})
	out = replaceFunction(out, "GET_PATH", translateGetPath)
	out = replaceFunction(out, "ARRAY_CONSTRUCT", func(arg string) string {
		if strings.TrimSpace(arg) == "" {
			return "CAST('[]' AS JSON)"
		}
		return "json_array(" + arg + ")"
	})
	out = replaceFunction(out, "ARRAY_SLICE", func(arg string) string {
		parts := SplitTopLevel(arg, ',')
		if len(parts) == 0 {
			return arg
		}
		return strings.TrimSpace(parts[0])
	})
	out = replaceFunction(out, "TO_VARCHAR", func(arg string) string {
		return "CAST(" + strings.TrimSpace(arg) + " AS VARCHAR)"
	})
	out = replaceFunction(out, "JSON_PARSE", func(arg string) string {
		return strings.TrimSpace(arg)
	})
	out = replaceFunction(out, "JSON_SERIALIZE", func(arg string) string {
		return "CAST(" + strings.TrimSpace(arg) + " AS VARCHAR)"
	})
	return out
}

func translateSubscriptionUnbox(sql string) (string, bool) {
	const prefix = `WITH _gj_sub AS (SELECT `
	const cteTail = ` FROM (SELECT JSON_PARSE(?) AS _gj_params) AS _gj_sub_input, UNNEST(_gj_sub_input._gj_params) WITH OFFSET AS _gj_sub_unboxed(value, idx)) SELECT `
	const suffix = ` AS "__root" FROM _gj_sub ORDER BY "__gj_sub_order"`

	if !strings.HasPrefix(sql, prefix) {
		return "", false
	}
	cteEnd := strings.Index(sql, cteTail)
	if cteEnd < 0 {
		return "", false
	}
	innerOpen := cteEnd + len(cteTail)
	if innerOpen >= len(sql) || sql[innerOpen] != '(' {
		return "", false
	}
	innerClose := FindMatchingParen(sql, innerOpen)
	if innerClose < 0 || !strings.HasSuffix(sql[innerClose+1:], suffix) {
		return "", false
	}

	cols, ok := translateSubscriptionColumns(sql[len(prefix):cteEnd])
	if !ok {
		return "", false
	}
	inner := lowerRedshift(sql[innerOpen+1 : innerClose])
	return `WITH _gj_sub AS (SELECT ` + strings.Join(cols, ", ") + ` FROM json_each(CAST(? AS JSON))) SELECT (` + inner + `) AS "__root" FROM _gj_sub ORDER BY "__gj_sub_order"`, true
}

var subCastRE = regexp.MustCompile(`(?is)^CAST\(_gj_sub_unboxed\.value\[(\d+)\]\s+AS\s+(.+?)\)\s+AS\s+(.+)$`)

func translateSubscriptionColumns(cols string) ([]string, bool) {
	parts := SplitTopLevel(cols, ',')
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		switch {
		case strings.EqualFold(part, `_gj_sub_unboxed.idx AS "__gj_sub_order"`):
			out = append(out, `CAST(key AS BIGINT) AS "__gj_sub_order"`)
		default:
			m := subCastRE.FindStringSubmatch(part)
			if m == nil {
				return nil, false
			}
			idx := strings.TrimSpace(m[1])
			typ := strings.TrimSpace(m[2])
			alias := strings.TrimSpace(m[3])
			out = append(out, translateSubscriptionColumn(idx, typ, alias))
		}
	}
	return out, true
}

func translateSubscriptionColumn(index, typ, alias string) string {
	path := `'$[` + index + `]'`
	switch strings.ToUpper(strings.Join(strings.Fields(typ), " ")) {
	case "VARCHAR", "CHAR", "CHARACTER", "CHARACTER VARYING", "TEXT":
		return `json_extract_string(value, ` + path + `) AS ` + alias
	case "SUPER", "JSON", "JSONB":
		return `json_extract(value, ` + path + `) AS ` + alias
	default:
		return `CAST(json_extract(value, ` + path + `) AS ` + MapType(typ) + `) AS ` + alias
	}
}

func lowerRedshiftDirect(sql string) string {
	out := lowerRedshift(sql)
	upper := strings.ToUpper(hostedNormalize(out))
	if strings.HasPrefix(upper, "CREATE TABLE ") ||
		strings.HasPrefix(upper, "CREATE TEMP TABLE ") ||
		strings.HasPrefix(upper, "ALTER TABLE ") ||
		strings.HasPrefix(upper, "DROP TABLE ") {
		return lowerRedshiftDDL(out)
	}
	return out
}

func lowerRedshiftDDL(sql string) string {
	out := sql
	out = regexp.MustCompile(`(?is)\s+GENERATED\s+BY\s+DEFAULT\s+AS\s+IDENTITY\s*(?:\([^)]*\))?`).ReplaceAllString(out, "")
	out = regexp.MustCompile(`(?is)\s+IDENTITY\s*(?:\([^)]*\))?`).ReplaceAllString(out, "")
	out = regexp.MustCompile(`(?is)\s+DISTSTYLE\s+(?:AUTO|EVEN|KEY|ALL)\b`).ReplaceAllString(out, "")
	out = regexp.MustCompile(`(?is)\s+DISTKEY\s*\([^)]*\)`).ReplaceAllString(out, "")
	out = regexp.MustCompile(`(?is)\s+(?:COMPOUND|INTERLEAVED)?\s*SORTKEY\s*\([^)]*\)`).ReplaceAllString(out, "")
	out = regexp.MustCompile(`(?is)\s+SORTKEY\b`).ReplaceAllString(out, "")
	out = regexp.MustCompile(`(?is)\s+ENCODE\s+(?:AUTO|RAW|AZ64|BYTEDICT|DELTA|DELTA32K|LZO|MOSTLY8|MOSTLY16|MOSTLY32|RUNLENGTH|TEXT255|TEXT32K|ZSTD)\b`).ReplaceAllString(out, "")
	out = replaceRedshiftDDLType(out, "SUPER", "JSON")
	out = replaceRedshiftDDLType(out, "VARBYTE", "BLOB")
	out = replaceRedshiftDDLType(out, "HLLSKETCH", "VARCHAR")
	out = replaceRedshiftDDLType(out, "GEOMETRY", "VARCHAR")
	out = replaceRedshiftDDLType(out, "GEOGRAPHY", "VARCHAR")
	out = replaceRedshiftDDLType(out, "TIMESTAMPTZ", "TIMESTAMP")
	out = replaceRedshiftDDLType(out, "TIMETZ", "TIME")
	return out
}

func replaceRedshiftDDLType(sql, from, to string) string {
	re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(from) + `\b`)
	return re.ReplaceAllString(sql, to)
}

func replaceFunctionName(sql, from, to string) string {
	return replaceFoldAll(sql, from, to)
}

func translateGetPath(arg string) string {
	parts := SplitTopLevel(arg, ',')
	if len(parts) < 2 {
		return "json_extract(" + arg + ")"
	}
	base := strings.TrimSpace(parts[0])
	path := strings.Trim(strings.TrimSpace(parts[1]), "'")
	if !strings.HasPrefix(path, "$") {
		path = "$." + strings.TrimPrefix(path, ".")
	}
	return "json_extract(" + base + ", '" + path + "')"
}

func translateGet(arg string) string {
	parts := SplitTopLevel(arg, ',')
	if len(parts) != 2 {
		return "GET(" + arg + ")"
	}
	target := strings.TrimSpace(parts[0])
	index := strings.TrimSpace(parts[1])
	if idx := indexFoldFrom(target, "ARRAY_AGG", 0); idx == 0 {
		open := idx + len("ARRAY_AGG")
		for open < len(target) && isDDLSpace(target[open]) {
			open++
		}
		close := FindMatchingParen(target, open)
		if close == len(target)-1 {
			aggArg := strings.TrimSpace(target[open+1 : close])
			return "list_extract(list(" + aggArg + "), " + duckDBListIndex(index) + ")"
		}
	}
	return "GET(" + arg + ")"
}

func duckDBListIndex(index string) string {
	if strings.EqualFold(strings.ReplaceAll(index, " ", ""), "COUNT(*)-1") {
		return "CAST(COUNT(*) AS BIGINT)"
	}
	return "(" + index + ") + 1"
}

func translateSetupInsert(sql string) string {
	return lowerRedshift(sql)
}

func MapType(t string) string {
	raw := strings.TrimSpace(t)
	upper := strings.ToUpper(strings.Join(strings.Fields(raw), " "))
	switch {
	case upper == "":
		return "VARCHAR"
	case strings.HasPrefix(upper, "SMALLINT") || upper == "INT2":
		return "SMALLINT"
	case strings.HasPrefix(upper, "INTEGER") || upper == "INT" || upper == "INT4":
		return "INTEGER"
	case strings.HasPrefix(upper, "BIGINT") || upper == "INT8":
		return "BIGINT"
	case strings.HasPrefix(upper, "DECIMAL") || strings.HasPrefix(upper, "NUMERIC"):
		if open := strings.IndexByte(raw, '('); open >= 0 {
			return "DECIMAL" + raw[open:]
		}
		return "DECIMAL"
	case strings.HasPrefix(upper, "REAL") || upper == "FLOAT4":
		return "REAL"
	case strings.HasPrefix(upper, "DOUBLE PRECISION") || upper == "FLOAT8" || upper == "FLOAT":
		return "DOUBLE"
	case strings.HasPrefix(upper, "BOOLEAN") || upper == "BOOL":
		return "BOOLEAN"
	case strings.HasPrefix(upper, "TIMESTAMP"):
		return "TIMESTAMP"
	case strings.HasPrefix(upper, "TIMESTAMPTZ"):
		return "TIMESTAMP"
	case strings.HasPrefix(upper, "TIMETZ"):
		return "TIME"
	case strings.HasPrefix(upper, "DATE"):
		return "DATE"
	case strings.HasPrefix(upper, "TIME"):
		return "TIME"
	case strings.HasPrefix(upper, "INTERVAL"):
		return "INTERVAL"
	case strings.HasPrefix(upper, "VARBYTE") || strings.HasPrefix(upper, "VARBINARY") || strings.HasPrefix(upper, "BINARY VARYING"):
		return "BLOB"
	case strings.HasPrefix(upper, "SUPER") || strings.HasPrefix(upper, "HLLSKETCH") ||
		strings.HasPrefix(upper, "GEOMETRY") || strings.HasPrefix(upper, "GEOGRAPHY"):
		return "VARCHAR"
	case strings.Contains(upper, "CHAR") || strings.Contains(upper, "TEXT"):
		return "VARCHAR"
	default:
		return upper
	}
}

func translateDefault(def string) string {
	def = strings.TrimSpace(def)
	switch strings.ToUpper(def) {
	case "CURRENT_TIMESTAMP", "TRUE", "FALSE", "NULL":
		return strings.ToUpper(def)
	default:
		return lowerRedshift(def)
	}
}

func tableType(t *Table) string {
	if t.IsView {
		return "VIEW"
	}
	return "TABLE"
}

func tableSubtype(t *Table) string {
	if t.IsView {
		return "REGULAR VIEW"
	}
	return "REGULAR TABLE"
}

func distStyle(t *Table) string {
	if t == nil || t.DistStyle == "" {
		return "AUTO"
	}
	if t.DistStyle == "KEY" && t.DistKey != "" {
		return "KEY(" + strings.ToLower(t.DistKey) + ")"
	}
	return t.DistStyle
}

func rowCount(t *Table) int64 {
	if t != nil && t.IsView {
		return 0
	}
	return 100
}

func nullable(c *Column) string {
	if c.NotNull || c.PrimaryKey {
		return "NO"
	}
	return "YES"
}

func encoding(c *Column) string {
	if c.Encoding != "" {
		return strings.ToLower(c.Encoding)
	}
	switch MapType(c.DDLType) {
	case "BOOLEAN", "REAL", "DOUBLE", "VARCHAR":
		if c.Type == "SUPER" {
			return "zstd"
		}
		if c.SortKey != 0 {
			return "raw"
		}
		if MapType(c.DDLType) == "VARCHAR" {
			return "lzo"
		}
		return "raw"
	case "SMALLINT", "INTEGER", "BIGINT", "DECIMAL", "DATE", "TIME", "TIMESTAMP":
		if c.SortKey != 0 {
			return "raw"
		}
		return "az64"
	default:
		return "raw"
	}
}

func redshiftDataType(c *Column) string {
	switch c.Type {
	case "VARCHAR":
		return "character varying"
	case "CHAR":
		return "character"
	case "INT", "INT4":
		return "integer"
	case "INT8":
		return "bigint"
	case "BOOL":
		return "boolean"
	case "TIMESTAMPTZ":
		return "timestamp with time zone"
	case "TIMESTAMP":
		return "timestamp without time zone"
	case "TIMETZ":
		return "time with time zone"
	case "TIME":
		return "time without time zone"
	case "DOUBLE PRECISION":
		return "double precision"
	default:
		return strings.ToLower(c.Type)
	}
}

func redshiftDDLType(c *Column) string {
	typ := strings.ToLower(strings.Join(strings.Fields(c.DDLType), " "))
	typ = strings.ReplaceAll(typ, "character varying", "character varying")
	return typ
}

func sqlString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func nullableSQLString(s string) string {
	if s == "" {
		return "NULL"
	}
	return sqlString(s)
}

func sqlNullableInt(v any) string {
	switch t := v.(type) {
	case nil:
		return "NULL"
	case int:
		return fmt.Sprintf("%d", t)
	case int64:
		return fmt.Sprintf("%d", t)
	default:
		return "NULL"
	}
}

func showTableColumns() []string {
	return []string{"database_name", "schema_name", "table_name", "table_type", "table_acl", "remarks", "owner", "last_altered_time", "last_modified_time", "dist_style", "table_subtype"}
}

func showColumnColumns() []string {
	return []string{"database_name", "schema_name", "table_name", "column_name", "ordinal_position", "column_default", "is_nullable", "data_type", "character_maximum_length", "numeric_precision", "numeric_scale", "remarks", "sort_key_type", "sort_key", "dist_key", "encoding", "collation_name"}
}

func tableRowValues(t *Table, cols []string) []driver.Value {
	row := make([]driver.Value, len(cols))
	for i, col := range cols {
		switch strings.ToLower(col) {
		case "database_name":
			row[i] = t.DBName
		case "schema_name":
			row[i] = t.Schema
		case "table_name":
			row[i] = t.Name
		case "table_type":
			row[i] = tableType(t)
		case "table_acl", "remarks":
			row[i] = ""
		case "owner":
			row[i] = "owner"
		case "dist_style":
			row[i] = distStyle(t)
		case "table_subtype":
			row[i] = tableSubtype(t)
		default:
			row[i] = nil
		}
	}
	return row
}

func columnRowValues(t *Table, c *Column, cols []string) []driver.Value {
	row := make([]driver.Value, len(cols))
	precision, scale := numericPrecisionScale(c.DDLType, c.Type)
	for i, col := range cols {
		switch strings.ToLower(col) {
		case "database_name":
			row[i] = t.DBName
		case "schema_name":
			row[i] = t.Schema
		case "table_name":
			row[i] = t.Name
		case "column_name":
			row[i] = c.Name
		case "ordinal_position":
			for j, tc := range t.Columns {
				if tc == c {
					row[i] = int64(j + 1)
					break
				}
			}
		case "sort_key":
			{
				row[i] = int64(c.SortKey)
			}
		case "column_default":
			row[i] = c.Default
		case "is_nullable":
			row[i] = nullable(c)
		case "data_type":
			row[i] = redshiftDataType(c)
		case "character_maximum_length":
			row[i] = charMaxLength(c.DDLType)
		case "numeric_precision":
			row[i] = precision
		case "numeric_scale":
			row[i] = scale
		case "remarks", "collation", "collation_name":
			row[i] = ""
		case "sort_key_type":
			row[i] = c.SortKeyType
		case "dist_key":
			row[i] = c.DistKey
		case "encoding":
			row[i] = encoding(c)
		}
	}
	return row
}

func selectColumns(sql string) []string {
	upper := strings.ToUpper(sql)
	if !strings.HasPrefix(upper, "SELECT ") {
		return []string{"JSON"}
	}
	from := indexTopLevelKeyword(upper[len("SELECT "):], "FROM")
	if from < 0 {
		return []string{"JSON"}
	}
	from += len("SELECT ")
	items := SplitTopLevel(sql[len("SELECT "):from], ',')
	cols := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			cols = append(cols, selectColumnName(item))
		}
	}
	if len(cols) == 0 {
		return []string{"JSON"}
	}
	return cols
}

func selectColumnName(expr string) string {
	fields := strings.Fields(expr)
	if len(fields) >= 3 && strings.EqualFold(fields[len(fields)-2], "AS") {
		return TrimIdent(fields[len(fields)-1])
	}
	if len(fields) != 0 {
		expr = fields[len(fields)-1]
	}
	if dot := strings.LastIndexByte(expr, '.'); dot >= 0 {
		expr = expr[dot+1:]
	}
	return TrimIdent(expr)
}

func valueForColumn(col string) driver.Value {
	switch strings.ToLower(TrimIdent(col)) {
	case "count", "count(*)", "row_count":
		return int64(1)
	case "id", "user_id", "org_id", "product_id", "quantity":
		return int64(2)
	case "price", "amount":
		return float64(24.99)
	case "disabled", "dist_key", "distkey", "notnull":
		return false
	case "created_at", "updated_at", "event_time":
		return "2024-01-01 00:00:00"
	default:
		return "mock"
	}
}

func parseShowObject(sql, marker string) string {
	idx := indexTopLevelKeyword(sql, marker)
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSpace(sql[idx+len(marker):])
	name, _ := readIdentToken(rest)
	return TrimIdent(name)
}

func showSchemaTarget(sql string, schema *Schema) (string, string) {
	target := parseShowObject(sql, "FROM SCHEMA")
	if target == "" {
		if schema == nil {
			return "", ""
		}
		return schema.DBName, schema.Schema
	}
	parts := splitQualified(target)
	if len(parts) >= 2 {
		return parts[len(parts)-2], parts[len(parts)-1]
	}
	dbName := ""
	if schema != nil {
		dbName = schema.DBName
	}
	return dbName, target
}

func showTableTarget(sql string, schema *Schema) (string, string, string) {
	target := parseShowObject(sql, "FROM TABLE")
	if target == "" {
		return "", "", ""
	}
	parts := splitQualified(target)
	switch len(parts) {
	case 1:
		if schema == nil {
			return "", "", parts[0]
		}
		return schema.DBName, schema.Schema, parts[0]
	case 2:
		dbName := ""
		if schema != nil {
			dbName = schema.DBName
		}
		return dbName, parts[0], parts[1]
	default:
		return parts[len(parts)-3], parts[len(parts)-2], parts[len(parts)-1]
	}
}

func appendLike(out, sql, column string) string {
	remainder := strings.ToUpper(sql)
	idx := indexTopLevelKeyword(remainder, "LIKE")
	if idx < 0 {
		return out
	}
	pattern := strings.TrimSpace(sql[idx+len("LIKE"):])
	if end := indexTopLevelKeyword(pattern, "LIMIT"); end >= 0 {
		pattern = strings.TrimSpace(pattern[:end])
	}
	filter := column + " LIKE " + pattern
	if strings.Contains(strings.ToUpper(out), " WHERE ") {
		return out + " AND " + filter
	}
	return out + " WHERE " + filter
}

func appendLimit(out, sql string) string {
	idx := indexTopLevelKeyword(sql, "LIMIT")
	if idx < 0 {
		return out
	}
	limit := strings.TrimSpace(sql[idx+len("LIMIT"):])
	if limit == "" {
		return out
	}
	return out + " LIMIT " + limit
}

func hostedNormalize(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}
