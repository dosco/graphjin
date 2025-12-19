//nolint:errcheck

package psql

import (
	"bytes"
	"strings"
	"fmt"

	"github.com/dosco/graphjin/core/v3/internal/graph"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
	"github.com/dosco/graphjin/core/v3/internal/util"
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
			c.w.WriteString(`WITH `)
			c.quoted("_sg_input")
			c.w.WriteString(` AS (SELECT `)
			c.renderParam(Param{Name: qc.ActionVar, Type: "json"})
			c.w.WriteString(` :: json AS j), `)
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

	c.renderUnionStmt()
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
	}

	c.dialect.RenderBegin(c)

	for _, mid := range ordered {
		m := c.qc.Mutates[mid]
		// fmt.Fprintf(os.Stderr, "DEBUG Mutation: %d Type: %d\n", mid, m.Type)

		if m.Type == qcode.MTNone || m.Type == qcode.MTKeyword {
			continue
		}

		switch m.Type {
		case qcode.MTInsert:
			c.renderLinearInsert(m)
		case qcode.MTUpdate:
			c.renderLinearUpdate(m)
		case qcode.MTConnect:
			c.renderLinearConnect(m)
		case qcode.MTDisconnect:
			c.renderLinearDisconnect(m)
		}

		c.w.WriteString(`; `)
	}

	if c.qc.Selects != nil {
        // For the final selection, we need to filter by the IDs we just generated.
        // We inject WHERE clauses into the Root Selects.
        for i := range c.qc.Roots {
            selID := c.qc.Roots[i]
            sel := &c.qc.Selects[selID]
            
            var mID int = -1
            for _, mid := range ordered {
                if c.qc.Mutates[mid].Ti.Name == sel.Ti.Name {
                    mID = mid
                    break
                }
            }
            
            if mID != -1 {
                m := c.qc.Mutates[mID]
                pk := m.Ti.PrimaryCol
                varName := c.getVarName(m)
                
                ex := &qcode.Exp{
                    Op: qcode.OpEquals,
                }
                ex.Left.ID = -1
                ex.Left.Col = pk
                ex.Right.ValType = qcode.ValDBVar
                ex.Right.Val = varName
                
                if sel.Where.Exp == nil {
                    sel.Where.Exp = ex
                } else {
                    newEx := &qcode.Exp{
                        Op: qcode.OpAnd,
                        Children: []*qcode.Exp{sel.Where.Exp, ex},
                    }
                    sel.Where.Exp = newEx
                }
            }
        }

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

func (c *compilerContext) renderLinearInsert(m qcode.Mutate) {
	// Insert
	c.dialect.RenderInsert(c, &m, func() {
		i := 0
		for _, col := range m.Cols {
			if i != 0 {
				c.w.WriteString(", ")
			}
			c.quoted(col.Col.Name)
			i++
		}
		// Relationship cols (FKs)
		for _, rcol := range m.RCols {
			if i != 0 {
				c.w.WriteString(", ")
			}
			c.quoted(rcol.Col.Name)
			i++
		}
	})

	varName := c.getVarName(m)
	hasExplicitPK := false

	if c.dialect.SupportsLinearExecution() && m.IsJSON {
		c.w.WriteString(" SELECT ")
	} else {
		c.w.WriteString(" VALUES (")
	}

	i := 0
	for _, col := range m.Cols {
		if i != 0 {
			c.w.WriteString(", ")
		}
		// MySQL uses inline variable assignment (@var := value) for PK capture
		// Oracle uses RETURNING INTO instead, so skip inline assignment for Oracle
		if c.dialect.Name() == "mysql" && col.Col.Name == m.Ti.PrimaryCol.Name {
			c.w.WriteString("@")
			c.w.WriteString(varName)
			c.w.WriteString(" := ")
			c.renderColumnValue(m, col)
			hasExplicitPK = true
		} else {
			c.renderColumnValue(m, col)
		}
		i++
	}
	for _, rcol := range m.RCols {
		if i != 0 {
			c.w.WriteString(", ")
		}
		// Find dependency
		found := false
		for id := range m.DependsOn {
			if c.qc.Mutates[id].Ti.Name == rcol.VCol.Table {
				c.dialect.RenderVar(c, c.getVarName(c.qc.Mutates[id]))
				found = true
				break
			}
		}
		if !found {
			c.w.WriteString("NULL")
		}

		i++
	}

	if c.dialect.SupportsLinearExecution() && m.IsJSON {
		c.w.WriteString(" FROM ")
		c.renderMutateToRecordSet(m, 0)
	} else {
		c.w.WriteString(")")
	}

	// Capture ID only if we don't have an explicit PK (auto-increment case)
	if !hasExplicitPK {
		if c.dialect.Name() == "oracle" {
			c.dialect.RenderIDCapture(c, varName)
			c.w.WriteString("; ")
		} else {
			c.w.WriteString("; ")
			c.dialect.RenderIDCapture(c, varName)
		}
	}
}

func (c *compilerContext) renderLinearUpdate(m qcode.Mutate) {
    c.dialect.RenderUpdate(c, &m, func() {
         i := 0
         for _, col := range m.Cols {
             if i != 0 { c.w.WriteString(", ") }
             c.w.WriteString(col.Col.Name)
             c.w.WriteString(" = ")
             c.renderColumnValue(m, col)
             i++
         }
         
         for _, rcol := range m.RCols {
             if i != 0 { c.w.WriteString(", ") }
             c.w.WriteString(rcol.Col.Name)
             c.w.WriteString(" = ")
             
             found := false
             for id := range m.DependsOn {
                 if c.qc.Mutates[id].Ti.Name == rcol.VCol.Table {
                     c.dialect.RenderVar(c, c.getVarName(c.qc.Mutates[id]))
                     found = true
                     break
                 }
             }
             if !found { c.w.WriteString("NULL") }
             i++
         }

		if i == 0 {
			// No columns to update, render dummy update to keep SQL valid
			// SET id = id  (or primary key)
			c.w.WriteString(m.Ti.PrimaryCol.Name)
			c.w.WriteString(" = ")
			c.colWithTable(m.Ti.Name, m.Ti.PrimaryCol.Name)
		}
    	}, func() {
		if m.IsJSON {
			c.renderMutateToRecordSet(m, 0)
		} 
	}, func() {
		hasWhere := false
		
		// MySQL/Postgres: Add join condition to WHERE clause
		if (c.dialect.Name() == "postgres" || c.dialect.Name() == "mysql") && m.IsJSON {
			c.colWithTable(m.Ti.Name, m.Ti.PrimaryCol.Name)
			c.w.WriteString(" = ")
			c.colWithTable("t", m.Ti.PrimaryCol.Name)
			hasWhere = true
		}

		if m.ParentID == -1 {
			if hasWhere {
				c.w.WriteString(" AND ")
			}
			c.renderExp(m.Ti, c.qc.Selects[0].Where.Exp, false)
		} else if !hasWhere {
			// For nested updates (non-root), if no json join (unlikely here if IsJSON),
			// and no parent (handled above), effectively WHERE 1=1 if nothing else specific.
			// But we usually rely on JOIN/WHERE from input ID matching.
			c.w.WriteString("1=1")
		}
	})
}

func (c *compilerContext) renderLinearConnect(m qcode.Mutate) {
	// SELECT IDs into variable
	c.w.WriteString(`SELECT JSON_ARRAYAGG(`)
	c.colWithTable(m.Ti.Name, m.Rel.Left.Col.Name)
	c.w.WriteString(`) INTO `)
	c.dialect.RenderVar(c, c.getVarName(m))
	
	if m.IsJSON {
		c.w.WriteString(` FROM `)
		c.renderMutateToRecordSet(m, 0)
		c.w.WriteString(`, `)
	} else {
		c.w.WriteString(` FROM `)
	}
	c.quoted(m.Ti.Name)

	c.w.WriteString(` WHERE `)
	// For connect, we join m.Ti (related table) with input (t) or args
	// The Where clause likely contains the join condition if it was generated by qcode
	c.renderExpPath(m.Ti, m.Where.Exp, false, m.Path)
}

func (c *compilerContext) renderLinearDisconnect(m qcode.Mutate) {
	// Disconnect typically updates the related table to NULL out the FK
	// Or if it's an array column on the other side, it removes it.
	
	c.w.WriteString(`SELECT JSON_ARRAYAGG(`)
	c.colWithTable(m.Ti.Name, m.Rel.Left.Col.Name)
	c.w.WriteString(`) INTO `)
	c.dialect.RenderVar(c, c.getVarName(m))

	if m.IsJSON {
		c.w.WriteString(` FROM `)
		c.renderMutateToRecordSet(m, 0)
		c.w.WriteString(`, `)
	} else {
		c.w.WriteString(` FROM `)
	}
	c.quoted(m.Ti.Name)

	c.w.WriteString(` WHERE `)
	c.renderExpPath(m.Ti, m.Where.Exp, false, m.Path)
}

func (c *compilerContext) getVarName(m qcode.Mutate) string {
    return m.Ti.Name + "_" + fmt.Sprintf("%d", m.ID)
}

func (c *compilerContext) renderUnionStmt() {
	for k, cids := range c.qc.MUnions {
		if len(cids) < 2 {
			continue
		}
		c.w.WriteString(`, `)
		c.quoted(k)
		c.w.WriteString(` AS (`)

		i := 0
		for _, id := range cids {
			m := c.qc.Mutates[id]
			if m.Rel.Type == sdata.RelOneToMany &&
				(m.Type == qcode.MTConnect || m.Type == qcode.MTDisconnect) {
				continue
			}
			if i != 0 {
				c.w.WriteString(` UNION ALL `)
			}
			c.w.WriteString(`SELECT * FROM `)
			c.renderCteName(m)
			i++
		}

		c.w.WriteString(`)`)
	}
}



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
	isList := false

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
			items := make([]string, 0, len(field.Children))
			for _, c := range field.Children {
				if c.Type == graph.NodeNum {
					items = append(items, c.Val)
				} else {
					items = append(items, (`'` + c.Val + `'`))
				}
			}
			vk = strings.Join(items, ",")
			isList = true
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
			c.colWithTable("t", col.FieldName)

		case isList:
			c.w.WriteString(`ARRAY [`)
			c.w.WriteString(vk)
			c.w.WriteString(`]`)

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
					c.w.WriteString(`ARRAY(SELECT `)
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
		c.w.WriteString(`ARRAY_AGG(DISTINCT `)
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
	c.renderExpPath(m.Ti, m.Where.Exp, false, m.Path)
	c.w.WriteString(` LIMIT 1)`)
}

