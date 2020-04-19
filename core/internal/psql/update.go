//nolint:errcheck
package psql

import (
	"errors"
	"fmt"
	"io"

	"github.com/dosco/super-graph/core/internal/qcode"
	"github.com/dosco/super-graph/core/internal/util"
)

func (c *compilerContext) renderUpdate(qc *qcode.QCode, w io.Writer,
	vars Variables, ti *DBTableInfo) (uint32, error) {

	update, ok := vars[qc.ActionVar]
	if !ok {
		return 0, fmt.Errorf("variable '%s' not !defined", qc.ActionVar)
	}
	if len(update) == 0 {
		return 0, fmt.Errorf("variable '%s' is empty", qc.ActionVar)
	}

	io.WriteString(c.w, `WITH "_sg_input" AS (SELECT '{{`)
	io.WriteString(c.w, qc.ActionVar)
	io.WriteString(c.w, `}}' :: json AS j)`)

	st := util.NewStack()
	st.Push(kvitem{_type: itemUpdate, key: ti.Name, val: update, ti: ti})

	for {
		if st.Len() == 0 {
			break
		}
		if update[0] == '[' && st.Len() > 1 {
			return 0, errors.New("Nested bulk update not supported")
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
			case itemConnect:
				err = c.renderConnectStmt(qc, w, item)
			case itemDisconnect:
				err = c.renderDisconnectStmt(qc, w, item)
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

func (c *compilerContext) renderUpdateStmt(w io.Writer, qc *qcode.QCode, item renitem) error {
	ti := item.ti
	jt := item.data
	sk := nestedUpdateRelColumnsMap(item.kvitem)

	io.WriteString(c.w, `, `)
	renderCteName(c.w, item.kvitem)
	io.WriteString(c.w, ` AS (`)

	io.WriteString(w, `UPDATE `)
	quoted(w, ti.Name)
	io.WriteString(w, ` SET (`)
	renderInsertUpdateColumns(w, qc, jt, ti, sk, false)
	renderNestedUpdateRelColumns(w, item.kvitem, false)

	io.WriteString(w, `) = (SELECT `)
	renderInsertUpdateColumns(w, qc, jt, ti, sk, true)
	renderNestedUpdateRelColumns(w, item.kvitem, true)

	io.WriteString(w, ` FROM "_sg_input" i`)
	renderNestedUpdateRelTables(w, item.kvitem)
	io.WriteString(w, `) `)

	if item.id != 0 {
		// Render sql to set id values if child-to-parent
		// relationship is one-to-one
		rel := item.relCP

		io.WriteString(w, `FROM `)
		quoted(w, rel.Right.Table)

		io.WriteString(w, ` WHERE ((`)
		colWithTable(w, rel.Left.Table, rel.Left.Col)
		io.WriteString(w, `) = (`)
		colWithTable(w, rel.Right.Table, rel.Right.Col)
		io.WriteString(w, `)`)

		if item.relPC.Type == RelOneToMany {
			if conn, ok := item.data["where"]; ok {
				io.WriteString(w, ` AND `)
				renderWhereFromJSON(w, item.kvitem, "where", conn)
			} else if conn, ok := item.data["_where"]; ok {
				io.WriteString(w, ` AND `)
				renderWhereFromJSON(w, item.kvitem, "_where", conn)
			}
		}
		io.WriteString(w, `)`)

	} else {
		if qc.Selects[0].Where != nil {
			io.WriteString(w, ` WHERE `)
			if err := c.renderWhere(&qc.Selects[0], ti); err != nil {
				return err
			}
		}
	}

	io.WriteString(w, ` RETURNING `)
	quoted(w, ti.Name)
	io.WriteString(w, `.*)`)

	return nil
}

func nestedUpdateRelColumnsMap(item kvitem) map[string]struct{} {
	sk := make(map[string]struct{}, len(item.items))

	for _, v := range item.items {
		if v._ctype > 0 && v.relCP.Type == RelOneToMany {
			sk[v.relCP.Right.Col] = struct{}{}
		}
	}

	return sk
}

func renderNestedUpdateRelColumns(w io.Writer, item kvitem, values bool) error {
	// Render child foreign key columns if child-to-parent
	// relationship is one-to-many
	for _, v := range item.items {
		if v._ctype > 0 && v.relCP.Type == RelOneToMany {
			if values {
				// if v.relCP.Right.Array {
				// 	io.WriteString(w, `array_diff(`)
				// 	colWithTable(w, v.relCP.Right.Table, v.relCP.Right.Col)
				// 	io.WriteString(w, `, `)
				// }

				if v._ctype > 0 {
					io.WriteString(w, `"_x_`)
					io.WriteString(w, v.relCP.Left.Table)
					io.WriteString(w, `".`)
					quoted(w, v.relCP.Left.Col)
				} else {
					colWithTable(w, v.relCP.Left.Table, v.relCP.Left.Col)
				}

				// if v.relCP.Right.Array {
				// 	io.WriteString(w, `)`)
				// }
			} else {

				quoted(w, v.relCP.Right.Col)

			}
		}
	}

	return nil
}

func renderNestedUpdateRelTables(w io.Writer, item kvitem) error {
	// Render tables needed to set values if child-to-parent
	// relationship is one-to-many
	for _, v := range item.items {
		if v._ctype > 0 && v.relCP.Type == RelOneToMany {
			io.WriteString(w, `, "_x_`)
			io.WriteString(w, v.relCP.Left.Table)
			io.WriteString(w, `"`)
		}
	}

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

	if root.Where == nil {
		return 0, errors.New("'where' clause missing in delete mutation")
	}

	if err := c.renderWhere(root, ti); err != nil {
		return 0, err
	}

	io.WriteString(w, ` RETURNING `)
	quoted(w, ti.Name)
	io.WriteString(w, `.*) `)
	return 0, nil
}
