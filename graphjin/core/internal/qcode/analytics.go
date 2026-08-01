package qcode

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/graph"
)

type analyticsDirectiveArgs struct {
	spec         *WindowSpec
	aggregate    string
	aggregateSet bool
	rows         int
	rowsSet      bool
	orderSet     bool
}

type analyticsDirectiveOpts struct {
	aggregate bool
	rows      bool
}

func (co *Compiler) compileDirectiveAnalytics(sel *Select, f *Field, d graph.Directive) error {
	if f.Window != nil {
		return fmt.Errorf("only one analytics directive can be used on field %q", f.FieldName)
	}
	if f.Type != FieldTypeCol {
		return fmt.Errorf("@%s can only be used on a column field", d.Name)
	}

	switch d.Name {
	case "running":
		args, err := co.parseAnalyticsDirectiveArgs(sel, f, d, analyticsDirectiveOpts{aggregate: true})
		if err != nil {
			return err
		}
		args.spec.Frame = "ROWS UNBOUNDED PRECEDING"
		return co.applyAggregateAnalytics(f, args)

	case "moving":
		args, err := co.parseAnalyticsDirectiveArgs(sel, f, d, analyticsDirectiveOpts{aggregate: true, rows: true})
		if err != nil {
			return err
		}
		if args.rows == 1 {
			args.spec.Frame = "ROWS CURRENT ROW"
		} else {
			args.spec.Frame = fmt.Sprintf("ROWS BETWEEN %d PRECEDING AND CURRENT ROW", args.rows-1)
		}
		return co.applyAggregateAnalytics(f, args)

	case "previous":
		return co.applyValueAnalytics(sel, f, d, WindowFuncLag)

	case "next":
		return co.applyValueAnalytics(sel, f, d, WindowFuncLead)

	case "first":
		return co.applyValueAnalytics(sel, f, d, WindowFuncFirstValue)

	case "last":
		return co.applyValueAnalytics(sel, f, d, WindowFuncLastValue)

	case "rank":
		return co.applyRankingAnalytics(sel, f, d, WindowFuncRank)

	case "denseRank":
		return co.applyRankingAnalytics(sel, f, d, WindowFuncDenseRank)

	case "rowNumber":
		return co.applyRankingAnalytics(sel, f, d, WindowFuncRowNumber)
	}

	return fmt.Errorf("unknown analytics directive @%s", d.Name)
}

func (co *Compiler) parseAnalyticsDirectiveArgs(
	sel *Select,
	f *Field,
	d graph.Directive,
	opts analyticsDirectiveOpts,
) (analyticsDirectiveArgs, error) {
	args := analyticsDirectiveArgs{spec: &WindowSpec{}}

	for _, arg := range d.Args {
		switch arg.Name {
		case "by":
			cols, err := co.analyticsColumnList(sel, d.Name, arg)
			if err != nil {
				return args, err
			}
			args.spec.Partition = append(args.spec.Partition, cols...)

		case "orderBy":
			if args.orderSet {
				return args, fmt.Errorf("@%s accepts only one of orderBy or order", d.Name)
			}
			orders, err := co.analyticsOrderBy(sel, d.Name, arg)
			if err != nil {
				return args, err
			}
			args.spec.OrderBy = append(args.spec.OrderBy, orders...)
			args.orderSet = true

		case "order":
			if args.orderSet {
				return args, fmt.Errorf("@%s accepts only one of orderBy or order", d.Name)
			}
			ord, err := analyticsOrderFromAnnotatedField(d.Name, f.Col.Name, arg)
			if err != nil {
				return args, err
			}
			args.spec.OrderBy = append(args.spec.OrderBy, ord)
			args.orderSet = true

		case "aggregate":
			if !opts.aggregate {
				return args, fmt.Errorf("@%s does not accept aggregate; use aggregate with @running or @moving", d.Name)
			}
			agg, err := analyticsAggregate(d.Name, arg)
			if err != nil {
				return args, err
			}
			args.aggregate = agg
			args.aggregateSet = true

		case "rows":
			if !opts.rows {
				return args, fmt.Errorf("@%s does not accept rows; use rows with @moving", d.Name)
			}
			rows, err := analyticsRows(d.Name, arg)
			if err != nil {
				return args, err
			}
			args.rows = rows
			args.rowsSet = true

		default:
			return args, unknownArg(arg)
		}
	}

	if opts.aggregate && !args.aggregateSet {
		return args, fmt.Errorf("@%s requires aggregate: sum, avg, count, min, or max", d.Name)
	}
	if opts.rows && !args.rowsSet {
		return args, fmt.Errorf("@%s requires rows: <positive integer>", d.Name)
	}
	if !args.orderSet {
		return args, fmt.Errorf("@%s requires orderBy: { column: asc|desc } or order: asc|desc", d.Name)
	}

	return args, nil
}

