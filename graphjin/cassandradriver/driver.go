package cassandradriver

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"

	"github.com/gocql/gocql"
)

func init() {
	sql.Register("cassandra", &Driver{})
}

// Driver implements database/sql/driver.Driver for Cassandra / Keyspaces.
type Driver struct{}

// Open is not supported — Cassandra needs a configured gocql session, so callers
// use sql.OpenDB with NewConnector.
func (d *Driver) Open(name string) (driver.Conn, error) {
	return nil, fmt.Errorf("cassandradriver: Open not supported, use sql.OpenDB with NewConnector")
}

// Connector implements driver.Connector over a live gocql session.
type Connector struct {
	session  *gocql.Session
	keyspace string
}

// NewConnector wraps a configured gocql session so it can back a *sql.DB.
func NewConnector(session *gocql.Session, keyspace string) *Connector {
	return &Connector{session: session, keyspace: keyspace}
}

// Connect returns a connection bound to the shared gocql session.
func (c *Connector) Connect(_ context.Context) (driver.Conn, error) {
	return &Conn{session: c.session, keyspace: c.keyspace}, nil
}

// Driver returns the underlying Driver.
func (c *Connector) Driver() driver.Driver { return &Driver{} }

// Session exposes the underlying gocql session.
func (c *Connector) Session() *gocql.Session { return c.session }

// Keyspace returns the default keyspace.
func (c *Connector) Keyspace() string { return c.keyspace }
