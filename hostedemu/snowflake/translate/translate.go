package translate

import (
	"database/sql/driver"
	"fmt"
	"strings"

	"github.com/dosco/graphjin/hostedemu/snowflake/catalog"
)

func TranslateSetup(sql string, schema *catalog.Schema) ([]string, error) {
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

	for _, stmt := range catalog.SplitStatements(catalog.StripSQLComments(sql)) {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := ParseStatement(parseReadySQL(stmt)); err != nil {
			return nil, err
		}
		upper := strings.ToUpper(stmt)
		switch {
		case strings.HasPrefix(upper, "CREATE VIEW "):
			out = append(out, lowerSnowflake(stmt))
		case strings.HasPrefix(upper, "INSERT "):
			out = append(out, translateSetupInsert(stmt))
		}
	}
	out = append(out, metadataCreateSQL(schema)...)
	out = append(out, TranslateMetadataRefresh(schema)...)
	return out, nil
}

func TranslateMetadataRefresh(schema *catalog.Schema) []string {
	if schema == nil {
		return nil
	}
	var out []string
	out = append(out,
		"UPDATE _gj_sf_session SET db_name = "+sqlString(schema.DBName)+", schema_name = "+sqlString(schema.Schema)+", discovery_mode = "+sqlString(discoveryMode(schema)),
		"DELETE FROM _gj_sf_information_schema_views",
		"DELETE FROM _gj_sf_tables",
		"DELETE FROM _gj_sf_columns",
		"DELETE FROM _gj_sf_table_constraints",
		"DELETE FROM _gj_sf_key_column_usage",
		"DELETE FROM _gj_sf_referential_constraints",
		"DELETE FROM _gj_sf_result_scan",
		"DELETE FROM _gj_fk_metadata",
	)
	if discoveryMode(schema) == "information_schema" {
		out = append(out, "INSERT INTO _gj_sf_information_schema_views VALUES ('INFORMATION_SCHEMA', 'KEY_COLUMN_USAGE')")
	}
	out = append(out, metadataTableRows(schema)...)
	out = append(out, metadataConstraintRows(schema)...)
	return out
}

func TranslateRuntime(sql string, args []driver.NamedValue, _ *catalog.Schema) (string, []driver.NamedValue, error) {
	if _, err := ParseStatement(parseReadySQL(sql)); err != nil {
		return "", args, err
	}
	return lowerSnowflake(sql), args, nil
}

func TranslateDirect(sql string, args []driver.NamedValue, _ *catalog.Schema) (string, []driver.NamedValue, error) {
	if _, err := ParseStatement(parseReadySQL(sql)); err != nil {
		return "", args, err
	}
	return lowerSnowflake(sql), args, nil
}

func TranslateDiscoveryQuery(sql string, args []driver.NamedValue, schema *catalog.Schema) (string, []driver.NamedValue, error) {
	norm := strings.TrimSpace(catalog.StripSQLComments(sql))
	upper := strings.ToUpper(hostedNormalize(norm))
	if discoveryMode(schema) == "show" && strings.Contains(upper, "INFORMATION_SCHEMA.COLUMNS") {
		return "", args, fmt.Errorf("snowflake emulator: information_schema column discovery disabled in show mode")
	}
	if strings.Contains(upper, "INFORMATION_SCHEMA.VIEWS") && strings.Contains(upper, "KEY_COLUMN_USAGE") && discoveryMode(schema) == "show" {
		return "SELECT 0 AS count", args, nil
	}
	if strings.Contains(upper, "CURRENT_SCHEMA()") && strings.Contains(upper, "CURRENT_DATABASE()") {
		return "SELECT 0 AS db_version, lower(schema_name) AS db_schema, db_name FROM _gj_sf_session LIMIT 1", args, nil
	}
	if strings.Contains(upper, "SELECT LAST_QUERY_ID()") {
		return "SELECT last_query_id AS LAST_QUERY_ID FROM _gj_sf_session LIMIT 1", args, nil
	}
	out := rewriteDiscoveryTables(norm)
	out = rewriteCurrentContext(out)
	out = rewriteResultScan(out)
	return lowerSnowflake(out), args, nil
}

func TranslateDiscoveryExec(sql string, args []driver.NamedValue, schema *catalog.Schema) ([]string, []driver.NamedValue, error) {
	norm := hostedNormalize(sql)
	upper := strings.ToUpper(norm)
	switch {
	case strings.HasPrefix(upper, "SHOW COLUMNS"):
		return showResultScanStatements("columns"), args, nil
	case strings.HasPrefix(upper, "SHOW PRIMARY KEYS"):
		return showResultScanStatements("pks"), args, nil
	case strings.HasPrefix(upper, "SHOW UNIQUE KEYS"):
		return showResultScanStatements("uks"), args, nil
	case strings.HasPrefix(upper, "SHOW IMPORTED KEYS"):
		return showResultScanStatements("fks"), args, nil
	case strings.HasPrefix(upper, "SHOW TABLES"):
		return showResultScanStatements("tables"), args, nil
	default:
		translated, translatedArgs, err := TranslateDiscoveryQuery(sql, args, schema)
		return []string{translated}, translatedArgs, err
	}
}

func createTableSQL(t *catalog.Table) string {
	cols := make([]string, 0, len(t.Columns)+1)
	for _, c := range t.Columns {
		colType := c.DDLType
		if colType == "" {
			colType = c.Type
		}
		col := NormalizeIdentifier(c.Name) + " " + MapType(colType)
		if c.Default != "" {
			col += " DEFAULT " + translateDefault(c.Default)
		}
		if c.NotNull {
			col += " NOT NULL"
		}
		cols = append(cols, col)
	}
	if len(t.PrimaryKeys) != 0 {
		pks := make([]string, 0, len(t.PrimaryKeys))
		for _, pk := range t.PrimaryKeys {
			pks = append(pks, NormalizeIdentifier(pk))
		}
		cols = append(cols, "PRIMARY KEY ("+strings.Join(pks, ", ")+")")
	}
	return "CREATE TABLE " + NormalizeIdentifier(t.Name) + " (" + strings.Join(cols, ", ") + ")"
}

