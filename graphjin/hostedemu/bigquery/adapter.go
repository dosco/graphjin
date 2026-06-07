package bigquery

import (
	"database/sql/driver"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/dosco/graphjin/hostedemu"
	"github.com/dosco/graphjin/hostedemu/snowflake/catalog"
)

type Adapter struct{}

func NewAdapter() Adapter {
	return Adapter{}
}

func (Adapter) Name() string {
	return "bigquery"
}

func (Adapter) DefaultSeedPath() string {
	return "bigquery.sql"
}

func (Adapter) ParseSeed(seedSQL string) (any, error) {
	return catalog.ParseSeedBytes([]byte(seedSQL))
}

func (Adapter) NewSession(c any) hostedemu.Session {
	schema, _ := c.(*catalog.Schema)
	return &Session{schema: schema}
}

func (Adapter) TranslateSetup(seedSQL string, c any) ([]string, error) {
	schema, ok := c.(*catalog.Schema)
	if !ok || schema == nil {
		return nil, fmt.Errorf("bigquery emulator: invalid catalog %T", c)
	}
	var out []string
	for _, t := range schema.Tables {
		if t.IsView {
			continue
		}
		out = append(out, createTableSQL(t))
	}
	for _, stmt := range catalog.SplitStatements(catalog.StripSQLComments(seedSQL)) {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		upper := strings.ToUpper(stmt)
		switch {
		case strings.HasPrefix(upper, "CREATE VIEW "):
			out = append(out, lowerBigQuery(stmt))
		case strings.HasPrefix(upper, "INSERT "):
			out = append(out, translateSetupInsert(stmt))
		}
	}
	out = append(out, metadataCreateSQL(schema)...)
	out = append(out, TranslateMetadataRefresh(schema)...)
	return out, nil
}

func (Adapter) TranslateDiscoveryQuery(sql string, args []driver.NamedValue, c any) (string, []driver.NamedValue, error) {
	schema, _ := c.(*catalog.Schema)
	return TranslateDiscoveryQuery(sql, args, schema)
}

func (Adapter) TranslateDiscoveryExec(sql string, args []driver.NamedValue, c any) ([]string, []driver.NamedValue, error) {
	translated, translatedArgs, err := TranslateDiscoveryQuery(sql, args, c.(*catalog.Schema))
	return []string{translated}, translatedArgs, err
}

func (Adapter) TranslateMetadataRefresh(c any) ([]string, error) {
	schema, ok := c.(*catalog.Schema)
	if !ok || schema == nil {
		return nil, fmt.Errorf("bigquery emulator: invalid catalog %T", c)
	}
	return TranslateMetadataRefresh(schema), nil
}

func (Adapter) TranslateRuntime(sql string, args []driver.NamedValue, c any) (string, []driver.NamedValue, error) {
	return lowerBigQuery(sql), args, nil
}

func (Adapter) TranslateDirect(sql string, args []driver.NamedValue, c any) (string, []driver.NamedValue, error) {
	return lowerBigQueryDirect(sql), args, nil
}

func (Adapter) NormalizeIdentifier(identifier string) string {
	return strings.ToLower(catalog.TrimIdent(identifier))
}

func (Adapter) MapType(sourceType string) string {
	return MapType(sourceType)
}

func (Adapter) ClassifyPhase(sql string) string {
	upper := strings.ToUpper(hostedemu.NormalizeSQL(sql))
	switch {
	case strings.Contains(upper, "INFORMATION_SCHEMA") ||
		strings.Contains(upper, "@@DATASET_ID") ||
		strings.Contains(upper, "@@PROJECT_ID"):
		return "discovery"
	case isRuntimeSQL(upper):
		return "runtime"
	case strings.HasPrefix(upper, "SELECT") || strings.HasPrefix(upper, "WITH") ||
		strings.HasPrefix(upper, "INSERT") || strings.HasPrefix(upper, "UPDATE") ||
		strings.HasPrefix(upper, "DELETE") || strings.HasPrefix(upper, "CREATE") ||
		strings.HasPrefix(upper, "DROP") || strings.HasPrefix(upper, "ALTER"):
		return "direct"
	default:
		return "unknown"
	}
}

type Session struct {
	schema *catalog.Schema
	mu     sync.Mutex
}

func (s *Session) ApplyDDL(sql string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.schema != nil {
		s.schema.ApplyDDL(sql)
	}
}

