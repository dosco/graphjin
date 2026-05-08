package qcode

import (
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/graph"
)

// WindowSpec captures a SQL window definition attached to an aggregate
// function field via the `@window` directive. When a Field has a non-nil
// Window, the SQL emitter wraps the aggregate as
//
//	<func>(args) OVER (PARTITION BY <p1>, <p2>, ... ORDER BY <o1>, <o2>, ... <frame>)
//
// and the field does NOT trigger a GROUP BY on the enclosing select —
// window functions return one row per input row, not one per group.
type WindowSpec struct {
	Partition []string  // column names to partition by (already validated)
	OrderBy   []WindowOrder
	Frame     string    // canonical frame clause, or "" for the default
}

// WindowOrder is one column entry in the OVER (... ORDER BY ...) list.
// Direction is "asc" (default) or "desc"; nulls handling intentionally
// omitted in the v1 cut.
type WindowOrder struct {
	Col  string
	Desc bool
}

// validWindowFrames is the allowlist of frame clauses the renderer will
// emit. Restricting to a finite set prevents user input from injecting
// arbitrary SQL fragments while still covering the common analytics
// patterns (running totals, lookback windows). Stored lowercase; matched
// case-insensitively.
var validWindowFrames = map[string]string{
	"rows unbounded preceding":                                "ROWS UNBOUNDED PRECEDING",
	"rows between unbounded preceding and current row":        "ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW",
	"rows between unbounded preceding and unbounded following": "ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING",
	"rows between current row and unbounded following":        "ROWS BETWEEN CURRENT ROW AND UNBOUNDED FOLLOWING",
	"range unbounded preceding":                               "RANGE UNBOUNDED PRECEDING",
	"range between unbounded preceding and current row":       "RANGE BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW",
	"range between unbounded preceding and unbounded following": "RANGE BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING",
}

// canonicalFrame returns the canonical (uppercase) frame clause for the
// supplied user input, or an error if the input is not in the allowlist.
func canonicalFrame(in string) (string, error) {
	key := strings.ToLower(strings.Join(strings.Fields(in), " "))
	if out, ok := validWindowFrames[key]; ok {
		return out, nil
	}
	return "", fmt.Errorf("frame %q is not in the allowlist (see qcode.validWindowFrames)", in)
}

// compileDirectiveWindow parses an @window directive and attaches the
// resulting WindowSpec to the field. Column names in `partition` and
// `order` are validated against the enclosing select's table to prevent
// arbitrary identifiers from reaching the SQL emitter.
//
// Accepted form:
//
//	@window(partition: ["user_id"], order: ["created_at", "total desc"], frame: "rows unbounded preceding")
//
// `partition` and `order` are optional individually but at least one
// must be present. `frame` is optional; when omitted the engine emits no
// frame clause and SQL applies the default.
func (co *Compiler) compileDirectiveWindow(sel *Select, f *Field, d graph.Directive) error {
	if f.Type != FieldTypeFunc {
		return fmt.Errorf("@window can only be used on aggregate or window function fields")
	}

	spec := &WindowSpec{}
	for _, arg := range d.Args {
		switch arg.Name {
		case "partition", "partitionBy", "partition_by":
			cols, err := windowColList(arg)
			if err != nil {
				return err
			}
			for _, c := range cols {
				if _, ok := sel.Ti.ColumnExists(c); !ok {
					return fmt.Errorf("@window partition column %q is not on table %q", c, sel.Ti.Name)
				}
				spec.Partition = append(spec.Partition, c)
			}

		case "order", "orderBy", "order_by":
			items, err := windowColList(arg)
			if err != nil {
				return err
			}
			for _, it := range items {
				ord, err := parseWindowOrder(sel, it)
				if err != nil {
					return err
				}
				spec.OrderBy = append(spec.OrderBy, ord)
			}

		case "frame":
			if err := validateArg(arg, graph.NodeStr, graph.NodeLabel); err != nil {
				return err
			}
			canon, err := canonicalFrame(arg.Val.Val)
			if err != nil {
				return err
			}
			spec.Frame = canon

		default:
			return unknownArg(arg)
		}
	}

	if len(spec.Partition) == 0 && len(spec.OrderBy) == 0 {
		return fmt.Errorf("@window requires `partition` and/or `order`")
	}

	f.Window = spec
	return nil
}

// windowColList accepts either a list of strings/labels or a single
// string/label and returns the values. Empty list errors.
func windowColList(arg graph.Arg) ([]string, error) {
	if arg.Val == nil {
		return nil, fmt.Errorf("@window: argument %q is empty", arg.Name)
	}
	if arg.Val.Type == graph.NodeList {
		out := make([]string, 0, len(arg.Val.Children))
		for _, ch := range arg.Val.Children {
			if ch.Type != graph.NodeStr && ch.Type != graph.NodeLabel {
				return nil, fmt.Errorf("@window: %q list items must be strings", arg.Name)
			}
			if ch.Val == "" {
				return nil, fmt.Errorf("@window: %q list contains an empty value", arg.Name)
			}
			out = append(out, ch.Val)
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("@window: %q list is empty", arg.Name)
		}
		return out, nil
	}
	if err := validateArg(arg, graph.NodeStr, graph.NodeLabel); err != nil {
		return nil, err
	}
	return []string{arg.Val.Val}, nil
}

// parseWindowOrder splits "col" or "col asc" / "col desc" into a
// validated WindowOrder. Column existence is checked against the
// enclosing select's table.
func parseWindowOrder(sel *Select, in string) (WindowOrder, error) {
	parts := strings.Fields(in)
	if len(parts) == 0 {
		return WindowOrder{}, fmt.Errorf("@window order entry is empty")
	}
	col := parts[0]
	if _, ok := sel.Ti.ColumnExists(col); !ok {
		return WindowOrder{}, fmt.Errorf("@window order column %q is not on table %q", col, sel.Ti.Name)
	}
	ord := WindowOrder{Col: col}
	if len(parts) > 1 {
		switch strings.ToLower(parts[1]) {
		case "asc":
		case "desc":
			ord.Desc = true
		default:
			return WindowOrder{}, fmt.Errorf("@window order direction %q must be asc or desc", parts[1])
		}
	}
	if len(parts) > 2 {
		return WindowOrder{}, fmt.Errorf("@window order entry %q has too many components", in)
	}
	return ord, nil
}
