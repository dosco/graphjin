package serv

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	core "github.com/dosco/graphjin/core/v3"
)

const enrichmentQueryTimeout = 10 * time.Second

const enumSampleCap = 101

func buildEnrichment(ctx context.Context, gj *core.GraphJin, database string, schemas []*core.TableSchema) map[string]*TableProfile {
	result := make(map[string]*TableProfile)

	for _, schema := range schemas {
		p := &TableProfile{
			DateRanges:   make(map[string]DateRange),
			EnumValues:   make(map[string]EnumProfile),
			NumericStats: make(map[string]NumericStats),
		}

		var numericCols, dateCols, enumCols []core.ColumnInfo
		var allColNames []string
		for _, col := range schema.Columns {
			allColNames = append(allColNames, col.Name)
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

		rc := &core.RequestConfig{}
		rc.SetNamespace(database)

		if schema.PrimaryKey != "" {
			qctx, cancel := context.WithTimeout(ctx, enrichmentQueryTimeout)
			q := fmt.Sprintf("{ %s { count_%s } }", schema.Name, schema.PrimaryKey)
			if res, err := gj.GraphQL(qctx, q, nil, rc); err == nil && res.Data != nil {
				p.RowCountApprox = extractCountFromResult(res.Data, schema.Name, schema.PrimaryKey)
			}
			cancel()
		}

		for _, col := range dateCols {
			qctx, cancel := context.WithTimeout(ctx, enrichmentQueryTimeout)
			q := fmt.Sprintf("{ %s { min_%s max_%s } }", schema.Name, col.Name, col.Name)
			if res, err := gj.GraphQL(qctx, q, nil, rc); err == nil && res.Data != nil {
				minV, maxV := extractMinMaxFromResult(res.Data, schema.Name, col.Name)
				if minV != "" || maxV != "" {
					p.DateRanges[col.Name] = DateRange{Min: minV, Max: maxV}
				}
			}
			cancel()
		}

		for _, col := range enumCols {
			if schema.PrimaryKey == "" {
				continue
			}
			qctx, cancel := context.WithTimeout(ctx, enrichmentQueryTimeout)
			q := fmt.Sprintf("{ %s(distinct: [%s], limit: %d, order_by: { count_%s: desc }) { %s count_%s } }",
				schema.Name, col.Name, enumSampleCap, schema.PrimaryKey, col.Name, schema.PrimaryKey)
			if res, err := gj.GraphQL(qctx, q, nil, rc); err == nil && res.Data != nil {
				p.EnumValues[col.Name] = extractEnumProfile(res.Data, schema.Name, col.Name, schema.PrimaryKey)
			}
			cancel()
		}

		for _, col := range numericCols {
			qctx, cancel := context.WithTimeout(ctx, enrichmentQueryTimeout)
			q := fmt.Sprintf("{ %s { min_%s max_%s avg_%s sum_%s count_%s } }",
				schema.Name, col.Name, col.Name, col.Name, col.Name, col.Name)
			if res, err := gj.GraphQL(qctx, q, nil, rc); err == nil && res.Data != nil {
				p.NumericStats[col.Name] = extractNumericStats(res.Data, schema.Name, col.Name)
			}
			cancel()
		}

		sampleCols := allColNames
		if len(sampleCols) > 10 {
			sampleCols = sampleCols[:10]
		}
		orderClause := ""
		if len(dateCols) > 0 {
			orderClause = fmt.Sprintf(", order_by: { %s: desc }", dateCols[0].Name)
		}
		qctx, cancel := context.WithTimeout(ctx, enrichmentQueryTimeout)
		q := fmt.Sprintf("{ %s(limit: 5%s) { %s } }", schema.Name, orderClause, strings.Join(sampleCols, " "))
		if res, err := gj.GraphQL(qctx, q, nil, rc); err == nil && res.Data != nil {
			p.SampleRows = extractRowsFromResult(res.Data, schema.Name)
		}
		cancel()

		result[schema.Name] = p
	}

	return result
}

func buildTableIndex(schemas []*core.TableSchema, enrichment map[string]*TableProfile) []TableIndexEntry {
	duplicateSchemas := buildDuplicateIndex(schemas)
	entries := make([]TableIndexEntry, 0, len(schemas))
	for _, s := range schemas {
		entries = append(entries, buildTableIndexEntry(s, enrichment[s.Name], duplicateSchemas))
	}
	return entries
}

func buildTableIndexEntry(schema *core.TableSchema, profile *TableProfile, duplicateSchemas map[string][]string) TableIndexEntry {
	entry := TableIndexEntry{
		Name:     schema.Name,
		Schema:   schema.Schema,
		Database: schema.Database,
		Type:     schema.Type,
		Comment:  schema.Comment,
	}
	if entry.Type == "" {
		entry.Type = "table"
	}
	if profile != nil {
		entry.RowCountApprox = profile.RowCountApprox
	}
	if len(schema.PrimaryKeys) > 0 {
		entry.PrimaryKeys = append([]string{}, schema.PrimaryKeys...)
	} else if schema.PrimaryKey != "" {
		entry.PrimaryKeys = []string{schema.PrimaryKey}
	}

	var keyCols []string
	typeSummary := map[string]int{}
	for _, col := range schema.Columns {
		if col.ForeignKey != "" {
			entry.ForeignKeys = append(entry.ForeignKeys, ForeignKeyRef{
				Column:   col.Name,
				Target:   col.ForeignKey,
				Database: col.ForeignKeyDatabase,
			})
		}
		if !col.PrimaryKey && col.ForeignKey == "" {
			if isNumericType(col.Type) {
				typeSummary["numeric"]++
			} else if isDateType(col.Type) {
				typeSummary["date"]++
			} else if col.FullText {
				typeSummary["fulltext"]++
			} else if col.Type == "text" || strings.HasPrefix(col.Type, "character") || strings.HasPrefix(col.Type, "varchar") || strings.HasPrefix(col.Type, "nvarchar") {
				typeSummary["text"]++
			} else {
				typeSummary["other"]++
			}
			if isDateType(col.Type) || isEnumCandidateCol(col) {
				keyCols = append(keyCols, col.Name)
			}
		}
	}
	if len(keyCols) > 0 {
		entry.KeyColumns = keyCols
	}
	if len(typeSummary) > 0 {
		entry.ColumnTypeSummary = typeSummary
	}

	joinTargets := map[string]struct{}{}
	for _, rel := range schema.Relationships.Outgoing {
		joinTargets[rel.Table] = struct{}{}
	}
	for _, rel := range schema.Relationships.Incoming {
		joinTargets[rel.Table] = struct{}{}
	}
	if len(joinTargets) > 0 {
		for t := range joinTargets {
			entry.JoinTargets = append(entry.JoinTargets, t)
		}
		sort.Strings(entry.JoinTargets)
	}

	if other, ok := duplicateSchemas[schema.Name]; ok {
		entry.DuplicateIn = append([]string{}, other...)
	}

	return entry
}

func buildTableDetails(schemas []*core.TableSchema, enrichment map[string]*TableProfile) []TableDetailEntry {
	duplicateSchemas := buildDuplicateIndex(schemas)
	out := make([]TableDetailEntry, 0, len(schemas))
	for _, s := range schemas {
		prof := enrichment[s.Name]
		entry := TableDetailEntry{
			TableIndexEntry: buildTableIndexEntry(s, prof, duplicateSchemas),
			Schema:          s,
			Profile:         prof,
		}
		out = append(out, entry)
	}
	return out
}

func buildDatabaseOverview(database string, schemas []*core.TableSchema, enrichment map[string]*TableProfile, functions []core.FunctionInfo) DatabaseOverview {
	overview := DatabaseOverview{
		Database:    database,
		TotalTables: len(schemas),
		Functions:   functions,
	}

	schemaRollup := map[string]*SchemaStats{}
	var overallMin, overallMax string
	type rowRef struct {
		name   string
		schema string
		rows   int64
	}
	var refs []rowRef

	for _, s := range schemas {
		overview.TotalColumns += len(s.Columns)

		schemaName := s.Schema
		if schemaName == "" {
			schemaName = "public"
		}
		stats, ok := schemaRollup[schemaName]
		if !ok {
			stats = &SchemaStats{Name: schemaName}
			schemaRollup[schemaName] = stats
		}
		stats.TableCount++

		if prof := enrichment[s.Name]; prof != nil {
			stats.ApproxRowTotal += prof.RowCountApprox
			refs = append(refs, rowRef{s.Name, s.Schema, prof.RowCountApprox})
			for _, dr := range prof.DateRanges {
				if dr.Min != "" && (overallMin == "" || dr.Min < overallMin) {
					overallMin = dr.Min
				}
				if dr.Max != "" && (overallMax == "" || dr.Max > overallMax) {
					overallMax = dr.Max
				}
			}
		}
	}

	names := make([]string, 0, len(schemaRollup))
	for n := range schemaRollup {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		overview.Schemas = append(overview.Schemas, *schemaRollup[n])
	}

	sort.Slice(refs, func(i, j int) bool { return refs[i].rows > refs[j].rows })
	top := 10
	if len(refs) < top {
		top = len(refs)
	}
	for i := 0; i < top; i++ {
		r := refs[i]
		if r.rows == 0 {
			break
		}
		overview.TopTablesByRows = append(overview.TopTablesByRows, TableRef{
			Name:           r.name,
			Schema:         r.schema,
			RowCountApprox: r.rows,
		})
	}

	if overallMin != "" || overallMax != "" {
		overview.OverallDateRange = &DateRange{Min: overallMin, Max: overallMax}
	}

	return overview
}

func buildSchemaInsights(gj *core.GraphJin, database string, schemas []*core.TableSchema, enrichment map[string]*TableProfile) SchemaInsights {
	ins := SchemaInsights{Database: database}

	byName := make(map[string][]*core.TableSchema)
	for _, s := range schemas {
		byName[s.Name] = append(byName[s.Name], s)
	}
	var dupNames []string
	for name, entries := range byName {
		if len(entries) > 1 {
			dupNames = append(dupNames, name)
		}
	}
	sort.Strings(dupNames)
	for _, name := range dupNames {
		entries := byName[name]
		flag := DuplicateFlag{Table: name}
		for _, e := range entries {
			flag.Schemas = append(flag.Schemas, e.Schema)
			hasFKs := false
			for _, col := range e.Columns {
				if col.ForeignKey != "" {
					hasFKs = true
					break
				}
			}
			if hasFKs && flag.FKSchema == "" {
				flag.FKSchema = e.Schema
				flag.Recommendation = fmt.Sprintf("Use @schema(name: %q) — this copy has foreign keys.", e.Schema)
			}
		}
		ins.DuplicateFlags = append(ins.DuplicateFlags, flag)
	}

	type tableWithFKs struct {
		name    string
		fkCount int
	}
	var hubs []tableWithFKs
	for _, s := range schemas {
		fkCount := 0
		for _, col := range s.Columns {
			if col.ForeignKey != "" {
				fkCount++
			}
		}
		if fkCount > 0 {
			hubs = append(hubs, tableWithFKs{s.Name, fkCount})
		}
	}
	sort.Slice(hubs, func(i, j int) bool { return hubs[i].fkCount > hubs[j].fkCount })
	hubCap := 10
	if len(hubs) < hubCap {
		hubCap = len(hubs)
	}
	for i := 0; i < hubCap; i++ {
		ins.HubTables = append(ins.HubTables, HubTable{Name: hubs[i].name, FKCount: hubs[i].fkCount})
	}

	pathsWritten := 0
	for i := 0; i < hubCap && pathsWritten < 20; i++ {
		for j := i + 1; j < hubCap && pathsWritten < 20; j++ {
			steps, err := gj.FindRelationshipPathForDatabase(database, hubs[i].name, hubs[j].name)
			if err != nil || len(steps) == 0 {
				continue
			}
			ins.RelationshipPaths = append(ins.RelationshipPaths, RelationshipPath{
				From:  hubs[i].name,
				To:    hubs[j].name,
				Steps: steps,
			})
			pathsWritten++
		}
	}

	names := gj.DatabaseNames()
	if len(names) > 1 {
		defaultDB := gj.DefaultDatabase()
		for _, n := range names {
			ins.NamespaceRouting = append(ins.NamespaceRouting, NamespaceRoute{
				Database:   n,
				TableCount: len(gj.GetTablesForDatabase(n)),
				Default:    n == defaultDB,
			})
		}
	}

	ins.QueryTemplates = buildQueryTemplates(schemas)
	ins.DataQualityFlags = buildDataQualityFlags(schemas, enrichment)

	return ins
}

func buildQueryTemplates(schemas []*core.TableSchema) []QueryTemplate {
	var templates []QueryTemplate
	for _, schema := range schemas {
		if len(templates) >= 15 {
			break
		}
		var dateCols, numericCols, enumCols []core.ColumnInfo
		var fkCols []core.ColumnInfo
		for _, col := range schema.Columns {
			if isDateType(col.Type) {
				dateCols = append(dateCols, col)
			}
			if isNumericType(col.Type) && !col.PrimaryKey && !strings.HasSuffix(col.Name, "_id") {
				numericCols = append(numericCols, col)
			}
			if isEnumCandidateCol(col) {
				enumCols = append(enumCols, col)
			}
			if col.ForeignKey != "" {
				fkCols = append(fkCols, col)
			}
		}

		if len(dateCols) > 0 && len(numericCols) > 0 && len(templates) < 15 {
			dc := dateCols[0]
			var aggFields []string
			for _, nc := range numericCols {
				aggFields = append(aggFields, fmt.Sprintf("sum_%s", nc.Name))
				if len(aggFields) >= 4 {
					break
				}
			}
			pkName := schema.PrimaryKey
			if pkName == "" && len(schema.Columns) > 0 {
				pkName = schema.Columns[0].Name
			}
			aggFields = append(aggFields, "count_"+pkName)

			q := fmt.Sprintf("{ %s(where: { %s: { gte: \"$START_DATE\" } }, distinct: [%s], order_by: { %s: asc }, limit: 100) { %s %s } }",
				schema.Name, dc.Name, dc.Name, dc.Name, dc.Name, strings.Join(aggFields, " "))
			templates = append(templates, QueryTemplate{
				Name:        fmt.Sprintf("%s_by_%s", schema.Name, dc.Name),
				Kind:        "time_series",
				Table:       schema.Name,
				Description: fmt.Sprintf("Time-series aggregation of %s grouped by %s.", schema.Name, dc.Name),
				Query:       q,
			})
		}

		if len(enumCols) > 0 && len(templates) < 15 {
			ec := enumCols[0]
			countField := "count_" + schema.PrimaryKey
			if schema.PrimaryKey == "" && len(schema.Columns) > 0 {
				countField = "count_" + schema.Columns[0].Name
			}
			q := fmt.Sprintf("{ %s(distinct: [%s]) { %s %s } }", schema.Name, ec.Name, ec.Name, countField)
			templates = append(templates, QueryTemplate{
				Name:        fmt.Sprintf("%s_breakdown_%s", schema.Name, ec.Name),
				Kind:        "breakdown",
				Table:       schema.Name,
				Description: fmt.Sprintf("Breakdown of %s by %s with counts.", schema.Name, ec.Name),
				Query:       q,
			})
		}

		if len(fkCols) > 0 && len(templates) < 15 {
			fk := fkCols[0]
			fkTarget := fk.ForeignKey
			if idx := strings.Index(fkTarget, "."); idx >= 0 {
				fkTarget = fkTarget[:idx]
			}
			var parentFields []string
			if schema.PrimaryKey != "" {
				parentFields = append(parentFields, schema.PrimaryKey)
			}
			for _, col := range schema.Columns {
				if !col.PrimaryKey && col.ForeignKey == "" && len(parentFields) < 4 {
					parentFields = append(parentFields, col.Name)
				}
			}
			q := fmt.Sprintf("{ %s(limit: 10) { %s %s { id } } }", schema.Name, strings.Join(parentFields, " "), fkTarget)
			templates = append(templates, QueryTemplate{
				Name:        fmt.Sprintf("%s_with_%s", schema.Name, fkTarget),
				Kind:        "join",
				Table:       schema.Name,
				Description: fmt.Sprintf("Join %s with related %s.", schema.Name, fkTarget),
				Query:       q,
			})
		}
	}
	return templates
}

func buildDataQualityFlags(schemas []*core.TableSchema, enrichment map[string]*TableProfile) []DataQualityFlag {
	var flags []DataQualityFlag
	for _, schema := range schemas {
		for _, col := range schema.Columns {
			if col.Nullable && !col.PrimaryKey {
				flags = append(flags, DataQualityFlag{
					Table:   schema.Name,
					Column:  col.Name,
					Kind:    "nullable",
					Message: "column allows NULL",
				})
			}
		}
		if enrichment != nil {
			if e := enrichment[schema.Name]; e != nil {
				for col, profile := range e.EnumValues {
					if profile.DistinctCount > 0 && profile.DistinctCount <= 2 {
						var sample []string
						for _, v := range profile.Values {
							sample = append(sample, v.Value)
						}
						flags = append(flags, DataQualityFlag{
							Table:   schema.Name,
							Column:  col,
							Kind:    "low_cardinality",
							Message: fmt.Sprintf("only %d distinct values (%s)", profile.DistinctCount, strings.Join(sample, ", ")),
						})
					}
				}
			}
		}
		if len(flags) >= 50 {
			break
		}
	}
	if len(flags) > 50 {
		flags = flags[:50]
	}
	return flags
}

func buildDuplicateIndex(schemas []*core.TableSchema) map[string][]string {
	byName := make(map[string][]string)
	for _, s := range schemas {
		byName[s.Name] = append(byName[s.Name], s.Schema)
	}
	for name, schemaList := range byName {
		if len(schemaList) <= 1 {
			delete(byName, name)
		}
	}
	return byName
}

func isNumericType(colType string) bool {
	t := strings.ToLower(colType)
	return strings.Contains(t, "int") ||
		strings.Contains(t, "serial") ||
		strings.Contains(t, "decimal") ||
		strings.Contains(t, "numeric") ||
		strings.Contains(t, "number") ||
		strings.Contains(t, "float") ||
		strings.Contains(t, "double") ||
		strings.Contains(t, "real") ||
		strings.Contains(t, "money")
}

func isDateType(colType string) bool {
	t := strings.ToLower(colType)
	return strings.Contains(t, "timestamp") ||
		strings.Contains(t, "date") ||
		strings.Contains(t, "time")
}

func isEnumCandidateCol(col core.ColumnInfo) bool {
	if col.PrimaryKey || col.ForeignKey != "" {
		return false
	}
	name := strings.ToLower(col.Name)
	for _, kw := range []string{"status", "state", "type", "category", "kind", "role",
		"stage", "priority", "level", "grade", "tier", "plan", "mode"} {
		if strings.Contains(name, kw) {
			return true
		}
	}
	t := strings.ToLower(col.Type)
	if (strings.Contains(t, "varchar") || strings.Contains(t, "char")) && !strings.Contains(t, "text") {
		return true
	}
	return false
}

func extractCountFromResult(data json.RawMessage, tableName, colName string) int64 {
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return 0
	}
	row := extractFirstRow(parsed, tableName)
	if row == nil {
		return 0
	}
	if count, ok := row["count_"+colName]; ok {
		return toInt64(count)
	}
	return 0
}

