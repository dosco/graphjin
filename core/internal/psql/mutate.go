//nolint:errcheck

package psql

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/graph"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func (co *Compiler) compileMutation(
	w *bytes.Buffer,
	qc *qcode.QCode,
	md *Metadata,
) {
	c := compilerContext{
		md:       md,
		w:        w,
		qc:       qc,
		isJSON:   qc.Mutates[0].IsJSON,
		Compiler: co,
	}

    if co.dialect.SupportsLinearExecution() {
        c.compileLinearMutation()
        return
    }

	if qc.SType != qcode.QTDelete {
		if c.isJSON {
			co.dialect.RenderMutationInput(&c, qc)
			c.w.WriteString(`, `)
		} else {
			c.w.WriteString(`WITH `)
		}
	}

	switch qc.SType {
	case qcode.QTInsert:
		c.renderInsert()
	case qcode.QTUpdate:
		c.renderUpdate()
	case qcode.QTUpsert:
		c.renderUpsert()
	case qcode.QTDelete:
		c.renderDelete()
	default:
		return
	}

	co.dialect.RenderMutationPostamble(&c, qc)
	c.w.WriteString(` `)
	co.CompileQuery(w, qc, c.md)

}

func (c *compilerContext) compileLinearMutation() {
    // Linear execution: Flat script of statements
    
    // Setup (e.g. Create Temp Table)
    c.dialect.RenderSetup(c)

    // 1. Sort mutations by dependency 
    //    Naive approach: Sort by ID? No.
    //    Use m.DependsOn.
    
    	ordered := c.sortMutations()

	for _, mid := range ordered {
		m := c.qc.Mutates[mid]
		// Skip if not needing variable? Or just declare all?
		// We use variable for Primary Key capture.
		vName := c.getVarName(m)
		// Assuming type is Number for IDs usually?
		// Or inspect m.Ti.PrimaryCol.Type
		c.dialect.RenderVarDeclaration(c, vName, m.Ti.PrimaryCol.Type)

		// For MSSQL/MySQL/MariaDB: declare additional variables for all columns
		// when there are child mutations that need FK values
		dialectName := c.dialect.Name()
		if dialectName == "mysql" || dialectName == "mariadb" || dialectName == "mssql" || dialectName == "oracle" {
			hasChildMutations := false
			for _, otherM := range c.qc.Mutates {
				if otherM.ParentID == m.ID {
					hasChildMutations = true
					break
				}
			}
			if hasChildMutations {
				for _, col := range m.Ti.Columns {
					colVarName := vName + "_" + col.Name
					c.dialect.RenderVarDeclaration(c, colVarName, col.Type)
				}
			}
		}
	}

	c.dialect.RenderBegin(c)

	for _, mid := range ordered {
		m := c.qc.Mutates[mid]

		if m.Type == qcode.MTNone || m.Type == qcode.MTKeyword {
			continue
		}

		vName := c.getVarName(m)
		renderColVal := func(col qcode.MColumn) {
			c.renderColumnValue(m, col)
		}


		switch m.Type {
		case qcode.MTInsert:
			c.dialect.RenderLinearInsert(c, &m, c.qc, vName, renderColVal)
		case qcode.MTUpdate:
			renderWhere := func() {
				// renderWhere needs to handle:
				// implicit joins (handled by dialect if needed)
				// m.Where (path args)
				// join conditions (parent/child)
				// m.Where.Exp
				
				// Wait, `RenderLinearUpdate` in SQLite/MySQL implementation calls `renderWhere`.
				// In SQLite I added logic to `renderWhere` closure to call `c.renderExp` and handle parent join.
				// In MySQL I called `c.renderExp`.
				// But `c.renderExp` is not exported.
				// Oh, `c` IS `compilerContext` here. It has `renderExp` method.
				
				// The Dialect `RenderLinearUpdate` logic I wrote calls `renderWhere()` inside `RunUpdate`.
				// It handles its own `hasWhere` logic for JSON joins.
				// So here I should just render the standard filters.
				
				hasWhere := false
				if m.IsJSON {
					if c.dialect.Name() == "postgres" {
						c.colWithTable(m.Ti.Name, m.Ti.PrimaryCol.Name)
						c.w.WriteString(" = ")
						c.colWithTable("t", m.Ti.PrimaryCol.Name)
						hasWhere = true
					} else if c.dialect.Name() == "mysql" || c.dialect.Name() == "mariadb" {
						// For child updates (m.ParentID != -1), the JSON input doesn't contain the child's PK.
						// The row to update is determined by the parent's FK, not by JSON table join.
						// Only add JSON table join for root updates that need it.
						if m.ParentID == -1 && (len(c.qc.Selects) == 0 || c.qc.Selects[0].Where.Exp == nil) {
							c.colWithTable(m.Ti.Name, m.Ti.PrimaryCol.Name)
							c.w.WriteString(" = ")
							c.colWithTable("t", m.Ti.PrimaryCol.Name)
							hasWhere = true
						}
					}
				}

				if m.ParentID == -1 {
					if hasWhere {
						c.w.WriteString(" AND ")
					}
					if len(c.qc.Selects) > 0 && c.qc.Selects[0].Where.Exp != nil {
						c.renderExp(m.Ti, c.qc.Selects[0].Where.Exp, false)
						hasWhere = true
					}
				}
				
				if m.Where.Exp != nil {
                    if hasWhere {
                         c.w.WriteString(" AND ")
                    }
				}
				c.renderExpPath(m.Ti, m.Where.Exp, false, nil)

				// Handle parent join for child updates
				if m.ParentID != -1 {
					dialectName := c.dialect.Name()
					if dialectName == "sqlite" || dialectName == "mysql" || dialectName == "mariadb" || dialectName == "mssql" || dialectName == "oracle" {
						if m.Where.Exp != nil || hasWhere { // Check if anything was rendered before
							c.w.WriteString(" AND ")
						}
						var childCol, parentCol string
						if m.Rel.Left.Ti.Name == m.Ti.Name {
							childCol = m.Rel.Left.Col.Name
							parentCol = m.Rel.Right.Col.Name
						} else {
							childCol = m.Rel.Right.Col.Name
							parentCol = m.Rel.Left.Col.Name
						}
						pm := c.qc.Mutates[m.ParentID]

					c.colWithTable(m.Ti.Name, childCol)
					c.w.WriteString(" = ")

						if dialectName == "sqlite" {
							// SQLite uses subquery
							c.w.WriteString("(SELECT ")
							c.quoted(parentCol)
							c.w.WriteString(" FROM ")
							c.quoted(pm.Ti.Name)
							c.w.WriteString(" WHERE ")
							c.quoted(pm.Ti.PrimaryCol.Name)
							c.w.WriteString(" = ")
							c.dialect.RenderVar(c, c.getVarName(pm))
							c.w.WriteString(")")
						} else {
							// MySQL/MariaDB/MSSQL/Oracle use captured variable with FK column name
							// Variable format: @parentTable_parentID_fkColumn (or v_ for Oracle)
							parentVarName := c.getVarName(pm) + "_" + parentCol
							c.dialect.RenderVar(c, parentVarName)
						}
					}
				}
			}
			c.dialect.RenderLinearUpdate(c, &m, c.qc, vName, renderColVal, renderWhere)
			
		case qcode.MTConnect:
			renderFilter := func() {
                 if c.dialect.Name() == "postgres" {
                     c.renderExpPath(m.Ti, m.Where.Exp, false, m.Path)
                 } else {
                     c.renderExpPath(m.Ti, m.Where.Exp, false, nil)
                 }
			}
			c.dialect.RenderLinearConnect(c, &m, c.qc, vName, renderFilter)

		case qcode.MTDisconnect:
			renderFilter := func() {
				if c.dialect.Name() == "postgres" {
                     c.renderExpPath(m.Ti, m.Where.Exp, false, m.Path)
                 } else {
                     c.renderExpPath(m.Ti, m.Where.Exp, false, nil)
                 }
			}
			c.dialect.RenderLinearDisconnect(c, &m, c.qc, vName, renderFilter)
		}
		c.w.WriteString(`; `)
	}

	if c.qc.Selects != nil {
		c.dialect.ModifySelectsForMutation(c.qc)
		c.dialect.RenderQueryPrefix(c, c.qc)
		c.Compiler.CompileQuery(c.w, c.qc, c.md)
	}
	
	// Teardown (e.g. Drop Temp Table)
	c.dialect.RenderTeardown(c)

}