func metadataCreateSQL(schema *catalog.Schema) []string {
	return []string{
		"CREATE TABLE _gj_sf_session (query_seq BIGINT, last_query_id VARCHAR, db_name VARCHAR, schema_name VARCHAR, discovery_mode VARCHAR)",
		"INSERT INTO _gj_sf_session VALUES (0, 'QID_0', " + sqlString(schema.DBName) + ", " + sqlString(schema.Schema) + ", " + sqlString(discoveryMode(schema)) + ")",
		"CREATE TABLE _gj_sf_information_schema_views (table_schema VARCHAR, table_name VARCHAR)",
		"CREATE TABLE _gj_sf_tables (table_catalog VARCHAR, table_schema VARCHAR, table_name VARCHAR, table_type VARCHAR, row_count BIGINT, clustering_key VARCHAR)",
		"CREATE TABLE _gj_sf_columns (table_catalog VARCHAR, table_schema VARCHAR, table_name VARCHAR, column_name VARCHAR, ordinal_position BIGINT, data_type VARCHAR, snowflake_show_data_type VARCHAR, is_nullable VARCHAR, is_array BOOLEAN)",
		"CREATE TABLE _gj_sf_table_constraints (constraint_catalog VARCHAR, constraint_schema VARCHAR, constraint_name VARCHAR, table_schema VARCHAR, table_name VARCHAR, constraint_type VARCHAR)",
		"CREATE TABLE _gj_sf_key_column_usage (constraint_catalog VARCHAR, constraint_schema VARCHAR, constraint_name VARCHAR, table_schema VARCHAR, table_name VARCHAR, column_name VARCHAR, ordinal_position BIGINT, position_in_unique_constraint BIGINT)",
		"CREATE TABLE _gj_sf_referential_constraints (constraint_catalog VARCHAR, constraint_schema VARCHAR, constraint_name VARCHAR, unique_constraint_catalog VARCHAR, unique_constraint_schema VARCHAR, unique_constraint_name VARCHAR)",
		"CREATE TABLE _gj_sf_result_scan (query_id VARCHAR, scan_type VARCHAR, schema_name VARCHAR, table_name VARCHAR, column_name VARCHAR, data_type VARCHAR, fk_schema_name VARCHAR, fk_table_name VARCHAR, fk_column_name VARCHAR, pk_schema_name VARCHAR, pk_table_name VARCHAR, pk_column_name VARCHAR, fk_name VARCHAR, key_sequence BIGINT, name VARCHAR, cluster_by VARCHAR, \"rows\" BIGINT)",
		"CREATE TABLE _gj_fk_metadata (table_schema VARCHAR, table_name VARCHAR, column_name VARCHAR, foreign_table_schema VARCHAR, foreign_table_name VARCHAR, foreign_column_name VARCHAR)",
	}
}

func metadataTableRows(schema *catalog.Schema) []string {
	var out []string
	for _, t := range schema.Tables {
		tableType := "BASE TABLE"
		rowCount := int64(100)
		if t.IsView {
			tableType = "VIEW"
			rowCount = 0
		}
		out = append(out, fmt.Sprintf(
			"INSERT INTO _gj_sf_tables VALUES (%s, %s, %s, %s, %d, %s)",
			sqlString(schema.DBName),
			sqlString(sfIdent(schema.Schema)),
			sqlString(sfIdent(t.Name)),
			sqlString(tableType),
			rowCount,
			sqlString(clusteringKey(t)),
		))
		for i, c := range t.Columns {
			nullable := "YES"
			if c.NotNull || c.PrimaryKey {
				nullable = "NO"
			}
			out = append(out, fmt.Sprintf(
				"INSERT INTO _gj_sf_columns VALUES (%s, %s, %s, %s, %d, %s, %s, %s, %t)",
				sqlString(schema.DBName),
				sqlString(sfIdent(schema.Schema)),
				sqlString(sfIdent(t.Name)),
				sqlString(sfIdent(c.Name)),
				i+1,
				sqlString(sfDataType(c)),
				sqlString(sfShowDataType(c, nullable == "YES")),
				sqlString(nullable),
				c.Array,
			))
		}
	}
	return out
}

func metadataConstraintRows(schema *catalog.Schema) []string {
	var out []string
	for _, t := range schema.Tables {
		if len(t.PrimaryKeys) != 0 {
			name := constraintName(t.Name, "PK")
			out = append(out, tableConstraintSQL(schema, t, name, "PRIMARY KEY"))
			for i, col := range t.PrimaryKeys {
				out = append(out, keyColumnSQL(schema, t, name, col, i+1, i+1))
			}
		}
		for _, c := range t.Columns {
			if !c.UniqueKey || c.PrimaryKey {
				continue
			}
			name := constraintName(t.Name+"_"+c.Name, "UK")
			out = append(out, tableConstraintSQL(schema, t, name, "UNIQUE"))
			out = append(out, keyColumnSQL(schema, t, name, c.Name, 1, 1))
		}
	}
	for _, fk := range foreignKeyGroups(schema) {
		out = append(out, tableConstraintSQL(schema, fk.table, fk.name, "FOREIGN KEY"))
		uniqueName := referencedConstraintName(fk.refTable)
		out = append(out, fmt.Sprintf(
			"INSERT INTO _gj_sf_referential_constraints VALUES (%s, %s, %s, %s, %s, %s)",
			sqlString(schema.DBName),
			sqlString(sfIdent(schema.Schema)),
			sqlString(fk.name),
			sqlString(schema.DBName),
			sqlString(sfIdent(schema.Schema)),
			sqlString(uniqueName),
		))
		for i, col := range fk.columns {
			pos := referencedPosition(fk.refTable, col.FKeyColumn)
			out = append(out, keyColumnSQL(schema, fk.table, fk.name, col.Name, i+1, pos))
		}
	}
	return out
}

