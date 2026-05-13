package qcode

import (
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/graph"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func (co *Compiler) isFunction(sel *Select, name string, f graph.Field) (
	fn Function, isFunc bool, err error,
) {
	// New path: any field carrying an `expr:` argument is treated as a
	// scalar-expression aggregate (`<aggregate>(expr: ...)`) regardless
	// of the legacy `<fn>_<col>` naming convention. This must be checked
	// before the search/legacy paths because the field name (e.g. "sum")
	// would otherwise be ambiguous.
	if exprArg, ok := findArg(f.Args, "expr"); ok {
		fn, isFunc, err = co.compileExprFunction(sel, name, exprArg)
		if err != nil || isFunc {
			if err == nil && co.c.DisableAgg && fn.Agg {
				err = fmt.Errorf("aggreation disabled: db function '%s' cannot be used", fn.Name)
			}
			return
		}
	}

	if fn, isFunc, err = co.compileWindowFunction(sel, name); isFunc || err != nil {
		return
	}

	switch {
	case name == "search_rank":
		isFunc = true
		if _, ok := sel.GetInternalArg("search"); !ok {
			err = fmt.Errorf("search argument not found: %s", name)
		}

	case strings.HasPrefix(name, "search_headline_"):
		isFunc = true
		fn.Name = "search_headline"
		fn.Args = []Arg{{Type: ArgTypeCol}}
		fn.Args[0].Col, err = sel.Ti.GetColumn(name[(len(fn.Name) + 1):])
		if err != nil {
			return
		}
		if _, ok := sel.GetInternalArg("search"); !ok {
			err = fmt.Errorf("no search defined: %s", name)
		}

	default:
		var fi funcInfo
		if fi, isFunc, err = co.isFunctionEx(sel, name, f); isFunc {
			fn.Name = fi.Name
			fn.Func = fi.Func
			fn.Agg = fi.Agg
			if fi.Col.Name != "" {
				fn.Args = []Arg{{Type: ArgTypeCol, Col: fi.Col}}
			}
			isFunc = true
		} else {
			return fn, false, err
		}
	}

	if co.c.DisableAgg && fn.Agg {
		err = fmt.Errorf("aggreation disabled: db function '%s' cannot be used", fn.Name)
	}

	return
}

func (co *Compiler) compileWindowFunction(sel *Select, name string) (
	fn Function, isFunc bool, err error,
) {
	switch name {
	case "row_number":
		return newWindowFunction(name, "bigint", WindowFuncRowNumber), true, nil
	case "rank":
		return newWindowFunction(name, "bigint", WindowFuncRank), true, nil
	case "dense_rank":
		return newWindowFunction(name, "bigint", WindowFuncDenseRank), true, nil
	}

	prefixes := []struct {
		prefix string
		wf     WindowFunc
	}{
		{"first_value_", WindowFuncFirstValue},
		{"last_value_", WindowFuncLastValue},
		{"lag_", WindowFuncLag},
		{"lead_", WindowFuncLead},
	}
	for _, p := range prefixes {
		if !strings.HasPrefix(name, p.prefix) {
			continue
		}
		colName := name[len(p.prefix):]
		if colName == "" {
			return fn, true, fmt.Errorf("window function %q requires a column suffix", strings.TrimSuffix(p.prefix, "_"))
		}
		col, err := sel.Ti.GetColumn(colName)
		if err != nil {
			return fn, true, fmt.Errorf("window function %q: %w", strings.TrimSuffix(p.prefix, "_"), err)
		}
		fn = newWindowFunction(strings.TrimSuffix(p.prefix, "_"), col.Type, p.wf)
		fn.Args = []Arg{{Type: ArgTypeCol, Col: col}}
		return fn, true, nil
	}

	return fn, false, nil
}

func newWindowFunction(name, typ string, wf WindowFunc) Function {
	return Function{
		Name:       name,
		WindowFunc: wf,
		Func: sdata.DBFunction{
			Name: name,
			Type: typ,
			Agg:  false,
		},
	}
}