func (co *Compiler) applyAggregateAnalytics(f *Field, args analyticsDirectiveArgs) error {
	dbFn, ok := co.s.GetFunctions()[args.aggregate]
	if !ok || !dbFn.Agg {
		return fmt.Errorf("unknown aggregate %q", args.aggregate)
	}
	f.Type = FieldTypeFunc
	f.Func = dbFn
	f.Args = []Arg{{Type: ArgTypeCol, Col: f.Col}}
	f.Window = args.spec
	return nil
}

func (co *Compiler) applyValueAnalytics(sel *Select, f *Field, d graph.Directive, wf WindowFunc) error {
	args, err := co.parseAnalyticsDirectiveArgs(sel, f, d, analyticsDirectiveOpts{})
	if err != nil {
		return err
	}
	if wf == WindowFuncFirstValue || wf == WindowFuncLastValue {
		args.spec.Frame = "ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING"
	}
	fn := newWindowFunction(wf.String(), f.Col.Type, wf)
	fn.Args = []Arg{{Type: ArgTypeCol, Col: f.Col}}
	f.Type = FieldTypeFunc
	f.Func = fn.Func
	f.Args = fn.Args
	f.WindowFunc = wf
	f.Window = args.spec
	return nil
}

func (co *Compiler) applyRankingAnalytics(sel *Select, f *Field, d graph.Directive, wf WindowFunc) error {
	args, err := co.parseAnalyticsDirectiveArgs(sel, f, d, analyticsDirectiveOpts{})
	if err != nil {
		return err
	}
	fn := newWindowFunction(wf.String(), "bigint", wf)
	f.Type = FieldTypeFunc
	f.Func = fn.Func
	f.Args = nil
	f.WindowFunc = wf
	f.Window = args.spec
	return nil
}

func (co *Compiler) analyticsColumnList(sel *Select, dir string, arg graph.Arg) ([]string, error) {
	if arg.Val == nil {
		return nil, fmt.Errorf("@%s: argument %q is empty", dir, arg.Name)
	}
	var vals []string
	if arg.Val.Type == graph.NodeList {
		if len(arg.Val.Children) == 0 {
			return nil, fmt.Errorf("@%s: by list is empty", dir)
		}
		for _, ch := range arg.Val.Children {
			if ch.Type != graph.NodeStr && ch.Type != graph.NodeLabel {
				return nil, fmt.Errorf("@%s: by list items must be column names", dir)
			}
			vals = append(vals, ch.Val)
		}
	} else {
		if err := validateArg(arg, graph.NodeStr, graph.NodeLabel); err != nil {
			return nil, err
		}
		vals = append(vals, arg.Val.Val)
	}

	for i, val := range vals {
		name := co.ParseName(val)
		if name == "" {
			return nil, fmt.Errorf("@%s: by contains an empty column name", dir)
		}
		if _, ok := sel.Ti.ColumnExists(name); !ok {
			return nil, fmt.Errorf("@%s by column %q is not on table %q", dir, val, sel.Ti.Name)
		}
		vals[i] = name
	}
	return vals, nil
}