type fkGroup struct {
	table    *catalog.Table
	refTable *catalog.Table
	name     string
	columns  []*catalog.Column
}

func foreignKeyGroups(schema *catalog.Schema) []fkGroup {
	type key struct {
		table string
		ref   string
	}
	groupMap := make(map[key]*fkGroup)
	var order []key
	for _, t := range schema.Tables {
		for _, c := range t.Columns {
			if c.FKeyTable == "" {
				continue
			}
			k := key{table: catalog.NormIdent(t.Name), ref: catalog.NormIdent(c.FKeyTable)}
			g := groupMap[k]
			if g == nil {
				ref := schema.Table(c.FKeyTable)
				g = &fkGroup{
					table:    t,
					refTable: ref,
					name:     sfIdent(t.Name) + "_" + sfIdent(c.FKeyTable) + "_FK",
				}
				groupMap[k] = g
				order = append(order, k)
			}
			g.columns = append(g.columns, c)
		}
	}
	out := make([]fkGroup, 0, len(order))
	for _, k := range order {
		out = append(out, *groupMap[k])
	}
	return out
}

func tableConstraintSQL(schema *catalog.Schema, t *catalog.Table, name, typ string) string {
	return fmt.Sprintf(
		"INSERT INTO _gj_sf_table_constraints VALUES (%s, %s, %s, %s, %s, %s)",
		sqlString(schema.DBName),
		sqlString(sfIdent(schema.Schema)),
		sqlString(name),
		sqlString(sfIdent(schema.Schema)),
		sqlString(sfIdent(t.Name)),
		sqlString(typ),
	)
}

func keyColumnSQL(schema *catalog.Schema, t *catalog.Table, name, col string, ordinal, uniquePos int) string {
	return fmt.Sprintf(
		"INSERT INTO _gj_sf_key_column_usage VALUES (%s, %s, %s, %s, %s, %s, %d, %d)",
		sqlString(schema.DBName),
		sqlString(sfIdent(schema.Schema)),
		sqlString(name),
		sqlString(sfIdent(schema.Schema)),
		sqlString(sfIdent(t.Name)),
		sqlString(sfIdent(col)),
		ordinal,
		uniquePos,
	)
}

func referencedConstraintName(t *catalog.Table) string {
	if t == nil {
		return ""
	}
	if len(t.PrimaryKeys) != 0 {
		return constraintName(t.Name, "PK")
	}
	return constraintName(t.Name, "PK")
}

func referencedPosition(t *catalog.Table, col string) int {
	if t == nil {
		return 1
	}
	for i, pk := range t.PrimaryKeys {
		if strings.EqualFold(pk, col) {
			return i + 1
		}
	}
	return 1
}

func constraintName(name, suffix string) string {
	return sfIdent(name) + "_" + suffix
}

func discoveryMode(schema *catalog.Schema) string {
	if schema != nil && schema.DiscoveryMode == "show" {
		return "show"
	}
	return "information_schema"
}

func clusteringKey(t *catalog.Table) string {
	if len(t.ClusteringKeys) == 0 {
		return ""
	}
	keys := make([]string, len(t.ClusteringKeys))
	for i, key := range t.ClusteringKeys {
		keys[i] = sfIdent(key)
	}
	return "LINEAR(" + strings.Join(keys, ", ") + ")"
}

func sfIdent(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(catalog.TrimIdent(s))
}

func sfDataType(c *catalog.Column) string {
	typ := c.Type
	if typ == "" {
		typ = c.DDLType
	}
	typ = strings.ToUpper(strings.TrimSpace(typ))
	switch {
	case strings.HasPrefix(typ, "NUMBER"):
		return "NUMBER"
	case typ == "TEXT":
		return "VARCHAR"
	default:
		return typ
	}
}

func sfShowDataType(c *catalog.Column, nullable bool) string {
	typ := sfDataType(c)
	switch {
	case strings.HasPrefix(typ, "NUMBER"), strings.HasPrefix(typ, "DECIMAL"), typ == "BIGINT", typ == "INTEGER":
		typ = "FIXED"
	case typ == "FLOAT", typ == "DOUBLE", typ == "REAL":
		typ = "REAL"
	case typ == "VARCHAR", typ == "TEXT", strings.HasPrefix(typ, "VARCHAR("):
		typ = "TEXT"
	}
	return fmt.Sprintf(`{"type":"%s","nullable":%t}`, typ, nullable)
}

func sqlString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func hostedNormalize(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}

func rewriteDiscoveryTables(sql string) string {
	repls := []struct{ from, to string }{
		{"information_schema.columns", "_gj_sf_columns"},
		{"information_schema.key_column_usage", "_gj_sf_key_column_usage"},
		{"information_schema.table_constraints", "_gj_sf_table_constraints"},
		{"information_schema.referential_constraints", "_gj_sf_referential_constraints"},
		{"information_schema.tables", "_gj_sf_tables"},
		{"information_schema.views", "_gj_sf_information_schema_views"},
		{"INFORMATION_SCHEMA.COLUMNS", "_gj_sf_columns"},
		{"INFORMATION_SCHEMA.KEY_COLUMN_USAGE", "_gj_sf_key_column_usage"},
		{"INFORMATION_SCHEMA.TABLE_CONSTRAINTS", "_gj_sf_table_constraints"},
		{"INFORMATION_SCHEMA.REFERENTIAL_CONSTRAINTS", "_gj_sf_referential_constraints"},
		{"INFORMATION_SCHEMA.TABLES", "_gj_sf_tables"},
		{"INFORMATION_SCHEMA.VIEWS", "_gj_sf_information_schema_views"},
	}
	out := sql
	for _, repl := range repls {
		out = replaceFoldAll(out, repl.from, repl.to)
	}
	return out
}

