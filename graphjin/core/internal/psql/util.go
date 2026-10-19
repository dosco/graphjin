package psql

import (
	"bytes"
	"strconv"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/dialect"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func (c *compilerContext) alias(alias string) {
	c.dialect.RenderTableAlias(c, alias)
}

func (c *compilerContext) aliasWithID(alias string, id int32) {
	c.dialect.RenderTableAlias(c, alias+"_"+strconv.Itoa(int(id)))
}

func (c *compilerContext) colWithTableID(table string, id int32, col string) {
	if id >= 0 {
		c.quoted(table + "_" + strconv.Itoa(int(id)))
	} else {
		c.quoted(table)
	}
	c.w.WriteString(`.`)
	if id >= 0 {
		table += "_" + strconv.Itoa(int(id))
	}
	c.quotedColumn(table, col)
}

func (c *compilerContext) table(sel *qcode.Select, schema, table string, alias bool) {
	if _, mapped := c.dialect.(dialect.NameMapSetter); mapped {
		c.dialect.RenderTableName(c, sel, schema, table)
	} else {
		if schema != "" {
			c.quoted(schema)
			c.w.WriteString(".")
		}
		c.quoted(table)
	}
	if alias {
		c.dialect.RenderTableAlias(c, table)
	}
}

func (c *compilerContext) colWithTable(table, col string) {
	qualifier := table
	if c.mutation {
		if _, mapped := c.dialect.(dialect.NameMapSetter); mapped {
			for _, m := range c.qc.Mutates {
				if c.columnScope.Name == table {
					qualifier = c.columnScope.SQLName()
					break
				}
				if m.Ti.Name == table {
					qualifier = m.Ti.SQLName()
					break
				}
			}
		}
	}
	c.quoted(qualifier)
	c.w.WriteString(`.`)
	c.quotedColumn(table, col)
}

func (c *compilerContext) quotedColumn(table, column string) {
	if quoter, ok := c.dialect.(dialect.ScopedColumnQuoter); ok {
		schema := ""
		lookup := table
		owner := c.columnScope

		if c.qc != nil {
			if i := strings.LastIndexByte(table, '_'); i > 0 {
				if id, err := strconv.Atoi(table[i+1:]); err == nil && id >= 0 && id < len(c.qc.Selects) {
					sel := &c.qc.Selects[id]
					if sel.Ti.Name == table[:i] || sel.Table == table[:i] {
						schema, lookup = sel.Ti.Schema, sel.Ti.Name
						owner = sel.Ti
					}
				}
			}
		}
		if schema == "" && owner.Name == lookup {
			schema = owner.Schema
		}
		if schema == "" && c.qc != nil {
			// Config pins and GraphQL schema directives select a schema even when
			// discovery contains same-named tables in other schemas.
			for _, sel := range c.qc.Selects {
				if sel.Ti.Name != lookup && sel.Table != lookup {
					continue
				}
				if schema != "" && schema != sel.Ti.Schema {
					schema = ""
					break
				}
				schema = sel.Ti.Schema
				lookup = sel.Ti.Name
				owner = sel.Ti
			}
		}
		if owner.Name == lookup && owner.Schema == schema {
			if col, ok := owner.ColumnExists(column); ok {
				c.quoted(col.SQLName())
				return
			}
		}
		quoted, err := quoter.QuoteColumn(schema, lookup, column)
		if err != nil {
			c.SetError(err)
			return
		}
		c.w.WriteString(quoted)
		return
	}
	c.quoted(column)
}

func (c *compilerContext) quoted(identifier string) {
	c.w.WriteString(c.dialect.QuoteIdentifier(identifier))
}

func (c *compilerContext) squoted(identifier string) {
	c.w.WriteByte('\'')
	c.w.WriteString(identifier)
	c.w.WriteByte('\'')
}

func int32String(w *bytes.Buffer, val int32) {
	w.WriteString(strconv.FormatInt(int64(val), 10))
}

func (c *compilerContext) Write(s string) (int, error) {
	return c.w.WriteString(s)
}

func (c *compilerContext) WriteString(s string) (int, error) {
	return c.w.WriteString(s)
}

func (c *compilerContext) AddParam(p dialect.Param) string {
	pp := Param{
		Name:        p.Name,
		Type:        p.Type,
		IsArray:     p.IsArray,
		IsNotNull:   p.IsNotNull,
		WrapInArray: p.WrapInArray,
	}
	c.renderParam(pp)
	return ""
}

func (c *compilerContext) Quote(s string) {
	c.quoted(s)
}

func (c *compilerContext) SetError(err error) {
	if c.err == nil {
		c.err = err
	}
}

func (c *compilerContext) ColWithTable(table, col string) {
	c.colWithTable(table, col)
}

func (c *compilerContext) RenderJSONFields(sel *qcode.Select) {
	c.renderJSONFields(sel)
}

// InlineChildRenderer interface implementations

func (c *compilerContext) RenderTable(sel *qcode.Select, schema, table string, alias bool) {
	c.table(sel, schema, table, alias)
}

func (c *compilerContext) RenderJoin(join qcode.Join) {
	c.renderJoin(join)
}

func (c *compilerContext) RenderLimit(sel *qcode.Select) {
	c.dialect.RenderLimit(c, sel)
}

func (c *compilerContext) RenderOrderBy(sel *qcode.Select) {
	c.dialect.RenderOrderBy(c, sel)
}

func (c *compilerContext) RenderWhereExp(psel, sel *qcode.Select, ex interface{}) {
	if exp, ok := ex.(*qcode.Exp); ok {
		c.renderExp(sel.Ti, exp, false)
	}
}

func (c *compilerContext) RenderExp(ti sdata.DBTable, ex *qcode.Exp) {
	c.renderExp(ti, ex, false)
}

func (c *compilerContext) RenderFieldFunction(sel *qcode.Select, f qcode.Field) {
	c.renderFieldFunction(sel, f)
}

func (c *compilerContext) RenderWindowFunction(sel *qcode.Select, f qcode.Field, table string) {
	if f.WindowFunc != qcode.WindowFuncNone {
		c.renderFieldWindowFunction(sel, f, table)
	} else if len(f.Args) == 1 && f.Args[0].Type == qcode.ArgTypeExpr {
		c.renderFieldExprFunction(sel, f)
	} else {
		c.renderFunctionWithTable(f.Func.Name, f.Args, table)
	}
	c.renderWindowOverWithTable(sel, f, table)
}

func (c *compilerContext) RenderInlineChild(psel, sel *qcode.Select) {
	previous := c.columnScope
	c.columnScope = sel.Ti
	defer func() { c.columnScope = previous }()
	c.dialect.RenderInlineChild(c, c, psel, sel)
}

func (c *compilerContext) RenderDefaultInlineChild(sel *qcode.Select) {
	c.renderInlineChild(sel)
}

func (c *compilerContext) GetChild(id int32) *qcode.Select {
	return &c.qc.Selects[id]
}

func (c *compilerContext) Quoted(s string) {
	c.quoted(s)
}

func (c *compilerContext) Squoted(s string) {
	c.squoted(s)
}

func (c *compilerContext) GetConfigVar(name string) (string, bool) {
	val, ok := c.svars[name]
	return val, ok
}

func (c *compilerContext) GetSecPrefix() string {
	return string(c.pf)
}

func (c *compilerContext) GetRootWithCursor() *qcode.Select {
	for _, id := range c.qc.Roots {
		sel := &c.qc.Selects[id]
		if sel.Paging.Cursor {
			return sel
		}
	}
	return nil
}