// compileExprFunction handles the `<name>(expr: <expression>)` syntax.
// If <name> is a known aggregate (sum/avg/min/max/count), the expression
// is wrapped in that aggregate at SQL render time. Otherwise, the
// expression is emitted bare — used for ratio-of-aggregates patterns
// where the expression itself contains aggregate-of-expression nodes.
//
// MongoDB doesn't support arithmetic in aggregation pipelines (v2 scope);
// this path errors clearly when the active dialect is mongodb.
func (co *Compiler) compileExprFunction(sel *Select, name string, exprArg graph.Arg) (
	fn Function, isFunc bool, err error,
) {
	if co.s.DBType() == "mongodb" {
		err = fmt.Errorf("expression aggregates are not supported on MongoDB (deferred to a future release)")
		return
	}

	exprRoot, err := co.compileExprArg(sel, exprArg.Val)
	if err != nil {
		return
	}

	// Wrap-in-aggregate vs bare-expression decision is based on whether
	// the field name matches a registered aggregate function. This keeps
	// the schema's function registry as the source of truth.
	dbFn, isAgg := co.s.GetFunctions()[name]
	hasInnerAgg := exprContainsAgg(exprRoot)

	if isAgg && dbFn.Agg {
		// `sum(expr: ...)` — wrap in SUM(<expr>). The inner expression
		// must NOT contain its own aggregates (would be SUM(SUM(...))).
		if hasInnerAgg {
			err = fmt.Errorf("expr inside %s(...) must not contain aggregate nodes", name)
			return
		}
		fn.Name = name
		fn.Func = dbFn
		fn.Agg = true
	} else {
		// Bare expression — used for ratio-of-aggregates or other
		// composed aggregates. Must contain at least one aggregate node
		// or the resulting SQL would fail GROUP BY validation when used
		// alongside aggregates on the same selection.
		if !hasInnerAgg {
			err = fmt.Errorf("expr field %q must either use a known aggregate name (sum, avg, min, max, count) or contain aggregate nodes inside the expression", name)
			return
		}
		fn.Name = name
		fn.Agg = true // treated as aggregate for GROUP BY purposes
	}

	fn.Args = []Arg{{Type: ArgTypeExpr, Expr: exprRoot}}
	isFunc = true
	return
}

// exprContainsAgg reports whether the expression tree includes any
// aggregate-of-expression op (OpAggSum/OpAggAvg/...). Used to decide
// between wrap-in-aggregate and bare-expression compilation paths.
func exprContainsAgg(ex *Exp) bool {
	if ex == nil {
		return false
	}
	switch ex.Op {
	case OpAggSum, OpAggAvg, OpAggMin, OpAggMax, OpAggCount:
		return true
	}
	for _, c := range ex.Children {
		if exprContainsAgg(c) {
			return true
		}
	}
	for _, arm := range ex.CaseArms {
		if exprContainsAgg(arm.Then) {
			return true
		}
	}
	if exprContainsAgg(ex.Else) {
		return true
	}
	return false
}

// findArg returns the named argument from a graph.Field's args, if present.
func findArg(args []graph.Arg, name string) (graph.Arg, bool) {
	for i := range args {
		if args[i].Name == name {
			return args[i], true
		}
	}
	return graph.Arg{}, false
}

type funcInfo struct {
	Name string
	Func sdata.DBFunction
	Col  sdata.DBColumn
	Agg  bool
}

func (co *Compiler) isFunctionEx(sel *Select, name string, f graph.Field) (
	fi funcInfo, isFunc bool, err error,
) {
	for k, v := range co.s.GetFunctions() {
		if k == name && len(f.Args) != 0 {
			fi.Name = k
			fi.Agg = false
			fi.Func = v
			isFunc = true
			return
		}

		kLen := len(k)
		if strings.HasPrefix(name, (k + "_")) {
			fi.Name = name[:kLen]
			fi.Col, err = sel.Ti.GetColumn(name[(kLen + 1):])
			if err != nil {
				return
			}
			fi.Agg = true
			fi.Func = v
			isFunc = true
			return
		}
	}

	return
}
