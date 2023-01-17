//go:build wasm && js

package main

import (
	"context"
	"database/sql/driver"
	"errors"
	"strconv"
	"sync"
	"syscall/js"
)

type PgDB struct{}

func (d *PgDB) Open(name string) (driver.Conn, error) {
	return nil, errors.New("use openwithclient")
}

type PgConnector struct {
	client js.Value
	pool   sync.Pool
}

func (c *PgConnector) Connect(ctx context.Context) (driver.Conn, error) {
	return &PgConn{client: c.client}, nil
}

func (c *PgConnector) Driver() driver.Driver {
	return &PgDB{}
}

func NewPgDBConn(client js.Value) driver.Connector {
	return &PgConnector{client: client}
}

type PgConn struct {
	client js.Value
}

func (c *PgConn) Prepare(query string) (driver.Stmt, error) {
	st := &PgStmt{
		client:   c.client,
		key:      strconv.FormatUint(uint64(hash(query)), 10),
		query:    query,
		numInput: -1,
	}
	return st, nil
}

func (c *PgConn) QueryContext(ctx context.Context, query string, nargs []driver.NamedValue) (driver.Rows, error) {
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

func (c *PgConn) Close() error {
	return nil
}

func (c *PgConn) Begin() (driver.Tx, error) {
	return c, nil
}

func (c *PgConn) Commit() error {
	return nil
}

func (c *PgConn) Rollback() error {
	return nil
}

type PgStmt struct {
	client   js.Value
	key      string
	query    string
	numInput int
}

func (s *PgStmt) Close() error {
	await(s.client.Call("end"))
	return nil
}

func (s *PgStmt) NumInput() int {
	return s.numInput
}

func (s *PgStmt) Exec(args []driver.Value) (driver.Result, error) {
	res := &Result{lastInsertId: -1}
	v, err := s.queryExec(args)
	if err != nil {
		return res, err
	}
	res.rowsAffected = int64(v.Get("rowCount").Int())
	return res, nil
}

func (s *PgStmt) Query(args []driver.Value) (driver.Rows, error) {
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

func getPGTypeParser() map[string]interface{} {
	fn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		return js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			return args[0]
		})
	})
	return map[string]interface{}{"getTypeParser": fn}
}

func (s *PgStmt) queryExec(args []driver.Value) (js.Value, error) {
	m := map[string]interface{}{
		"rowMode": "array",
		"types":   getPGTypeParser(),
		"name":    s.key,
		"text":    s.query,
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