func (s *Session) PlaceholderQuery(sql string) (*hostedemu.Rows, string, error) {
	norm := hostedemu.NormalizeSQL(sql)
	upper := strings.ToUpper(norm)
	switch {
	case isSessionInfoQuery(upper):
		return s.placeholderSessionInfo()
	case isTableRowCountQuery(upper):
		return s.placeholderTableRowCounts(norm)
	case isConstraintPreflightQuery(upper):
		return s.placeholderConstraintCount()
	case isPrimaryKeyDiscoveryQuery(upper):
		return s.placeholderPrimaryKeyRows()
	case isForeignKeyDiscoveryQuery(upper):
		return s.placeholderForeignKeyRows()
	case isCompositeFKDiscoveryQuery(upper):
		return s.placeholderCompositeFKRows()
	case isColumnDiscoveryQuery(upper):
		return s.placeholderColumnRows()
	case isCountQuery(upper):
		return hostedemu.NewRows([]string{"COUNT"}, []driver.Value{int64(100)}), "rows:1", nil
	case isRuntimeSQL(upper):
		return hostedemu.NewRows([]string{"JSON"}, []driver.Value{[]byte(`{"id":1}`)}), "rows:1", nil
	case strings.HasPrefix(upper, "SELECT"):
		cols := selectColumns(norm)
		vals := make([]driver.Value, len(cols))
		for i, col := range cols {
			vals[i] = valueForColumn(col)
		}
		return hostedemu.NewRows(cols, vals), "rows:1", nil
	default:
		return hostedemu.NewRows([]string{"JSON"}, []driver.Value{[]byte(`{"id":1}`)}), "rows:1", nil
	}
}

func isSessionInfoQuery(upper string) bool {
	return (strings.Contains(upper, "@@DATASET_ID") || strings.Contains(upper, "@@PROJECT_ID")) &&
		!strings.Contains(upper, "INFORMATION_SCHEMA")
}

func isColumnDiscoveryQuery(upper string) bool {
	return strings.Contains(upper, "INFORMATION_SCHEMA.COLUMNS")
}

func isConstraintPreflightQuery(upper string) bool {
	return strings.Contains(upper, "INFORMATION_SCHEMA.TABLE_CONSTRAINTS") &&
		strings.Contains(upper, "COUNT(")
}

func isPrimaryKeyDiscoveryQuery(upper string) bool {
	return strings.Contains(upper, "INFORMATION_SCHEMA.KEY_COLUMN_USAGE") &&
		strings.Contains(upper, "INFORMATION_SCHEMA.TABLE_CONSTRAINTS") &&
		strings.Contains(upper, "PRIMARY KEY") &&
		strings.Contains(upper, "AS PRIMARY_KEY")
}

func isForeignKeyDiscoveryQuery(upper string) bool {
	return strings.Contains(upper, "INFORMATION_SCHEMA.CONSTRAINT_COLUMN_USAGE") &&
		strings.Contains(upper, "FOREIGNKEY_COLUMN")
}

func isTableRowCountQuery(upper string) bool {
	return (strings.Contains(upper, "INFORMATION_SCHEMA.TABLES") && strings.Contains(upper, "ROW_COUNT")) ||
		(strings.Contains(upper, "INFORMATION_SCHEMA.TABLE_STORAGE") && strings.Contains(upper, "TOTAL_ROWS"))
}

func isCompositeFKDiscoveryQuery(upper string) bool {
	return strings.Contains(upper, "INFORMATION_SCHEMA.KEY_COLUMN_USAGE") &&
		strings.Contains(upper, "STRING_AGG(") &&
		strings.Contains(upper, "HAVING COUNT(*) > 1")
}

func isCountQuery(upper string) bool {
	return strings.HasPrefix(upper, "SELECT COUNT(") || strings.HasPrefix(upper, "SELECT COUNT(*)")
}

func isRuntimeSQL(upper string) bool {
	return strings.Contains(upper, "JSON_OBJECT") ||
		strings.Contains(upper, "ARRAY_AGG") ||
		strings.Contains(upper, "_GJ_IDS") ||
		strings.Contains(upper, "PARSE_JSON(")
}

func (s *Session) placeholderSessionInfo() (*hostedemu.Rows, string, error) {
	dbName := "DB"
	if s.schema != nil && s.schema.DBName != "" {
		dbName = s.schema.DBName
	}
	return hostedemu.NewRows(
		[]string{"db_version", "db_schema", "db_name"},
		[]driver.Value{int64(0), "", dbName},
	), "rows:1", nil
}

