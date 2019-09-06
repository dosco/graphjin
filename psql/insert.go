package psql

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/dosco/super-graph/jsn"
	"github.com/dosco/super-graph/qcode"
)

func (co *Compiler) compileMutation(qc *qcode.QCode, w *bytes.Buffer, vars Variables) (uint32, error) {
	if len(qc.Selects) == 0 {
		return 0, errors.New("empty query")
	}

	c := &compilerContext{w, qc.Selects, co}
	root := &qc.Selects[0]

	c.w.WriteString(`WITH `)
	c.w.WriteString(root.Table)
	c.w.WriteString(` AS (`)

	switch root.Action {
	case qcode.ActionInsert:
		if _, err := c.renderInsert(qc, w, vars); err != nil {
			return 0, err
		}

	case qcode.ActionUpdate:
		if _, err := c.renderUpdate(qc, w, vars); err != nil {
			return 0, err
		}

	default:
		return 0, errors.New("valid mutations are 'insert' and 'update'")
	}

	c.w.WriteString(`) `)

	return c.compileQuery(qc, w)
}

func (c *compilerContext) renderInsert(qc *qcode.QCode, w *bytes.Buffer, vars Variables) (uint32, error) {
	root := &qc.Selects[0]

	insert, ok := vars[root.ActionVar]
	if !ok {
		return 0, fmt.Errorf("Variable '%s' not defined", root.ActionVar)
	}

	ti, err := c.schema.GetTable(root.Table)
	if err != nil {
		return 0, err
	}

	jt, array, err := jsn.Tree(insert)
	if err != nil {
		return 0, err
	}

	c.w.WriteString(`WITH input AS (SELECT {{insert}}::json AS j) INSERT INTO `)
	c.w.WriteString(root.Table)
	io.WriteString(c.w, ` (`)
	c.renderInsertColumns(qc, w, jt, ti)
	io.WriteString(c.w, `)`)

	c.w.WriteString(` SELECT `)
	c.renderInsertColumns(qc, w, jt, ti)
	c.w.WriteString(` FROM input i, `)

	if array {
		c.w.WriteString(`json_populate_recordset`)
	} else {
		c.w.WriteString(`json_populate_record`)
	}

	c.w.WriteString(`(NULL::`)
	c.w.WriteString(root.Table)
	c.w.WriteString(`, i.j) t  RETURNING * `)

	return 0, nil
}

func (c *compilerContext) renderInsertColumns(qc *qcode.QCode, w *bytes.Buffer,
	jt map[string]interface{}, ti *DBTableInfo) (uint32, error) {

	i := 0
	for _, cn := range ti.ColumnNames {
		if _, ok := jt[cn]; !ok {
			continue
		}
		if i != 0 {
			io.WriteString(c.w, `, `)
		}
		c.w.WriteString(cn)
		i++
	}

	return 0, nil
}

func (c *compilerContext) renderUpdate(qc *qcode.QCode, w *bytes.Buffer, vars Variables) (uint32, error) {
	root := &qc.Selects[0]

	update, ok := vars[root.ActionVar]
	if !ok {
		return 0, fmt.Errorf("Variable '%s' not defined", root.ActionVar)
	}

	ti, err := c.schema.GetTable(root.Table)
	if err != nil {
		return 0, err
	}

	jt, array, err := jsn.Tree(update)
	if err != nil {
		return 0, err
	}

	c.w.WriteString(`WITH input AS (SELECT {{update}}::json AS j) UPDATE `)
	c.w.WriteString(root.Table)
	io.WriteString(c.w, ` SET (`)
	c.renderInsertColumns(qc, w, jt, ti)

	c.w.WriteString(`) = (SELECT `)
	c.renderInsertColumns(qc, w, jt, ti)
	c.w.WriteString(` FROM input i, `)

	if array {
		c.w.WriteString(`json_populate_recordset`)
	} else {
		c.w.WriteString(`json_populate_record`)
	}

	c.w.WriteString(`(NULL::`)
	c.w.WriteString(root.Table)
	c.w.WriteString(`, i.j) t)`)

	io.WriteString(c.w, ` WHERE `)

	if err := c.renderWhere(root, ti); err != nil {
		return 0, err
	}

	io.WriteString(c.w, ` RETURNING *`)

	return 0, nil
}

func (c *compilerContext) renderUpdateColumns(qc *qcode.QCode, w *bytes.Buffer,
	jt map[string]interface{}, ti *DBTableInfo) (uint32, error) {

	i := 0
	for _, cn := range ti.ColumnNames {
		if _, ok := jt[cn]; !ok {
			continue
		}
		if i != 0 {
			io.WriteString(c.w, `, `)
		}
		c.w.WriteString(cn)
		c.w.WriteString(` = {{`)
		c.w.WriteString(cn)
		c.w.WriteString(`}}`)
		i++
	}

	return 0, nil
}
