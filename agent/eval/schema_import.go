package eval

import (
	"fmt"
	"sort"
	"strings"
)

// Importing a schema from a running GraphJin server.
//
// This is how an organization gets an environment of its own without anyone
// touching its database. Everything here reads catalog cards — the same
// description of the schema any connected agent already sees — and never a row
// of real data. What comes out is enough to rebuild the shape of the database
// locally and fill it with synthetic records.
//
// It is deliberately separate from the generator's own catalog snapshot. That
// snapshot's exact query is hashed into every frozen suite's fingerprint, so
// widening it to carry what an import needs would invalidate published
// benchmarks. This reads its own query, including the per-column evidence the
// snapshot leaves out.

// ImportRow is a catalog card as the importer reads it: the snapshot's fields
// plus evidence_json, which carries a column's full metadata and the closed set
// of values it was observed to hold.
type ImportRow struct {
	CatalogRow
	EvidenceJSON any `json:"evidence_json,omitempty"`
}

type ImportedColumn struct {
	Name       string
	Type       string
	NotNull    bool
	PrimaryKey bool
	UniqueKey  bool
	Ordinal    int
	// ObservedValues is the closed set the catalog publishes for this column.
	// It is the only real data an import carries, and it is already visible to
	// any agent with catalog access.
	ObservedValues []string
}

type ImportedTable struct {
	Name       string
	Columns    []ImportedColumn
	PrimaryKey string
}

type ImportedRelationship struct {
	FromTable  string
	FromColumn string
	ToTable    string
	ToColumn   string
}

type ImportedSavedQuery struct {
	Name  string
	Query string
}

type ImportedSchema struct {
	Tables        []ImportedTable
	Relationships []ImportedRelationship
	SavedQueries  []ImportedSavedQuery
}

// ImportDrop records something the import refused, and why. A silent drop turns
// into a table the clone is missing and a task family that mysteriously
// produces nothing, so every refusal is named.
type ImportDrop struct {
	Kind   string
	ID     string
	Reason string
}

type ImportReport struct {
	Drops []ImportDrop
	Notes []string
}

func (r *ImportReport) drop(kind, id, reason string) {
	r.Drops = append(r.Drops, ImportDrop{Kind: kind, ID: id, Reason: reason})
}

// ImportSchema rebuilds a schema description from catalog cards.
func ImportSchema(rows []ImportRow) (ImportedSchema, ImportReport, error) {
	var schema ImportedSchema
	var report ImportReport

	columnsByTable := map[string][]ImportedColumn{}
	tableNames := map[string]bool{}

	for _, row := range rows {
		switch row.Kind {
		case "table":
			name := strings.TrimSpace(row.TableName)
			if name == "" || strings.HasPrefix(name, "gj_") {
				continue
			}
			if looksVirtualTable(row) {
				report.drop("table", name, "served by a file or API source rather than the database")
				continue
			}
			tableNames[name] = true
		case "column":
			table := strings.TrimSpace(row.TableName)
			name := strings.TrimSpace(row.ColumnName)
			if table == "" || name == "" || strings.HasPrefix(table, "gj_") {
				continue
			}
			column, ok := importColumn(row)
			if !ok {
				report.drop("column", row.ID, "card carried no usable column type")
				continue
			}
			columnsByTable[table] = append(columnsByTable[table], column)
		}
	}

	for name := range tableNames {
		columns := columnsByTable[name]
		if len(columns) == 0 {
			report.drop("table", name, "no column cards described this table")
			continue
		}
		sort.Slice(columns, func(i, j int) bool {
			if columns[i].Ordinal != columns[j].Ordinal {
				return columns[i].Ordinal < columns[j].Ordinal
			}
			return columns[i].Name < columns[j].Name
		})
		table := ImportedTable{Name: name, Columns: columns, PrimaryKey: importedPrimaryKey(columns)}
		if table.PrimaryKey == "" {
			// Without a key there is no way to identify one row, which is what
			// every write task and every collateral check depends on.
			report.drop("table", name, "no primary key and no column named id")
			continue
		}
		schema.Tables = append(schema.Tables, table)
	}
	sort.Slice(schema.Tables, func(i, j int) bool { return schema.Tables[i].Name < schema.Tables[j].Name })

	if len(schema.Tables) == 0 {
		return schema, report, fmt.Errorf("no usable tables found in the catalog")
	}

	schema.Relationships = importRelationships(rows, schema.Tables, &report)
	schema.SavedQueries = importSavedQueries(rows, &report)
	return schema, report, nil
}