func rewriteCurrentContext(sql string) string {
	out := replaceFunction(sql, "CURRENT_SCHEMA", func(string) string {
		return "(SELECT schema_name FROM _gj_sf_session LIMIT 1)"
	})
	out = replaceFunction(out, "CURRENT_DATABASE", func(string) string {
		return "(SELECT db_name FROM _gj_sf_session LIMIT 1)"
	})
	return out
}

func rewriteResultScan(sql string) string {
	for {
		idx := indexFold(sql, "TABLE(RESULT_SCAN")
		if idx < 0 {
			return sql
		}
		tableOpen := indexByteAfter(sql, '(', idx)
		if tableOpen < 0 {
			return sql
		}
		tableClose := catalog.FindMatchingParen(sql, tableOpen)
		if tableClose < 0 {
			return sql
		}
		sql = sql[:idx] + "(SELECT * FROM _gj_sf_result_scan WHERE query_id = ?)" + sql[tableClose+1:]
	}
}

func replaceFoldAll(sql, from, to string) string {
	start := 0
	for {
		idx := indexFoldFrom(sql, from, start)
		if idx < 0 {
			return sql
		}
		sql = sql[:idx] + to + sql[idx+len(from):]
		start = idx + len(to)
	}
}

func showResultScanStatements(kind string) []string {
	base := []string{
		"UPDATE _gj_sf_session SET query_seq = query_seq + 1",
		"UPDATE _gj_sf_session SET last_query_id = 'QID_' || CAST(query_seq AS VARCHAR)",
		"DELETE FROM _gj_sf_result_scan WHERE query_id = (SELECT last_query_id FROM _gj_sf_session LIMIT 1)",
	}
	var insert string
	switch kind {
	case "columns":
		insert = `INSERT INTO _gj_sf_result_scan (query_id, scan_type, schema_name, table_name, column_name, data_type, key_sequence)
SELECT (SELECT last_query_id FROM _gj_sf_session LIMIT 1), 'columns', table_schema, table_name, column_name, snowflake_show_data_type, ordinal_position
FROM _gj_sf_columns
WHERE UPPER(table_schema) = UPPER((SELECT schema_name FROM _gj_sf_session LIMIT 1))
ORDER BY table_schema, table_name, ordinal_position`
	case "pks":
		insert = `INSERT INTO _gj_sf_result_scan (query_id, scan_type, schema_name, table_name, column_name, key_sequence)
SELECT (SELECT last_query_id FROM _gj_sf_session LIMIT 1), 'pks', k.table_schema, k.table_name, k.column_name, k.ordinal_position
FROM _gj_sf_key_column_usage k
JOIN _gj_sf_table_constraints c
  ON c.constraint_schema = k.constraint_schema
 AND c.constraint_name = k.constraint_name
 AND c.table_schema = k.table_schema
 AND c.table_name = k.table_name
WHERE c.constraint_type = 'PRIMARY KEY'
  AND UPPER(k.table_schema) = UPPER((SELECT schema_name FROM _gj_sf_session LIMIT 1))
ORDER BY k.table_schema, k.table_name, k.ordinal_position`
	case "uks":
		insert = `INSERT INTO _gj_sf_result_scan (query_id, scan_type, schema_name, table_name, column_name, key_sequence)
SELECT (SELECT last_query_id FROM _gj_sf_session LIMIT 1), 'uks', k.table_schema, k.table_name, k.column_name, k.ordinal_position
FROM _gj_sf_key_column_usage k
JOIN _gj_sf_table_constraints c
  ON c.constraint_schema = k.constraint_schema
 AND c.constraint_name = k.constraint_name
 AND c.table_schema = k.table_schema
 AND c.table_name = k.table_name
WHERE c.constraint_type IN ('UNIQUE', 'PRIMARY KEY')
  AND UPPER(k.table_schema) = UPPER((SELECT schema_name FROM _gj_sf_session LIMIT 1))
ORDER BY k.table_schema, k.table_name, k.ordinal_position`
	case "fks":
		insert = `INSERT INTO _gj_sf_result_scan (query_id, scan_type, fk_schema_name, fk_table_name, fk_column_name, pk_schema_name, pk_table_name, pk_column_name, fk_name, key_sequence)
SELECT (SELECT last_query_id FROM _gj_sf_session LIMIT 1), 'fks',
       fk.table_schema, fk.table_name, fk.column_name,
       pk.table_schema, pk.table_name, pk.column_name,
       fk.constraint_name, fk.ordinal_position
FROM _gj_sf_referential_constraints r
JOIN _gj_sf_key_column_usage fk
  ON fk.constraint_schema = r.constraint_schema AND fk.constraint_name = r.constraint_name
JOIN _gj_sf_key_column_usage pk
  ON pk.constraint_schema = r.unique_constraint_schema
 AND pk.constraint_name = r.unique_constraint_name
 AND pk.ordinal_position = fk.position_in_unique_constraint
WHERE UPPER(fk.table_schema) = UPPER((SELECT schema_name FROM _gj_sf_session LIMIT 1))
ORDER BY fk.table_schema, fk.table_name, fk.constraint_name, fk.ordinal_position`
	case "tables":
		insert = `INSERT INTO _gj_sf_result_scan (query_id, scan_type, schema_name, name, cluster_by, "rows")
SELECT (SELECT last_query_id FROM _gj_sf_session LIMIT 1), 'tables', table_schema, table_name, clustering_key, row_count
FROM _gj_sf_tables
WHERE table_type = 'BASE TABLE'
  AND UPPER(table_schema) = UPPER((SELECT schema_name FROM _gj_sf_session LIMIT 1))
ORDER BY table_schema, table_name`
	default:
		insert = `INSERT INTO _gj_sf_result_scan (query_id, scan_type)
SELECT (SELECT last_query_id FROM _gj_sf_session LIMIT 1), 'unknown'`
	}
	return append(base, insert)
}

