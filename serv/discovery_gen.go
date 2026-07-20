package serv

import (
	"fmt"
	"sort"
	"strings"

	core "github.com/dosco/graphjin/core/v3"
)

const enumSampleCap = 101

func buildTableIndexEntry(schema *core.TableSchema, profile *TableProfile, duplicateSchemas map[string][]string) TableIndexEntry {
	entry := TableIndexEntry{
		Name:        schema.Name,
		Schema:      schema.Schema,
		Database:    schema.Database,
		Type:        schema.Type,
		Comment:     schema.Comment,
		ColumnCount: len(schema.Columns),
	}
	if entry.Type == "" {
		entry.Type = "table"
	}
	if profile != nil && profile.RowCountApprox != nil {
		v := *profile.RowCountApprox
		entry.RowCountApprox = &v
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

func buildCheapTableIndex(tables []core.TableInfo) []TableIndexEntry {
	entries := make([]TableIndexEntry, 0, len(tables))
	for _, t := range tables {
		typ := t.Type
		if typ == "" {
			typ = "table"
		}
		entries = append(entries, TableIndexEntry{
			Name:        t.Name,
			Schema:      t.Schema,
			Database:    t.Database,
			Type:        typ,
			Comment:     t.Comment,
			ColumnCount: t.ColumnCount,
		})
	}
	return entries
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

func buildDatabaseOverview(database string, schemas []*core.TableSchema, enrichment map[string]*TableProfile, functions []core.FunctionInfo, analyticsMode bool) DatabaseOverview {
	overview := DatabaseOverview{
		Database:      database,
		AnalyticsMode: analyticsMode,
		TotalTables:   len(schemas),
		Functions:     functions,
	}

	schemaRollup := map[string]*SchemaStats{}
	var overallMin, overallMax string

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

	if overallMin != "" || overallMax != "" {
		overview.OverallDateRange = &DateRange{Min: overallMin, Max: overallMax}
	}

	return overview
}

func buildCheapDatabaseOverview(database string, tables []core.TableInfo, functions []core.FunctionInfo, analyticsMode bool) DatabaseOverview {
	overview := DatabaseOverview{
		Database:      database,
		AnalyticsMode: analyticsMode,
		TotalTables:   len(tables),
		Functions:     functions,
	}

	schemaRollup := map[string]*SchemaStats{}
	for _, t := range tables {
		overview.TotalColumns += t.ColumnCount
		schemaName := t.Schema
		if schemaName == "" {
			schemaName = "public"
		}
		stats, ok := schemaRollup[schemaName]
		if !ok {
			stats = &SchemaStats{Name: schemaName}
			schemaRollup[schemaName] = stats
		}
		stats.TableCount++
	}

	names := make([]string, 0, len(schemaRollup))
	for n := range schemaRollup {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		overview.Schemas = append(overview.Schemas, *schemaRollup[n])
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
