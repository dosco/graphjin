//nolint:errcheck
package psql

import (
	"errors"
	"fmt"
	"io"

	"github.com/dosco/super-graph/jsn"
	"github.com/dosco/super-graph/qcode"
)

var noLimit = qcode.Paging{NoLimit: true}

func (co *Compiler) compileMutation(qc *qcode.QCode, w io.Writer, vars Variables) (uint32, error) {
	if len(qc.Selects) == 0 {
		return 0, errors.New("empty query")
	}

	c := &compilerContext{w, qc.Selects, co}
	root := &qc.Selects[0]

	ti, err := c.schema.GetTable(root.Table)
	if err != nil {
		return 0, err
	}

	io.WriteString(c.w, `WITH `)
	quoted(c.w, ti.Name)
	io.WriteString(c.w, ` AS `)

	switch qc.Type {
	case qcode.QTInsert:
		if _, err := c.renderInsert(qc, w, vars, ti); err != nil {
			return 0, err
		}

	case qcode.QTUpdate:
		if _, err := c.renderUpdate(qc, w, vars, ti); err != nil {
			return 0, err
		}

	case qcode.QTUpsert:
		if _, err := c.renderUpsert(qc, w, vars, ti); err != nil {
			return 0, err
		}

	case qcode.QTDelete:
		if _, err := c.renderDelete(qc, w, vars, ti); err != nil {
			return 0, err
		}

	default:
		return 0, errors.New("valid mutations are 'insert', 'update', 'upsert' and 'delete'")
	}

	io.WriteString(c.w, ` RETURNING *) `)

	root.Paging = noLimit
	root.DistinctOn = root.DistinctOn[:]
	root.OrderBy = root.OrderBy[:]
	root.Where = nil
	root.Args = nil

	return c.compileQuery(qc, w)
}

func (c *compilerContext) renderInsert(qc *qcode.QCode, w io.Writer,
	vars Variables, ti *DBTableInfo) (uint32, error) {

	insert, ok := vars[qc.ActionVar]
	if !ok {
		return 0, fmt.Errorf("Variable '%s' not defined", qc.ActionVar)
	}

	jt, array, err := jsn.Tree(insert)
	if err != nil {
		return 0, err
	}

	io.WriteString(c.w, `(WITH "input" AS (SELECT '{{`)
	io.WriteString(c.w, qc.ActionVar)
	io.WriteString(c.w, `}}' :: json AS j) INSERT INTO `)
	quoted(c.w, ti.Name)
	io.WriteString(c.w, ` (`)
	c.renderInsertUpdateColumns(qc, w, jt, ti, false)
	io.WriteString(c.w, `)`)

	io.WriteString(c.w, ` SELECT `)
	c.renderInsertUpdateColumns(qc, w, jt, ti, true)
	io.WriteString(c.w, ` FROM input i, `)

	if array {
		io.WriteString(c.w, `json_populate_recordset`)
	} else {
		io.WriteString(c.w, `json_populate_record`)
	}

	io.WriteString(c.w, `(NULL::`)
	io.WriteString(c.w, ti.Name)
	io.WriteString(c.w, `, i.j) t`)

	if w := qc.Selects[0].Where; w != nil && w.Op == qcode.OpFalse {
		io.WriteString(c.w, ` WHERE false`)
	}

	return 0, nil
}

func (c *compilerContext) renderInsertUpdateColumns(qc *qcode.QCode, w io.Writer,
	jt map[string]interface{}, ti *DBTableInfo, values bool) (uint32, error) {
	root := &qc.Selects[0]

	i := 0
	for _, cn := range ti.ColumnNames {
		if _, ok := jt[cn]; !ok {
			continue
		}
		if _, ok := root.PresetMap[cn]; ok {
			continue
		}
		if len(root.Allowed) != 0 {
			if _, ok := root.Allowed[cn]; !ok {
				continue
			}
		}
		if i != 0 {
			io.WriteString(c.w, `, `)
		}
		io.WriteString(c.w, `"`)
		io.WriteString(c.w, cn)
		io.WriteString(c.w, `"`)
		i++
	}

	if i != 0 && len(root.PresetList) != 0 {
		io.WriteString(c.w, `, `)
	}

	for i := range root.PresetList {
		cn := root.PresetList[i]
		col, ok := ti.Columns[cn]
		if !ok {
			continue
		}
		if i != 0 {
			io.WriteString(c.w, `, `)
		}
		if values {
			io.WriteString(c.w, `'`)
			io.WriteString(c.w, root.PresetMap[cn])
			io.WriteString(c.w, `' :: `)
			io.WriteString(c.w, col.Type)

		} else {
			io.WriteString(c.w, `"`)
			io.WriteString(c.w, cn)
			io.WriteString(c.w, `"`)
		}
	}
	return 0, nil
}

