package mongodriver

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"sync"

	"go.mongodb.org/mongo-driver/v2/mongo"
)

func init() {
	sql.Register("mongodb", &Driver{})
}

// Driver implements database/sql/driver.Driver for MongoDB.
type Driver struct{}

// Open is not supported - use OpenDB with a Connector instead.
func (d *Driver) Open(name string) (driver.Conn, error) {
	return nil, fmt.Errorf("mongodriver: Open not supported, use sql.OpenDB with NewConnector")
}

// Connector implements driver.Connector for MongoDB.
type Connector struct {
	client   *mongo.Client
	database string
	mu       sync.Mutex
}

// NewConnector creates a new MongoDB connector that can be used with sql.OpenDB.
func NewConnector(client *mongo.Client, database string) *Connector {
	return &Connector{
		client:   client,
		database: database,
	}
}

// Connect returns a connection to the MongoDB database.
func (c *Connector) Connect(ctx context.Context) (driver.Conn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	db := c.client.Database(c.database)
	return &Conn{
		db:     db,
		client: c.client,
	}, nil
}

// Driver returns the underlying Driver.
func (c *Connector) Driver() driver.Driver {
	return &Driver{}
}

// Client returns the underlying MongoDB client.
func (c *Connector) Client() *mongo.Client {
	return c.client
}

// Database returns the database name.
func (c *Connector) Database() string {
	return c.database
}
