package serv

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	core "github.com/dosco/graphjin/core/v3"
)

const tableSampleQueryTimeout = 10 * time.Second

func (dm *DiscoveryManager) TableSample(ctx context.Context, database, schemaName, table, mode string) (*TableSampleResult, error) {
	if mode == "" {
		mode = "light"
	}
	schema, resolvedDB, err := dm.resolveSampleSchema(database, schemaName, table)
	if err != nil {
		return nil, err
	}
	if schemaName == "" {
		schemaName = schema.Schema
	}

	key := profileCacheKey(resolvedDB, schemaName, table, mode)
	if v, ok := dm.profileCache.Load(key); ok {
		result := *(v.(*TableSampleResult))
		result.Cost.Cache = "hit"
		return &result, nil
	}

	v, err, _ := dm.profileGroup.Do(key, func() (any, error) {
		if cached, ok := dm.profileCache.Load(key); ok {
			return cached, nil
		}
		profile, err := buildTableSample(ctx, dm.gj, resolvedDB, schema, mode)
		if err != nil {
			return nil, err
		}
		result := &TableSampleResult{
			Database: resolvedDB,
			Schema:   schema.Schema,
			Table:    schema.Name,
			Mode:     mode,
			Status:   "ready",
			Stats:    profile,
			Cost: DiscoveryCost{
				UsesLiveQueries: true,
				Scope:           "single_table",
				Cache:           "miss",
			},
		}
		dm.profileCache.Store(key, result)
		return result, nil
	})
	if err != nil {
		return nil, err
	}

	result := *(v.(*TableSampleResult))
	if result.Cost.Cache == "" {
		result.Cost.Cache = "hit"
	}
	return &result, nil
}

func (dm *DiscoveryManager) resolveSampleSchema(database, schemaName, table string) (*core.TableSchema, string, error) {
	if database != "" {
		schema, err := dm.gj.GetTableSchemaForDatabaseSchema(database, schemaName, table)
		return schema, database, err
	}

	if schemaName != "" {
		schema, err := dm.gj.GetTableSchemaForDatabaseSchema("", schemaName, table)
		if err != nil {
			return nil, "", addTableCandidates(err, dm.gj.GetTables(), table)
		}
		resolvedDB := schema.Database
		if resolvedDB == "" {
			resolvedDB = dm.gj.DefaultDatabase()
		}
		return schema, resolvedDB, nil
	}

	schema, err := dm.gj.GetTableSchema(table)
	if err != nil {
		return nil, "", addTableCandidates(err, dm.gj.GetTables(), table)
	}
	resolvedDB := schema.Database
	if resolvedDB == "" {
		resolvedDB = dm.gj.DefaultDatabase()
	}
	return schema, resolvedDB, nil
}

func addTableCandidates(err error, tables []core.TableInfo, table string) error {
	var matches []string
	for _, t := range tables {
		if t.Name == table {
			matches = append(matches, fmt.Sprintf("%s.%s.%s", t.Database, t.Schema, t.Name))
		}
	}
	if len(matches) == 0 {
		return err
	}
	return fmt.Errorf("%w; candidates: %s", err, strings.Join(matches, ", "))
}

func profileCacheKey(database, schemaName, table, mode string) string {
	return database + ":" + schemaName + ":" + table + ":" + mode
}

