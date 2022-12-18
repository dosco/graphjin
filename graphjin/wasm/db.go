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
	"sync"
	"syscall/js"
)

var rowsPool = sync.Pool{
	New: func() interface{} {
		return new(Rows)
	},
}

type JSPostgresDB struct {
}

func (d *JSPostgresDB) Open(name string) (driver.Conn, error) {
	return nil, errors.New("use openwithclient")
}

type Connector struct {
	client js.Value
	pool   sync.Pool
}

func (c *Connector) Connect(ctx context.Context) (driver.Conn, error) {
	return &Conn{client: c.client}, nil
}

func (c *Connector) Driver() driver.Driver {
	return &JSPostgresDB{}
}

func NewJSPostgresDBConn(client js.Value) driver.Connector {
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

func (c *Conn) QueryContext(ctx context.Context, query string, nargs []driver.NamedValue) (driver.Rows, error) {
	args := make([]driver.Value, len(nargs))
	for _, v := range nargs {
		args[(v.Ordinal - 1)] = v.Value
	}

	st, err := c.Prepare(query)
	if err != nil {
		return nil, err
	}
	return st.Query(args)
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

	cl := cols.Length()
	rl := rows.Length()

	ret := rowsPool.Get().(*Rows)
	*ret = zeroRows // wipe clean for reuse

	ret.useArray = (cl == 1 && rl == 1 && rows.Index(0).Length() == 1)

	if ret.useArray {
		ret.colsA[0] = cols.Index(0).Get("name").String()
		ret.rowsA[0][0] = colVal(rows.Index(0).Index(0))
		return ret, nil
	}

	ret.cols = make([]string, cols.Length())
	ret.rows = make([][]interface{}, rows.Length())

	rowLen := -1
	if rl != 0 && rows.Index(0).Length() != 0 {
		rowLen = rows.Index(0).Length()
	}

	for i := 0; i < len(ret.cols); i++ {
		name := cols.Index(i).Get("name").String()
		ret.cols[i] = name
	}

	for i := 0; i < len(ret.rows); i++ {
		row := rows.Index(i)
		ret.rows[i] = make([]interface{}, rowLen)

		for j := 0; j < rowLen; j++ {
			ret.rows[i][j] = colVal(row.Index(j))
		}
	}
	return ret, nil
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

func getTypeParser() js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		return js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			return args[0]
		})
	})
}

func (s *Stmt) queryExec(args []driver.Value) (js.Value, error) {
	m := map[string]interface{}{
		"rowMode": "array",
		"name":    s.key,
		"text":    s.query,
		"types": map[string]interface{}{
			"getTypeParser": getTypeParser(),
		},
	}

	vals := make([]interface{}, len(args))
	for i, a := range args {
		switch v := a.(type) {
		case []byte:
			vals[i] = js.ValueOf(string(v))
		default:
			vals[i] = js.ValueOf(v)
		}
	}
	m["values"] = vals

	res, rej := await(s.client.Call("query", m))

	if len(rej) != 0 {
		err := errors.New(rej[0].Get("message").String())
		return js.Null(), err
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

func hash(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}