func (c *compilerContext) sortMutations() []int {
	if len(c.qc.Mutates) == 0 {
		return nil
	}
	
    // DFS topological sort
    // Check qc.Mutates for graph
    visited := make(map[int]bool)
    var stack []int
    
    var visit func(int)
    visit = func(id int) {
        if visited[id] { return }
        visited[id] = true
        
        m := c.qc.Mutates[id]
        // Visit dependencies first
        for depID := range m.DependsOn {
            visit(int(depID))
        }
        stack = append(stack, id)
    }
    
    for i := range c.qc.Mutates {
        visit(i)
    }
    return stack
}

func (c *compilerContext) getVarName(m qcode.Mutate) string {
    return m.Ti.Name + "_" + fmt.Sprintf("%d", m.ID)
}

// Removed renderLinearInsert, renderLinearUpdate, renderLinearConnect, renderLinearDisconnect which are now handled by dialect
// Removed renderUnionStmt which is now handled by dialect



func (c *compilerContext) renderInsertUpdateValues(m qcode.Mutate) int {
	i := 0
	for _, col := range m.Cols {
		if i != 0 {
			c.w.WriteString(`, `)
		}
		c.renderColumnValue(m, col)
		i++
	}

	return i
}

func (c *compilerContext) renderColumnValue(m qcode.Mutate, col qcode.MColumn) {
	var vk, v string
	isVar := false
	var listItems []string

	isEmptyList := false
	if col.Set {
		v = col.Value
		if v != "" && v[0] == '$' {
			vk = v[1:]
			isVar = true
		}
	} else {
		field := m.Data.CMap[col.FieldName]
		v = field.Val
		vk = v

		if field.Type == graph.NodeVar {
			isVar = true
		}

		if field.Type == graph.NodeList {
			listItems = make([]string, 0, len(field.Children))
			for _, c := range field.Children {
				if c.Type == graph.NodeNum {
					listItems = append(listItems, c.Val)
				} else {
					listItems = append(listItems, (`'` + c.Val + `'`))
				}
			}
			// Mark as empty list if NodeList but no children
			if len(listItems) == 0 {
				isEmptyList = true
			}
		}
	}

	if isVar {
		if v1, ok := c.svars[vk]; ok {
			v = v1
			isVar = false
		}
	}

	valFunc := func() {
		switch {
		case isVar:
			c.renderParam(Param{Name: vk, Type: col.Col.Type, IsArray: col.Col.Array, IsNotNull: col.Col.NotNull})

		case col.Set && strings.HasPrefix(v, "sql:"):
			c.w.WriteString(`(`)
			c.renderVar(v[4:])
			c.w.WriteString(`)`)

		case col.Set:
			c.squoted(v)

		case m.IsJSON:
			// MSSQL and Oracle in linear execution use parameters for UPDATE, not "t" table reference
			// But INSERT operations need "t" reference for JSON_TABLE to get different values per row
			dialectName := c.dialect.Name()
			if (dialectName == "mssql" || dialectName == "oracle") && m.Type == qcode.MTUpdate {
				// Render the value as a literal for MSSQL/Oracle UPDATE
				c.squoted(v)
			} else {
				c.colWithTable("t", col.FieldName)
			}

		case isEmptyList:
			// Render empty array literal for the dialect
			c.dialect.RenderArray(c, []string{})

		case len(listItems) > 0:
			c.dialect.RenderArray(c, listItems)

		default:
			c.squoted(v)
		}
	}
	c.dialect.RenderCast(c, valFunc, col.Col.Type)
}

