package qcode

import (
	"errors"
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/graph"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
	"github.com/dosco/graphjin/core/v3/internal/util"
)

// Limits applied by validateExprTree to prevent pathological nesting.
const (
	exprMaxDepth = 16
	exprMaxNodes = 64
)

// compileExprArg parses the structured `expr:` argument of an aggregate
// field into a scalar expression tree (*Exp). The grammar is recursive:
// each object contains exactly one operator key whose value is either
// another expression node, an array of expression nodes, or (for `case`)
// a custom shape.
//
// Leaf keys: col, num, str, var.
// Op keys:   add, sub, mul, div, mod, neg, coalesce, nullif, case, cast.
// Aggregate keys (only legal at the top of a non-aggregate's expr arg,
// for ratio-of-aggregates): sum, avg, min, max, count.
//
// Errors carry the offending key path so users can locate problems.
func (co *Compiler) compileExprArg(sel *Select, node *graph.Node) (*Exp, error) {
	if node == nil {
		return nil, errors.New("expr: missing expression body")
	}
	return co.compileExprNode(sel, node, 0)
}

func (co *Compiler) compileExprNode(sel *Select, node *graph.Node, depth int) (*Exp, error) {
	if depth > exprMaxDepth {
		return nil, fmt.Errorf("expr: nesting exceeds maximum depth of %d", exprMaxDepth)
	}
	if node == nil {
		return nil, errors.New("expr: empty node")
	}

	// Bare-leaf shorthand: a parser-tagged leaf value (identifier, quoted
	// string, number, variable, boolean) is treated as the equivalent
	// explicit-wrapper form at the same position. This matches the way
	// the where-clause parser already dispatches on NodeLabel / NodeStr /
	// NodeNum / NodeVar. The explicit `{ col: ... }` / `{ num: ... }` /
	// `{ var: ... }` forms still work (handled below) for backwards
	// compat and for cases where explicit disambiguation is preferred.
	switch node.Type {
	case graph.NodeLabel:
		return co.compileExprColFromName(sel, node.Val)
	case graph.NodeStr:
		// Quoted strings are column references (the dot-notation case
		// needs quotes because GraphQL identifiers can't contain dots).
		// v1 has no string literals in expressions; if one is needed
		// later, { str: "..." } remains available as an escape hatch.
		return co.compileExprColFromName(sel, node.Val)
	case graph.NodeNum:
		ex := newExpOp(OpLiteral)
		ex.Lit = ExpLit{Val: node.Val, ValType: ValNum}
		return ex, nil
	case graph.NodeVar:
		ex := newExpOp(OpLiteral)
		ex.Lit = ExpLit{Val: node.Val, ValType: ValVar}
		return ex, nil
	case graph.NodeBool:
		ex := newExpOp(OpLiteral)
		ex.Lit = ExpLit{Val: node.Val, ValType: ValBool}
		return ex, nil
	}

	// Otherwise: object with exactly one operator key.
	if node.Type != graph.NodeObj || len(node.Children) != 1 {
		return nil, fmt.Errorf("expr: each expression node must be a column (bare identifier or quoted string), a number, a $var, or an object with exactly one operator key — got %s with %d children",
			parserTypeName(node.Type), len(node.Children))
	}

	child := node.Children[0]
	key := strings.ToLower(child.Name)

	switch key {
	case "col":
		return co.compileExprCol(sel, child)
	case "num", "str":
		return compileExprLiteral(child, key)
	case "var":
		return compileExprVar(child)
	case "add":
		return co.compileExprVariadic(sel, child, OpAdd, depth, 2)
	case "sub":
		return co.compileExprVariadic(sel, child, OpSub, depth, 2)
	case "mul":
		return co.compileExprVariadic(sel, child, OpMul, depth, 2)
	case "div":
		return co.compileExprVariadic(sel, child, OpDiv, depth, 2)
	case "mod":
		return co.compileExprVariadic(sel, child, OpMod, depth, 2)
	case "neg":
		return co.compileExprUnary(sel, child, OpNeg, depth)
	case "coalesce":
		return co.compileExprVariadic(sel, child, OpCoalesce, depth, 1)
	case "nullif":
		return co.compileExprVariadic(sel, child, OpNullIf, depth, 2)
	case "case":
		return co.compileExprCase(sel, child, depth)
	case "cast":
		return co.compileExprCast(sel, child, depth)
	case "sum":
		return co.compileExprAgg(sel, child, OpAggSum, depth)
	case "avg":
		return co.compileExprAgg(sel, child, OpAggAvg, depth)
	case "min":
		return co.compileExprAgg(sel, child, OpAggMin, depth)
	case "max":
		return co.compileExprAgg(sel, child, OpAggMax, depth)
	case "count":
		return co.compileExprAgg(sel, child, OpAggCount, depth)
	}

	return nil, fmt.Errorf("expr: unknown operator key %q", child.Name)
}

