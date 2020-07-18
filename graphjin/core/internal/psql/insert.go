//nolint:errcheck
package psql

import (
	"errors"
	"fmt"
	"io"

	"github.com/dosco/super-graph/core/internal/qcode"
	"github.com/dosco/super-graph/core/internal/util"
)

func (c *compilerContext) renderInsert(
	w io.Writer, qc *qcode.QCode, vars Variables, ti *DBTableInfo, embedded bool) (uint32, error) {

	insert, ok := vars[qc.ActionVar]
	if !ok {
		return 0, fmt.Errorf("variable '%s' not defined", qc.ActionVar)
	}
	if len(insert) == 0 {
		return 0, fmt.Errorf("variable '%s' is empty", qc.ActionVar)
	}

	io.WriteString(c.w, `WITH "_sg_input" AS (SELECT `)
	c.md.renderParam(c.w, Param{Name: qc.ActionVar, Type: "json"})
	io.WriteString(c.w, ` :: json AS j)`)

	st := util.NewStack()
	st.Push(kvitem{_type: itemInsert, key: ti.Name, val: insert, ti: ti})

	for {
		if st.Len() == 0 {
			break
		}
		if insert[0] == '[' && st.Len() > 1 {
			return 0, errors.New("Nested bulk insert not supported")
		}
		intf := st.Pop()

		switch item := intf.(type) {
		case kvitem:
			if err := c.handleKVItem(st, item); err != nil {
				return 0, err
			}

		case renitem:
			var err error

			switch item._type {
			case itemInsert:
				err = c.renderInsertStmt(qc, w, item, embedded)
			case itemConnect:
				err = c.renderConnectStmt(qc, w, item)
			case itemUnion:
				err = c.renderUnionStmt(w, item)
			}

			if err != nil {
				return 0, err
			}
		}
	}
	io.WriteString(c.w, ` `)

	return 0, nil
}

func (c *compilerContext) renderInsertStmt(qc *qcode.QCode, w io.Writer, item renitem, embedded bool) error {
	ti := item.ti
	jt := item.data
	sk := nestedInsertRelColumnsMap(item.kvitem)

	io.WriteString(c.w, `, `)
	renderCteName(w, item.kvitem)
	io.WriteString(w, ` AS (`)

	io.WriteString(w, `INSERT INTO `)
	quoted(w, ti.Name)
	io.WriteString(w, ` (`)

	rc1, err := c.renderInsertUpdateColumns(qc, jt, ti, sk, false)
	if err != nil {
		return err
	}
	renderNestedInsertRelColumns(w, item.kvitem, false, rc1)

	io.WriteString(w, `)`)

	io.WriteString(w, ` SELECT `)
	rc2, err := c.renderInsertUpdateColumns(qc, jt, ti, sk, true)
	if err != nil {
		return err
	}
	renderNestedInsertRelColumns(w, item.kvitem, true, rc2)

	io.WriteString(w, ` FROM "_sg_input" i`)
	renderNestedInsertRelTables(w, item.kvitem)

	if item.array {
		io.WriteString(w, `, json_populate_recordset`)
	} else {
		io.WriteString(w, `, json_populate_record`)
	}

	io.WriteString(w, `(NULL::`)
	io.WriteString(w, ti.Name)

	if len(item.path) == 0 {
		io.WriteString(w, `, i.j) t`)
	} else {
		io.WriteString(w, `, i.j->`)
		joinPath(w, item.path)
		io.WriteString(w, `) t`)
	}

	if !embedded {
		io.WriteString(w, ` RETURNING *)`)
	}
	return nil
}

func nestedInsertRelColumnsMap(item kvitem) map[string]struct{} {
	sk := make(map[string]struct{}, len(item.items))

	if len(item.items) == 0 {
		if item.relPC != nil && item.relPC.Type == RelOneToMany {
			sk[item.relPC.Right.Col] = struct{}{}
		}
	} else {
		for _, v := range item.items {
			if v.relCP.Type == RelOneToMany {
				sk[v.relCP.Right.Col] = struct{}{}
			}
		}
	}

	return sk
}

func renderNestedInsertRelColumns(w io.Writer, item kvitem, values, colsRendered bool) error {
	// Render columns and values needed to connect current table and the parent table
	if item.relCP != nil && item.relCP.Type == RelOneToOne {
		if colsRendered {
			io.WriteString(w, `, `)
		}
		if values {
			colWithTable(w, item.relCP.Right.Table, item.relCP.Right.Col)
		} else {
			quoted(w, item.relCP.Left.Col)
		}
	}
	// Render columns and values needed to connect parent level with it's children
	// this is for when the parent actually depends on the child level
	// the order of the table rendering if handled upstream
	if len(item.items) == 0 {
		if item.relPC != nil && item.relPC.Type == RelOneToMany {
			if colsRendered {
				io.WriteString(w, `, `)
			}
			if values {
				colWithTable(w, item.relPC.Left.Table, item.relPC.Left.Col)
			} else {
				quoted(w, item.relPC.Right.Col)
			}
		}
	} else {
		// Render columns and values needed by the children of the current level
		// Render child foreign key columns if child-to-parent
		// relationship is one-to-many
		i := 0
		for _, v := range item.items {
			if v.relCP.Type != RelOneToMany {
				continue
			}

			if i != 0 || colsRendered {
				io.WriteString(w, `, `)
			}
			if values {
				if v._ctype > 0 {
					io.WriteString(w, `"_x_`)
					io.WriteString(w, v.relCP.Left.Table)
					io.WriteString(w, `".`)
					quoted(w, v.relCP.Left.Col)
				} else {
					colWithTable(w, v.relCP.Left.Table, v.relCP.Left.Col)
				}
			} else {
				quoted(w, v.relCP.Right.Col)
			}
			i++
		}
	}

	return nil
}

func renderNestedInsertRelTables(w io.Writer, item kvitem) error {
	if item.relCP != nil && item.relCP.Type == RelOneToOne {
		io.WriteString(w, `, `)
		quoted(w, item.relPC.Left.Table)
	}

	if len(item.items) == 0 {
		if item.relPC != nil && item.relPC.Type == RelOneToMany {
			io.WriteString(w, `, `)
			quoted(w, item.relPC.Left.Table)
		}
	} else {
		// Render tables needed to set values if child-to-parent
		// relationship is one-to-many
		for _, v := range item.items {
			if v.relCP.Type == RelOneToMany {
				io.WriteString(w, `, `)
				if v._ctype > 0 {
					io.WriteString(w, `"_x_`)
					io.WriteString(w, v.relCP.Left.Table)
					io.WriteString(w, `"`)
				} else {
					quoted(w, v.relCP.Left.Table)
				}
			}
		}
	}

	return nil
}