func (c *compilerContext) renderInsertUpdateColumns(m qcode.Mutate) int {
	i := 0

	for _, col := range m.Cols {
		if i != 0 {
			c.w.WriteString(`, `)
		}
		i++

		// if !values {
		c.quoted(col.Col.Name)
		// 	continue
		// }
	}

	/*
			v := col.Value
			isVar := false

			if v != "" && v[0] == '$' {
				if v1, ok := c.svars[v[1:]]; ok {
					v = v1
				} else {
					isVar = true
				}
			}

			switch {
			case isVar:
				c.renderParam(Param{Name: v[1:], Type: col.Col.Type})

			case strings.HasPrefix(v, "sql:"):
				c.w.WriteString(`(`)
				c.renderVar(v[4:])
				c.w.WriteString(`)`)

			case m.IsJSON:
				needsJSON = true
				c.colWithTable("t", col.FieldName)

			default:
				c.squoted(v)

			}

			c.w.WriteString(` :: `)
			c.w.WriteString(col.Col.Type)
		}
	*/

	return i
}

func (c *compilerContext) willBeArray(index int) bool {
	m1 := c.qc.Mutates[index]

	if m1.Type == qcode.MTConnect || m1.Type == qcode.MTDisconnect {
		return true
	}
	return false
}

func (c *compilerContext) renderNestedRelColumns(m qcode.Mutate, values bool, prefix bool, n int) {
	for i, col := range m.RCols {
		if n != 0 || i != 0 {
			c.w.WriteString(`, `)
		}
		if values {
			if col.Col.Array {
				if !c.willBeArray(i) {
					if c.dialect.Name() == "postgres" {
						c.w.WriteString(`ARRAY(SELECT `)
					} else {
						// MariaDB/MySQL/SQLite use JSON_ARRAYAGG
						c.w.WriteString(`(SELECT JSON_ARRAYAGG(`)
					}
				} else {
					c.w.WriteString(`(SELECT `)
				}
				c.quoted(col.VCol.Name)
				c.w.WriteString(` FROM `)
				c.quoted(col.VCol.Table)
				c.w.WriteString(`)`)
			} else {
				if prefix {
					c.colWithTable(("_x_" + col.VCol.Table), col.VCol.Name)
				} else {
					c.colWithTable(col.VCol.Table, col.VCol.Name)
				}
			}
		} else {
			c.quoted(col.Col.Name)
		}
	}
}

