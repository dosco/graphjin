// Package queryshape inspects GraphJin query syntax without executing it.
// Execution success and schema authorization must be established by the caller.
package queryshape

import (
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/graph"
)

// Aggregate identifies a selected database aggregate, independently of aliases
// and native versus Hasura-compatible spelling. An empty Column denotes the
// compatibility form's implicit row count or a compound expression; it is not
// evidence of an aggregate over any particular column.
type Aggregate struct {
	Table     string
	Operation string
	Column    string
}

// Aggregates parses selections using the engine's GraphQL parser. Names in
// comments, argument values, operation names, and aliases do not count.
func Aggregates(query string) ([]Aggregate, error) {
	op, err := graph.Parse([]byte(query))
	if err != nil {
		return nil, err
	}
	var out []Aggregate
	var visit func(int32, string)
	visit = func(id int32, table string) {
		f := op.Fields[id]
		if strings.HasSuffix(f.Name, "_aggregate") && len(f.Children) > 0 {
			base := strings.TrimSuffix(f.Name, "_aggregate")
			compat := false
			for _, child := range f.Children {
				cf := op.Fields[child]
				fields := []int32{child}
				if cf.Name == "aggregate" {
					fields = cf.Children
					compat = true
				}
				for _, aid := range fields {
					af := op.Fields[aid]
					if !aggregateOp(af.Name) {
						continue
					}
					compat = true
					if af.Name == "count" && len(af.Children) == 0 && len(af.Args) == 0 {
						out = append(out, Aggregate{Table: base, Operation: "count"})
					} else if af.Name != "count" && len(af.Args) == 0 {
						for _, cid := range af.Children {
							col := op.Fields[cid]
							if len(col.Children) == 0 && len(col.Args) == 0 {
								out = append(out, Aggregate{Table: base, Operation: af.Name, Column: col.Name})
							}
						}
					}
				}
			}
			if compat {
				return
			}
		}
		if len(f.Children) == 0 {
			fn, col, ok := aggregatePrefix(f.Name)
			if ok && aggregateOp(fn) && col != "" && len(f.Args) == 0 {
				out = append(out, Aggregate{Table: table, Operation: fn, Column: col})
			} else if aggregateOp(f.Name) {
				for _, arg := range f.Args {
					if arg.Name == "column" && (arg.Val.Type == graph.NodeLabel || arg.Val.Type == graph.NodeStr) {
						out = append(out, Aggregate{Table: table, Operation: f.Name, Column: arg.Val.Val})
					} else if arg.Name == "expr" {
						out = append(out, Aggregate{Table: table, Operation: f.Name})
					}
				}
			}
		}
		for _, child := range f.Children {
			visit(child, f.Name)
		}
	}
	for _, f := range op.Fields {
		if f.ParentID == -1 {
			visit(f.ID, "")
		}
	}
	return out, nil
}

func aggregateOp(name string) bool {
	switch name {
	case "count", "sum", "avg", "min", "max", "stddev", "variance",
		"stddev_pop", "stddev_samp", "var_pop", "var_samp":
		return true
	}
	return false
}

// Match the longest registered operation before treating the remainder as a
// column name. Both aggregate names and column names can contain underscores.
func aggregatePrefix(name string) (string, string, bool) {
	for end := strings.LastIndexByte(name, '_'); end >= 0; end = strings.LastIndexByte(name[:end], '_') {
		if aggregateOp(name[:end]) && end+1 < len(name) {
			return name[:end], name[end+1:], true
		}
	}
	return "", "", false
}
