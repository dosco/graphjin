package psql

import (
	"fmt"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// RenderScalarExp is the exported wrapper that lets dialects delegate
// scalar-expression rendering through the dialect.InlineChildRenderer
// interface (dialects don't have direct access to the unexported
// renderScalarExp). Used by mariadb.renderInlineJSONFields and similar
// inline-child renderers that emit JSON fields without going through
// the standard renderColumns path.
func (c *compilerContext) RenderScalarExp(sel *qcode.Select, ex *qcode.Exp) error {
	return c.renderScalarExp(sel, ex)
}

// renderScalarExp emits the SQL for a scalar expression tree (*Exp with
// arithmetic, literal, column, case, cast, coalesce, nullif, or
// agg-of-expr nodes). Distinct from renderExp in psql/exp.go which
// emits boolean predicates for WHERE clauses.
//
// All output is from controlled sources:
//   - column refs go through colWithTable -> dialect.QuoteIdentifier
//   - literals go through dialect.RenderLiteral (parameterized/squoted)
//   - operator tokens come from dialect.RenderArithOp (allowlist)
//   - casts go through dialect.RenderCast
//
// User strings never reach the SQL stream directly. Always parenthesize
// arithmetic to avoid relying on per-dialect precedence rules.
func (c *compilerContext) renderScalarExp(sel *qcode.Select, ex *qcode.Exp) error {
	if ex == nil {
		return fmt.Errorf("renderScalarExp: nil expression")
	}

	switch ex.Op {
	case qcode.OpColRef:
		if len(ex.RelPath) > 0 {
			return c.renderRelColRef(sel, ex)
		}
		// Table-qualification is dialect-specific: Postgres uses the bare
		// table name (`"products"."id"`) while MariaDB uses the ID-suffixed
		// alias (`"products_0"."id"`). Let the dialect decide via the
		// ColWithTable helper which resolves the correct form; fall back
		// to aliased form when sel is the current selection.
		c.renderScalarColRef(sel, ex)
		return nil

	case qcode.OpLiteral:
		c.dialect.RenderLiteral(c, ex.Lit.Val, ex.Lit.ValType)
		return nil

	case qcode.OpAdd, qcode.OpSub, qcode.OpMul, qcode.OpDiv, qcode.OpMod:
		return c.renderArithChain(sel, ex)

	case qcode.OpNeg:
		if len(ex.Children) != 1 {
			return fmt.Errorf("renderScalarExp: neg requires exactly one operand, got %d", len(ex.Children))
		}
		c.w.WriteString("(-")
		if err := c.renderScalarExp(sel, ex.Children[0]); err != nil {
			return err
		}
		c.w.WriteString(")")
		return nil

	case qcode.OpCoalesce:
		return c.renderScalarFunc(sel, "COALESCE", ex.Children)

	case qcode.OpNullIf:
		if len(ex.Children) != 2 {
			return fmt.Errorf("renderScalarExp: nullif requires exactly 2 operands, got %d", len(ex.Children))
		}
		return c.renderScalarFunc(sel, "NULLIF", ex.Children)

	case qcode.OpCase:
		return c.renderScalarCase(sel, ex)

	case qcode.OpCast:
		if len(ex.Children) != 1 {
			return fmt.Errorf("renderScalarExp: cast requires exactly one operand, got %d", len(ex.Children))
		}
		// dialect.RenderCast wraps the inner SQL with the dialect's
		// CAST syntax (e.g. CAST(x AS numeric) for postgres,
		// TRY_CAST for mssql variants). The val callback emits the
		// inner expression into the same buffer.
		c.dialect.RenderCast(c, func() {
			if err := c.renderScalarExp(sel, ex.Children[0]); err != nil {
				// the val callback can't return errors; the surrounding
				// renderScalarExp call already validated structure, so
				// unexpected nil children here are a programmer bug.
				c.w.WriteString("/* renderScalarExp cast error: ")
				c.w.WriteString(err.Error())
				c.w.WriteString(" */")
			}
		}, ex.CastType)
		return nil

	case qcode.OpAggSum:
		return c.renderAggOfExp(sel, "SUM", ex)
	case qcode.OpAggAvg:
		return c.renderAggOfExp(sel, "AVG", ex)
	case qcode.OpAggMin:
		return c.renderAggOfExp(sel, "MIN", ex)
	case qcode.OpAggMax:
		return c.renderAggOfExp(sel, "MAX", ex)
	case qcode.OpAggCount:
		return c.renderAggOfExp(sel, "COUNT", ex)
	}

	return fmt.Errorf("renderScalarExp: unsupported op %s in scalar context", ex.Op)
}

// renderArithChain writes "(child0 OP child1 OP child2 ...)" with the
// dialect-specific operator. Always parenthesized so precedence is
// explicit and dialect-neutral.
func (c *compilerContext) renderArithChain(sel *qcode.Select, ex *qcode.Exp) error {
	if len(ex.Children) < 2 {
		return fmt.Errorf("renderScalarExp: arithmetic op %s requires at least 2 operands, got %d", ex.Op, len(ex.Children))
	}
	opTok, err := c.dialect.RenderArithOp(ex.Op)
	if err != nil {
		return err
	}
	c.w.WriteString("(")
	for i, ch := range ex.Children {
		if i > 0 {
			c.w.WriteString(" ")
			c.w.WriteString(opTok)
			c.w.WriteString(" ")
		}
		if err := c.renderScalarExp(sel, ch); err != nil {
			return err
		}
	}
	c.w.WriteString(")")
	return nil
}

// renderScalarFunc writes "NAME(child0, child1, ...)" — used for the
// standard SQL functions COALESCE and NULLIF that all 7 SQL dialects
// support identically.
func (c *compilerContext) renderScalarFunc(sel *qcode.Select, name string, children []*qcode.Exp) error {
	c.w.WriteString(name)
	c.w.WriteString("(")
	for i, ch := range children {
		if i > 0 {
			c.w.WriteString(", ")
		}
		if err := c.renderScalarExp(sel, ch); err != nil {
			return err
		}
	}
	c.w.WriteString(")")
	return nil
}

// renderScalarCase writes "CASE WHEN ... THEN ... [ELSE ...] END" using
// the existing boolean expression renderer for the WHEN sub-tree and
// renderScalarExp for the THEN / ELSE branches.
func (c *compilerContext) renderScalarCase(sel *qcode.Select, ex *qcode.Exp) error {
	if len(ex.CaseArms) == 0 {
		return fmt.Errorf("renderScalarExp: case requires at least one arm")
	}
	c.w.WriteString("(CASE")
	for _, arm := range ex.CaseArms {
		c.w.WriteString(" WHEN ")
		// when sub-tree uses the boolean predicate renderer; pass
		// sel.Ti so column refs inside the predicate are resolved
		// against the current selection's table.
		c.renderExp(sel.Ti, arm.When, false)
		c.w.WriteString(" THEN ")
		if err := c.renderScalarExp(sel, arm.Then); err != nil {
			return err
		}
	}
	if ex.Else != nil {
		c.w.WriteString(" ELSE ")
		if err := c.renderScalarExp(sel, ex.Else); err != nil {
			return err
		}
	}
	c.w.WriteString(" END)")
	return nil
}

// renderScalarColRef picks the correct table-qualified form for a
// local column reference based on which dialect is active. Postgres and
// most SQL dialects use the bare table name when there is no table
// aliasing in the FROM clause; MariaDB aliases the base table as
// `<name>_<sel.ID>` and needs the suffix.
func (c *compilerContext) renderScalarColRef(sel *qcode.Select, ex *qcode.Exp) {
	if sel != nil && ex.Left.Col.Table == sel.Table && dialectAliasesBaseTable(c.dialect) {
		c.colWithTableID(sel.Table, sel.ID, ex.Left.Col.Name)
		return
	}
	c.colWithTable(ex.Left.Col.Table, ex.Left.Col.Name)
}

// dialectAliasesBaseTable reports whether the dialect aliases the base
// table with a `_<sel.ID>` suffix in its FROM clause. Both MariaDB and
// MSSQL use inline subqueries that alias the table (e.g.
// `FROM products AS products_0`), while Postgres/MySQL/Oracle/SQLite/
// Snowflake use the bare name. Encoded here because the dialect
// interface doesn't expose this distinction directly.
func dialectAliasesBaseTable(d interface{ Name() string }) bool {
	switch d.Name() {
	case "mariadb", "mssql":
		return true
	}
	return false
}

// renderRelColRef emits one or more nested correlated scalar subqueries
// for a relationship-qualified column reference. A single-hop path
// produces one subquery (direct FK lookup); an N-hop path nests N
// subqueries, each projecting the next hop's join column and filtered
// by the previous hop's join condition.
//
// Single-hop (direct FK from A to B, target column B.col):
//
//	(SELECT B.col FROM B WHERE B.pk = A.fk)
//
// Two-hop (A → B → C, target column C.col):
//
//	(SELECT C.col FROM C
//	 WHERE C.pk = (
//	   SELECT B.fk_to_c FROM B
//	   WHERE B.pk = A.fk_to_b))
//
// Composite FKs at any hop contribute AND-joined equality predicates.
// Aliasing matches each dialect's table-aliasing convention — MariaDB
// and MSSQL use <name>_<sel.ID> for the outermost reference; the
// correlated subquery reaches back to that alias in the innermost WHERE.
func (c *compilerContext) renderRelColRef(sel *qcode.Select, ex *qcode.Exp) error {
	if len(ex.RelPath) == 0 {
		return fmt.Errorf("renderScalarExp: relationship-qualified col ref has empty RelPath")
	}
	// Render outermost-first. The outer hop's target is where the column
	// lives; each subsequent inner level projects the join FK for the
	// NEXT outer hop.
	path := ex.RelPath

	// Count open parens so we know how many to close at the end. One
	// per hop (each hop opens a "(SELECT ...").
	closeParens := len(path)

	// Outermost: SELECT <target column> FROM <last hop's RT> WHERE <last hop's RC> = (inner)
	lastHop := path[len(path)-1]
	c.w.WriteString("(SELECT ")
	c.colWithTable(ex.Left.Col.Table, ex.Left.Col.Name)
	c.w.WriteString(" FROM ")
	c.writeQualifiedTable(lastHop.Right.Ti)
	c.w.WriteString(" WHERE ")
	c.colWithTable(lastHop.Right.Col.Table, lastHop.Right.Col.Name)
	c.w.WriteString(" = ")

	// Walk inner hops, outer-to-inner. Each intermediate hop opens
	// another subquery that projects the OUTER hop's LC (the column on
	// THIS hop's RT that joins to the next hop up).
	for i := len(path) - 2; i >= 0; i-- {
		hop := path[i]        // inner hop
		outerHop := path[i+1] // the hop that consumes this subquery's output
		c.w.WriteString("(SELECT ")
		c.colWithTable(outerHop.Left.Col.Table, outerHop.Left.Col.Name)
		c.w.WriteString(" FROM ")
		c.writeQualifiedTable(hop.Right.Ti)
		c.w.WriteString(" WHERE ")
		c.colWithTable(hop.Right.Col.Table, hop.Right.Col.Name)
		c.w.WriteString(" = ")
		if i > 0 {
			// Another level to go — continue nesting.
			continue
		}
		// Innermost: bind to the outer query (sel.Ti — the current
		// selection's table). The dialect may alias the outer table
		// as <name>_<sel.ID>; emit accordingly.
		c.writeOuterColRef(sel, hop.Left.Col)
		for _, pair := range hop.ExtraPairs {
			c.w.WriteString(" AND ")
			c.colWithTable(pair.R.Table, pair.R.Name)
			c.w.WriteString(" = ")
			c.writeOuterColRef(sel, pair.L)
		}
	}

	// Single-hop: the outermost hop IS the innermost. Write the outer
	// column reference directly.
	if len(path) == 1 {
		c.writeOuterColRef(sel, path[0].Left.Col)
		for _, pair := range path[0].ExtraPairs {
			c.w.WriteString(" AND ")
			c.colWithTable(pair.R.Table, pair.R.Name)
			c.w.WriteString(" = ")
			c.writeOuterColRef(sel, pair.L)
		}
	}

	for i := 0; i < closeParens; i++ {
		c.w.WriteString(")")
	}
	return nil
}

// writeQualifiedTable emits `"schema"."table"` (or just `"table"` if
// the table has no schema). Used by the correlated-subquery FROM clause.
func (c *compilerContext) writeQualifiedTable(ti sdata.DBTable) {
	if ti.Schema != "" {
		c.quoted(ti.Schema)
		c.w.WriteString(".")
	}
	c.quoted(ti.Name)
}

// writeOuterColRef emits a reference to a column on the OUTER (current)
// selection's table, respecting each dialect's aliasing convention so
// the correlated subquery binds to the right outer alias.
func (c *compilerContext) writeOuterColRef(sel *qcode.Select, col sdata.DBColumn) {
	if sel != nil && col.Table == sel.Table && dialectAliasesBaseTable(c.dialect) {
		c.colWithTableID(sel.Table, sel.ID, col.Name)
		return
	}
	c.colWithTable(col.Table, col.Name)
}

// renderAggOfExp writes "AGG(<scalar-expression>)" for OpAggSum / etc.
// Used by the ratio-of-aggregates path where the field's outer "function"
// is just an alias and the actual aggregates live inside the expression.
func (c *compilerContext) renderAggOfExp(sel *qcode.Select, aggName string, ex *qcode.Exp) error {
	if len(ex.Children) != 1 {
		return fmt.Errorf("renderScalarExp: %s requires exactly one operand, got %d", aggName, len(ex.Children))
	}
	c.w.WriteString(aggName)
	c.w.WriteString("(")
	if err := c.renderScalarExp(sel, ex.Children[0]); err != nil {
		return err
	}
	c.w.WriteString(")")
	return nil
}