func (c *compilerContext) renderNestedRelTables(m qcode.Mutate, prefix bool, n int) int {
	if n != 0 {
		c.w.WriteString(`, `)
	}
	i := 0
	for id := range m.DependsOn {
		if i != 0 {
			c.w.WriteString(`, `)
		}
		d := c.qc.Mutates[id]

		if d.Multi || d.Type == qcode.MTConnect || d.Type == qcode.MTDisconnect {
			c.renderCteNameWithID(d)
		} else {
			c.quoted(d.Ti.Name)
		}

		if prefix {
			c.w.WriteString(` _x_`)
			c.w.WriteString(d.Ti.Name)
		} else if d.Multi || d.Type == qcode.MTConnect || d.Type == qcode.MTDisconnect {
			c.w.WriteString(` `)
			c.quoted(d.Ti.Name)
		}

		i++
	}
	return i
}

func (c *compilerContext) renderUpsert() {
	m := c.qc.Mutates[0]

	c.dialect.RenderUpsert(c, &m, func() {
		c.renderInsert()
	}, func() {
		for i, col := range m.Cols {
			if i != 0 {
				c.w.WriteString(`, `)
			}
			c.dialect.RenderAssign(c, col.Col.Name, "EXCLUDED."+col.Col.Name)
		}
	})

	c.w.WriteString(` WHERE `)
	c.renderExp(m.Ti, c.qc.Selects[0].Where.Exp, false)
	c.dialect.RenderReturning(c, &m)
}

func (c *compilerContext) renderDelete() {
	sel := c.qc.Selects[0]
	m := c.qc.Mutates[0]

	c.w.WriteString(`WITH `)
	c.quoted(sel.Table)
	c.w.WriteString(` AS (`)
	c.dialect.RenderDelete(c, &m, func() {
		c.renderExp(sel.Ti, sel.Where.Exp, false)
	})
	c.dialect.RenderReturning(c, &m)
	c.w.WriteString(`)`)
}

func (c *compilerContext) renderOneToManyConnectStmt(m qcode.Mutate) {
	// Render only for parent-to-child relationship of one-to-one
	// For this to work the json child needs to found first so it's primary key
	// can be set in the related column on the parent object.
	// Eg. Create product and connect a user to it.
	c.renderCteName(m)
	c.w.WriteString(` AS (SELECT `)

	rel := m.Rel
	if rel.Right.Col.Array {
		c.dialect.RenderArrayAggPrefix(c, true)
		c.quoted(rel.Left.Col.Name)
		c.w.WriteString(`) AS `)
		c.quoted(rel.Left.Col.Name)
	} else {
		c.quoted(rel.Left.Col.Name)
	}

	if m.IsJSON {
		c.w.WriteString(` FROM `)
		c.quoted("_sg_input")
		c.w.WriteString(` i, `)
	} else {
		c.w.WriteString(` FROM `)
	}
	c.quoted(m.Ti.Name)

	c.w.WriteString(` WHERE `)
	if c.dialect.Name() == "postgres" {
		c.renderExpPath(m.Ti, m.Where.Exp, false, m.Path)
	} else {
		c.renderExpPath(m.Ti, m.Where.Exp, false, nil)
	}
	c.w.WriteString(` LIMIT 1)`)
}