func (s *Session) placeholderTableRowCounts(sql string) (*hostedemu.Rows, string, error) {
	cols := selectColumns(sql)
	if len(cols) == 1 && strings.EqualFold(cols[0], "row_count") {
		return hostedemu.NewRows(cols, []driver.Value{int64(100)}), "rows:1", nil
	}
	if len(cols) == 0 || (len(cols) == 1 && strings.EqualFold(cols[0], "json")) {
		cols = []string{"table_name", "row_count"}
	}
	if s.schema == nil {
		return hostedemu.NewRows(cols), "rows:0", nil
	}

	var vals [][]driver.Value
	for _, t := range s.schema.Tables {
		if t.IsView {
			continue
		}
		row := make([]driver.Value, len(cols))
		for i, col := range cols {
			switch strings.ToLower(catalog.TrimIdent(col)) {
			case "table_name":
				row[i] = bqIdent(t.Name)
			case "row_count", "total_rows":
				row[i] = int64(100)
			default:
				row[i] = valueForColumn(col)
			}
		}
		vals = append(vals, row)
	}
	return hostedemu.NewRows(cols, vals...), fmt.Sprintf("rows:%d", len(vals)), nil
}

func (s *Session) placeholderConstraintCount() (*hostedemu.Rows, string, error) {
	var n int64
	if s.schema != nil {
		for _, t := range s.schema.Tables {
			n += int64(len(t.PrimaryKeys))
		}
		n += int64(len(foreignKeyGroups(s.schema)))
	}
	return hostedemu.NewRows([]string{"constraint_count"}, []driver.Value{n}), "rows:1", nil
}

func (s *Session) placeholderColumnRows() (*hostedemu.Rows, string, error) {
	cols := []string{
		"schema_name",
		"table_name",
		"column_name",
		"col_type",
		"not_null",
		"primary_key",
		"unique_key",
		"is_array",
		"full_text",
		"foreignkey_schema",
		"foreignkey_table",
		"foreignkey_column",
	}
	if s.schema == nil {
		return hostedemu.NewRows(cols), "rows:0", nil
	}

	var vals [][]driver.Value
	for _, t := range s.schema.Tables {
		for _, c := range t.Columns {
			vals = append(vals, []driver.Value{
				"",
				bqIdent(t.Name),
				bqIdent(c.Name),
				strings.ToLower(bqDataType(c)),
				c.NotNull || c.PrimaryKey,
				false,
				false,
				c.Array,
				false,
				"",
				"",
				"",
			})
		}
	}
	return hostedemu.NewRows(cols, vals...), fmt.Sprintf("rows:%d", len(vals)), nil
}

func (s *Session) placeholderPrimaryKeyRows() (*hostedemu.Rows, string, error) {
	cols := discoveryColumnNames()
	if s.schema == nil {
		return hostedemu.NewRows(cols), "rows:0", nil
	}

	var vals [][]driver.Value
	for _, t := range s.schema.Tables {
		for _, pk := range t.PrimaryKeys {
			vals = append(vals, []driver.Value{
				"",
				bqIdent(t.Name),
				bqIdent(pk),
				"",
				false,
				true,
				false,
				false,
				false,
				"",
				"",
				"",
			})
		}
	}
	return hostedemu.NewRows(cols, vals...), fmt.Sprintf("rows:%d", len(vals)), nil
}

func (s *Session) placeholderForeignKeyRows() (*hostedemu.Rows, string, error) {
	cols := discoveryColumnNames()
	if s.schema == nil {
		return hostedemu.NewRows(cols), "rows:0", nil
	}

	var vals [][]driver.Value
	for _, t := range s.schema.Tables {
		for _, c := range t.Columns {
			if c.FKeyTable == "" {
				continue
			}
			fkCol := c.FKeyColumn
			if fkCol == "" {
				fkCol = "id"
			}
			vals = append(vals, []driver.Value{
				"",
				bqIdent(t.Name),
				bqIdent(c.Name),
				"",
				false,
				false,
				false,
				false,
				false,
				"",
				bqIdent(c.FKeyTable),
				bqIdent(fkCol),
			})
		}
	}
	return hostedemu.NewRows(cols, vals...), fmt.Sprintf("rows:%d", len(vals)), nil
}