// importColumn reads a column card's evidence. Field names arrive as Go
// identifiers ("ColumnName", "NotNull"); the detail lookup normalizes case and
// underscores, so both spellings resolve.
func importColumn(row ImportRow) (ImportedColumn, bool) {
	typeName := strings.TrimSpace(detailString(row.EvidenceJSON, "type", "data_type", "db_type"))
	if typeName == "" {
		return ImportedColumn{}, false
	}
	column := ImportedColumn{
		Name:       row.ColumnName,
		Type:       typeName,
		NotNull:    detailBool(row.EvidenceJSON, "not_null"),
		PrimaryKey: detailBool(row.EvidenceJSON, "primary_key"),
		UniqueKey:  detailBool(row.EvidenceJSON, "unique_key"),
		Ordinal:    detailInt(row.EvidenceJSON, "ordinal"),
	}
	column.ObservedValues = importObservedValues(row)
	return column, true
}

// importObservedValues prefers the structured set on the evidence and falls
// back to the prose form published in the column's examples.
func importObservedValues(row ImportRow) []string {
	if values := detailStringList(mapValue(toMap(decodeDetailValue(row.EvidenceJSON)), "observed_values")); len(values) != 0 {
		return values
	}
	return observedColumnValues(row.CatalogRow)
}

// looksVirtualTable reports whether a table card describes something other than
// a database table.
//
// GraphJin exposes file and API sources as tables in the same namespace, with
// the same shape of card, so nothing about the columns says which is which. What
// does say it is how the catalog shows them being queried: a file listing is
// read with prefix: or key: as root arguments, which no database table accepts.
// Cloning one produces a table the schema cannot create and the seed cannot
// insert into, so it is dropped with a reason rather than failing the boot.
func looksVirtualTable(row ImportRow) bool {
	table := strings.TrimSpace(row.TableName)
	if table == "" {
		return false
	}
	for _, example := range detailStringList(row.ExamplesJSON) {
		compact := strings.ReplaceAll(example, " ", "")
		for _, argument := range []string{"(prefix:", "(key:"} {
			if strings.Contains(compact, table+argument) {
				return true
			}
		}
	}
	return false
}

func importedPrimaryKey(columns []ImportedColumn) string {
	for _, column := range columns {
		if column.PrimaryKey {
			return column.Name
		}
	}
	for _, column := range columns {
		if strings.EqualFold(column.Name, "id") {
			return column.Name
		}
	}
	return ""
}

// importRelationships reuses the generator's fail-closed edge parser, so an
// import and a generated task family agree on what counts as a relationship.
func importRelationships(rows []ImportRow, tables []ImportedTable, report *ImportReport) []ImportedRelationship {
	catalogRows := make([]CatalogRow, 0, len(rows))
	for _, row := range rows {
		catalogRows = append(catalogRows, row.CatalogRow)
	}
	generatorTables := make([]generatorTable, 0, len(tables))
	for _, table := range tables {
		columns := make([]generatorColumn, 0, len(table.Columns))
		for _, column := range table.Columns {
			columns = append(columns, generatorColumn{Name: column.Name, Type: column.Type, NotNull: column.NotNull})
		}
		generatorTables = append(generatorTables, generatorTable{Name: table.Name, Columns: columns, PrimaryKey: table.PrimaryKey})
	}
	edges := catalogRelationships(catalogRows, generatorTables)
	out := make([]ImportedRelationship, 0, len(edges))
	for _, edge := range edges {
		out = append(out, ImportedRelationship{
			FromTable: edge.FromTable, FromColumn: edge.FromColumn,
			ToTable: edge.ToTable, ToColumn: edge.ToColumn,
		})
	}
	relationshipCards := 0
	for _, row := range rows {
		if row.Kind == "relationship" {
			relationshipCards++
		}
	}
	if dropped := relationshipCards - len(out); dropped > 0 {
		report.Notes = append(report.Notes,
			fmt.Sprintf("%d relationship card(s) did not resolve to two known tables and were skipped", dropped))
	}
	return out
}

func importSavedQueries(rows []ImportRow, report *ImportReport) []ImportedSavedQuery {
	var out []ImportedSavedQuery
	for _, row := range rows {
		if row.Kind != "saved_query" {
			continue
		}
		name := strings.TrimSpace(row.Name)
		query := strings.TrimSpace(queryFromDetails(row.DetailsJSON))
		if name == "" || query == "" {
			// Mutations and subscriptions are stored the same way but are not
			// read queries; the extractor only accepts reads.
			report.drop("saved_query", row.ID, "card carried no readable query text")
			continue
		}
		out = append(out, ImportedSavedQuery{Name: name, Query: query})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