func MapType(t string) string {
	raw := strings.TrimSpace(t)
	upper := strings.ToUpper(raw)
	switch {
	case upper == "TIMESTAMP_NTZ" || upper == "TIMESTAMP_LTZ" || upper == "TIMESTAMP_TZ":
		return "TIMESTAMP"
	case upper == "VARIANT" || upper == "ARRAY":
		return "JSON"
	case strings.HasPrefix(upper, "NUMBER("):
		return "DECIMAL" + raw[len("NUMBER"):]
	case strings.HasPrefix(upper, "NUMBER"):
		return "DECIMAL"
	case strings.HasPrefix(upper, "DECIMAL"):
		return upper
	case upper == "BOOLEAN" || upper == "BIGINT" || upper == "INTEGER" || upper == "VARCHAR":
		return upper
	default:
		return upper
	}
}

func translateDefault(def string) string {
	def = strings.TrimSpace(def)
	if def == "" {
		return ""
	}
	switch strings.ToUpper(def) {
	case "CURRENT_TIMESTAMP":
		return "CURRENT_TIMESTAMP"
	case "TRUE", "FALSE", "NULL":
		return strings.ToUpper(def)
	default:
		return lowerSnowflake(def)
	}
}

func NormalizeIdentifier(s string) string {
	return strings.ToLower(catalog.TrimIdent(s))
}

func translateSetupInsert(sql string) string {
	out := lowerSnowflake(sql)
	out = replaceGenerator(out)
	out = replaceSeq4(out)
	return out
}

func lowerSnowflake(sql string) string {
	out := strings.TrimSpace(sql)
	out = strings.ReplaceAll(out, `"json"FROM`, `"json" FROM`)
	out = strings.ReplaceAll(out, `"PUBLIC".`, "")
	out = replaceFunctionName(out, "OBJECT_CONSTRUCT_KEEP_NULL", "json_object")
	out = replaceFunctionName(out, "OBJECT_CONSTRUCT", "json_object")
	out = replaceFunction(out, "GET", translateGet)
	out = replaceFunctionName(out, "ARRAY_AGG", "json_group_array")
	out = replaceFunctionName(out, "ANY_VALUE", "any_value")
	out = replaceFunction(out, "ARRAY_CONSTRUCT", func(arg string) string {
		if strings.TrimSpace(arg) == "" {
			return "CAST('[]' AS JSON)"
		}
		return "json_array(" + arg + ")"
	})
	out = replaceFunction(out, "ARRAY_SLICE", func(arg string) string {
		parts := catalog.SplitTopLevel(arg, ',')
		if len(parts) == 0 {
			return arg
		}
		return strings.TrimSpace(parts[0])
	})
	out = replaceFunction(out, "TO_VARCHAR", func(arg string) string {
		return "CAST(" + arg + " AS VARCHAR)"
	})
	out = replaceFunction(out, "PARSE_JSON", func(arg string) string {
		return "CAST(" + arg + " AS JSON)"
	})
	out = replaceFunction(out, "TRY_PARSE_JSON", func(arg string) string {
		return "CAST(" + arg + " AS JSON)"
	})
	out = translateFlatten(out)
	out = replaceFunction(out, "GET_PATH", translateGetPath)
	out = replaceFunction(out, "ARRAYS_OVERLAP", translateArraysOverlap)
	out = replaceFunction(out, "REGEXP_INSTR", translateRegexpInstr)
	out = translateListAgg(out)
	out = translateJSONPathCasts(out)
	out = translateSnowflakeCasts(out)
	out = translateCastTypeNames(out)
	out = translateFlattenIndexRefs(out)
	out = translateTempIDComparisons(out)
	out = lowerSnowflakeIdentifiers(out)
	return out
}

func parseReadySQL(sql string) string {
	return strings.ReplaceAll(sql, `"json"FROM`, `"json" FROM`)
}

func replaceGenerator(sql string) string {
	for {
		idx := indexFold(sql, "FROM TABLE(GENERATOR")
		if idx < 0 {
			return sql
		}
		tableOpen := indexByteAfter(sql, '(', idx)
		if tableOpen < 0 {
			return sql
		}
		tableClose := catalog.FindMatchingParen(sql, tableOpen)
		if tableClose < 0 {
			return sql
		}
		inside := sql[tableOpen+1 : tableClose]
		rowCount, ok := generatorRowCount(inside)
		if !ok {
			return sql
		}
		sql = sql[:idx] + "FROM range(" + rowCount + ") AS _gj_gen(i)" + sql[tableClose+1:]
	}
}

func translateFlatten(sql string) string {
	for {
		idx := indexFold(sql, "LATERAL FLATTEN")
		if idx < 0 {
			return sql
		}
		fnIdx := indexFunctionFrom(sql, "FLATTEN", idx)
		if fnIdx < 0 {
			return sql
		}
		open := fnIdx + len("FLATTEN")
		close := catalog.FindMatchingParen(sql, open)
		if close < 0 {
			return sql
		}
		arg := strings.TrimSpace(sql[open+1 : close])
		expr := flattenInputExpr(arg)
		if expr == "" {
			return sql
		}
		alias, aliasEnd := readFlattenAlias(sql, close+1)
		repl := "json_each(" + expr + ")"
		if alias != "" {
			repl += " AS " + alias
		}
		sql = sql[:idx] + repl + sql[aliasEnd:]
	}
}

func flattenInputExpr(arg string) string {
	upper := strings.ToUpper(arg)
	idx := strings.Index(upper, "INPUT")
	if idx < 0 {
		return strings.TrimSpace(arg)
	}
	rest := strings.TrimSpace(arg[idx+len("INPUT"):])
	if !strings.HasPrefix(rest, "=>") {
		return ""
	}
	return strings.TrimSpace(rest[len("=>"):])
}