func discoveryColumnNames() []string {
	return []string{
		"schema_name",
		"table_name",
		"column_name",
		"col_type",
		"not_null",
		"primary_key",
		"unique_key",
		"is_array",
		"full_text",
		"foreignkey_schema",
		"foreignkey_table",
		"foreignkey_column",
	}
}

func (s *Session) placeholderCompositeFKRows() (*hostedemu.Rows, string, error) {
	cols := []string{
		"table_schema",
		"table_name",
		"constraint_name",
		"local_columns",
		"fkey_schema",
		"fkey_table",
		"fkey_columns",
	}
	if s.schema == nil {
		return hostedemu.NewRows(cols), "rows:0", nil
	}

	var vals [][]driver.Value
	for _, fk := range foreignKeyGroups(s.schema) {
		if len(fk.columns) < 2 {
			continue
		}
		localCols := make([]string, 0, len(fk.columns))
		fkeyCols := make([]string, 0, len(fk.columns))
		for _, col := range fk.columns {
			localCols = append(localCols, bqIdent(col.Name))
			fkeyCol := col.FKeyColumn
			if fkeyCol == "" {
				fkeyCol = "id"
			}
			fkeyCols = append(fkeyCols, bqIdent(fkeyCol))
		}
		fkeyTable := ""
		if fk.refTable != nil {
			fkeyTable = bqIdent(fk.refTable.Name)
		}
		vals = append(vals, []driver.Value{
			"",
			bqIdent(fk.table.Name),
			fk.name,
			strings.Join(localCols, ","),
			"",
			fkeyTable,
			strings.Join(fkeyCols, ","),
		})
	}
	return hostedemu.NewRows(cols, vals...), fmt.Sprintf("rows:%d", len(vals)), nil
}

func TranslateDiscoveryQuery(sql string, args []driver.NamedValue, schema *catalog.Schema) (string, []driver.NamedValue, error) {
	norm := strings.TrimSpace(catalog.StripSQLComments(sql))
	upper := strings.ToUpper(hostedNormalize(norm))
	if isSessionInfoQuery(upper) {
		return "SELECT 0 AS db_version, dataset_id AS db_schema, project_id AS db_name FROM _gj_bq_session LIMIT 1", args, nil
	}
	out := rewriteDiscoveryTables(norm)
	out = rewriteSessionVariables(out, schema)
	return lowerBigQuery(out), args, nil
}

func rewriteSessionVariables(sql string, schema *catalog.Schema) string {
	projectID := ""
	if schema != nil {
		projectID = schema.DBName
	}
	sql = regexp.MustCompile(`(?i)COALESCE\(@@dataset_id,\s*''\)`).ReplaceAllString(sql, sqlString(""))
	sql = regexp.MustCompile(`(?i)COALESCE\(@@project_id,\s*''\)`).ReplaceAllString(sql, sqlString(projectID))
	return sql
}

func TranslateMetadataRefresh(schema *catalog.Schema) []string {
	if schema == nil {
		return nil
	}
	var out []string
	out = append(out,
		"UPDATE _gj_bq_session SET project_id = "+sqlString(schema.DBName)+", dataset_id = ''",
		"DELETE FROM _gj_bq_tables",
		"DELETE FROM _gj_bq_columns",
		"DELETE FROM _gj_bq_table_constraints",
		"DELETE FROM _gj_bq_key_column_usage",
		"DELETE FROM _gj_bq_constraint_column_usage",
		"DELETE FROM _gj_fk_metadata",
	)
	out = append(out, metadataTableRows(schema)...)
	out = append(out, metadataConstraintRows(schema)...)
	return out
}

