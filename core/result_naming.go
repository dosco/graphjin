package core

import (
	"encoding/json"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// NamingGapInfo reports rows a query result identifies only by key while the
// schema has a column that names them: a list of subscriptions carrying
// account_id but not the account's name, or accounts carrying id but not
// name. A key means nothing to a person reading the answer, so callers (the
// server agent, MCP tools) can tell the author which name to select before
// the answer is written. It is derived from the compiled query and the
// schema, never from the question.
type NamingGapInfo struct {
	// Path is the dotted JSON path of the list or object holding the rows.
	Path string `json:"path"`
	// Table is the table the key identifies.
	Table string `json:"table"`
	// KeyColumn is the selected column that identifies those rows: the
	// table's own primary key, or a foreign key on the selected table.
	KeyColumn string `json:"key_column"`
	// NameColumn is the column on Table that names its rows.
	NameColumn string `json:"name_column"`
	// Relationship is the field to select under Path to reach NameColumn
	// through the foreign key; empty when the rows are Table's own.
	Relationship string `json:"relationship,omitempty"`
}

// namingGapCandidate is a gap found at compile time; it is reported only when
// its path holds at least one row.
type namingGapCandidate struct {
	info NamingGapInfo
	path []string
}

// NamingGaps returns the rows in Data identified only by key, per the
// compiled query. It is empty for mutations, subscriptions, multi-database
// parallel-root queries, and results with no rows at the reported paths.
func (r *Result) NamingGaps() []NamingGapInfo {
	if len(r.namingGaps) == 0 || len(r.Data) == 0 {
		return nil
	}
	var data any
	if err := json.Unmarshal(r.Data, &data); err != nil {
		return nil
	}
	var out []NamingGapInfo
	for _, gap := range r.namingGaps {
		if pathHasRows(data, gap.path) {
			out = append(out, gap.info)
		}
	}
	return out
}

// namingLookup resolves a foreign key's target table for namingGapsFromQCode.
func (gj *graphjinEngine) namingLookup(database, schema, table string) (sdata.DBTable, bool) {
	ctx, ok := gj.GetDatabase(database)
	if !ok || ctx == nil || ctx.schema == nil {
		return sdata.DBTable{}, false
	}
	t, err := ctx.schema.Find(schema, table)
	if err != nil {
		return sdata.DBTable{}, false
	}
	return t, true
}

// nameColumnCandidates are the column names that name a row, in order of
// preference. A table with none of them falls back to its first unique,
// non-key text column (a reference, a slug, an email).
var nameColumnCandidates = []string{"name", "title", "full_name", "display_name", "label", "username"}

// nameColumn returns the column that names t's rows, or "" when t has none.
func nameColumn(t sdata.DBTable) string {
	for _, want := range nameColumnCandidates {
		for _, col := range t.Columns {
			if col.Name == want && namingColumnUsable(col) {
				return col.Name
			}
		}
	}
	for _, col := range t.Columns {
		if col.UniqueKey && namingColumnUsable(col) {
			return col.Name
		}
	}
	return ""
}

func namingColumnUsable(col sdata.DBColumn) bool {
	if col.PrimaryKey || col.Blocked || col.Array || col.FKeyTable != "" {
		return false
	}
	typ := strings.ToLower(col.Type)
	return strings.Contains(typ, "text") || strings.Contains(typ, "char") || strings.Contains(typ, "string")
}

// namingGapsFromQCode finds, for every rendered select of a compiled query,
// the rows it identifies only by key. lookup resolves a foreign key's target
// table; a target it cannot resolve is skipped.
func namingGapsFromQCode(qc *qcode.QCode, lookup func(database, schema, table string) (sdata.DBTable, bool)) []namingGapCandidate {
	if qc == nil || qc.Type != qcode.QTQuery || lookup == nil {
		return nil
	}
	var out []namingGapCandidate
	seen := map[string]bool{}
	add := func(path []string, info NamingGapInfo) {
		info.Path = joinPath(path)
		key := info.Path + "|" + info.Table + "|" + info.KeyColumn
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, namingGapCandidate{info: info, path: path})
	}
	// mapped holds the tables some select in this query returns with both key
	// and name: the result carries its own id-to-name map for them, the way a
	// query lists accounts { id name } beside invoices { account_id }.
	mapped := map[string]bool{}
	for i := range qc.Selects {
		sel := &qc.Selects[i]
		if sel.SkipRender != qcode.SkipTypeNone || sel.GlobalAgg {
			continue
		}
		selected := selectedColumns(sel)
		if name := nameColumn(sel.Ti); name != "" && selected[name] {
			for _, pk := range primaryColumns(sel.Ti) {
				if selected[pk] {
					mapped[sel.Table] = true
					break
				}
			}
		}
	}
	// named holds, for the selects enclosing the current one, the tables
	// whose name is already selected: a nested row's foreign key back to such
	// a parent is the join, and the parent row already says who it is.
	var walk func(id int32, prefix []string, depth int, named map[string]bool)
	walk = func(id int32, prefix []string, depth int, named map[string]bool) {
		if depth > 3 || int(id) >= len(qc.Selects) {
			return
		}
		sel := &qc.Selects[id]
		if sel.SkipRender != qcode.SkipTypeNone {
			return
		}
		path := make([]string, len(prefix)+1)
		copy(path, prefix)
		path[len(prefix)] = sel.FieldName

		if !sel.GlobalAgg {
			selected := selectedColumns(sel)
			// The rows' own key without their name. A select filtering on the
			// name already knows it: accounts(where: {name: ...}) { id } is a
			// lookup, not an answer that hides who the rows are.
			if name := nameColumn(sel.Ti); name != "" && !selected[name] && !filterUsesColumn(sel.Where.Exp, name) {
				for _, pk := range primaryColumns(sel.Ti) {
					if selected[pk] {
						add(path, NamingGapInfo{Table: sel.Table, KeyColumn: pk, NameColumn: name})
						break
					}
				}
			}
			// A foreign key without the name of the row it points to.
			for _, field := range sel.Fields {
				col := field.Col
				if field.Type != qcode.FieldTypeCol || field.SkipRender != qcode.SkipTypeNone || col.FKeyTable == "" {
					continue
				}
				database := col.FKeyDatabase
				if database == "" {
					database = sel.Database
				}
				target, ok := lookup(database, col.FKeySchema, col.FKeyTable)
				if !ok {
					continue
				}
				name := nameColumn(target)
				if name == "" || named[col.FKeyTable] || mapped[col.FKeyTable] || childSelectsName(qc, sel, col.FKeyTable, name) {
					continue
				}
				relationship := col.FKeyAlias
				if relationship == "" {
					relationship = col.FKeyTable
				}
				add(path, NamingGapInfo{Table: col.FKeyTable, KeyColumn: col.Name, NameColumn: name, Relationship: relationship})
			}
		}
		inner := named
		if name := nameColumn(sel.Ti); name != "" && selectedColumns(sel)[name] {
			inner = make(map[string]bool, len(named)+1)
			for table := range named {
				inner[table] = true
			}
			inner[sel.Table] = true
		}
		for _, child := range sel.Children {
			walk(child, path, depth+1, inner)
		}
	}
	for _, root := range qc.Roots {
		walk(root, nil, 0, nil)
	}
	return out
}

