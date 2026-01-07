package psql

import (
	"strconv"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func (c *compilerContext) renderColumns(sel *qcode.Select) {
	i := 0
	for _, f := range sel.Fields {
		if i != 0 {
			c.w.WriteString(", ")
		}

		switch {
		case f.SkipRender == qcode.SkipTypeNulled:
			c.w.WriteString(`NULL`)
		case f.Type == qcode.FieldTypeFunc:
			c.renderFuncColumn(sel, f)
		case f.Type == qcode.FieldTypeCol:
			c.renderStdColumn(sel, f)
		default:
			continue
		}
		c.alias(f.FieldName)
		i++
	}
	if sel.Typename {
		if i != 0 {
			c.w.WriteString(`, `)
		}
		c.renderTypename(sel)
		i++
	}
	c.renderJoinColumns(sel, i)
}

func (c *compilerContext) renderStdColumn(sel *qcode.Select, f qcode.Field) {
	if f.FieldFilter.Exp != nil {
		c.w.WriteString(`(CASE WHEN `)
		c.renderExp(sel.Ti, f.FieldFilter.Exp, false)
		c.w.WriteString(` THEN `)
	}



	c.colWithTableID(sel.Table, sel.ID, f.Col.Name)

	if f.FieldFilter.Exp != nil {
		c.w.WriteString(` ELSE null END)`)
	}
}

func (c *compilerContext) renderFuncColumn(sel *qcode.Select, f qcode.Field) {
	c.colWithTableID(sel.Table, sel.ID, f.FieldName)
}

func (c *compilerContext) renderJoinColumns(sel *qcode.Select, n int) {
	i := n
	for _, cid := range sel.Children {
		csel := &c.qc.Selects[cid]

		if csel.SkipRender == qcode.SkipTypeDrop ||
			csel.SkipRender == qcode.SkipTypeRemote {
			continue
		}

		if i != 0 {
			c.w.WriteString(", ")
		}

		// TODO: log what and why this is being skipped
		switch csel.SkipRender {
		case qcode.SkipTypeUserNeeded, qcode.SkipTypeBlocked,
			qcode.SkipTypeNulled:

			c.w.WriteString(`NULL`)
			c.alias(csel.FieldName)

			if sel.Paging.Cursor {
				c.w.WriteString(`, NULL`)
				c.alias(sel.FieldName)
			}

		default:
			switch csel.Rel.Type {
			case sdata.RelPolymorphic:
				c.renderUnionColumn(sel, csel)

			default:
				if !c.dialect.SupportsLateral() {
					// Dialects without LATERAL support use inline child rendering
					if c.dialect.RequiresJSONQueryWrapper() {
						// Wrap with JSON_QUERY to prevent double-escaping since
						// MariaDB treats JSON as LONGTEXT and json_object would escape it
						c.w.WriteString(`JSON_QUERY(`)
						c.dialect.RenderInlineChild(c, c, sel, csel)
						c.w.WriteString(`, '$')`)
						c.alias(csel.FieldName)
					} else if c.dialect.Name() == "mssql" {
						// MSSQL needs its own inline child rendering
						c.dialect.RenderInlineChild(c, c, sel, csel)
						c.alias(csel.FieldName)
					} else {
						c.renderInlineChild(csel)
						c.alias(csel.FieldName)
					}
				} else {
					c.colWithTableID("__sj", csel.ID, "json")
					c.alias(csel.FieldName)
				}
			}

			// return the cursor for the this child selector as part of the parents json
			// Only for LATERAL supporting dialects - SQLite/MariaDB handle cursor differently
			if csel.Paging.Cursor && (c.dialect.SupportsLateral() || c.dialect.Name() == "sqlite" || c.dialect.Name() == "mariadb") {
				c.w.WriteString(`, `)
				c.colWithTableID("__sj", csel.ID, "__cursor")
				c.w.WriteString(` AS `)
				c.w.WriteString(csel.FieldName)
				c.w.WriteString(`_cursor`)
			}
		}
		i++
	}
	// when no columns are rendered for certain databases
	if c.dialect.RequiresNullOnEmptySelect() && i == 0 {
		c.w.WriteString(`NULL`)
	}
}

func (c *compilerContext) renderUnionColumn(sel, csel *qcode.Select) {
	c.w.WriteString(`(CASE `)
	for _, cid := range csel.Children {
		usel := &c.qc.Selects[cid]

		c.w.WriteString(`WHEN `)
		c.colWithTableID(sel.Table, sel.ID, csel.Rel.Left.Col.FKeyCol)
		c.w.WriteString(` = `)
		c.squoted(usel.Table)
		c.w.WriteString(` THEN `)

		switch usel.SkipRender {
		case qcode.SkipTypeUserNeeded, qcode.SkipTypeBlocked,
			qcode.SkipTypeNulled:
			c.w.WriteString(`NULL `)
		default:
			if c.dialect.SupportsLateral() {
				c.colWithTableID("__sj", usel.ID, "json")
				c.w.WriteString(` `)
			} else if c.dialect.RequiresJSONQueryWrapper() {
				// MariaDB needs simplified inline child rendering
				c.w.WriteString(`JSON_QUERY(`)
				c.dialect.RenderInlineChild(c, c, sel, usel)
				c.w.WriteString(`, '$') `)
			} else if c.dialect.Name() == "mssql" {
				// MSSQL needs its own inline child rendering for polymorphic unions
				c.dialect.RenderInlineChild(c, c, sel, usel)
				c.w.WriteString(` `)
			} else {
				c.renderInlineChild(usel)
				c.w.WriteString(` `)
			}
		}
	}
	c.w.WriteString(`END)`)
	c.alias(csel.FieldName)
}

func (c *compilerContext) renderBaseColumns(sel *qcode.Select) {
	i := 0
	for _, col := range sel.BCols {
		if i != 0 {
			c.w.WriteString(`, `)
		}
		// Handle JSON table columns in SQLite
		if c.dialect.Name() == "sqlite" && (sel.Ti.Type == "json" || sel.Ti.Type == "jsonb") {
			c.w.WriteString(`json_extract(`)
			c.quoted("__sr_" + strconv.Itoa(int(sel.ID)))
			c.w.WriteString(`."value", '$."`)
			c.w.WriteString(col.Col.Name)
			c.w.WriteString(`"') AS `)
			c.quoted(col.Col.Name)
		} else {
			c.colWithTable(col.Col.Table, col.Col.Name)
		}
		i++
	}

	// render only function columns
	for _, f := range sel.Fields {
		if f.Type != qcode.FieldTypeFunc {
			continue
		}
		if i != 0 {
			c.w.WriteString(`, `)
		}

		if f.FieldFilter.Exp != nil {
			c.w.WriteString(`(CASE WHEN `)
			c.renderExp(sel.Ti, f.FieldFilter.Exp, false)
			c.w.WriteString(` THEN `)
		}
		c.renderFieldFunction(sel, f)

		if f.FieldFilter.Exp != nil {
			c.w.WriteString(` ELSE null END)`)
		}
		c.alias(f.FieldName)
		i++
	}
}

func (c *compilerContext) renderTypename(sel *qcode.Select) {
	c.squoted(sel.Table)
	// Oracle uppercases all quoted identifiers, so we need to use uppercase
	// to match when the column is later referenced
	if c.dialect.Name() == "oracle" {
		c.w.WriteString(` AS "__TYPENAME"`)
	} else {
		c.w.WriteString(` AS "__typename"`)
	}
}

func (c *compilerContext) renderJSONFields(sel *qcode.Select) {
	i := 0
	for _, f := range sel.Fields {
		if i != 0 {
			c.w.WriteString(", ")
		}

		if c.dialect.Name() == "sqlite" {
			c.squoted(f.FieldName)
			c.w.WriteString(", ")

			if f.Col.Array {
				c.w.WriteString(`(CASE WHEN json_valid(`)
				c.w.WriteString(`__sr_`)
				int32String(c.w, sel.ID)
				c.w.WriteString(`.`)
				c.w.WriteString(f.FieldName)
				c.w.WriteString(`) THEN json(`)
				c.w.WriteString(`__sr_`)
				int32String(c.w, sel.ID)
				c.w.WriteString(`.`)
				c.w.WriteString(f.FieldName)
				c.w.WriteString(`) ELSE `)
				c.w.WriteString(`__sr_`)
				int32String(c.w, sel.ID)
				c.w.WriteString(`.`)
				c.w.WriteString(f.FieldName)
				c.w.WriteString(` END)`)
			} else {
				c.w.WriteString(`__sr_`)
				int32String(c.w, sel.ID)
				c.w.WriteString(`.`)
				c.w.WriteString(f.FieldName)
			}

		} else if c.dialect.Name() == "oracle" {
			// Check if this is a boolean function that needs conversion from NUMBER to JSON boolean
			isBoolFunc := f.Type == qcode.FieldTypeFunc && f.Func.Type == "boolean"
			if isBoolFunc {
				// For Oracle, convert numeric 0/1 to JSON boolean true/false
				c.w.WriteString(`KEY '`)
				c.w.WriteString(f.FieldName)
				c.w.WriteString(`' VALUE CASE WHEN `)
				c.quoted("__sr_" + strconv.Itoa(int(sel.ID)))
				c.w.WriteString(`.`)
				c.quoted(f.FieldName)
				c.w.WriteString(` = 1 THEN 'true' ELSE 'false' END FORMAT JSON`)
			} else {
				c.dialect.RenderJSONField(c, f.FieldName, "__sr_"+strconv.Itoa(int(sel.ID)), f.FieldName, false, false)
			}
		} else if c.dialect.Name() == "mariadb" {
			// MariaDB: use dialect method with isJSON flag for JSON columns
			isJSON := f.Col.Type == "json" || f.Col.Array
			c.dialect.RenderJSONField(c, f.FieldName, "__sr_"+strconv.Itoa(int(sel.ID)), f.FieldName, false, isJSON)
		} else if c.dialect.Name() == "mssql" {
			// Check if this is a boolean function that needs conversion from BIT to JSON boolean
			isBoolFunc := f.Type == qcode.FieldTypeFunc && f.Func.Type == "boolean"
			if isBoolFunc {
				// For MSSQL, convert BIT 0/1 to JSON boolean true/false
				// Use CASE WHEN with string literals that FOR JSON PATH will interpret correctly
				c.w.WriteString(`CASE WHEN `)
				c.quoted("__sr_" + strconv.Itoa(int(sel.ID)))
				c.w.WriteString(`.`)
				c.quoted(f.FieldName)
				c.w.WriteString(` = 1 THEN CAST(1 AS BIT) ELSE CAST(0 AS BIT) END AS `)
				c.quoted(f.FieldName)
			} else {
				isJSON := f.Col.Type == "json" || f.Col.Array
				c.dialect.RenderJSONField(c, f.FieldName, "__sr_"+strconv.Itoa(int(sel.ID)), f.FieldName, false, isJSON)
			}
		} else {
			c.renderJSONField(f.FieldName, sel.ID)
		}
		i++
	}

	if sel.Typename {
		if i != 0 {
			c.w.WriteString(`, `)
		}
		if c.dialect.Name() == "oracle" {
			c.dialect.RenderJSONField(c, "__typename", "__sr_"+strconv.Itoa(int(sel.ID)), "__typename", false, false)
		} else {
			c.renderJSONField("__typename", sel.ID)
		}
		i++
	}

	for _, cid := range sel.Children {
		csel := &c.qc.Selects[cid]

		if csel.SkipRender == qcode.SkipTypeRemote {
			continue
		}

		if i != 0 {
			c.w.WriteString(", ")
		}

		// TODO: log what and why this is being skipped
		if csel.SkipRender != qcode.SkipTypeNone {
			if c.dialect.Name() == "oracle" {
				c.dialect.RenderJSONField(c, csel.FieldName, "", "", true, false)
			} else {
				c.renderJSONNullField(csel.FieldName)
			}

			if sel.Paging.Cursor {
				c.w.WriteString(", ")
				if c.dialect.Name() == "oracle" {
					c.dialect.RenderJSONField(c, sel.FieldName+`_cursor`, "", "", true, false)
				} else {
					c.renderJSONNullField(sel.FieldName + `_cursor`)
				}
			}

		} else {
			if c.dialect.Name() == "sqlite" {
				c.squoted(csel.FieldName)
				c.w.WriteString(`, json(__sr_`)
				int32String(c.w, sel.ID)
				c.w.WriteString(`.`)
				c.w.WriteString(csel.FieldName)
				c.w.WriteString(`)`)
			} else if c.dialect.Name() == "oracle" {
				// Child selections are nested JSON, need FORMAT JSON to prevent double-escaping
				c.dialect.RenderJSONField(c, csel.FieldName, "__sr_"+strconv.Itoa(int(sel.ID)), csel.FieldName, false, true)
			} else {
				c.renderJSONField(csel.FieldName, sel.ID)
			}

			// return the cursor for the this child selector as part of the parents json
			if csel.Paging.Cursor {
				c.w.WriteString(", ")
				if c.dialect.Name() == "oracle" {
					c.dialect.RenderJSONField(c, csel.FieldName+`_cursor`, "__sr_"+strconv.Itoa(int(sel.ID)), csel.FieldName+`_cursor`, false, false)
				} else {
					c.renderJSONField(csel.FieldName+`_cursor`, sel.ID)
				}
			}
		}
		i++
	}
}

func (c *compilerContext) renderJSONField(name string, selID int32) {
	c.squoted(name)
	c.w.WriteString(`, `)
	c.quoted("__sr_" + strconv.Itoa(int(selID)))
	c.w.WriteString(`.`)
	c.quoted(name)
}

func (c *compilerContext) renderJSONNullField(name string) {
	c.squoted(name)
	c.w.WriteString(`, NULL`)
}
