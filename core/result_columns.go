package core

import (
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

// ResultColumn describes one field a query returned, from the compiled query
// and the schema rather than from the values, so a client can lay a result
// out without guessing what a number or a string means.
type ResultColumn struct {
	// Path is the field's result path, e.g. "accounts.plan.name".
	Path string `json:"path"`
	// Root is the top-level field the column belongs to.
	Root string `json:"root"`
	Name string `json:"name"`
	// Type is the database type, e.g. integer, text, timestamp with time zone.
	Type string `json:"type,omitempty"`
	// Role is key (the row's own id), reference (a foreign key), measure
	// (a number or an aggregate), time, or text.
	Role string `json:"role"`
	// Aggregate is the function an aggregate field applies (count, sum, avg,
	// min, max).
	Aggregate string `json:"aggregate,omitempty"`
}

// Columns describes the fields of a query result. It is computed on request,
// so queries that never ask pay nothing.
func (r *Result) Columns() []ResultColumn {
	if r == nil || r.qc == nil || r.qc.Type != qcode.QTQuery {
		return nil
	}
	qc := r.qc
	var out []ResultColumn
	var walk func(id int32, prefix []string, root string, depth int)
	walk = func(id int32, prefix []string, root string, depth int) {
		if depth > 3 || id < 0 || int(id) >= len(qc.Selects) {
			return
		}
		sel := &qc.Selects[id]
		if sel.SkipRender != qcode.SkipTypeNone {
			return
		}
		path := append(append([]string{}, prefix...), sel.FieldName)
		if root == "" {
			root = sel.FieldName
		}
		for _, field := range sel.Fields {
			if field.SkipRender != qcode.SkipTypeNone {
				continue
			}
			info := ResultColumn{Path: joinPath(append(append([]string{}, path...), field.FieldName)), Root: root, Name: field.FieldName}
			switch field.Type {
			case qcode.FieldTypeCol:
				info.Type = field.Col.Type
				info.Role = columnRole(field.Col.PrimaryKey, field.Col.FKeyTable != "", field.Col.Type)
			case qcode.FieldTypeFunc:
				info.Aggregate = strings.ToLower(field.Func.Name)
				info.Type = field.Func.Type
				if info.Type == "" {
					info.Type = field.Col.Type
				}
				info.Role = "measure"
				if !field.Func.Agg && !isAggregateName(info.Aggregate) {
					info.Aggregate = ""
					info.Role = columnRole(false, false, info.Type)
				}
			default:
				continue
			}
			out = append(out, info)
		}
		for _, child := range sel.Children {
			walk(child, path, root, depth+1)
		}
	}
	for _, rootID := range qc.Roots {
		walk(rootID, nil, "", 0)
	}
	return out
}

func isAggregateName(name string) bool {
	switch name {
	case "count", "sum", "avg", "min", "max":
		return true
	}
	return false
}

// columnRole classifies a column by what the schema says about it.
func columnRole(primaryKey, foreignKey bool, typ string) string {
	switch {
	case primaryKey:
		return "key"
	case foreignKey:
		return "reference"
	}
	t := strings.ToLower(typ)
	switch {
	case strings.Contains(t, "time"), strings.Contains(t, "date"):
		return "time"
	case strings.Contains(t, "int"), strings.Contains(t, "numeric"), strings.Contains(t, "decimal"),
		strings.Contains(t, "float"), strings.Contains(t, "double"), strings.Contains(t, "real"), strings.Contains(t, "money"):
		return "measure"
	}
	return "text"
}
