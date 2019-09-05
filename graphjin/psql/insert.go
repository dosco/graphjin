package psql

import (
	"bytes"
	"errors"
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

	if _, err := c.renderInsert(qc, w, vars); err != nil {
		return 0, err
	}

	c.w.WriteString(`) `)

	return c.compileQuery(qc, w)
}

func (c *compilerContext) renderInsert(qc *qcode.QCode, w *bytes.Buffer, vars Variables) (uint32, error) {
	root := &qc.Selects[0]

	insert, ok := vars["insert"]
	if !ok {
		return 0, errors.New("Variable 'insert' not defined")
	}

	jt, array, err := jsn.Tree(insert)
	if err != nil {
		return 0, err
	}

	c.w.WriteString(`WITH input AS (SELECT {{insert}}::json AS j) INSERT INTO `)
	c.w.WriteString(root.Table)
	io.WriteString(c.w, " (")
	c.renderInsertColumns(qc, w, jt)
	io.WriteString(c.w, ")")

	c.w.WriteString(` SELECT `)
	c.renderInsertColumns(qc, w, jt)
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
	jt map[string]interface{}) (uint32, error) {

	ti, err := c.schema.GetTable(qc.Selects[0].Table)
	if err != nil {
		return 0, err
	}

	i := 0
	for _, cn := range ti.ColumnNames {
		if _, ok := jt[cn]; !ok {
			continue
		}
		if i != 0 {
			io.WriteString(c.w, ", ")
		}
		c.w.WriteString(cn)
		i++
	}

	return 0, nil
}
