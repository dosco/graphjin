package clickhousedriver

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
)

// Conn intercepts the JSON DSL; leaf SQL runs on the shared *sql.DB.
type Conn struct {
	inner    *sql.DB
	database string
	exec     Executor // test seam; nil in production → sqlExecutor over inner
}

func (c *Conn) Prepare(query string) (driver.Stmt, error) {
	return &Stmt{conn: c, query: query}, nil
}

func (c *Conn) Close() error { return nil }

// Begin is unsupported — ClickHouse has no general multi-statement transactions.
func (c *Conn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("clickhousedriver: transactions are not supported")
}

// QueryContext parses the DSL, resolves it, and returns the JSON result as a
// single column (GraphJin reads one JSON value per root selection).
func (c *Conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	q, err := c.prepare(query, args)
	if err != nil {
		return nil, err
	}
	switch q.Operation {
	case OpIntrospectInfo, OpIntrospectColumns:
		return c.introspect(ctx, q)
	default:
		data, err := c.resolver().ResolveQuery(ctx, q)
		if err != nil {
			return nil, err
		}
		return NewSingleValueRows(data, []string{"__root"}), nil
	}
}

// ExecContext runs a mutation whose result is discarded.
func (c *Conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	q, err := c.prepare(query, args)
	if err != nil {
		return nil, err
	}
	if _, err := c.resolver().ResolveQuery(ctx, q); err != nil {
		return nil, err
	}
	return driver.RowsAffected(0), nil
}

func (c *Conn) resolver() *Resolver {
	return &Resolver{Exec: c.executor(), DefaultDatabase: c.database}
}

func (c *Conn) executor() Executor {
	if c.exec != nil {
		return c.exec
	}
	return &sqlExecutor{db: c.inner}
}

func (c *Conn) prepare(query string, args []driver.NamedValue) (*QueryDSL, error) {
	q, err := ParseQuery(query)
	if err != nil {
		return nil, err
	}
	if err := q.SubstituteParams(positionalArgs(args)); err != nil {
		return nil, err
	}
	return q, nil
}

// positionalArgs maps ordinal NamedValues ($1, $2, …) to a positional slice.
func positionalArgs(args []driver.NamedValue) []any {
	out := make([]any, len(args))
	for _, a := range args {
		if a.Ordinal > 0 && a.Ordinal <= len(out) {
			out[a.Ordinal-1] = a.Value
		}
	}
	return out
}

// Stmt implements driver.Stmt.
type Stmt struct {
	conn  *Conn
	query string
}

func (s *Stmt) Close() error  { return nil }
func (s *Stmt) NumInput() int { return -1 }

func (s *Stmt) Exec(args []driver.Value) (driver.Result, error) {
	return s.conn.ExecContext(context.Background(), s.query, namedValues(args))
}

func (s *Stmt) Query(args []driver.Value) (driver.Rows, error) {
	return s.conn.QueryContext(context.Background(), s.query, namedValues(args))
}

func namedValues(args []driver.Value) []driver.NamedValue {
	out := make([]driver.NamedValue, len(args))
	for i, a := range args {
		out[i] = driver.NamedValue{Ordinal: i + 1, Value: a}
	}
	return out
}