// filterUsesColumn reports whether a filter expression compares column.
func filterUsesColumn(exp *qcode.Exp, column string) bool {
	if exp == nil {
		return false
	}
	if exp.Left.Col.Name == column || exp.Left.ColName == column {
		return true
	}
	for _, child := range exp.Children {
		if filterUsesColumn(child, column) {
			return true
		}
	}
	return false
}

func selectedColumns(sel *qcode.Select) map[string]bool {
	out := map[string]bool{}
	for _, field := range sel.Fields {
		if field.Type == qcode.FieldTypeCol && field.SkipRender == qcode.SkipTypeNone {
			out[field.Col.Name] = true
		}
	}
	return out
}

func primaryColumns(t sdata.DBTable) []string {
	var out []string
	for _, col := range t.PrimaryCols {
		out = append(out, col.Name)
	}
	if len(out) == 0 && t.PrimaryCol.Name != "" {
		out = append(out, t.PrimaryCol.Name)
	}
	return out
}

// childSelectsName reports whether sel already selects table's name through
// a nested select.
func childSelectsName(qc *qcode.QCode, sel *qcode.Select, table, name string) bool {
	for _, child := range sel.Children {
		if int(child) >= len(qc.Selects) {
			continue
		}
		cs := &qc.Selects[child]
		if cs.Table == table && cs.SkipRender == qcode.SkipTypeNone && selectedColumns(cs)[name] {
			return true
		}
	}
	return false
}

// pathHasRows reports whether path leads to at least one row: a non-empty
// list or an object.
func pathHasRows(data any, path []string) bool {
	current := []any{data}
	for _, segment := range path {
		var next []any
		for _, node := range current {
			switch typed := node.(type) {
			case map[string]any:
				if child, ok := typed[segment]; ok {
					next = append(next, child)
				}
			case []any:
				for _, item := range typed {
					if m, ok := item.(map[string]any); ok {
						if child, ok := m[segment]; ok {
							next = append(next, child)
						}
					}
				}
			}
			if len(next) > 1000 {
				break
			}
		}
		if len(next) == 0 {
			return false
		}
		current = next
	}
	for _, node := range current {
		switch typed := node.(type) {
		case []any:
			if len(typed) != 0 {
				return true
			}
		case map[string]any:
			return true
		}
	}
	return false
}
