package catalog

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dosco/graphjin/core/v3/whereop"
)

// relationshipsContent explains a table card's relationships detail.
const relationshipsContent = "Tables this one joins to. Select a related table by nesting its field inside this table's selection; name_column is the column that names those rows, so select it whenever an answer points at them."

// maxRelatedTables bounds the relationships detail on a heavily joined table.
const maxRelatedTables = 12

// maxExampleJoins bounds the joins a table card's example selects.
const maxExampleJoins = 2

// relatedTable is one entry in a table card's relationships detail.
type relatedTable struct {
	// Field is the field to nest inside this table's selection.
	Field string `json:"field"`
	// Rows is "one" when each row reaches a single related row (its parent
	// through a foreign key) and "many" for a list of child rows.
	Rows string `json:"rows"`
	// Via names the key column that links the two tables, as table.column.
	Via string `json:"via"`
	// NameColumn is the related table's column that names its rows.
	NameColumn string `json:"name_column,omitempty"`
	Example    string `json:"example"`
}

// tableLinks indexes a snapshot's relationships by table so each table card
// can teach its own joins. Relationships otherwise live only on separate
// relationship cards, which a model rarely fetches before it writes a query:
// it then selects the foreign key and stops, or guesses the related field.
type tableLinks struct {
	tables   map[string]MetadataTable
	columns  map[string][]MetadataColumn
	names    map[string]string
	dbTypes  map[string]string
	outgoing map[string][]MetadataRelationship
	incoming map[string][]MetadataRelationship
	// fkCounts counts a table's foreign keys to each target table. Two keys to
	// one table (sender_id, receiver_id) make the target's field ambiguous, so
	// neither is taught.
	fkCounts map[string]map[string]int
}

func indexTableLinks(tables []MetadataTable, rels []MetadataRelationship, columnsByTable map[string][]MetadataColumn, databases []MetadataDatabase) tableLinks {
	l := tableLinks{
		tables:   make(map[string]MetadataTable, len(tables)),
		columns:  columnsByTable,
		names:    make(map[string]string, len(tables)),
		dbTypes:  make(map[string]string, len(databases)),
		outgoing: map[string][]MetadataRelationship{},
		incoming: map[string][]MetadataRelationship{},
		fkCounts: map[string]map[string]int{},
	}
	for _, db := range databases {
		l.dbTypes[db.Name] = strings.ToLower(db.Type)
	}
	fkColumns := map[string]bool{}
	for _, r := range rels {
		if r.Source == "foreign_key" {
			fkColumns[r.FromColumnID] = true
		}
	}
	for _, t := range tables {
		l.tables[t.ID] = t
		l.names[t.ID] = metadataNameColumn(columnsByTable[t.ID], fkColumns)
	}
	for _, r := range rels {
		from, to := parentTableID(r.FromColumnID), parentTableID(r.ToColumnID)
		if _, ok := l.tables[from]; !ok {
			continue
		}
		if _, ok := l.tables[to]; !ok {
			continue
		}
		switch r.Source {
		case "foreign_key":
			// A self-reference nests a table in itself, which needs recursive
			// syntax; a cross-database key is not taught as a plain nesting.
			if from == to || r.IsCrossDatabase {
				continue
			}
			l.outgoing[from] = append(l.outgoing[from], r)
			l.incoming[to] = append(l.incoming[to], r)
			if l.fkCounts[from] == nil {
				l.fkCounts[from] = map[string]int{}
			}
			l.fkCounts[from][to]++
		case "remote_join":
			// The remote rows hang off the parent only; remoteJoinNestedExample
			// already teaches the remote table's own card.
			l.incoming[to] = append(l.incoming[to], r)
		}
	}
	return l
}

// unambiguous reports whether the foreign key r is its table's only key to
// the target table.
func (l tableLinks) unambiguous(r MetadataRelationship) bool {
	return l.fkCounts[parentTableID(r.FromColumnID)][parentTableID(r.ToColumnID)] == 1
}

// related lists the tables a table joins to, parents first.
func (l tableLinks) related(tableID string) []relatedTable {
	t, ok := l.tables[tableID]
	if !ok || isRemoteTable(t) {
		return nil
	}
	label := l.labelField(tableID)
	var out []relatedTable
	for _, r := range sortedRelationships(l.outgoing[tableID], func(r MetadataRelationship) string { return r.ToTableName }) {
		if !l.unambiguous(r) {
			continue
		}
		target := parentTableID(r.ToColumnID)
		out = append(out, relatedTable{
			Field:      r.ToTableName,
			Rows:       "one",
			Via:        r.FromTableName + "." + r.FromColumnName,
			NameColumn: l.names[target],
			Example:    fmt.Sprintf("{ %s(limit: 10) { %s %s { %s } } }", t.TableName, label, r.ToTableName, l.labelField(target)),
		})
	}
	for _, r := range sortedRelationships(l.incoming[tableID], func(r MetadataRelationship) string { return r.FromTableName }) {
		child := parentTableID(r.FromColumnID)
		entry := relatedTable{Field: r.FromTableName, NameColumn: l.names[child]}
		if r.Source == "remote_join" {
			childCols, _ := keyColumns(l.columns[child])
			entry.Rows = "one"
			entry.Via = r.ToTableName + "." + r.ToColumnName
			entry.Example = fmt.Sprintf("{ %s(limit: 10) { %s %s { %s } } }", t.TableName, label, r.FromTableName, exampleFieldList(childCols, 3, false))
		} else {
			if !l.unambiguous(r) {
				continue
			}
			entry.Rows = "many"
			if r.RelType == "one_to_one" {
				entry.Rows = "one"
			}
			entry.Via = r.FromTableName + "." + r.FromColumnName
			entry.Example = fmt.Sprintf("{ %s(limit: 10) { %s %s(limit: 5) { %s } } }", t.TableName, label, r.FromTableName, l.labelField(child))
		}
		out = append(out, entry)
	}
	if len(out) > maxRelatedTables {
		out = out[:maxRelatedTables]
	}
	return out
}