func createTableSQL(t *catalog.Table) string {
	cols := make([]string, 0, len(t.Columns)+1)
	for _, c := range t.Columns {
		colType := c.DDLType
		if colType == "" {
			colType = c.Type
		}
		col := NormalizeIdentifier(c.Name) + " " + MapType(colType)
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
		"CREATE TABLE _gj_bq_session (project_id VARCHAR, dataset_id VARCHAR)",
		"INSERT INTO _gj_bq_session VALUES (" + sqlString(schema.DBName) + ", '')",
		"CREATE TABLE _gj_bq_tables (table_catalog VARCHAR, table_schema VARCHAR, table_name VARCHAR, table_type VARCHAR, row_count BIGINT)",
		"CREATE TABLE _gj_bq_columns (table_catalog VARCHAR, table_schema VARCHAR, table_name VARCHAR, column_name VARCHAR, ordinal_position BIGINT, data_type VARCHAR, is_nullable VARCHAR, is_array BOOLEAN)",
		"CREATE TABLE _gj_bq_table_constraints (constraint_catalog VARCHAR, constraint_schema VARCHAR, constraint_name VARCHAR, table_schema VARCHAR, table_name VARCHAR, constraint_type VARCHAR)",
		"CREATE TABLE _gj_bq_key_column_usage (constraint_catalog VARCHAR, constraint_schema VARCHAR, constraint_name VARCHAR, table_schema VARCHAR, table_name VARCHAR, column_name VARCHAR, ordinal_position BIGINT, position_in_unique_constraint BIGINT)",
		"CREATE TABLE _gj_bq_constraint_column_usage (table_catalog VARCHAR, table_schema VARCHAR, table_name VARCHAR, column_name VARCHAR, constraint_catalog VARCHAR, constraint_schema VARCHAR, constraint_name VARCHAR)",
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
			"INSERT INTO _gj_bq_tables VALUES (%s, '', %s, %s, %d)",
			sqlString(schema.DBName),
			sqlString(bqIdent(t.Name)),
			sqlString(tableType),
			rowCount,
		))
		for i, c := range t.Columns {
			nullable := "YES"
			if c.NotNull || c.PrimaryKey {
				nullable = "NO"
			}
			out = append(out, fmt.Sprintf(
				"INSERT INTO _gj_bq_columns VALUES (%s, '', %s, %s, %d, %s, %s, %t)",
				sqlString(schema.DBName),
				sqlString(bqIdent(t.Name)),
				sqlString(bqIdent(c.Name)),
				i+1,
				sqlString(bqDataType(c)),
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
				out = append(out, keyColumnSQL(schema, t, name, col, i+1, 0))
			}
		}
	}
	for _, fk := range foreignKeyGroups(schema) {
		out = append(out, tableConstraintSQL(schema, fk.table, fk.name, "FOREIGN KEY"))
		for i, col := range fk.columns {
			pos := referencedPosition(fk.refTable, col.FKeyColumn)
			out = append(out, keyColumnSQL(schema, fk.table, fk.name, col.Name, i+1, pos))
			out = append(out, constraintColumnUsageSQL(schema, fk.refTable, fk.name, col.FKeyColumn))
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
	type key struct{ table, ref string }
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
				g = &fkGroup{
					table:    t,
					refTable: schema.Table(c.FKeyTable),
					name:     bqIdent(t.Name) + "_" + bqIdent(c.FKeyTable) + "_fk",
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
		"INSERT INTO _gj_bq_table_constraints VALUES (%s, '', %s, '', %s, %s)",
		sqlString(schema.DBName),
		sqlString(name),
		sqlString(bqIdent(t.Name)),
		sqlString(typ),
	)
}

func keyColumnSQL(schema *catalog.Schema, t *catalog.Table, name, col string, ordinal, uniquePos int) string {
	return fmt.Sprintf(
		"INSERT INTO _gj_bq_key_column_usage VALUES (%s, '', %s, '', %s, %s, %d, %d)",
		sqlString(schema.DBName),
		sqlString(name),
		sqlString(bqIdent(t.Name)),
		sqlString(bqIdent(col)),
		ordinal,
		uniquePos,
	)
}

func constraintColumnUsageSQL(schema *catalog.Schema, t *catalog.Table, name, col string) string {
	tableName := ""
	if t != nil {
		tableName = t.Name
	}
	return fmt.Sprintf(
		"INSERT INTO _gj_bq_constraint_column_usage VALUES (%s, '', %s, %s, %s, '', %s)",
		sqlString(schema.DBName),
		sqlString(bqIdent(tableName)),
		sqlString(bqIdent(col)),
		sqlString(schema.DBName),
		sqlString(name),
	)
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
	return bqIdent(name) + "_" + strings.ToLower(suffix)
}

func bqIdent(s string) string {
	return strings.ToLower(catalog.TrimIdent(s))
}

func bqDataType(c *catalog.Column) string {
	typ := c.Type
	if typ == "" {
		typ = c.DDLType
	}
	typ = strings.ToUpper(strings.TrimSpace(typ))
	switch {
	case strings.HasPrefix(typ, "ARRAY<"):
		return typ
	case typ == "VARCHAR" || typ == "TEXT":
		return "STRING"
	case typ == "BIGINT" || typ == "INTEGER":
		return "INT64"
	case typ == "BOOLEAN":
		return "BOOL"
	case typ == "VARIANT":
		return "JSON"
	case strings.HasPrefix(typ, "NUMBER"):
		return "NUMERIC"
	default:
		return typ
	}
}

func MapType(t string) string {
	raw := strings.TrimSpace(t)
	upper := strings.ToUpper(raw)
	switch {
	case strings.HasPrefix(upper, "ARRAY<"):
		return "JSON"
	case upper == "JSON":
		return "JSON"
	case upper == "BOOL":
		return "BOOLEAN"
	case upper == "INT64":
		return "BIGINT"
	case upper == "FLOAT64":
		return "DOUBLE"
	case upper == "NUMERIC" || upper == "BIGNUMERIC":
		return "DECIMAL"
	case upper == "STRING":
		return "VARCHAR"
	case upper == "TIMESTAMP" || upper == "DATETIME":
		return "TIMESTAMP"
	default:
		return upper
	}
}

func NormalizeIdentifier(s string) string {
	return strings.ToLower(catalog.TrimIdent(s))
}

func translateSetupInsert(sql string) string {
	out := lowerBigQuery(sql)
	out = translateGenerateArray(out)
	return out
}

func lowerBigQuery(sql string) string {
	out := strings.TrimSpace(sql)
	out = strings.ReplaceAll(out, "`", `"`)
	out = replaceFunction(out, "TO_JSON_STRING", func(arg string) string {
		return "CAST(" + arg + " AS VARCHAR)"
	})
	out = replaceFunctionName(out, "TO_JSON", "to_json")
	out = replaceFunctionName(out, "JSON_OBJECT", "json_object")
	out = replaceFunctionName(out, "JSON_ARRAY", "json_array")
	out = replaceFunctionName(out, "ARRAY_AGG", "json_group_array")
	out = replaceFunctionName(out, "ANY_VALUE", "any_value")
	out = replaceFunctionName(out, "REGEXP_CONTAINS", "regexp_matches")
	out = replaceFunction(out, "PARSE_JSON", func(arg string) string {
		return "CAST(" + arg + " AS JSON)"
	})
	out = replaceFunction(out, "SAFE_CAST", translateSafeCast)
	out = replaceFunction(out, "JSON_VALUE", translateJSONValue)
	out = replaceFunction(out, "JSON_QUERY_ARRAY", func(arg string) string {
		return "json_each(" + arg + ")"
	})
	out = translateJSONQuery(out)
	out = translateSplitSafeOffset(out)
	out = translateUnnestJSONValueArray(out)
	out = translateGenerateArray(out)
	out = translateTypeNames(out)
	out = lowerQuotedIdentifiers(out)
	return out
}

func lowerBigQueryDirect(sql string) string {
	out := lowerBigQuery(sql)
	upper := strings.ToUpper(hostedNormalize(out))
	if strings.HasPrefix(upper, "CREATE TABLE ") ||
		strings.HasPrefix(upper, "CREATE OR REPLACE TABLE ") ||
		strings.HasPrefix(upper, "ALTER TABLE ") ||
		strings.HasPrefix(upper, "DROP TABLE ") {
		return lowerBigQueryDDL(out)
	}
	return out
}

func lowerBigQueryDDL(sql string) string {
	out := replaceFoldAll(sql, " NOT ENFORCED", "")
	out = replaceBigQueryDDLType(out, "INT64", "BIGINT")
	out = replaceBigQueryDDLType(out, "FLOAT64", "DOUBLE")
	out = replaceBigQueryDDLType(out, "NUMERIC", "DECIMAL")
	out = replaceBigQueryDDLType(out, "BIGNUMERIC", "DECIMAL")
	out = replaceBigQueryDDLType(out, "STRING", "VARCHAR")
	out = replaceBigQueryDDLType(out, "BOOL", "BOOLEAN")
	out = replaceBigQueryDDLType(out, "BYTES", "BLOB")
	out = replaceBigQueryDDLType(out, "GEOGRAPHY", "VARCHAR")
	out = regexp.MustCompile(`(?is)\bARRAY\s*<[^>]+>`).ReplaceAllString(out, "JSON")
	out = regexp.MustCompile(`(?is)\s+CLUSTER\s+BY\s+[^;]+`).ReplaceAllString(out, "")
	return out
}

func replaceBigQueryDDLType(sql, from, to string) string {
	re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(from) + `\b`)
	return re.ReplaceAllString(sql, to)
}