func (co *Compiler) analyticsOrderBy(sel *Select, dir string, arg graph.Arg) ([]WindowOrder, error) {
	if err := validateArg(arg, graph.NodeObj); err != nil {
		return nil, fmt.Errorf("@%s orderBy must be an object like { month: asc }", dir)
	}
	if len(arg.Val.Children) == 0 {
		return nil, fmt.Errorf("@%s orderBy cannot be empty", dir)
	}

	orders := make([]WindowOrder, 0, len(arg.Val.Children))
	seen := make(map[string]struct{}, len(arg.Val.Children))
	for _, node := range arg.Val.Children {
		if node.Type != graph.NodeStr && node.Type != graph.NodeLabel {
			return nil, fmt.Errorf("@%s orderBy value for %q must be asc or desc", dir, node.Name)
		}
		colName := co.ParseName(node.Name)
		if colName == "" {
			return nil, fmt.Errorf("@%s orderBy contains an empty column name", dir)
		}
		if _, ok := seen[colName]; ok {
			return nil, fmt.Errorf("@%s orderBy column %q can only be defined once", dir, node.Name)
		}
		seen[colName] = struct{}{}
		if _, ok := sel.Ti.ColumnExists(colName); !ok {
			return nil, fmt.Errorf("@%s orderBy column %q is not on table %q", dir, node.Name, sel.Ti.Name)
		}
		desc, err := analyticsDesc(dir, node.Val)
		if err != nil {
			return nil, err
		}
		orders = append(orders, WindowOrder{Col: colName, Desc: desc})
	}
	return orders, nil
}

// validateAnalytics enforces SELECT-list column authorization on the
// partitioning and ordering columns stored in analytics WindowSpecs. Window
// specs retain only column names, so resolve each name back to its DBColumn to
// enforce the config-level Blocked flag as well as the role allowlist.
//
// Without this check, a role could infer a hidden column through the row order,
// rank, lag, or first-value output produced by @running, @moving, @previous,
// @next, @first, @last, @rank, @denseRank, or @rowNumber.
func (co *Compiler) validateAnalytics(qc *QCode, sel *Select, tr trval) error {
	for _, f := range sel.Fields {
		if f.Window == nil {
			continue
		}

		for _, name := range f.Window.Partition {
			if err := validateAnalyticsColumn(qc, sel, tr, name); err != nil {
				return fmt.Errorf("analytics partition: %w", err)
			}
		}
		for _, order := range f.Window.OrderBy {
			if err := validateAnalyticsColumn(qc, sel, tr, order.Col); err != nil {
				return fmt.Errorf("analytics order: %w", err)
			}
		}
	}
	return nil
}

func validateAnalyticsColumn(qc *QCode, sel *Select, tr trval, name string) error {
	col, err := sel.Ti.GetColumn(name)
	if err != nil {
		return err
	}
	if col.Blocked || !tr.columnAllowed(qc, col.Name) {
		return validateErr(tr, col.Name, "db column blocked")
	}
	return nil
}

func analyticsOrderFromAnnotatedField(dir, col string, arg graph.Arg) (WindowOrder, error) {
	if err := validateArg(arg, graph.NodeStr, graph.NodeLabel); err != nil {
		return WindowOrder{}, err
	}
	desc, err := analyticsDesc(dir, arg.Val.Val)
	if err != nil {
		return WindowOrder{}, err
	}
	return WindowOrder{Col: col, Desc: desc}, nil
}

func analyticsDesc(dir, val string) (bool, error) {
	switch strings.ToLower(val) {
	case "asc":
		return false, nil
	case "desc":
		return true, nil
	default:
		return false, fmt.Errorf("@%s order must be asc or desc, got %q", dir, val)
	}
}

func analyticsAggregate(dir string, arg graph.Arg) (string, error) {
	if err := validateArg(arg, graph.NodeStr, graph.NodeLabel); err != nil {
		return "", err
	}
	agg := strings.ToLower(arg.Val.Val)
	switch agg {
	case "sum", "avg", "count", "min", "max":
		return agg, nil
	default:
		return "", fmt.Errorf("@%s aggregate must be one of sum, avg, count, min, or max", dir)
	}
}

func analyticsRows(dir string, arg graph.Arg) (int, error) {
	if err := validateArg(arg, graph.NodeNum); err != nil {
		return 0, err
	}
	rows, err := strconv.Atoi(arg.Val.Val)
	if err != nil || rows <= 0 {
		return 0, fmt.Errorf("@%s rows must be a positive integer", dir)
	}
	return rows, nil
}