func (c *compilerContext) renderUpdate(qc *qcode.QCode, w io.Writer,
	vars Variables, ti *DBTableInfo) (uint32, error) {
	root := &qc.Selects[0]

	update, ok := vars[qc.ActionVar]
	if !ok {
		return 0, fmt.Errorf("Variable '%s' not defined", qc.ActionVar)
	}

	jt, array, err := jsn.Tree(update)
	if err != nil {
		return 0, err
	}

	io.WriteString(c.w, `(WITH "input" AS (SELECT '{{`)
	io.WriteString(c.w, qc.ActionVar)
	io.WriteString(c.w, `}}' :: json AS j) UPDATE `)
	quoted(c.w, ti.Name)
	io.WriteString(c.w, ` SET (`)
	c.renderInsertUpdateColumns(qc, w, jt, ti, false)

	io.WriteString(c.w, `) = (SELECT `)
	c.renderInsertUpdateColumns(qc, w, jt, ti, true)
	io.WriteString(c.w, ` FROM input i, `)

	if array {
		io.WriteString(c.w, `json_populate_recordset`)
	} else {
		io.WriteString(c.w, `json_populate_record`)
	}

	io.WriteString(c.w, `(NULL::`)
	io.WriteString(c.w, ti.Name)
	io.WriteString(c.w, `, i.j) t)`)

	io.WriteString(c.w, ` WHERE `)

	if err := c.renderWhere(root, ti); err != nil {
		return 0, err
	}

	return 0, nil
}

func (c *compilerContext) renderDelete(qc *qcode.QCode, w io.Writer,
	vars Variables, ti *DBTableInfo) (uint32, error) {
	root := &qc.Selects[0]

	io.WriteString(c.w, `(DELETE FROM `)
	quoted(c.w, ti.Name)
	io.WriteString(c.w, ` WHERE `)

	if err := c.renderWhere(root, ti); err != nil {
		return 0, err
	}

	return 0, nil
}

func (c *compilerContext) renderUpsert(qc *qcode.QCode, w io.Writer,
	vars Variables, ti *DBTableInfo) (uint32, error) {
	root := &qc.Selects[0]

	upsert, ok := vars[qc.ActionVar]
	if !ok {
		return 0, fmt.Errorf("Variable '%s' not defined", qc.ActionVar)
	}

	jt, _, err := jsn.Tree(upsert)
	if err != nil {
		return 0, err
	}

	if _, err := c.renderInsert(qc, w, vars, ti); err != nil {
		return 0, err
	}

	io.WriteString(c.w, ` ON CONFLICT (`)
	i := 0

	for _, cn := range ti.ColumnNames {
		if _, ok := jt[cn]; !ok {
			continue
		}

		if col, ok := ti.Columns[cn]; !ok || !(col.UniqueKey || col.PrimaryKey) {
			continue
		}

		if i != 0 {
			io.WriteString(c.w, `, `)
		}
		io.WriteString(c.w, cn)
		i++
	}
	if i == 0 {
		io.WriteString(c.w, ti.PrimaryCol)
	}
	io.WriteString(c.w, `)`)

	if root.Where != nil {
		io.WriteString(c.w, ` WHERE `)

		if err := c.renderWhere(root, ti); err != nil {
			return 0, err
		}
	}

	io.WriteString(c.w, ` DO UPDATE SET `)

	i = 0
	for _, cn := range ti.ColumnNames {
		if _, ok := jt[cn]; !ok {
			continue
		}
		if i != 0 {
			io.WriteString(c.w, `, `)
		}
		io.WriteString(c.w, cn)
		io.WriteString(c.w, ` = EXCLUDED.`)
		io.WriteString(c.w, cn)
		i++
	}

	return 0, nil
}

func quoted(w io.Writer, identifier string) {
	io.WriteString(w, `"`)
	io.WriteString(w, identifier)
	io.WriteString(w, `"`)
}
