//nolint:errcheck
package psql

import (
	"errors"
	"fmt"
	"io"

	"github.com/dosco/super-graph/core/internal/qcode"
	"github.com/dosco/super-graph/core/internal/util"
)

func (c *compilerContext) renderInsert(qc *qcode.QCode, w io.Writer,
	vars Variables, ti *DBTableInfo) (uint32, error) {

	insert, ok := vars[qc.ActionVar]
	if !ok {
		return 0, fmt.Errorf("variable '%s' not defined", qc.ActionVar)
	}
	if len(insert) == 0 {
		return 0, fmt.Errorf("variable '%s' is empty", qc.ActionVar)
	}

	io.WriteString(c.w, `WITH "_sg_input" AS (SELECT `)
	if insert[0] == '[' {
		io.WriteString(c.w, `json_array_elements(`)
	}
	io.WriteString(c.w, `'{{`)
	io.WriteString(c.w, qc.ActionVar)
	io.WriteString(c.w, `}}' :: json`)
	if insert[0] == '[' {
		io.WriteString(c.w, `)`)
	}
	io.WriteString(c.w, ` AS j)`)

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

			// if w := qc.Selects[0].Where; w != nil && w.Op == qcode.OpFalse {
			// 	io.WriteString(c.w, ` WHERE false`)
			// }

			switch item._type {
			case itemInsert:
				err = c.renderInsertStmt(qc, w, item)
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

func (c *compilerContext) renderInsertStmt(qc *qcode.QCode, w io.Writer, item renitem) error {

	ti := item.ti
	jt := item.data
	sk := nestedInsertRelColumnsMap(item.kvitem)

	io.WriteString(c.w, `, `)
	renderCteName(w, item.kvitem)
	io.WriteString(w, ` AS (`)

	io.WriteString(w, `INSERT INTO `)
	quoted(w, ti.Name)
	io.WriteString(w, ` (`)
	renderInsertUpdateColumns(w, qc, jt, ti, sk, false)
	renderNestedInsertRelColumns(w, item.kvitem, false)
	io.WriteString(w, `)`)

	io.WriteString(w, ` SELECT `)
	renderInsertUpdateColumns(w, qc, jt, ti, sk, true)
	renderNestedInsertRelColumns(w, item.kvitem, true)

	io.WriteString(w, ` FROM "_sg_input" i`)
	renderNestedInsertRelTables(w, item.kvitem)
	io.WriteString(w, ` RETURNING *)`)
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

func renderNestedInsertRelColumns(w io.Writer, item kvitem, values bool) error {
	if len(item.items) == 0 {
		if item.relPC != nil && item.relPC.Type == RelOneToMany {
			if values {
				colWithTable(w, item.relPC.Left.Table, item.relPC.Left.Col)
			} else {
				quoted(w, item.relPC.Right.Col)
			}
		}
	} else {
		// Render child foreign key columns if child-to-parent
		// relationship is one-to-many
		i := 0
		for _, v := range item.items {
			if v.relCP.Type == RelOneToMany {
				if i != 0 {
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
	}

	return nil
}

func renderNestedInsertRelTables(w io.Writer, item kvitem) error {
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
