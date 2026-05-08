package qcode

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/graph"
)

// WindowSpec captures a SQL window definition attached to an aggregate
// function field via the `@window` directive. When a Field has a non-nil
// Window, the SQL emitter wraps the aggregate as
//
//	<func>(args) OVER (PARTITION BY <p1>, ... ORDER BY <o1>, ... <frame>)
//
// and the field does NOT trigger a GROUP BY on the enclosing select —
// window functions return one row per input row, not one per group.
//
// All three sections are optional individually. An empty WindowSpec
// emits a bare `OVER ()`, which is valid SQL for window functions that
// support the implicit window frame (e.g. row_number on a single
// partition).
type WindowSpec struct {
	Partition []string
	OrderBy   []WindowOrder
	Frame     string // canonical, uppercase frame clause; "" = engine default
}

// NullsHandling controls placement of NULL values in the OVER ORDER BY.
// Most SQL dialects (Snowflake, Postgres, Oracle) accept NULLS FIRST /
// NULLS LAST after the direction; MSSQL/MySQL silently ignore it but
// don't error.
type NullsHandling int8

const (
	NullsDefault NullsHandling = iota
	NullsFirst
	NullsLast
)

// WindowOrder is one column entry in the OVER (... ORDER BY ...) list.
type WindowOrder struct {
	Col   string
	Desc  bool
	Nulls NullsHandling
}

// compileDirectiveWindow parses an @window directive and attaches the
// resulting WindowSpec to the field. Column names in `partition` and
// `order` are validated against the enclosing select's table — only
// validated identifiers ever reach the SQL emitter.
//
// Examples:
//
//	@window(partition: ["user_id"], order: ["created_at"])
//	@window(partition: ["user_id"], order: ["total desc nulls last"],
//	        frame: "rows between 5 preceding and current row")
//	@window(order: ["id"], frame: "range between unbounded preceding and current row")
//	@window  # empty — emits OVER ()
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
			canon, err := parseFrameClause(arg.Val.Val)
			if err != nil {
				return err
			}
			spec.Frame = canon

		default:
			return unknownArg(arg)
		}
	}

	f.Window = spec
	return nil
}

// windowColList accepts a list of strings/labels (the GraphQL value
// `["a", "b"]`) or a single string/label and returns the values. Empty
// list errors.
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

// parseWindowOrder accepts entries shaped like:
//
//	"col"
//	"col asc" / "col desc"
//	"col desc nulls last"
//	"col nulls first"
//
// It validates the column against the enclosing select's table.
func parseWindowOrder(sel *Select, in string) (WindowOrder, error) {
	parts := strings.Fields(strings.ToLower(in))
	rawParts := strings.Fields(in)
	if len(parts) == 0 {
		return WindowOrder{}, fmt.Errorf("@window order entry is empty")
	}
	col := rawParts[0]
	if _, ok := sel.Ti.ColumnExists(col); !ok {
		return WindowOrder{}, fmt.Errorf("@window order column %q is not on table %q", col, sel.Ti.Name)
	}
	ord := WindowOrder{Col: col}
	i := 1
	if i < len(parts) && (parts[i] == "asc" || parts[i] == "desc") {
		ord.Desc = parts[i] == "desc"
		i++
	}
	if i < len(parts) {
		if parts[i] != "nulls" {
			return WindowOrder{}, fmt.Errorf("@window order entry %q: unexpected token %q", in, parts[i])
		}
		i++
		if i >= len(parts) {
			return WindowOrder{}, fmt.Errorf("@window order entry %q: NULLS without FIRST/LAST", in)
		}
		switch parts[i] {
		case "first":
			ord.Nulls = NullsFirst
		case "last":
			ord.Nulls = NullsLast
		default:
			return WindowOrder{}, fmt.Errorf("@window order entry %q: NULLS must be FIRST or LAST", in)
		}
		i++
	}
	if i != len(parts) {
		return WindowOrder{}, fmt.Errorf("@window order entry %q has trailing tokens", in)
	}
	return ord, nil
}

// parseFrameClause normalises a user-supplied frame string into the
// canonical, uppercased SQL form. Token-based parsing keeps the
// allowed shapes explicit while still accepting numeric offsets — every
// `<n>` is parsed as a non-negative integer rather than passed through
// as a string, so SQL injection via the frame argument is impossible.
//
// Supported (uppercase here for clarity, lower/mixed case input
// accepted):
//
//	ROWS UNBOUNDED PRECEDING
//	ROWS CURRENT ROW
//	ROWS <n> PRECEDING
//	ROWS <n> FOLLOWING
//	ROWS BETWEEN <bound> AND <bound>
//	(same with RANGE)
//
// where `<bound>` is one of: UNBOUNDED PRECEDING / UNBOUNDED FOLLOWING
// / CURRENT ROW / `<n> PRECEDING` / `<n> FOLLOWING`.
//
// INTERVAL constants for timestamp-typed RANGE frames are not yet
// supported; they're a Snowflake/Postgres-specific extension that
// requires a separate type-aware code path.
func parseFrameClause(in string) (string, error) {
	toks := strings.Fields(strings.ToLower(in))
	if len(toks) == 0 {
		return "", fmt.Errorf("frame clause is empty")
	}
	mode := strings.ToUpper(toks[0])
	switch mode {
	case "ROWS", "RANGE":
	default:
		return "", fmt.Errorf("frame must start with ROWS or RANGE, got %q", toks[0])
	}
	rest := toks[1:]
	if len(rest) == 0 {
		return "", fmt.Errorf("frame %s missing bound", mode)
	}

	// Two shapes: BETWEEN <a> AND <b>, or single-bound `<a>`.
	if rest[0] == "between" {
		andIdx := -1
		for i, t := range rest {
			if t == "and" {
				andIdx = i
				break
			}
		}
		if andIdx == -1 || andIdx == 1 || andIdx == len(rest)-1 {
			return "", fmt.Errorf("frame BETWEEN requires <bound> AND <bound>: %q", in)
		}
		startStr, err := renderBound(rest[1:andIdx])
		if err != nil {
			return "", fmt.Errorf("frame %q: start bound: %w", in, err)
		}
		endStr, err := renderBound(rest[andIdx+1:])
		if err != nil {
			return "", fmt.Errorf("frame %q: end bound: %w", in, err)
		}
		return fmt.Sprintf("%s BETWEEN %s AND %s", mode, startStr, endStr), nil
	}

	bound, err := renderBound(rest)
	if err != nil {
		return "", fmt.Errorf("frame %q: %w", in, err)
	}
	return fmt.Sprintf("%s %s", mode, bound), nil
}

// renderBound takes the trailing tokens of a frame bound and returns
// the canonical SQL fragment, or an error if the tokens don't form a
// recognised bound. `toks` MUST be non-empty.
func renderBound(toks []string) (string, error) {
	switch len(toks) {
	case 2:
		switch {
		case toks[0] == "unbounded" && toks[1] == "preceding":
			return "UNBOUNDED PRECEDING", nil
		case toks[0] == "unbounded" && toks[1] == "following":
			return "UNBOUNDED FOLLOWING", nil
		case toks[0] == "current" && toks[1] == "row":
			return "CURRENT ROW", nil
		case toks[1] == "preceding" || toks[1] == "following":
			n, err := strconv.Atoi(toks[0])
			if err != nil || n < 0 {
				return "", fmt.Errorf("expected non-negative integer offset, got %q", toks[0])
			}
			return fmt.Sprintf("%d %s", n, strings.ToUpper(toks[1])), nil
		}
	}
	return "", fmt.Errorf("unrecognised bound %q", strings.Join(toks, " "))
}