func extractMinMaxFromResult(data json.RawMessage, tableName, colName string) (string, string) {
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", ""
	}
	row := extractFirstRow(parsed, tableName)
	if row == nil {
		return "", ""
	}
	return optionalString(row["min_"+colName]), optionalString(row["max_"+colName])
}

func extractNumericStats(data json.RawMessage, tableName, colName string) NumericStats {
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return NumericStats{}
	}
	row := extractFirstRow(parsed, tableName)
	if row == nil {
		return NumericStats{}
	}
	return NumericStats{
		Min:   optionalString(row["min_"+colName]),
		Max:   optionalString(row["max_"+colName]),
		Avg:   optionalString(row["avg_"+colName]),
		Sum:   optionalString(row["sum_"+colName]),
		Count: toInt64(row["count_"+colName]),
	}
}

func extractEnumProfile(data json.RawMessage, tableName, colName, pkName string) EnumProfile {
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return EnumProfile{}
	}
	rows := extractAllRows(parsed, tableName)
	profile := EnumProfile{DistinctCount: int64(len(rows))}
	if len(rows) == 0 {
		return profile
	}
	countKey := "count_" + pkName
	reachedCap := len(rows) >= enumSampleCap
	displayLimit := len(rows)
	if reachedCap {
		profile.Truncated = true
		displayLimit = 0
	} else if len(rows) > 20 {
		profile.Truncated = true
		displayLimit = 10
	}
	for i := 0; i < displayLimit && i < len(rows); i++ {
		row := rows[i]
		val := optionalString(row[colName])
		if val == "" {
			continue
		}
		profile.Values = append(profile.Values, EnumValue{
			Value: val,
			Count: toInt64(row[countKey]),
		})
	}
	return profile
}