func readFlattenAlias(sql string, start int) (string, int) {
	i := start
	for i < len(sql) && isSpace(sql[i]) {
		i++
	}
	if hasFoldPrefix(sql[i:], "AS") {
		j := i + len("AS")
		if j >= len(sql) || !isIdentByte(sql[j]) {
			i = j
			for i < len(sql) && isSpace(sql[i]) {
				i++
			}
		}
	}
	if i >= len(sql) {
		return "", i
	}
	if sql[i] == '"' {
		end := i + 1
		for end < len(sql) && sql[end] != '"' {
			end++
		}
		if end >= len(sql) {
			return "", i
		}
		return sql[i : end+1], end + 1
	}
	if !isIdentByte(sql[i]) {
		return "", i
	}
	end := i
	for end < len(sql) && isIdentByte(sql[end]) {
		end++
	}
	word := sql[i:end]
	switch strings.ToUpper(word) {
	case "WHERE", "GROUP", "ORDER", "HAVING", "LIMIT", "JOIN", "ON", "LEFT", "RIGHT", "INNER", "FULL", "CROSS":
		return "", i
	default:
		return word, end
	}
}

func generatorRowCount(sql string) (string, bool) {
	upper := strings.ToUpper(sql)
	idx := strings.Index(upper, "ROWCOUNT")
	if idx < 0 {
		return "", false
	}
	rest := strings.TrimSpace(sql[idx+len("ROWCOUNT"):])
	if !strings.HasPrefix(rest, "=>") {
		return "", false
	}
	rest = strings.TrimSpace(rest[len("=>"):])
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 {
		return "", false
	}
	return rest[:end], true
}

func replaceSeq4(sql string) string {
	for {
		idx := indexFold(sql, "SEQ4(")
		if idx < 0 {
			return sql
		}
		open := indexByteAfter(sql, '(', idx)
		if open < 0 {
			return sql
		}
		close := catalog.FindMatchingParen(sql, open)
		if close < 0 || strings.TrimSpace(sql[open+1:close]) != "" {
			return sql
		}
		sql = sql[:idx] + "i" + sql[close+1:]
	}
}

func translateListAgg(sql string) string {
	for {
		idx := indexFunction(sql, "LISTAGG")
		if idx < 0 {
			return sql
		}
		open := idx + len("LISTAGG")
		close := catalog.FindMatchingParen(sql, open)
		if close < 0 {
			return sql
		}
		after := strings.TrimLeft(sql[close+1:], " \n\r\t")
		if !strings.HasPrefix(strings.ToUpper(after), "WITHIN GROUP") {
			return sql
		}
		groupStart := len(sql[close+1:]) - len(after) + close + 1
		groupOpen := indexByteAfter(sql, '(', groupStart)
		if groupOpen < 0 {
			return sql
		}
		groupClose := catalog.FindMatchingParen(sql, groupOpen)
		if groupClose < 0 {
			return sql
		}
		args := catalog.SplitTopLevel(sql[open+1:close], ',')
		if len(args) != 2 {
			return sql
		}
		order := strings.TrimSpace(sql[groupOpen+1 : groupClose])
		if strings.HasPrefix(strings.ToUpper(order), "ORDER BY ") {
			order = strings.TrimSpace(order[len("ORDER BY "):])
		}
		repl := "string_agg(" + strings.TrimSpace(args[0]) + ", " + strings.TrimSpace(args[1]) + " ORDER BY " + order + ")"
		sql = sql[:idx] + repl + sql[groupClose+1:]
	}
}

func translateGetPath(arg string) string {
	parts := catalog.SplitTopLevel(arg, ',')
	if len(parts) < 2 {
		return "json_extract(" + arg + ")"
	}
	base := strings.TrimSpace(parts[0])
	path := strings.TrimSpace(parts[1])
	path = strings.Trim(path, "'")
	if !strings.HasPrefix(path, "$") {
		path = "$." + strings.TrimPrefix(path, ".")
	}
	return "json_extract(" + base + ", '" + path + "')"
}