// labelField is the field that best says which row is which: the naming
// column, else id, else the first listed key column.
func (l tableLinks) labelField(tableID string) string {
	if name := l.names[tableID]; name != "" {
		return name
	}
	cols, _ := keyColumns(l.columns[tableID])
	return exampleFieldList(cols, 1, true)
}

// examples returns a database table card's examples: a row read that names
// its rows and the parents its foreign keys point at, then, where the
// database aggregates, the same table counted and summed by the database.
// Models copy the first example on the card they read, so it must not be the
// id-and-foreign-key page that leaves an answer naming nothing.
func (l tableLinks) examples(t MetadataTable, keyCols []MetadataColumn) []string {
	name := l.names[t.ID]
	var joins []string
	joined := map[string]bool{}
	if !isRemoteTable(t) {
		for _, r := range sortedRelationships(l.outgoing[t.ID], func(r MetadataRelationship) string { return r.FromColumnName }) {
			if len(joins) >= maxExampleJoins {
				break
			}
			target := parentTableID(r.ToColumnID)
			if !l.unambiguous(r) || l.names[target] == "" {
				continue
			}
			joins = append(joins, fmt.Sprintf("%s { %s }", r.ToTableName, l.names[target]))
			joined[r.FromColumnName] = true
		}
	}

	// Only lead with id when the table actually has one; remote tables often do
	// not, and an invented field in the one example a model reads is a trap.
	fields := []string{}
	for _, c := range keyCols {
		if c.ColumnName == "id" {
			fields = append(fields, "id")
			break
		}
	}
	if name != "" {
		fields = append(fields, name)
	}
	for _, c := range keyCols {
		if len(fields) >= 4 {
			break
		}
		if c.ColumnName == "id" || c.ColumnName == name || joined[c.ColumnName] {
			continue
		}
		fields = append(fields, c.ColumnName)
	}
	if len(fields) == 0 {
		fields = append(fields, "id")
	}
	fields = append(fields, joins...)
	out := []string{fmt.Sprintf("{ %s(limit: 10) { %s } }", t.TableName, strings.Join(fields, " "))}
	if agg := l.aggregateExample(t); agg != "" {
		out = append(out, agg)
	}
	return out
}

// aggregateExample counts a table's rows and sums one numeric column in the
// database, or "" when the table has no key to count or its source does not
// aggregate.
func (l tableLinks) aggregateExample(t MetadataTable) string {
	if isRemoteTable(t) || l.dbTypes[t.DatabaseName] == "mongodb" {
		return ""
	}
	cols := l.columns[t.ID]
	key := ""
	for _, c := range cols {
		if c.ColumnName == "id" {
			key = "id"
			break
		}
	}
	if key == "" && t.PrimaryKey != "" && !strings.Contains(t.PrimaryKey, ",") {
		key = t.PrimaryKey
	}
	if key == "" {
		return ""
	}
	fields := []string{"count_" + key}
	if metric := metricColumn(cols); metric != "" {
		fields = append(fields, "sum_"+metric)
	}
	return fmt.Sprintf("{ %s { %s } }", t.TableName, strings.Join(fields, " "))
}

// metricColumn picks a numeric column worth summing: a metric-named one
// first, else the first numeric column that is not a key.
func metricColumn(cols []MetadataColumn) string {
	first := ""
	for _, c := range cols {
		if c.PrimaryKey || c.Array || looksForeignKey(c) || whereop.Classify(c.Type, c.Array) != whereop.Numeric {
			continue
		}
		if looksMetricColumn(c) {
			return c.ColumnName
		}
		if first == "" {
			first = c.ColumnName
		}
	}
	return first
}

func isRemoteTable(t MetadataTable) bool {
	return strings.EqualFold(strings.TrimSpace(t.Type), "remote")
}

func sortedRelationships(rels []MetadataRelationship, key func(MetadataRelationship) string) []MetadataRelationship {
	out := append([]MetadataRelationship(nil), rels...)
	sort.SliceStable(out, func(i, j int) bool {
		if key(out[i]) != key(out[j]) {
			return key(out[i]) < key(out[j])
		}
		return out[i].ID < out[j].ID
	})
	return out
}