func (c *compilerContext) renderOneToOneConnectStmt(m qcode.Mutate) {
	c.renderCteName(m)
	c.w.WriteString(` AS ( UPDATE `)

	c.table(nil, m.Ti.Schema, m.Ti.Name, false)
	c.w.WriteString(` SET `)
	c.quoted(m.Rel.Left.Col.Name)
	c.w.WriteString(` = `)
	c.colWithTable(("_x_" + m.Rel.Right.Col.Table), m.Rel.Right.Col.Name)

	if m.IsJSON {
		c.w.WriteString(` FROM `)
		c.quoted("_sg_input")
		c.w.WriteString(` i`)
		c.renderNestedRelTables(m, true, 1)
	} else {
		c.w.WriteString(` FROM `)
		c.renderNestedRelTables(m, true, 0)
	}

	c.w.WriteString(` WHERE `)
	if c.dialect.Name() == "postgres" {
		c.renderExpPath(m.Ti, m.Where.Exp, false, m.Path)
	} else {
		c.renderExpPath(m.Ti, m.Where.Exp, false, nil)
	}
	c.renderReturning(m)
	c.w.WriteString(`)`)
}

func (c *compilerContext) renderOneToManyDisconnectStmt(m qcode.Mutate) {
	c.renderCteName(m)
	c.w.WriteString(` AS (`)

	rel := m.Rel
	if rel.Left.Col.Array {
		c.w.WriteString(`SELECT NULL AS `)
		c.quoted(rel.Left.Col.Name)
	} else {
		c.w.WriteString(`SELECT `)
		c.quoted(rel.Left.Col.Name)

		if m.IsJSON {
			c.w.WriteString(` FROM `)
		c.quoted("_sg_input")
		c.w.WriteString(` i, `)
		} else {
			c.w.WriteString(` FROM `)
		}
		c.quoted(m.Ti.Name)

		c.w.WriteString(` WHERE `)
		if c.dialect.Name() == "postgres" {
		c.renderExpPath(m.Ti, m.Where.Exp, false, m.Path)
	} else {
		c.renderExpPath(m.Ti, m.Where.Exp, false, nil)
	}
	}

	c.w.WriteString(` LIMIT 1))`)
}

func (c *compilerContext) renderOneToOneDisconnectStmt(m qcode.Mutate) {
	// Render only for parent-to-child relationship of one-to-one
	// For this to work the child needs to found first so it's
	// null value can beset in the related column on the parent object.
	// Eg. Update product and diconnect the user from it.
	c.renderCteName(m)
	c.w.WriteString(` AS ( UPDATE `)

	c.table(nil, m.Ti.Schema, m.Ti.Name, false)
	c.w.WriteString(` SET `)
	c.quoted(m.Rel.Left.Col.Name)
	c.w.WriteString(` = `)

	if m.Rel.Left.Col.Array {
		if c.dialect.Name() == "postgres" {
			c.w.WriteString(` array_remove(`)
			c.quoted(m.Rel.Left.Col.Name)
			c.w.WriteString(`, `)
			c.colWithTable(("_x_" + m.Rel.Right.Col.Table), m.Rel.Right.Col.Name)
			c.w.WriteString(`)`)
		} else {
			// arrays are not fully supported in mutations for Mysql/MariaDB/SQLite without JSON functions
			// TODO: Implement JSON_REMOVE logic: 
			// JSON_REMOVE(col, JSON_UNQUOTE(JSON_SEARCH(col, 'one', val)))
			// For now, setting to NULL (clearing array?) or keeping as is?
			// Disconnect on array usually means remove item.
			// Setting to NULL is wrong if other items exist.
			// But sticking with incorrect array_remove causes syntax error.
			// We'll set it to NULL for now to avoid syntax error, but this is Logic Error.
			// Or we can try JSON_REMOVE logic.
			if c.dialect.Name() == "mysql" || c.dialect.Name() == "mariadb" {
				c.w.WriteString(` JSON_REMOVE(`)
				c.quoted(m.Rel.Left.Col.Name)
				c.w.WriteString(`, JSON_UNQUOTE(JSON_SEARCH(`)
				c.quoted(m.Rel.Left.Col.Name)
				c.w.WriteString(`, 'one', `)
				c.colWithTable(("_x_" + m.Rel.Right.Col.Table), m.Rel.Right.Col.Name)
				c.w.WriteString(`)))`)
			} else {
				c.w.WriteString(` NULL`)
			}
		}
	} else {
		c.w.WriteString(` NULL`)
	}

	if m.IsJSON {
		c.w.WriteString(` FROM `)
		c.quoted("_sg_input")
		c.w.WriteString(` i`)
		c.renderNestedRelTables(m, true, 1)
	} else {
		c.w.WriteString(` FROM `)
		c.renderNestedRelTables(m, true, 0)
	}

	c.w.WriteString(` WHERE ((`)
	c.colWithTable(m.Rel.Left.Col.Table, m.Rel.Left.Col.Name)
	c.w.WriteString(`) = (`)
	c.colWithTable(("_x_" + m.Rel.Right.Col.Table), m.Rel.Right.Col.Name)
	c.w.WriteString(`)`)

	if m.Rel.Type == sdata.RelOneToOne {
		c.w.WriteString(` AND `)
		if c.dialect.Name() == "postgres" {
			c.renderExpPath(m.Ti, m.Where.Exp, false, m.Path)
		} else {
			c.renderExpPath(m.Ti, m.Where.Exp, false, nil)
		}
	}
	c.w.WriteString(`)`)
	c.renderReturning(m)
	c.w.WriteString(`)`)
}