func (c *compilerContext) renderOneToOneConnectStmt(m qcode.Mutate) {
	c.renderCteName(m)
	c.w.WriteString(` AS ( UPDATE `)

	c.table(m.Ti.Schema, m.Ti.Name, false)
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
	c.renderExpPath(m.Ti, m.Where.Exp, false, m.Path)
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
		c.renderExpPath(m.Ti, m.Where.Exp, false, m.Path)
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

	c.table(m.Ti.Schema, m.Ti.Name, false)
	c.w.WriteString(` SET `)
	c.quoted(m.Rel.Left.Col.Name)
	c.w.WriteString(` = `)

	if m.Rel.Left.Col.Array {
		c.w.WriteString(` array_remove(`)
		c.quoted(m.Rel.Left.Col.Name)
		c.w.WriteString(`, `)
		c.colWithTable(("_x_" + m.Rel.Right.Col.Table), m.Rel.Right.Col.Name)
		c.w.WriteString(`)`)
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
		c.renderExpPath(m.Ti, m.Where.Exp, false, m.Path)
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
				c.renderParam(Param{Name: c.qc.ActionVar, Type: "json"})
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

func joinPath(w *bytes.Buffer, prefix string, path []string, enableCamelcase bool) {
	w.WriteString(prefix)
	for i := range path {
		w.WriteString(`->`)
		w.WriteString(`'`)
		if enableCamelcase {
			w.WriteString(util.ToCamel(path[i]))
		} else {
			w.WriteString(path[i])
		}
		w.WriteString(`'`)
	}
}