func buildTableSample(ctx context.Context, gj *core.GraphJin, database string, schema *core.TableSchema, mode string) (*TableProfile, error) {
	db, dbtype, err := gj.DBForDatabase(database)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(dbtype, "mongodb") {
		return nil, fmt.Errorf("get_table_sample is not supported for mongodb yet")
	}

	qctx, cancel := context.WithTimeout(ctx, tableSampleQueryTimeout)
	defer cancel()

	profile := &TableProfile{
		DateRanges:   make(map[string]DateRange),
		EnumValues:   make(map[string]EnumProfile),
		NumericStats: make(map[string]NumericStats),
	}
	if n, ok := approxRowCount(qctx, db, dbtype, schema); ok {
		profile.RowCountApprox = n
	}

	var numericCols, dateCols, enumCols []core.ColumnInfo
	var sampleCols []core.ColumnInfo
	for _, col := range schema.Columns {
		if len(sampleCols) < 10 {
			sampleCols = append(sampleCols, col)
		}
		if isNumericType(col.Type) && !col.PrimaryKey && !strings.HasSuffix(col.Name, "_id") {
			numericCols = append(numericCols, col)
		}
		if isDateType(col.Type) {
			dateCols = append(dateCols, col)
		}
		if isEnumCandidateCol(col) {
			enumCols = append(enumCols, col)
		}
	}
	if mode != "deep" {
		numericCols = capColumns(numericCols, 5)
		dateCols = capColumns(dateCols, 3)
		enumCols = capColumns(enumCols, 3)
	}

	for _, col := range dateCols {
		if minV, maxV, ok := directMinMax(qctx, db, dbtype, schema, col); ok {
			profile.DateRanges[col.Name] = DateRange{Min: minV, Max: maxV}
		}
	}
	for _, col := range numericCols {
		if stats, ok := directNumericStats(qctx, db, dbtype, schema, col); ok {
			profile.NumericStats[col.Name] = stats
		}
	}

	groupCol := schema.PrimaryKey
	if groupCol == "" && len(schema.Columns) > 0 {
		groupCol = schema.Columns[0].Name
	}
	if groupCol != "" {
		for _, col := range enumCols {
			if prof, ok := directEnumProfile(qctx, db, dbtype, schema, col); ok {
				profile.EnumValues[col.Name] = prof
			}
		}
	}

	rows, _ := directSampleRows(qctx, db, dbtype, schema, sampleCols)
	profile.SampleRows = rows
	return profile, nil
}

func capColumns(cols []core.ColumnInfo, n int) []core.ColumnInfo {
	if len(cols) <= n {
		return cols
	}
	return cols[:n]
}

func directMinMax(ctx context.Context, db *sql.DB, dbtype string, schema *core.TableSchema, col core.ColumnInfo) (string, string, bool) {
	q := fmt.Sprintf("SELECT MIN(%s), MAX(%s) FROM %s",
		quoteIdent(dbtype, col.Name), quoteIdent(dbtype, col.Name), qualifiedTable(dbtype, schema))
	var minV, maxV any
	if err := db.QueryRowContext(ctx, q).Scan(&minV, &maxV); err != nil {
		return "", "", false
	}
	return valueString(minV), valueString(maxV), true
}

func directNumericStats(ctx context.Context, db *sql.DB, dbtype string, schema *core.TableSchema, col core.ColumnInfo) (NumericStats, bool) {
	qc := quoteIdent(dbtype, col.Name)
	q := fmt.Sprintf("SELECT MIN(%s), MAX(%s), AVG(%s), SUM(%s), COUNT(%s) FROM %s",
		qc, qc, qc, qc, qc, qualifiedTable(dbtype, schema))
	var minV, maxV, avgV, sumV any
	var count sql.NullInt64
	if err := db.QueryRowContext(ctx, q).Scan(&minV, &maxV, &avgV, &sumV, &count); err != nil {
		return NumericStats{}, false
	}
	out := NumericStats{
		Min: valueString(minV),
		Max: valueString(maxV),
		Avg: valueString(avgV),
		Sum: valueString(sumV),
	}
	if count.Valid {
		out.Count = count.Int64
	}
	return out, true
}