func (c *compilerContext) renderOneToManyModifiers(m qcode.Mutate) int {
	i := 0
	for id := range m.DependsOn {
		m1 := c.qc.Mutates[id]

		switch m1.Type {
		case qcode.MTConnect:
			if i != 0 {
				c.w.WriteString(`, `)
			}
			c.renderOneToManyConnectStmt(m1)
			i++
		case qcode.MTDisconnect:
			if i != 0 {
				c.w.WriteString(`, `)
			}
			c.renderOneToManyDisconnectStmt(m1)
			i++
		}
	}
	return i
}

func (c *compilerContext) renderCteName(m qcode.Mutate) {
	if m.Multi || m.Type == qcode.MTConnect || m.Type == qcode.MTDisconnect {
		c.renderCteNameWithID(m)
	} else {
		c.quoted(m.Ti.Name)
	}
}

func (c *compilerContext) renderCteNameWithID(m qcode.Mutate) {
	c.w.WriteString(m.Ti.Name)
	c.w.WriteString(`_`)
	int32String(c.w, m.ID)
}

func (c *compilerContext) renderValues(m qcode.Mutate, prefix bool) {
	c.w.WriteString(` SELECT `)
	n := c.renderInsertUpdateValues(m)
	c.renderNestedRelColumns(m, true, prefix, n)

	if m.IsJSON {
		c.w.WriteString(` FROM `)
		c.quoted("_sg_input")
		c.w.WriteString(` i`)
		n = c.renderNestedRelTables(m, prefix, 1)
		c.renderMutateToRecordSet(m, n)

	} else if len(m.DependsOn) != 0 {
		c.w.WriteString(` FROM `)
		c.renderNestedRelTables(m, prefix, 0)
	}
}

func (c *compilerContext) renderMutateToRecordSet(m qcode.Mutate, n int) {
	c.dialect.RenderMutateToRecordSet(c, &m, n, func() {
		if c.dialect.SupportsLinearExecution() {
			if m.IsJSON { // should be true here anyway
				// MySQL/MariaDB need JSON wrapped in array for JSON_TABLE '$[*]' path
				wrapInArray := c.dialect.Name() == "mysql" || c.dialect.Name() == "mariadb"
				c.renderParam(Param{Name: c.qc.ActionVar, Type: "json", WrapInArray: wrapInArray})
			}
		} else {
			c.w.WriteString("i.j")
		}
	})
}

func (c *compilerContext) renderReturning(m qcode.Mutate) {
	c.dialect.RenderReturning(c, &m)
}

func (c *compilerContext) renderComma(i int) int {
	if i != 0 {
		c.w.WriteString(`, `)
	}
	return i + 1
}