func translateGet(arg string) string {
	parts := catalog.SplitTopLevel(arg, ',')
	if len(parts) != 2 {
		return "GET(" + arg + ")"
	}
	target := strings.TrimSpace(parts[0])
	index := strings.TrimSpace(parts[1])
	if idx := indexFunction(target, "ARRAY_AGG"); idx == 0 {
		open := idx + len("ARRAY_AGG")
		close := catalog.FindMatchingParen(target, open)
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

func translateArraysOverlap(arg string) string {
	parts := catalog.SplitTopLevel(arg, ',')
	if len(parts) != 2 {
		return "ARRAYS_OVERLAP(" + arg + ")"
	}
	left := strings.TrimSpace(parts[0])
	right := strings.TrimSpace(parts[1])
	return "EXISTS (SELECT 1 FROM json_each(" + left + ") AS _gj_ao_l JOIN json_each(" + right + ") AS _gj_ao_r ON CAST(_gj_ao_l.value AS VARCHAR) = CAST(_gj_ao_r.value AS VARCHAR))"
}

func translateRegexpInstr(arg string) string {
	parts := catalog.SplitTopLevel(arg, ',')
	if len(parts) < 2 {
		return "REGEXP_INSTR(" + arg + ")"
	}
	flags := ""
	if len(parts) >= 6 {
		flags = ", " + strings.TrimSpace(parts[5])
	}
	return "(CASE WHEN regexp_matches(" + strings.TrimSpace(parts[0]) + ", " + strings.TrimSpace(parts[1]) + flags + ") THEN 1 ELSE 0 END)"
}

func translateJSONPathCasts(sql string) string {
	start := 0
	for {
		castIdx := nextCastOperatorFrom(sql, start)
		if castIdx < 0 {
			return sql
		}
		colon := jsonPathColon(sql, castIdx)
		if colon < 0 {
			start = castIdx + len("::")
			continue
		}
		typeStart := castIdx + len("::")
		typeEnd := typeEnd(sql, typeStart)
		mapped, ok := mapCastType(sql[typeStart:typeEnd])
		if !ok {
			start = typeEnd
			continue
		}
		baseStart := jsonBaseStart(sql, colon)
		if baseStart < 0 {
			start = typeEnd
			continue
		}
		base := strings.TrimSpace(sql[baseStart:colon])
		path := strings.Trim(strings.TrimSpace(sql[colon+1:castIdx]), `"`)
		if path == "" {
			start = typeEnd
			continue
		}
		var repl string
		if mapped == "VARCHAR" {
			repl = "json_extract_string(" + base + ", '$." + path + "')"
		} else {
			repl = "CAST(json_extract(" + base + ", '$." + path + "') AS " + mapped + ")"
		}
		sql = sql[:baseStart] + repl + sql[typeEnd:]
		start = baseStart + len(repl)
	}
}

func translateSnowflakeCasts(sql string) string {
	start := 0
	for {
		idx := nextCastOperatorFrom(sql, start)
		if idx < 0 {
			return sql
		}
		typeStart := idx + len("::")
		typeEnd := typeEnd(sql, typeStart)
		if typeEnd == typeStart {
			start = typeStart
			continue
		}
		castType := sql[typeStart:typeEnd]
		mapped, ok := mapCastType(castType)
		if !ok {
			start = typeEnd
			continue
		}
		exprStart := castExprStart(sql, idx)
		if exprStart < 0 {
			start = typeEnd
			continue
		}
		expr := strings.TrimSpace(sql[exprStart:idx])
		var repl string
		if mapped == "VARCHAR" && strings.HasPrefix(strings.ToLower(expr), "json_extract(") {
			repl = "json_extract_string" + expr[len("json_extract"):]
		} else {
			repl = "CAST(" + expr + " AS " + mapped + ")"
		}
		sql = sql[:exprStart] + repl + sql[typeEnd:]
		start = exprStart + len(repl)
	}
}

func jsonPathColon(sql string, castIdx int) int {
	i := castIdx - 1
	for i >= 0 && isSpace(sql[i]) {
		i--
	}
	for i >= 0 && (isIdentByte(sql[i]) || sql[i] == '"') {
		i--
	}
	if i < 0 || sql[i] != ':' {
		return -1
	}
	if i > 0 && sql[i-1] == ':' {
		return -1
	}
	return i
}

func jsonBaseStart(sql string, colon int) int {
	i := colon - 1
	for i >= 0 && isSpace(sql[i]) {
		i--
	}
	if i < 0 {
		return -1
	}
	start := castExprStart(sql, colon)
	if start < 0 {
		return -1
	}
	if sql[i] == ')' {
		open := matchingOpenParen(sql, i)
		if open >= 0 {
			for j := open - 1; j >= 0 && isIdentByte(sql[j]); j-- {
				start = j
			}
		}
	}
	return start
}

func mapCastType(t string) (string, bool) {
	switch strings.ToUpper(strings.TrimSpace(t)) {
	case "VARIANT":
		return "JSON", true
	case "TEXT":
		return "VARCHAR", true
	case "TIMESTAMP_NTZ":
		return "TIMESTAMP", true
	case "NUMBER":
		return "DECIMAL", true
	case "BIGINT", "BOOLEAN", "INTEGER", "FLOAT", "VARCHAR":
		return strings.ToUpper(t), true
	default:
		return "", false
	}
}

func translateCastTypeNames(sql string) string {
	replacements := []struct {
		from string
		to   string
	}{
		{"TIMESTAMP_NTZ", "TIMESTAMP"},
		{"TIMESTAMP_LTZ", "TIMESTAMP"},
		{"TIMESTAMP_TZ", "TIMESTAMP"},
		{"NUMBER", "DECIMAL"},
		{"TEXT", "VARCHAR"},
	}
	for _, repl := range replacements {
		sql = replaceFoldTokenAfterAS(sql, repl.from, repl.to)
	}
	return sql
}

func replaceFoldTokenAfterAS(sql, from, to string) string {
	start := 0
	for {
		idx := indexFoldFrom(sql, " AS "+from, start)
		if idx < 0 {
			return sql
		}
		typeStart := idx + len(" AS ")
		typeStop := typeStart + len(from)
		if typeStop < len(sql) && isIdentByte(sql[typeStop]) {
			start = typeStop
			continue
		}
		sql = sql[:typeStart] + to + sql[typeStop:]
		start = typeStart + len(to)
	}
}

func translateFlattenIndexRefs(sql string) string {
	start := 0
	for {
		idx := indexFoldFrom(sql, ".index", start)
		if idx < 0 {
			return sql
		}
		aliasStart := idx - 1
		for aliasStart >= 0 && (isIdentByte(sql[aliasStart]) || sql[aliasStart] == '.') {
			aliasStart--
		}
		aliasStart++
		if aliasStart >= idx {
			start = idx + len(".index")
			continue
		}
		alias := sql[aliasStart:idx]
		repl := "CAST(" + alias + ".key AS BIGINT)"
		sql = sql[:aliasStart] + repl + sql[idx+len(".index"):]
		start = aliasStart + len(repl)
	}
}

func translateTempIDComparisons(sql string) string {
	start := 0
	for {
		idx := indexFoldFrom(sql, ") IN (SELECT id FROM _gj_ids", start)
		if idx < 0 {
			return sql
		}
		open := matchingOpenParen(sql, idx)
		if open < 0 {
			start = idx + 1
			continue
		}
		expr := strings.TrimSpace(sql[open+1 : idx])
		if strings.HasPrefix(strings.ToUpper(expr), "CAST(") {
			start = idx + 1
			continue
		}
		repl := "(CAST(" + expr + " AS VARCHAR))"
		sql = sql[:open] + repl + sql[idx+1:]
		start = open + len(repl)
	}
}

func lowerSnowflakeIdentifiers(sql string) string {
	var b strings.Builder
	inSingle := false
	for i := 0; i < len(sql); i++ {
		if sql[i] == '\'' {
			b.WriteByte(sql[i])
			if inSingle && i+1 < len(sql) && sql[i+1] == '\'' {
				i++
				b.WriteByte(sql[i])
				continue
			}
			inSingle = !inSingle
			continue
		}
		if inSingle {
			b.WriteByte(sql[i])
			continue
		}
		if sql[i] != '"' {
			b.WriteByte(sql[i])
			continue
		}
		end := i + 1
		for end < len(sql) {
			if sql[end] == '"' {
				break
			}
			end++
		}
		if end >= len(sql) {
			b.WriteByte(sql[i])
			continue
		}
		name := sql[i+1 : end]
		if isUpperIdentifier(name) {
			b.WriteString(NormalizeIdentifier(name))
		} else {
			b.WriteString(sql[i : end+1])
		}
		i = end
	}
	return b.String()
}

func isUpperIdentifier(name string) bool {
	if name == "" {
		return false
	}
	hasLetter := false
	for i := 0; i < len(name); i++ {
		ch := name[i]
		switch {
		case ch >= 'A' && ch <= 'Z':
			hasLetter = true
		case ch >= '0' && ch <= '9', ch == '_':
		default:
			return false
		}
	}
	return hasLetter
}

func replaceFunctionName(sql, source, target string) string {
	start := 0
	for {
		idx := indexFunctionFrom(sql, source, start)
		if idx < 0 {
			return sql
		}
		sql = sql[:idx] + target + sql[idx+len(source):]
		start = idx + len(target)
	}
}

func replaceFunction(sql, name string, repl func(string) string) string {
	for {
		idx := indexFunction(sql, name)
		if idx < 0 {
			return sql
		}
		open := idx + len(name)
		close := catalog.FindMatchingParen(sql, open)
		if close < 0 {
			return sql
		}
		arg := sql[open+1 : close]
		sql = sql[:idx] + repl(arg) + sql[close+1:]
	}
}

func indexFunction(sql, name string) int {
	return indexFunctionFrom(sql, name, 0)
}

func indexFunctionFrom(sql, name string, start int) int {
	upperName := strings.ToUpper(name)
	upper := strings.ToUpper(sql)
	needle := upperName + "("
	for {
		idx := strings.Index(upper[start:], needle)
		if idx < 0 {
			return -1
		}
		idx += start
		if idx == 0 || !isIdentByte(sql[idx-1]) {
			return idx
		}
		start = idx + len(needle)
	}
}

func indexFold(sql, needle string) int {
	return indexFoldFrom(sql, needle, 0)
}

func indexFoldFrom(sql, needle string, start int) int {
	if start >= len(sql) {
		return -1
	}
	idx := strings.Index(strings.ToUpper(sql[start:]), strings.ToUpper(needle))
	if idx < 0 {
		return -1
	}
	return start + idx
}

func hasFoldPrefix(sql, prefix string) bool {
	if len(sql) < len(prefix) {
		return false
	}
	return strings.EqualFold(sql[:len(prefix)], prefix)
}

func indexByteAfter(sql string, b byte, start int) int {
	for i := start; i < len(sql); i++ {
		if sql[i] == b {
			return i
		}
	}
	return -1
}

func nextCastOperator(sql string) int {
	return nextCastOperatorFrom(sql, 0)
}

func nextCastOperatorFrom(sql string, start int) int {
	inSingle := false
	inDouble := false
	for i := start; i < len(sql)-1; i++ {
		switch sql[i] {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case ':':
			if !inSingle && !inDouble && sql[i+1] == ':' {
				return i
			}
		}
	}
	return -1
}

func typeEnd(sql string, start int) int {
	i := start
	for i < len(sql) && isIdentByte(sql[i]) {
		i++
	}
	return i
}

func castExprStart(sql string, castIdx int) int {
	i := castIdx - 1
	for i >= 0 && isSpace(sql[i]) {
		i--
	}
	if i < 0 {
		return -1
	}
	switch sql[i] {
	case '\'':
		return singleQuotedStart(sql, i)
	case '"':
		return doubleQuotedStart(sql, i)
	case ')':
		open := matchingOpenParen(sql, i)
		if open < 0 {
			return -1
		}
		return functionCallStart(sql, open)
	default:
		for i >= 0 && (isIdentByte(sql[i]) || sql[i] == '.' || sql[i] == '?') {
			i--
		}
		return i + 1
	}
}

func functionCallStart(sql string, open int) int {
	i := open - 1
	for i >= 0 && isSpace(sql[i]) {
		i--
	}
	if i < 0 || !isIdentByte(sql[i]) {
		return open
	}
	for i >= 0 && (isIdentByte(sql[i]) || sql[i] == '.') {
		i--
	}
	return i + 1
}

func singleQuotedStart(sql string, end int) int {
	for i := end - 1; i >= 0; i-- {
		if sql[i] == '\'' {
			if i > 0 && sql[i-1] == '\'' {
				i--
				continue
			}
			return i
		}
	}
	return -1
}

func doubleQuotedStart(sql string, end int) int {
	for i := end - 1; i >= 0; i-- {
		if sql[i] == '"' {
			return i
		}
	}
	return -1
}

func matchingOpenParen(sql string, close int) int {
	depth := 0
	inSingle := false
	inDouble := false
	for i := close; i >= 0; i-- {
		ch := sql[i]
		switch ch {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case ')':
			if !inSingle && !inDouble {
				depth++
			}
		case '(':
			if !inSingle && !inDouble {
				depth--
				if depth == 0 {
					return i
				}
			}
		}
	}
	return -1
}

func isIdentByte(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_'
}

func isSpace(ch byte) bool {
	return ch == ' ' || ch == '\n' || ch == '\r' || ch == '\t'
}