func (co *Compiler) compileExprCol(sel *Select, node *graph.Node) (*Exp, error) {
	if node.Type != graph.NodeStr && node.Type != graph.NodeLabel {
		return nil, fmt.Errorf("expr.col: expected column name, got %s", parserTypeName(node.Type))
	}
	return co.compileExprColFromName(sel, node.Val)
}

// compileExprColFromName resolves a column name string (from either the
// bare-leaf shorthand `price` or the explicit `{ col: "price" }` wrapper)
// to an OpColRef expression. Dot-notation routes to a related table
// (e.g. "product.standardcost"); relationship traversal is delegated to
// compileExprRelCol which supports single-hop and multi-hop belongs-to
// chains up to exprMaxRelHops.
func (co *Compiler) compileExprColFromName(sel *Select, name string) (*Exp, error) {
	if name == "" {
		return nil, errors.New("expr: empty column name")
	}
	if i := strings.IndexByte(name, '.'); i > 0 {
		return co.compileExprRelCol(sel, name[:i], name[i+1:])
	}
	col, err := sel.Ti.GetColumn(name)
	if err != nil {
		return nil, fmt.Errorf("expr.col: %w", err)
	}
	if col.Blocked {
		return nil, fmt.Errorf("expr.col: column %q is blocked", name)
	}
	ex := newExpOp(OpColRef)
	ex.Left.Col = col
	ex.Left.Table = col.Table
	return ex, nil
}

// exprMaxRelHops caps the number of FK hops a relationship-qualified
// expr.col may traverse. More hops means more nested correlated
// subqueries, which some planners struggle to flatten — and deep chains
// usually indicate a schema modeling smell. Three hops is generous for
// practical schemas (e.g. AdventureWorks's salesorderdetail → product
// goes through specialofferproduct — two hops).
const exprMaxRelHops = 3

