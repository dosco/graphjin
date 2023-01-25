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
	switch c.ct {
	case "mysql":
		c.w.WriteString(` LIMIT 1, 18446744073709551610`)
	default:
		c.w.WriteString(` OFFSET 1`)
	}
	c.w.WriteString(`) `)
	c.alias(sel.Table)
	c.renderRecursiveGroupBy(sel)
	c.renderLimit(sel)
}

func (c *compilerContext) renderRecursiveCTE(sel *qcode.Select) {
	c.w.WriteString(`WITH RECURSIVE `)
	c.quoted("__rcte_" + sel.Table)
	c.w.WriteString(` AS (`)
	c.renderCursorCTE(sel)
	c.renderRecursiveSelect(sel)
	c.w.WriteString(`) `)
}

func (c *compilerContext) renderRecursiveSelect(sel *qcode.Select) {
	psel := &c.qc.Selects[sel.ParentID]

	c.w.WriteString(`(SELECT `)
	c.renderRecursiveBaseColumns(sel)
	c.renderFrom(psel)
	c.w.WriteString(` WHERE (`)
	c.colWithTable(sel.Table, sel.Ti.PrimaryCol.Name)
	c.w.WriteString(`) = (`)
	c.colWithTableID(psel.Table, psel.ID, sel.Ti.PrimaryCol.Name)
	c.w.WriteString(`) `)
	c.w.WriteString(` LIMIT 1) UNION ALL `)

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
