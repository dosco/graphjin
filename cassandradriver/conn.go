package cassandradriver

import (
	"context"
	"database/sql/driver"
	"fmt"

	"github.com/gocql/gocql"
)

// Conn implements driver.Conn over a shared gocql session.
type Conn struct {
	session  *gocql.Session
	keyspace string
	exec     Executor // test seam; nil in production → gocql executor
}

// Prepare returns a prepared statement. gocql prepares server-side on first use;
// reuse is automatic because every value is bound with `?`.
func (c *Conn) Prepare(query string) (driver.Stmt, error) {
	return &Stmt{conn: c, query: query}, nil
}

// Close is a no-op: the gocql session is owned by the Connector and shared.
func (c *Conn) Close() error { return nil }

// Begin is unsupported — Cassandra has no multi-statement transactions.
func (c *Conn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("cassandradriver: transactions are not supported")
}

// QueryContext parses the DSL, resolves it, and returns the JSON result as a
// single column (GraphJin reads one JSON value per root selection).
func (c *Conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	q, err := c.prepare(query, args)
	if err != nil {
		return nil, err
	}
	switch q.Operation {
	case OpIntrospectInfo, OpIntrospectColumns, OpIntrospectKeys:
		return c.introspect(ctx, q)
	default:
		data, err := c.resolver().ResolveQuery(ctx, q)
		if err != nil {
			return nil, err
		}
		return NewSingleValueRows(data, []string{"__root"}), nil
	}
}

// ExecContext runs a statement whose result is discarded.
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
	if c.exec != nil {
		return &Resolver{Exec: c.exec, DefaultKeyspace: c.keyspace}
	}
	return &Resolver{Exec: newGocqlExecutor(c.session), DefaultKeyspace: c.keyspace}
}

// prepare turns the wire query + ordinal args into a ready-to-resolve DSL.
func (c *Conn) prepare(query string, args []driver.NamedValue) (*QueryDSL, error) {
	q, err := ParseQuery(query)
	if err != nil {
		return nil, err
	}
	if err := q.SubstituteParams(positionalArgs(args)); err != nil {
		return nil, err
	}
	if err := q.BindValues(); err != nil {
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