// compileExprRelCol resolves a relationship-qualified column reference.
// Syntax: { col: "<relname>.<colname>" }. The schema's relationship
// graph may reach the target table in one or more FK hops — each hop
// becomes one nested correlated subquery at render time. Every hop
// must be RelOneToOne (scalar belongs-to); one-to-many dereference is
// rejected because a scalar expression can't consume a list.
//
// Single-hop SQL (one FK):
//
//	(SELECT rt.col FROM rt WHERE rt.pk = lt.fk)
//
// Multi-hop SQL (two FKs, e.g. A → B → C):
//
//	(SELECT C.col FROM C
//	 WHERE C.pk = (
//	   SELECT B.fk_to_c FROM B
//	   WHERE B.pk = A.fk_to_b
//	 ))
//
// Composite FKs at any hop become AND-joined equality predicates in
// that hop's WHERE.
func (co *Compiler) compileExprRelCol(sel *Select, relName, colName string) (*Exp, error) {
	if relName == "" || colName == "" {
		return nil, fmt.Errorf("expr.col: empty relationship or column name in %q", relName+"."+colName)
	}

	// FindPath resolves from → to via the schema's relationship graph.
	// Each element in the returned slice represents one FK hop.
	paths, err := co.s.FindPath(sel.Ti.Name, relName, "")
	if err != nil {
		return nil, fmt.Errorf("expr.col: no relationship %q from %q: %w", relName, sel.Ti.Name, err)
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("expr.col: no relationship path from %q to %q", sel.Ti.Name, relName)
	}
	if len(paths) > exprMaxRelHops {
		return nil, fmt.Errorf("expr.col: relationship %q from %q needs %d hops, exceeds limit of %d",
			relName, sel.Ti.Name, len(paths), exprMaxRelHops)
	}

	// Validate every hop is a scalar belongs-to. Any one-to-many hop
	// means the chain can yield multiple target rows — nonsensical for
	// a scalar expression.
	for i, path := range paths {
		if path.Rel != sdata.RelOneToOne {
			return nil, fmt.Errorf("expr.col: hop %d of relationship %q from %q has cardinality %s — scalar expressions require belongs-to at every hop",
				i+1, relName, sel.Ti.Name, path.Rel)
		}
		if path.LC.Array || path.RC.Array {
			return nil, fmt.Errorf("expr.col: hop %d of relationship %q from %q uses an array column — scalar expressions require scalar FKs",
				i+1, relName, sel.Ti.Name)
		}
	}

	// Target column lives on the LAST hop's RT.
	lastHop := paths[len(paths)-1]
	col, err := lastHop.RT.GetColumn(colName)
	if err != nil {
		return nil, fmt.Errorf("expr.col: column %q not found on related table %q: %w", colName, lastHop.RT.Name, err)
	}
	if col.Blocked {
		return nil, fmt.Errorf("expr.col: column %q on %q is blocked", colName, lastHop.RT.Name)
	}

	ex := newExpOp(OpColRef)
	ex.Left.Col = col
	ex.Left.Table = col.Table
	ex.RelPath = make([]sdata.DBRel, len(paths))
	for i, p := range paths {
		ex.RelPath[i] = sdata.PathToRel(p)
	}
	return ex, nil
}

func compileExprLiteral(node *graph.Node, kind string) (*Exp, error) {
	switch kind {
	case "num":
		if node.Type != graph.NodeNum {
			return nil, fmt.Errorf("expr.num: expected numeric literal, got %s", parserTypeName(node.Type))
		}
		ex := newExpOp(OpLiteral)
		ex.Lit = ExpLit{Val: node.Val, ValType: ValNum}
		return ex, nil
	case "str":
		if node.Type != graph.NodeStr {
			return nil, fmt.Errorf("expr.str: expected string literal, got %s", parserTypeName(node.Type))
		}
		ex := newExpOp(OpLiteral)
		ex.Lit = ExpLit{Val: node.Val, ValType: ValStr}
		return ex, nil
	}
	return nil, fmt.Errorf("expr: internal error — unknown literal kind %q", kind)
}

func compileExprVar(node *graph.Node) (*Exp, error) {
	if node.Type != graph.NodeStr && node.Type != graph.NodeVar && node.Type != graph.NodeLabel {
		return nil, fmt.Errorf("expr.var: expected variable name, got %s", parserTypeName(node.Type))
	}
	ex := newExpOp(OpLiteral)
	ex.Lit = ExpLit{Val: node.Val, ValType: ValVar}
	return ex, nil
}

// compileExprVariadic builds a node from a list of child expressions.
// minArgs is the minimum number of children the op accepts (e.g. add/mul
// need at least 2; coalesce accepts 1+; nullif requires exactly 2).
func (co *Compiler) compileExprVariadic(sel *Select, node *graph.Node, op ExpOp, depth, minArgs int) (*Exp, error) {
	children, err := co.collectExprChildren(sel, node, depth+1)
	if err != nil {
		return nil, err
	}
	if len(children) < minArgs {
		return nil, fmt.Errorf("expr.%s: requires at least %d arguments, got %d", opLowerName(op), minArgs, len(children))
	}
	if op == OpDiv || op == OpMod || op == OpNullIf {
		if len(children) != 2 {
			return nil, fmt.Errorf("expr.%s: requires exactly 2 arguments, got %d", opLowerName(op), len(children))
		}
	}
	ex := newExpOp(op)
	ex.Children = append(ex.Children[:0], children...)
	return ex, nil
}

