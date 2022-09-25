//go:build js && wasm

package main

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"strconv"
	"syscall/js"
)

type Driver struct{}

func (d *Driver) Open(name string) (driver.Conn, error) {
	return nil, errors.New("use openwithclient")
}

type Connector struct {
	client js.Value
}

func (c *Connector) Connect(ctx context.Context) (driver.Conn, error) {
	return &Conn{client: c.client}, nil
}

func (c *Connector) Driver() driver.Driver {
	return &Driver{}
}

func NewDriverConn(client js.Value) driver.Connector {
	return &Connector{client: client}
}

type Conn struct {
	client js.Value
}

func (c *Conn) Prepare(query string) (driver.Stmt, error) {
	st := &Stmt{
		client:   c.client,
		key:      strconv.FormatUint(uint64(hash(query)), 10),
		query:    query,
		numInput: -1,
	}
	return st, nil
}

func (c *Conn) Close() error {
	return nil
}

func (c *Conn) Begin() (driver.Tx, error) {
	return &Tx{client: c.client}, nil
}

type Tx struct {
	client js.Value
}

func (t *Tx) Begin() (driver.Tx, error) {
	return t, nil
}

func (t *Tx) Commit() error {
	return nil
}

func (t *Tx) Rollback() error {
	return nil
}

type Stmt struct {
	client   js.Value
	key      string
	query    string
	numInput int
}

func (s *Stmt) Close() error {
	await(s.client.Call("end"))
	return nil
}

func (s *Stmt) NumInput() int {
	return s.numInput
}

func (s *Stmt) Exec(args []driver.Value) (driver.Result, error) {
	res := &Result{lastInsertId: -1}
	v, err := s.queryExec(args)
	if err != nil {
		return res, err
	}
	res.rowsAffected = int64(v.Get("rowCount").Int())
	return res, nil
}

func (s *Stmt) Query(args []driver.Value) (driver.Rows, error) {
	v, err := s.queryExec(args)
	if err != nil {
		return nil, err
	}

	cols := v.Get("fields")
	rows := v.Get("rows")

	ret := &Rows{
		cols: make([]string, cols.Length()),
		rows: make([][]interface{}, rows.Length()),
	}

	for i := 0; i < len(ret.cols); i++ {
		name := cols.Index(i).Get("name").String()
		ret.cols[i] = name
	}

	rowLen := -1

	for i := 0; i < len(ret.rows); i++ {
		row := rows.Index(i)

		if rowLen == -1 {
			rowLen = row.Length()
		}

		ret.rows[i] = make([]interface{}, rowLen)

		for j := 0; j < rowLen; j++ {
			var val interface{}
			col := row.Index(j)

			switch col.Type() {
			case js.TypeBoolean:
				val = col.Bool()
			case js.TypeNumber:
				val = col.Int()
			case js.TypeString:
				val = col.String()
			default:
				val = nil
			}
			ret.rows[i][j] = val
		}
	}
	return ret, nil
}

func (s *Stmt) queryExec(args []driver.Value) (js.Value, error) {
	m := map[string]interface{}{
		"rowMode": "array",
		"name":    s.key,
		"text":    s.query,
	}
	vals := make([]interface{}, 0, len(args))
	for _, v := range args {
		vals = append(vals, v)
	}
	m["values"] = vals

	res, rej := await(s.client.Call("query", m))
	fmt.Println(">", s.key)

	if len(rej) != 0 {
		err := errors.New(rej[0].Get("message").String())
		return js.ValueOf(nil), err
	}

	return res[0], nil
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

type Rows struct {
	cols  []string
	rows  [][]interface{}
	index int
}

func (r *Rows) Columns() []string {
	return r.cols
}

func (r *Rows) Close() error {
	return nil
}

func (r *Rows) Next(dest []driver.Value) error {
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

func hash(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}

func await(awaitable js.Value) ([]js.Value, []js.Value) {
	then := make(chan []js.Value)
	defer close(then)
	thenFunc := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		then <- args
		return nil
	})
	defer thenFunc.Release()

	catch := make(chan []js.Value)
	defer close(catch)
	catchFunc := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		catch <- args
		return nil
	})
	defer catchFunc.Release()

	awaitable.Call("then", thenFunc).Call("catch", catchFunc)

	select {
	case result := <-then:
		return result, nil
	case err := <-catch:
		return nil, err
	}
}