func directEnumProfile(ctx context.Context, db *sql.DB, dbtype string, schema *core.TableSchema, col core.ColumnInfo) (EnumProfile, bool) {
	qc := quoteIdent(dbtype, col.Name)
	q := fmt.Sprintf("SELECT %s, COUNT(*) FROM %s GROUP BY %s ORDER BY COUNT(*) DESC %s",
		qc, qualifiedTable(dbtype, schema), qc, limitClause(dbtype, enumSampleCap))
	if strings.Contains(strings.ToLower(dbtype), "mssql") {
		q = fmt.Sprintf("SELECT TOP %d %s, COUNT(*) FROM %s GROUP BY %s ORDER BY COUNT(*) DESC",
			enumSampleCap, qc, qualifiedTable(dbtype, schema), qc)
	}
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return EnumProfile{}, false
	}
	defer rows.Close()

	var prof EnumProfile
	for rows.Next() {
		var value any
		var count sql.NullInt64
		if err := rows.Scan(&value, &count); err != nil {
			return EnumProfile{}, false
		}
		prof.Values = append(prof.Values, EnumValue{Value: valueString(value), Count: count.Int64})
	}
	prof.DistinctCount = int64(len(prof.Values))
	if len(prof.Values) >= enumSampleCap {
		prof.Truncated = true
		prof.Values = prof.Values[:enumSampleCap-1]
		prof.DistinctCount = int64(len(prof.Values))
	}
	return prof, true
}

func directSampleRows(ctx context.Context, db *sql.DB, dbtype string, schema *core.TableSchema, cols []core.ColumnInfo) ([]map[string]any, bool) {
	if len(cols) == 0 {
		return nil, false
	}
	names := make([]string, 0, len(cols))
	for _, col := range cols {
		names = append(names, quoteIdent(dbtype, col.Name))
	}
	q := fmt.Sprintf("SELECT %s FROM %s %s", strings.Join(names, ", "), qualifiedTable(dbtype, schema), limitClause(dbtype, 5))
	if strings.Contains(strings.ToLower(dbtype), "mssql") {
		q = fmt.Sprintf("SELECT TOP 5 %s FROM %s", strings.Join(names, ", "), qualifiedTable(dbtype, schema))
	}
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, false
	}
	defer rows.Close()

	out := make([]map[string]any, 0, 5)
	for rows.Next() {
		values := make([]any, len(cols))
		dest := make([]any, len(cols))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, false
		}
		row := make(map[string]any, len(cols))
		for i, col := range cols {
			row[col.Name] = normalizeValue(values[i])
		}
		out = append(out, row)
	}
	return out, true
}

func qualifiedTable(dbtype string, schema *core.TableSchema) string {
	name := quoteIdent(dbtype, schema.Name)
	if schema.Schema == "" {
		return name
	}
	return quoteIdent(dbtype, schema.Schema) + "." + name
}

func quoteIdent(dbtype, ident string) string {
	if ident == "" {
		return ident
	}
	switch strings.ToLower(dbtype) {
	case "mysql", "mariadb":
		return "`" + strings.ReplaceAll(ident, "`", "``") + "`"
	case "mssql":
		return "[" + strings.ReplaceAll(ident, "]", "]]") + "]"
	case "snowflake", "oracle":
		if isPlainIdent(ident) {
			return ident
		}
		return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
	default:
		return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
	}
}

func isPlainIdent(s string) bool {
	for i, r := range s {
		if i == 0 {
			if !(r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z') {
				return false
			}
			continue
		}
		if !(r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func limitClause(dbtype string, n int) string {
	switch strings.ToLower(dbtype) {
	case "oracle":
		return fmt.Sprintf("FETCH FIRST %d ROWS ONLY", n)
	case "mssql":
		return ""
	default:
		return fmt.Sprintf("LIMIT %d", n)
	}
}

func valueString(v any) string {
	switch t := normalizeValue(v).(type) {
	case nil:
		return ""
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}

func normalizeValue(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case []byte:
		return string(t)
	case time.Time:
		return t.Format(time.RFC3339)
	case sql.NullString:
		if t.Valid {
			return t.String
		}
		return nil
	case sql.NullInt64:
		if t.Valid {
			return t.Int64
		}
		return nil
	case sql.NullFloat64:
		if t.Valid {
			return t.Float64
		}
		return nil
	case float32:
		return strconv.FormatFloat(float64(t), 'f', -1, 32)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return t
	}
}