func translateSafeCast(arg string) string {
	parts := splitAs(arg)
	if len(parts) != 2 {
		return "CAST(" + arg + ")"
	}
	return "TRY_CAST(" + parts[0] + " AS " + mapBigQueryRuntimeType(parts[1]) + ")"
}

func translateJSONValue(arg string) string {
	parts := catalog.SplitTopLevel(arg, ',')
	if len(parts) < 2 {
		return "json_extract_string(" + arg + ")"
	}
	return "json_extract_string(" + strings.TrimSpace(parts[0]) + ", " + strings.TrimSpace(parts[1]) + ")"
}

func translateJSONQuery(sql string) string {
	return replaceFunction(sql, "JSON_QUERY", func(arg string) string {
		parts := catalog.SplitTopLevel(arg, ',')
		if len(parts) < 2 {
			return "json_extract(" + arg + ")"
		}
		return "json_extract(" + strings.TrimSpace(parts[0]) + ", " + strings.TrimSpace(parts[1]) + ")"
	})
}

func translateSplitSafeOffset(sql string) string {
	re := regexp.MustCompile(`(?is)SPLIT\(([^,]+),\s*'([^']*)'\)\[SAFE_OFFSET\((\d+)\)\]`)
	return re.ReplaceAllStringFunc(sql, func(m string) string {
		parts := re.FindStringSubmatch(m)
		idx, _ := strconv.Atoi(parts[3])
		return fmt.Sprintf("split_part(%s, '%s', %d)", strings.TrimSpace(parts[1]), parts[2], idx+1)
	})
}

