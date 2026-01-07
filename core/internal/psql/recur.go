package psql

import (
	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

func (c *compilerContext) renderRecursiveBaseSelect(sel *qcode.Select) {
	c.renderRecursiveCTE(sel)
	c.w.WriteString(`SELECT `)
	c.renderDistinctOn(sel)
	c.renderRecursiveColumns(sel)
	c.w.WriteString(` FROM (SELECT * FROM `)
	c.quoted("__rcte_" + sel.Table)
	// Use dialect-specific recursive offset syntax
	c.dialect.RenderRecursiveOffset(c)
	c.w.WriteString(`) `)
	c.alias(sel.Table)
	c.renderRecursiveGroupBy(sel)
	c.renderLimit(sel)
}

func (c *compilerContext) renderRecursiveCTE(sel *qcode.Select) {
	c.w.WriteString(`WITH `)
	// Some databases (Oracle) don't use the RECURSIVE keyword
	if c.dialect.RequiresRecursiveKeyword() {
		c.w.WriteString(`RECURSIVE `)
	}
	c.quoted("__rcte_" + sel.Table)
	// Oracle/MSSQL require explicit column alias list in recursive CTEs
	if c.dialect.RequiresRecursiveCTEColumnList() {
		c.w.WriteString(`(`)
		c.renderRecursiveCTEColumnList(sel)
		c.w.WriteString(`)`)
	}
	c.w.WriteString(` AS (`)
	c.renderCursorCTE(sel)
	c.renderRecursiveSelect(sel)
	c.w.WriteString(`) `)
}

func (c *compilerContext) renderRecursiveCTEColumnList(sel *qcode.Select) {
	for i, col := range sel.BCols {
		if i != 0 {
			c.w.WriteString(`, `)
		}
		c.quoted(col.Col.Name)
	}
}

func (c *compilerContext) renderRecursiveSelect(sel *qcode.Select) {
	psel := &c.qc.Selects[sel.ParentID]

	// Some databases (SQLite) need extra wrapping for recursive select
	if c.dialect.WrapRecursiveSelect() {
		c.w.WriteString(`SELECT * FROM (SELECT `)
	} else {
		c.w.WriteString(`(SELECT `)
	}
	c.renderRecursiveBaseColumns(sel)
	c.renderFrom(psel)
	c.w.WriteString(` WHERE `)
	// Use dialect-specific WHERE clause for recursive CTE anchor
	// Oracle/MSSQL: inline parent's WHERE expression (no outer scope correlation)
	// Postgres/MySQL: correlate with outer scope table alias
	if !c.dialect.RenderRecursiveAnchorWhere(c, psel, sel.Ti, sel.Ti.PrimaryCol.Name) {
		// Default: correlate with outer scope (works in Postgres/MySQL)
		c.w.WriteString(`(`)
		c.colWithTable(sel.Table, sel.Ti.PrimaryCol.Name)
		c.w.WriteString(`) = (`)
		c.colWithTableID(psel.Table, psel.ID, sel.Ti.PrimaryCol.Name)
		c.w.WriteString(`)`)
	}
	c.w.WriteString(` `)
	// Use dialect-specific LIMIT 1 syntax
	c.dialect.RenderRecursiveLimit1(c)
	c.w.WriteString(`) UNION ALL `)

	c.w.WriteString(`SELECT `)
	c.renderRecursiveBaseColumns(sel)
	c.renderFrom(sel)
	c.w.WriteString(`, `)
	c.quoted("__rcte_" + sel.Rel.Right.Ti.Name)
	c.renderWhere(sel)
}

func (c *compilerContext) renderRecursiveBaseColumns(sel *qcode.Select) {
	i := 0

	for _, col := range sel.BCols {
		if i != 0 {
			c.w.WriteString(`, `)
		}
		c.colWithTable(col.Col.Table, col.Col.Name)
		i++
	}
}

func (c *compilerContext) renderRecursiveColumns(sel *qcode.Select) {
	i := 0
	for _, f := range sel.Fields {
		if i != 0 {
			c.w.WriteString(", ")
		}
		if f.FieldFilter.Exp != nil {
			c.w.WriteString(`(CASE WHEN `)
			c.renderExp(sel.Ti, f.FieldFilter.Exp, false)
			c.w.WriteString(` THEN `)
		}
		if f.Type == qcode.FieldTypeFunc {
			c.renderFieldFunction(sel, f)
		} else {
			c.colWithTable(f.Col.Table, f.Col.Name)
		}
		if f.FieldFilter.Exp != nil {
			c.w.WriteString(` ELSE null END)`)
		}
		c.alias(f.FieldName)
		i++
	}
	if sel.Typename {
		if i != 0 {
			c.w.WriteString(`, `)
		}
		c.renderTypename(sel)
	}
}

func (c *compilerContext) renderRecursiveGroupBy(sel *qcode.Select) {
	if !sel.GroupCols {
		return
	}

	i := 0
	for _, f := range sel.Fields {
		if i != 0 {
			c.w.WriteString(`, `)
		}
		if f.Type != qcode.FieldTypeCol {
			continue
		}
		if i == 0 {
			c.w.WriteString(` GROUP BY `)
		}
		c.colWithTable(sel.Table, f.Col.Name)
		i++
	}
}
