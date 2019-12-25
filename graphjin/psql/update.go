package psql

import (
	"fmt"
	"io"

	"github.com/dosco/super-graph/qcode"
	"github.com/dosco/super-graph/util"
)

func (c *compilerContext) renderUpdate(qc *qcode.QCode, w io.Writer,
	vars Variables, ti *DBTableInfo) (uint32, error) {

	insert, ok := vars[qc.ActionVar]
	if !ok {
		return 0, fmt.Errorf("Variable '%s' not !defined", qc.ActionVar)
	}

	io.WriteString(c.w, `WITH "_sg_input" AS (SELECT '{{`)
	io.WriteString(c.w, qc.ActionVar)
	io.WriteString(c.w, `}}' :: json AS j)`)

	st := util.NewStack()
	st.Push(kvitem{_type: itemUpdate, key: ti.Name, val: insert, ti: ti})

	for {
		if st.Len() == 0 {
			break
		}
		intf := st.Pop()

		switch item := intf.(type) {
		case kvitem:
			if err := c.handleKVItem(st, item); err != nil {
				return 0, err
			}

		case renitem:
			var err error

			// if w := qc.Selects[0].Where; w != nil && w.Op == qcode.OpFalse {
			// 	io.WriteString(c.w, ` WHERE false`)
			// }

			switch item._type {
			case itemUpdate:
				err = c.renderUpdateStmt(w, qc, item)
			// case itemConnect:
			// 	err = c.renderConnectStmt(qc, w, item)
			// case itemDisconnect:
			// 	err = c.renderDisconnectStmt(qc, w, item)
			case itemUnion:
				err = c.renderUpdateUnionStmt(w, item)
			}

			if err != nil {
				return 0, err
			}

		}
	}
	io.WriteString(c.w, ` `)

	return 0, nil
}

func (c *compilerContext) renderUpdateStmt(w io.Writer, qc *qcode.QCode, item renitem) error {
	ti := item.ti
	jt := item.data

	io.WriteString(c.w, `, `)
	renderCteName(c.w, item.kvitem)
	io.WriteString(c.w, ` AS (`)

	io.WriteString(w, `UPDATE `)
	quoted(w, ti.Name)
	io.WriteString(w, ` SET (`)
	renderInsertUpdateColumns(w, qc, jt, ti, nil, false)

	io.WriteString(w, `) = (SELECT `)
	renderInsertUpdateColumns(w, qc, jt, ti, nil, true)

	io.WriteString(w, ` FROM "_sg_input" i, `)

	if item.array {
		io.WriteString(w, `json_populate_recordset`)
	} else {
		io.WriteString(w, `json_populate_record`)
	}

	io.WriteString(w, `(NULL::`)
	io.WriteString(w, ti.Name)
	io.WriteString(w, `, i.j) t`)

	io.WriteString(w, ` WHERE `)

	if item.id != 0 {
		// Render sql to set id values if child-to-parent
		// relationship is one-to-one
		rel := item.relCP
		io.WriteString(w, `((`)
		colWithTable(w, rel.Left.Table, rel.Left.Col)
		io.WriteString(w, `) = (`)
		colWithTable(w, rel.Right.Table, rel.Right.Col)
		io.WriteString(w, `)`)

		if item.relPC.Type == RelOneToMany {
			if conn, ok := item.data["where"]; ok {
				io.WriteString(w, ` AND `)
				renderWhereFromJSON(w, conn)
			} else if conn, ok := item.data["_where"]; ok {
				io.WriteString(w, ` AND `)
				renderWhereFromJSON(w, conn)
			}
		}
		io.WriteString(w, `)`)

	} else {
		if err := c.renderWhere(&qc.Selects[0], ti); err != nil {
			return err
		}
	}

	io.WriteString(w, `) RETURNING *)`)

	return nil
}

func (c *compilerContext) renderUpdateUnionStmt(w io.Writer, item renitem) error {
	renderCteName(w, item.kvitem)
	io.WriteString(w, ` AS (`)

	i := 0
	for _, v := range item.items {
		if v._type == itemConnect {
			if i == 0 {
				io.WriteString(w, `UPDATE `)
				quoted(w, v.ti.Name)
				io.WriteString(w, ` SET `)
				quoted(w, v.relPC.Right.Col)
				io.WriteString(w, ` = `)
				colWithTable(w, v.relPC.Left.Table, v.relPC.Left.Col)
				io.WriteString(w, ` WHERE `)
			} else {
				io.WriteString(w, ` OR (`)
			}
			if err := renderKVItemWhere(w, v); err != nil {
				return err
			}
			if i != 0 {
				io.WriteString(w, `)`)
			}
			i++
		}
	}
	io.WriteString(w, `)`)

	return nil
}

func (c *compilerContext) renderDelete(qc *qcode.QCode, w io.Writer,
	vars Variables, ti *DBTableInfo) (uint32, error) {
	root := &qc.Selects[0]

	io.WriteString(c.w, `WITH `)
	quoted(c.w, ti.Name)

	io.WriteString(c.w, ` AS (DELETE FROM `)
	quoted(c.w, ti.Name)
	io.WriteString(c.w, ` WHERE `)

	if err := c.renderWhere(root, ti); err != nil {
		return 0, err
	}

	io.WriteString(c.w, ` RETURNING *) `)

	return 0, nil
}