func (co *Compiler) compileExprUnary(sel *Select, node *graph.Node, op ExpOp, depth int) (*Exp, error) {
	// Accept any expression node (bare leaf or nested operator object) —
	// compileExprNode handles both forms. E.g. { neg: price } is valid.
	child, err := co.compileExprNode(sel, node, depth+1)
	if err != nil {
		return nil, err
	}
	ex := newExpOp(op)
	ex.Children = append(ex.Children[:0], child)
	return ex, nil
}

// collectExprChildren handles both list-form (`mul: [...]`) and the
// degenerate single-object form. Variadic operators always expect a list.
func (co *Compiler) collectExprChildren(sel *Select, node *graph.Node, depth int) ([]*Exp, error) {
	if node.Type != graph.NodeList {
		return nil, fmt.Errorf("expr: expected list of arguments, got %s", parserTypeName(node.Type))
	}
	out := make([]*Exp, 0, len(node.Children))
	for _, ch := range node.Children {
		c, err := co.compileExprNode(sel, ch, depth)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// compileExprCase parses a CASE expression. Shape:
//
//	{ case: [{ when: <bool-expr>, then: <scalar-expr> }, ...], else: <scalar-expr> }
//
// The else key sits as a sibling of `case` at the parent object level —
// callers pass us only the `case` node, so we look up the parent's CMap
// for `else`. To keep the grammar self-contained, we accept both forms:
//
//	{ case: { arms: [...], else: ... } }     (nested form)
//	{ case: [{ when: ..., then: ... }, ...] } (flat form, no else)
//
// In v1 we only support the nested form to keep parsing unambiguous.
func (co *Compiler) compileExprCase(sel *Select, node *graph.Node, depth int) (*Exp, error) {
	if node.Type != graph.NodeObj {
		return nil, fmt.Errorf("expr.case: expected object with `arms` and optional `else`, got %s", parserTypeName(node.Type))
	}
	var armsNode, elseNode *graph.Node
	for _, ch := range node.Children {
		switch strings.ToLower(ch.Name) {
		case "arms":
			armsNode = ch
		case "else":
			elseNode = ch
		default:
			return nil, fmt.Errorf("expr.case: unknown key %q (expected `arms` or `else`)", ch.Name)
		}
	}
	if armsNode == nil || armsNode.Type != graph.NodeList || len(armsNode.Children) == 0 {
		return nil, errors.New("expr.case.arms: must be a non-empty list of {when, then} objects")
	}

	ex := newExpOp(OpCase)
	ex.CaseArms = make([]CaseArm, 0, len(armsNode.Children))

	for i, armNode := range armsNode.Children {
		if armNode.Type != graph.NodeObj {
			return nil, fmt.Errorf("expr.case.arms[%d]: expected {when, then} object", i)
		}
		var whenNode, thenNode *graph.Node
		for _, ch := range armNode.Children {
			switch strings.ToLower(ch.Name) {
			case "when":
				whenNode = ch
			case "then":
				thenNode = ch
			default:
				return nil, fmt.Errorf("expr.case.arms[%d]: unknown key %q (expected `when` or `then`)", i, ch.Name)
			}
		}
		if whenNode == nil || thenNode == nil {
			return nil, fmt.Errorf("expr.case.arms[%d]: requires both `when` and `then`", i)
		}

		// Compile the boolean predicate via the existing where-clause path.
		// The when clause uses the same syntax as `where`: { col: { gt: 5 } }.
		// compileBaseExpNode's entry expects the root node to be nameless
		// (representing the whole expression value); whenNode has Name="when"
		// which would otherwise be interpreted as a column name. Wrap its
		// children in an unnamed object so the where-parser descends
		// correctly into the condition body.
		wrapper := &graph.Node{
			Type:     graph.NodeObj,
			Children: whenNode.Children,
		}
		stack := util.NewStackInf()
		whenExp, _, err := co.compileBaseExpNode("", sel.Ti, stack, wrapper, false)
		if err != nil {
			return nil, fmt.Errorf("expr.case.arms[%d].when: %w", i, err)
		}

		// Compile the scalar then-branch via our recursive descent.
		thenExp, err := co.compileExprNode(sel, thenNode, depth+1)
		if err != nil {
			return nil, fmt.Errorf("expr.case.arms[%d].then: %w", i, err)
		}

		ex.CaseArms = append(ex.CaseArms, CaseArm{When: whenExp, Then: thenExp})
	}

	if elseNode != nil {
		elseExp, err := co.compileExprNode(sel, elseNode, depth+1)
		if err != nil {
			return nil, fmt.Errorf("expr.case.else: %w", err)
		}
		ex.Else = elseExp
	}

	return ex, nil
}

// compileExprCast parses { cast: { expr: <scalar-expr>, as: "<sql-type>" } }.
func (co *Compiler) compileExprCast(sel *Select, node *graph.Node, depth int) (*Exp, error) {
	if node.Type != graph.NodeObj {
		return nil, fmt.Errorf("expr.cast: expected object with `expr` and `as`, got %s", parserTypeName(node.Type))
	}
	var exprNode, asNode *graph.Node
	for _, ch := range node.Children {
		switch strings.ToLower(ch.Name) {
		case "expr":
			exprNode = ch
		case "as":
			asNode = ch
		default:
			return nil, fmt.Errorf("expr.cast: unknown key %q (expected `expr` or `as`)", ch.Name)
		}
	}
	if exprNode == nil || asNode == nil {
		return nil, errors.New("expr.cast: requires both `expr` and `as`")
	}
	if asNode.Type != graph.NodeStr {
		return nil, fmt.Errorf("expr.cast.as: expected string SQL type, got %s", parserTypeName(asNode.Type))
	}
	if !isAllowedCastType(asNode.Val) {
		return nil, fmt.Errorf("expr.cast.as: type %q is not allowed (numeric, integer, bigint, decimal, real, double precision, text)", asNode.Val)
	}

	child, err := co.compileExprNode(sel, exprNode, depth+1)
	if err != nil {
		return nil, err
	}
	ex := newExpOp(OpCast)
	ex.CastType = asNode.Val
	ex.Children = append(ex.Children[:0], child)
	return ex, nil
}

// compileExprAgg parses { sum|avg|min|max|count: <scalar-expr> }.
// Aggregate-of-expression nodes are only legal at the immediate top level
// of a non-aggregate's expr argument, used for ratio-of-aggregates like
// div(expr: { num_: { sum: { col: "..." } }, den: ... }). The validator
// enforces this.
func (co *Compiler) compileExprAgg(sel *Select, node *graph.Node, op ExpOp, depth int) (*Exp, error) {
	// Accept any expression node (bare leaf or nested operator object).
	// { sum: price } and { sum: { mul: [...] } } are both valid.
	child, err := co.compileExprNode(sel, node, depth+1)
	if err != nil {
		return nil, err
	}
	ex := newExpOp(op)
	ex.Children = append(ex.Children[:0], child)
	return ex, nil
}

// validateExprTree walks the compiled expression tree and enforces:
//   - boolean ops (OpAnd/OpEquals/...) appear only inside CaseArm.When
//   - arithmetic ops have only scalar children (column refs, literals,
//     other arithmetic, coalesce/nullif/cast/case/agg-of-expr)
//   - column references resolve to numeric types when used under
//     arithmetic ops (unless wrapped in OpCast)
//   - column references are present in the role's column allowlist
//   - tree depth and total node count stay within limits
//
// allowedCols is the set of column names the current role may read on
// sel.Ti. If nil, the role check is skipped (caller decides).
func validateExprTree(ex *Exp, ti sdata.DBTable, allowedCols map[string]struct{}) error {
	if ex == nil {
		return errors.New("expr: nil expression")
	}
	v := exprValidator{ti: ti, allowedCols: allowedCols}
	if err := v.walk(ex, 0, false); err != nil {
		return err
	}
	if v.nodes > exprMaxNodes {
		return fmt.Errorf("expr: tree has %d nodes, exceeds limit of %d", v.nodes, exprMaxNodes)
	}
	return nil
}

type exprValidator struct {
	ti          sdata.DBTable
	allowedCols map[string]struct{}
	nodes       int
}

// walk descends the expression tree. underArith is true when the parent
// is an arithmetic op (Add/Sub/Mul/Div/Mod/Neg) — in that case, leaf
// columns must be numeric and literal types must be numeric.
func (v *exprValidator) walk(ex *Exp, depth int, underArith bool) error {
	if ex == nil {
		return errors.New("expr: nil sub-expression")
	}
	if depth > exprMaxDepth {
		return fmt.Errorf("expr: nesting exceeds maximum depth of %d", exprMaxDepth)
	}
	v.nodes++
	if v.nodes > exprMaxNodes {
		return fmt.Errorf("expr: tree has more than %d nodes", exprMaxNodes)
	}

	switch ex.Op {
	case OpColRef:
		// Role allowlist check applies to columns on the CURRENT table.
		// Relationship-qualified refs (ex.RelPath non-empty) reference a
		// column on a related table (possibly via multiple hops) — the
		// role's allowlist for those other tables is not available here
		// without threading Compiler + role name through the validator.
		// v1 trade-off: trust the relationship graph (which respects
		// table-level Block configs) and defer per-column allowlist
		// enforcement on related tables to a follow-up.
		if len(ex.RelPath) == 0 && v.allowedCols != nil {
			if _, ok := v.allowedCols[ex.Left.Col.Name]; !ok {
				return fmt.Errorf("expr: column %q is not in the allowlist for this role", strings.ToLower(ex.Left.Col.Name))
			}
		}
		if underArith && !isNumericSQLType(ex.Left.Col.Type) {
			return fmt.Errorf("expr: column %q has type %q which is not numeric — wrap in cast",
				ex.Left.Col.Name, ex.Left.Col.Type)
		}
		return nil

	case OpLiteral:
		if underArith && ex.Lit.ValType != ValNum && ex.Lit.ValType != ValVar {
			return fmt.Errorf("expr: literal of type %d is not numeric under an arithmetic operator", ex.Lit.ValType)
		}
		return nil

	case OpAdd, OpSub, OpMul, OpDiv, OpMod, OpNeg:
		for _, c := range ex.Children {
			if err := v.walk(c, depth+1, true); err != nil {
				return err
			}
		}
		return nil

	case OpCoalesce, OpNullIf:
		for _, c := range ex.Children {
			if err := v.walk(c, depth+1, underArith); err != nil {
				return err
			}
		}
		return nil

	case OpCast:
		// Cast resets the arithmetic context — its child can be anything,
		// the cast handles type conversion. The cast's RESULT is treated
		// as numeric (or whatever the target type is) by the parent.
		for _, c := range ex.Children {
			if err := v.walk(c, depth+1, false); err != nil {
				return err
			}
		}
		return nil

	case OpCase:
		if len(ex.CaseArms) == 0 {
			return errors.New("expr.case: must have at least one when/then arm")
		}
		for i, arm := range ex.CaseArms {
			// when is a boolean predicate — validate column refs within it
			// against the allowlist, but skip arithmetic-context checks.
			if arm.When == nil {
				return fmt.Errorf("expr.case.arms[%d]: missing when", i)
			}
			if err := v.walkBool(arm.When, depth+1); err != nil {
				return err
			}
			if arm.Then == nil {
				return fmt.Errorf("expr.case.arms[%d]: missing then", i)
			}
			if err := v.walk(arm.Then, depth+1, underArith); err != nil {
				return err
			}
		}
		if ex.Else != nil {
			if err := v.walk(ex.Else, depth+1, underArith); err != nil {
				return err
			}
		}
		return nil

	case OpAggSum, OpAggAvg, OpAggMin, OpAggMax, OpAggCount:
		// Aggregate-of-expression: child is a scalar tree. The aggregate
		// itself is numeric in the parent context.
		for _, c := range ex.Children {
			if err := v.walk(c, depth+1, underArith); err != nil {
				return err
			}
		}
		return nil
	}

	// Anything that lands here is a boolean/comparison op leaking into
	// scalar context — that's an AST-discipline violation.
	return fmt.Errorf("expr: operator %s is not valid in a scalar expression", ex.Op)
}

// walkBool walks a boolean sub-tree (used inside CaseArm.When). It
// collects column refs for the role allowlist check but does not enforce
// numeric typing.
func (v *exprValidator) walkBool(ex *Exp, depth int) error {
	if ex == nil {
		return nil
	}
	if depth > exprMaxDepth {
		return fmt.Errorf("expr: when-clause nesting exceeds maximum depth of %d", exprMaxDepth)
	}
	v.nodes++
	if v.nodes > exprMaxNodes {
		return fmt.Errorf("expr: tree has more than %d nodes", exprMaxNodes)
	}
	if v.allowedCols != nil && ex.Left.Col.Name != "" {
		if _, ok := v.allowedCols[ex.Left.Col.Name]; !ok {
			return fmt.Errorf("expr.case.when: column %q is not in the allowlist for this role", strings.ToLower(ex.Left.Col.Name))
		}
	}
	for _, c := range ex.Children {
		if err := v.walkBool(c, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// isNumericSQLType returns true when the given SQL type string represents
// a numeric type that can participate in arithmetic. Matches against the
// most common numeric type names across all supported dialects.
func isNumericSQLType(typ string) bool {
	t := strings.ToLower(strings.TrimSpace(typ))
	// Strip parameters like "decimal(10,2)" or "numeric(38)".
	if i := strings.IndexByte(t, '('); i >= 0 {
		t = strings.TrimSpace(t[:i])
	}
	switch t {
	case "int", "int2", "int4", "int8",
		"integer", "bigint", "smallint", "tinyint", "mediumint",
		"numeric", "decimal", "dec",
		"real", "float", "float4", "float8",
		"double", "double precision",
		"money", "smallmoney",
		"number": // Oracle
		return true
	}
	// Catch types like "decimal(10,2)" stripped above, plus precision-encoded
	// forms like "numeric_38_2" used by some introspection paths.
	if strings.HasPrefix(t, "numeric") || strings.HasPrefix(t, "decimal") ||
		strings.HasPrefix(t, "float") || strings.HasPrefix(t, "double") {
		return true
	}
	return false
}

// isAllowedCastType allowlists the SQL types accepted by `cast: { as: ... }`.
// Keeping this constrained avoids users sneaking arbitrary type strings
// (which would land in CAST(x AS <user-string>) — even though not direct
// SQL injection, it's a needless surface for engine-specific behavior).
func isAllowedCastType(typ string) bool {
	t := strings.ToLower(strings.TrimSpace(typ))
	switch t {
	case "numeric", "decimal",
		"integer", "int", "bigint", "smallint",
		"real", "float", "double precision",
		"text", "varchar", "char",
		"boolean", "bool",
		"date", "timestamp", "timestamptz":
		return true
	}
	return false
}

// opLowerName returns the user-facing operator key for a scalar Exp op,
// used in error messages so users can correlate failures to their input.
func opLowerName(op ExpOp) string {
	switch op {
	case OpAdd:
		return "add"
	case OpSub:
		return "sub"
	case OpMul:
		return "mul"
	case OpDiv:
		return "div"
	case OpMod:
		return "mod"
	case OpNeg:
		return "neg"
	case OpCoalesce:
		return "coalesce"
	case OpNullIf:
		return "nullif"
	case OpCase:
		return "case"
	case OpCast:
		return "cast"
	case OpLiteral:
		return "literal"
	case OpColRef:
		return "col"
	case OpAggSum:
		return "sum"
	case OpAggAvg:
		return "avg"
	case OpAggMin:
		return "min"
	case OpAggMax:
		return "max"
	case OpAggCount:
		return "count"
	}
	return op.String()
}

// parserTypeName maps a graph.ParserType to a short user-facing name
// for error messages. Avoids leaking internal enum string output.
func parserTypeName(t graph.ParserType) string {
	switch t {
	case graph.NodeStr:
		return "string"
	case graph.NodeNum:
		return "number"
	case graph.NodeBool:
		return "boolean"
	case graph.NodeObj:
		return "object"
	case graph.NodeList:
		return "list"
	case graph.NodeVar:
		return "variable"
	case graph.NodeLabel:
		return "label"
	}
	return "unknown"
}
