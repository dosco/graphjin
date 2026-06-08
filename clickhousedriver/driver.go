// Package clickhousedriver is a database/sql driver that accepts the JSON query
// DSL emitted by GraphJin's ClickHouse dialect. ClickHouse has no LATERAL joins
// and no json_build_object, so nested data is assembled here via bounded, batched
// N+1; leaf reads run as real SQL on a wrapped clickhouse-go *sql.DB.
package clickhousedriver

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
)

func init() {
	// clickhouse-go already registers "clickhouse"; use a distinct name.
	sql.Register("clickhouse-gj", &Driver{})
}

// Driver implements database/sql/driver.Driver.
type Driver struct{}

// Open is unsupported — callers use sql.OpenDB with NewConnector.
func (d *Driver) Open(name string) (driver.Conn, error) {
	return nil, fmt.Errorf("clickhousedriver: Open not supported, use sql.OpenDB with NewConnector")
}

// Connector wraps an already-opened clickhouse-go *sql.DB (the leaf-SQL executor)
// so GraphJin's JSON DSL is intercepted while real SQL is delegated to it.
type Connector struct {
	inner    *sql.DB
	database string
}

// NewConnector wraps a clickhouse-go *sql.DB and its default database.
func NewConnector(inner *sql.DB, database string) *Connector {
	return &Connector{inner: inner, database: database}
}

// Connect returns a connection that shares the wrapped *sql.DB.
func (c *Connector) Connect(_ context.Context) (driver.Conn, error) {
	return &Conn{inner: c.inner, database: c.database}, nil
}

func (c *Connector) Driver() driver.Driver { return &Driver{} }

// DB exposes the wrapped clickhouse-go handle.
func (c *Connector) DB() *sql.DB { return c.inner }

// Database returns the default database name.
func (c *Connector) Database() string { return c.database }