func translateUnnestJSONValueArray(sql string) string {
	re := regexp.MustCompile(`(?is)UNNEST\(JSON_VALUE_ARRAY\(CAST\((.+?) AS JSON\)\)\) AS ([A-Za-z_][A-Za-z0-9_]*)(?: WITH OFFSET AS ([A-Za-z_][A-Za-z0-9_]*))?`)
	return re.ReplaceAllStringFunc(sql, func(m string) string {
		parts := re.FindStringSubmatch(m)
		expr := strings.TrimSpace(parts[1])
		alias := parts[2]
		offset := parts[3]
		if offset == "" {
			return fmt.Sprintf("json_each(CAST(%s AS JSON)) AS %s", expr, alias)
		}
		return fmt.Sprintf("json_each(CAST(%s AS JSON)) AS _gj_each(key, %s) CROSS JOIN (SELECT CAST(_gj_each.key AS BIGINT) AS %s)", expr, alias, offset)
	})
}

func translateGenerateArray(sql string) string {
	re := regexp.MustCompile(`(?is)UNNEST\(GENERATE_ARRAY\((\d+),\s*(\d+)\)\) AS ([A-Za-z_][A-Za-z0-9_]*)`)
	return re.ReplaceAllStringFunc(sql, func(m string) string {
		parts := re.FindStringSubmatch(m)
		start, _ := strconv.Atoi(parts[1])
		end, _ := strconv.Atoi(parts[2])
		return fmt.Sprintf("range(%d, %d) AS _gj_range(%s)", start, end+1, parts[3])
	})
}

func translateTypeNames(sql string) string {
	repls := []struct{ from, to string }{
		{" AS INT64", " AS BIGINT"},
		{" AS FLOAT64", " AS DOUBLE"},
		{" AS NUMERIC", " AS DECIMAL"},
		{" AS STRING", " AS VARCHAR"},
		{" AS BOOL", " AS BOOLEAN"},
	}
	for _, repl := range repls {
		sql = replaceFoldAll(sql, repl.from, repl.to)
	}
	return sql
}

func mapBigQueryRuntimeType(t string) string {
	return MapType(strings.TrimSpace(t))
}

func splitAs(s string) []string {
	upper := strings.ToUpper(s)
	depth := 0
	inSingle := false
	for i := 0; i <= len(s)-4; i++ {
		switch s[i] {
		case '\'':
			inSingle = !inSingle
		case '(':
			if !inSingle {
				depth++
			}
		case ')':
			if !inSingle && depth > 0 {
				depth--
			}
		}
		if inSingle || depth != 0 {
			continue
		}
		if strings.HasPrefix(upper[i:], " AS ") {
			return []string{strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+4:])}
		}
	}
	return nil
}

