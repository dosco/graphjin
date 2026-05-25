package serv

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	core "github.com/dosco/graphjin/core/v3"
	corediscovery "github.com/dosco/graphjin/core/v3/discovery"
)

const tableSampleQueryTimeout = 10 * time.Second

func (dm *DiscoveryManager) TableSample(ctx context.Context, database, schemaName, table string) (*TableSampleResult, error) {
	schema, resolvedDB, err := dm.resolveSampleSchema(database, schemaName, table)
	if err != nil {
		return nil, err
	}
	if schemaName == "" {
		schemaName = schema.Schema
	}

	key := profileCacheKey(resolvedDB, schemaName, table)
	if v, ok := dm.profileCache.Load(key); ok {
		result := *(v.(*TableSampleResult))
		result.Cost.Cache = "hit"
		return &result, nil
	}

	v, err, _ := dm.profileGroup.Do(key, func() (any, error) {
		if cached, ok := dm.profileCache.Load(key); ok {
			return cached, nil
		}
		profile, err := buildTableSample(ctx, dm.gj, resolvedDB, schema)
		if err != nil {
			return nil, err
		}
		outgoing := relationshipRefs(schema.Relationships.Outgoing)
		analyticsMode := dm.gj != nil && dm.gj.EffectiveAnalyticsMode(resolvedDB)
		result := &TableSampleResult{
			Database:         resolvedDB,
			Schema:           schema.Schema,
			Table:            schema.Name,
			Status:           "ready",
			Stats:            profile,
			PrimaryKeys:      primaryKeysFromSchema(schema),
			ForeignKeys:      foreignKeysFromSchema(schema),
			OutgoingRels:     outgoing,
			IncomingRels:     relationshipRefs(schema.Relationships.Incoming),
			FKDisambiguation: detectFKDisambiguation(outgoing),
			Indexes:          loadIndexes(ctx, dm.gj, resolvedDB, schema),
			Aggregations:     aggregationInfoForSchema(schema),
			ExampleQueries:   generateExampleQueries(schema),
			Cost: DiscoveryCost{
				UsesLiveQueries: true,
				Scope:           "single_table",
				Cache:           "miss",
			},
		}
		if analyticsMode {
			result.AnalyticsModeRules = analyticsModeRules()
			if isFactShapedTable(schema, profile) {
				result.AggregationHint = "This looks like a fact table (high row count, multiple outgoing FKs). To aggregate by a dimension, root your query at the dimension table (small side) and nest down to here, not the other way around. distinct: dedupes; it does not bucket."
			}
			result.TemporalFilterWarning = temporalFilterWarning(schema)
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

func profileCacheKey(database, schemaName, table string) string {
	return database + ":" + schemaName + ":" + table
}

func buildTableSample(ctx context.Context, gj *core.GraphJin, database string, schema *core.TableSchema) (*TableProfile, error) {
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
	if n, ok := corediscovery.ApproxRowCount(qctx, db, dbtype, schema); ok {
		v := n
		profile.RowCountApprox = &v
	}
	profile.ColumnStats = loadColumnStats(qctx, gj, database, schema)

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

	if profile.RowCountApprox != nil {
		fillMostCommonCountsFromFractions(profile.ColumnStats, *profile.RowCountApprox)
	}

	rows, _ := directSampleRows(qctx, db, dbtype, schema, sampleCols)
	profile.SampleRows = rows
	return profile, nil
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
	colList := strings.Join(names, ", ")
	tableRef := qualifiedTable(dbtype, schema)

	q := buildSampleQuery(dbtype, colList, tableRef, 5)
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		// Fallback for views and backends that reject TABLESAMPLE/random().
		fallback := fmt.Sprintf("SELECT %s FROM %s %s", colList, tableRef, limitClause(dbtype, 5))
		if strings.Contains(strings.ToLower(dbtype), "mssql") {
			fallback = fmt.Sprintf("SELECT TOP 5 %s FROM %s", colList, tableRef)
		}
		rows, err = db.QueryContext(ctx, fallback)
		if err != nil {
			return nil, false
		}
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

// buildSampleQuery picks a randomized-sample SQL per dialect.
func buildSampleQuery(dbtype, colList, tableRef string, limit int) string {
	dt := strings.ToLower(dbtype)
	switch dt {
	case "postgres", "postgresql", "cockroachdb", "cockroach":
		// BERNOULLI is row-level; LIMIT short-circuits the scan.
		return fmt.Sprintf("SELECT %s FROM %s TABLESAMPLE BERNOULLI (1) LIMIT %d", colList, tableRef, limit)
	case "mssql":
		return fmt.Sprintf("SELECT TOP %d %s FROM %s TABLESAMPLE (1 PERCENT)", limit, colList, tableRef)
	case "mysql", "mariadb":
		return fmt.Sprintf("SELECT %s FROM %s ORDER BY RAND() LIMIT %d", colList, tableRef, limit)
	case "sqlite":
		return fmt.Sprintf("SELECT %s FROM %s ORDER BY RANDOM() LIMIT %d", colList, tableRef, limit)
	case "oracle":
		return fmt.Sprintf("SELECT * FROM (SELECT %s FROM %s SAMPLE(1) ORDER BY DBMS_RANDOM.VALUE) WHERE ROWNUM <= %d",
			colList, tableRef, limit)
	case "snowflake":
		return fmt.Sprintf("SELECT %s FROM %s SAMPLE (%d ROWS)", colList, tableRef, limit)
	default:
		return fmt.Sprintf("SELECT %s FROM %s %s", colList, tableRef, limitClause(dbtype, limit))
	}
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

func primaryKeysFromSchema(schema *core.TableSchema) []string {
	if len(schema.PrimaryKeys) > 0 {
		out := make([]string, len(schema.PrimaryKeys))
		copy(out, schema.PrimaryKeys)
		return out
	}
	if schema.PrimaryKey != "" {
		return []string{schema.PrimaryKey}
	}
	return nil
}

func foreignKeysFromSchema(schema *core.TableSchema) []ForeignKeyRef {
	var fks []ForeignKeyRef
	for _, col := range schema.Columns {
		if col.ForeignKey != "" {
			fks = append(fks, ForeignKeyRef{
				Column:   col.Name,
				Target:   col.ForeignKey,
				Database: col.ForeignKeyDatabase,
			})
		}
	}
	return fks
}

func relationshipRefs(rels []core.RelationInfo) []RelationshipRef {
	if len(rels) == 0 {
		return nil
	}
	out := make([]RelationshipRef, 0, len(rels))
	for _, r := range rels {
		out = append(out, RelationshipRef{
			Table:  r.Table,
			Column: r.ForeignKey,
			Type:   r.Type,
		})
	}
	return out
}

// temporalFilterWarning is the per-table paste-ready warning surfaced when analytics_mode requires a date filter.
func temporalFilterWarning(schema *core.TableSchema) string {
	if schema == nil {
		return ""
	}
	col := schema.PartitionKey
	kind := "partition"
	if col == "" && schema.ImplicitPartitionKey != "" {
		col = schema.ImplicitPartitionKey
		kind = "temporal"
	}
	if col == "" {
		return ""
	}
	return fmt.Sprintf(
		"Analytics mode requires a filter on %s column %q. Add one of:\n  where: { %s: { gte: \"<date>\" } }\n  (unrestricted: true)  # full-scan override",
		kind, col, col)
}

// isFactShapedTable returns true when the table looks like an analytics fact
// table — many outgoing FKs (the dimension keys) and a high approximate row
// count. Used to attach an aggregation hint.
func isFactShapedTable(schema *core.TableSchema, profile *TableProfile) bool {
	if schema == nil {
		return false
	}
	if len(schema.Relationships.Outgoing) < 2 {
		return false
	}
	if profile == nil || profile.RowCountApprox == nil {
		return false
	}
	return *profile.RowCountApprox >= 10000
}

// detectFKDisambiguation flags multi-FK targets: when this table has more
// than one outgoing relationship to the same target table, the compiler
// cannot pick a foreign key without an explicit @through(column:) hint.
func detectFKDisambiguation(outgoing []RelationshipRef) []FKDisambiguationEntry {
	groups := map[string][]RelationshipRef{}
	order := []string{}
	for _, r := range outgoing {
		if r.Table == "" || r.Column == "" {
			continue
		}
		if _, seen := groups[r.Table]; !seen {
			order = append(order, r.Table)
		}
		groups[r.Table] = append(groups[r.Table], r)
	}

	var out []FKDisambiguationEntry
	for _, target := range order {
		rels := groups[target]
		if len(rels) < 2 {
			continue
		}
		entry := FKDisambiguationEntry{
			Target:     target,
			Ambiguous:  true,
			SyntaxHint: `add @through(column: "<fk_column>") on the nested selection`,
		}
		for _, r := range rels {
			entry.Candidates = append(entry.Candidates, FKDisambiguationCandidate{
				Column:  r.Column,
				Snippet: fmt.Sprintf(`%s @through(column: %q) { ... }`, target, r.Column),
			})
		}
		out = append(out, entry)
	}
	return out
}

func aggregationInfoForSchema(schema *core.TableSchema) *AggregationInfo {
	if schema == nil || len(schema.Columns) == 0 {
		return nil
	}
	a := generateAggregations(schema)
	return &a
}