func extractRowsFromResult(data json.RawMessage, tableName string) []map[string]any {
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil
	}
	return extractAllRows(parsed, tableName)
}

func extractFirstRow(parsed map[string]any, tableName string) map[string]any {
	tableData, ok := parsed[tableName]
	if !ok {
		return nil
	}
	switch v := tableData.(type) {
	case []any:
		if len(v) > 0 {
			if row, ok := v[0].(map[string]any); ok {
				return row
			}
		}
	case map[string]any:
		return v
	}
	return nil
}

func extractAllRows(parsed map[string]any, tableName string) []map[string]any {
	tableData, ok := parsed[tableName]
	if !ok {
		return nil
	}
	rows, ok := tableData.([]any)
	if !ok {
		return nil
	}
	var out []map[string]any
	for _, r := range rows {
		if row, ok := r.(map[string]any); ok {
			out = append(out, row)
		}
	}
	return out
}

func toInt64(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return i
		}
	}
	return 0
}

func optionalString(v any) string {
	if v == nil {
		return ""
	}
	switch s := v.(type) {
	case string:
		return s
	case json.Number:
		return s.String()
	case float64:
		return fmt.Sprintf("%g", s)
	case int64:
		return fmt.Sprintf("%d", s)
	case int:
		return fmt.Sprintf("%d", s)
	case bool:
		if s {
			return "true"
		}
		return "false"
	}
	return fmt.Sprintf("%v", v)
}