func rewriteDiscoveryTables(sql string) string {
	repls := []struct{ from, to string }{
		{"information_schema.table_storage", "_gj_bq_tables"},
		{"information_schema.columns", "_gj_bq_columns"},
		{"information_schema.key_column_usage", "_gj_bq_key_column_usage"},
		{"information_schema.table_constraints", "_gj_bq_table_constraints"},
		{"information_schema.constraint_column_usage", "_gj_bq_constraint_column_usage"},
		{"information_schema.tables", "_gj_bq_tables"},
		{"INFORMATION_SCHEMA.TABLE_STORAGE", "_gj_bq_tables"},
		{"INFORMATION_SCHEMA.COLUMNS", "_gj_bq_columns"},
		{"INFORMATION_SCHEMA.KEY_COLUMN_USAGE", "_gj_bq_key_column_usage"},
		{"INFORMATION_SCHEMA.TABLE_CONSTRAINTS", "_gj_bq_table_constraints"},
		{"INFORMATION_SCHEMA.CONSTRAINT_COLUMN_USAGE", "_gj_bq_constraint_column_usage"},
		{"INFORMATION_SCHEMA.TABLES", "_gj_bq_tables"},
	}
	out := sql
	for _, repl := range repls {
		out = replaceFoldAll(out, repl.from, repl.to)
	}
	out = replaceFoldAll(out, "total_rows", "row_count")
	out = replaceFoldAll(out, "TOTAL_ROWS", "ROW_COUNT")
	return out
}

func replaceFunctionName(sql, from, to string) string {
	return replaceFoldAll(sql, from+"(", to+"(")
}

func replaceFunction(sql, name string, fn func(string) string) string {
	for {
		idx := indexFold(sql, name+"(")
		if idx < 0 {
			return sql
		}
		open := idx + len(name)
		close := catalog.FindMatchingParen(sql, open)
		if close < 0 {
			return sql
		}
		sql = sql[:idx] + fn(sql[open+1:close]) + sql[close+1:]
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

func indexFold(sql, needle string) int {
	return indexFoldFrom(sql, needle, 0)
}

func indexFoldFrom(sql, needle string, start int) int {
	upper := strings.ToUpper(sql)
	needle = strings.ToUpper(needle)
	if start < 0 {
		start = 0
	}
	idx := strings.Index(upper[start:], needle)
	if idx < 0 {
		return -1
	}
	return start + idx
}

func lowerQuotedIdentifiers(sql string) string {
	var b strings.Builder
	inSingle := false
	inDouble := false
	for i := 0; i < len(sql); i++ {
		ch := sql[i]
		switch ch {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
			b.WriteByte(ch)
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
			b.WriteByte(ch)
		default:
			if inDouble && ch >= 'A' && ch <= 'Z' {
				b.WriteByte(ch + ('a' - 'A'))
			} else {
				b.WriteByte(ch)
			}
		}
	}
	return b.String()
}

func sqlString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func hostedNormalize(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}

func selectColumns(sql string) []string {
	if !strings.HasPrefix(strings.ToUpper(sql), "SELECT ") {
		return []string{"JSON"}
	}
	from := strings.Index(strings.ToUpper(sql), " FROM ")
	if from < 0 {
		return []string{"JSON"}
	}
	items := catalog.SplitTopLevel(sql[len("SELECT "):from], ',')
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
		return catalog.TrimIdent(fields[len(fields)-1])
	}
	if len(fields) != 0 {
		expr = fields[len(fields)-1]
	}
	if dot := strings.LastIndexByte(expr, '.'); dot >= 0 {
		expr = expr[dot+1:]
	}
	return catalog.TrimIdent(expr)
}

func valueForColumn(col string) driver.Value {
	switch strings.ToLower(catalog.TrimIdent(col)) {
	case "count", "count(*)", "row_count", "product_id":
		return int64(1)
	case "id", "variant_id", "order_id", "customer_id", "owner_id", "subject_id", "quantity":
		return int64(2)
	case "variant_name":
		return "Medium"
	case "sku":
		return "PROD1-M"
	case "price", "amount":
		return float64(24.99)
	case "full_name":
		return "User 1"
	case "email":
		return "user1@test.com"
	case "name":
		return "Product 1"
	case "description":
		return "Description"
	case "country_code", "region":
		return "US"
	case "disabled":
		return false
	case "created_at", "updated_at", "event_time", "returned_at":
		return "2021-01-09 16:37:01"
	case "tags", "category_ids", "metadata", "payload", "category_counts", "validity_period":
		return []byte(`[]`)
	default:
		return "mock"
	}
}
