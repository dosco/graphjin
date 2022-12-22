//go:build wasm && js

package main

import (
	"database/sql/driver"
	"fmt"
	"hash/fnv"
	"io"
	"sync"
	"syscall/js"
)

var rowsPool = sync.Pool{
	New: func() interface{} {
		return new(Rows)
	},
}

type Rows struct {
	cols  []string
	rows  [][]interface{}
	index int

	colsA    [1]string
	rowsA    [1][1]interface{}
	useArray bool
}

var zeroRows = Rows{}

func (r *Rows) Columns() []string {
	if r.useArray {
		return r.colsA[:]
	} else {
		return r.cols
	}
}

func (r *Rows) Close() error {
	rowsPool.Put(r)
	return nil
}

func (r *Rows) Next(dest []driver.Value) error {
	if r.useArray {
		if r.index > 0 {
			return io.EOF
		}
		dest[0] = r.rowsA[0][0]
		r.index++
		return nil
	}
	if len(r.rows) == 0 || r.index >= len(r.rows) {
		return io.EOF
	}
	if len(r.rows[0]) != len(dest) {
		return fmt.Errorf("expected %d destination fields got %d",
			len(dest), len(r.rows[0]))
	}
	for i := range dest {
		dest[i] = r.rows[r.index][i]
	}
	r.index++
	return nil
}

type Result struct {
	lastInsertId int64
	rowsAffected int64
}

func (r *Result) LastInsertId() (int64, error) {
	return r.lastInsertId, nil
}

func (r *Result) RowsAffected() (int64, error) {
	return r.rowsAffected, nil
}

func colVal(col js.Value) interface{} {
	switch col.Type() {
	case js.TypeBoolean:
		return col.Bool()
	case js.TypeNumber:
		return col.Int()
	case js.TypeString:
		return []byte(col.String())
	default:
		return nil
	}
}

func hash(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}
