package mongodriver

import (
	"context"
	"database/sql/driver"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/mongo"
)

// Conn implements driver.Conn for MongoDB.
type Conn struct {
	db     *mongo.Database
	client *mongo.Client
}

// Prepare returns a prepared statement.
func (c *Conn) Prepare(query string) (driver.Stmt, error) {
	return &Stmt{
		conn:  c,
		query: query,
	}, nil
}

// Close closes the connection.
func (c *Conn) Close() error {
	// Connections are managed by the mongo.Client pool
	return nil
}

// Ping verifies the MongoDB connection is alive.
func (c *Conn) Ping(ctx context.Context) error {
	return c.client.Ping(ctx, nil)
}

// Begin starts a transaction. MongoDB transactions require replica sets.
func (c *Conn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("mongodriver: transactions require BeginTx with context")
}

// BeginTx starts a transaction with context.
func (c *Conn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	session, err := c.client.StartSession()
	if err != nil {
		return nil, fmt.Errorf("mongodriver: start session: %w", err)
	}
	if err := session.StartTransaction(); err != nil {
		session.EndSession(ctx)
		return nil, fmt.Errorf("mongodriver: start transaction: %w", err)
	}
	return &Tx{session: session, ctx: ctx}, nil
}

// QueryContext executes a query and returns rows.
func (c *Conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	// Convert NamedValue to positional args
	positionalArgs := make([]any, len(args))
	for _, arg := range args {
		if arg.Ordinal > 0 {
			positionalArgs[arg.Ordinal-1] = arg.Value
		}
	}

	// Parse the JSON query DSL
	q, err := ParseQuery(query)
	if err != nil {
		return nil, err
	}

	// Substitute parameters
	if err := q.SubstituteParams(positionalArgs); err != nil {
		return nil, err
	}

	// Execute based on operation
	return c.executeQuery(ctx, q)
}

// ExecContext executes a statement that doesn't return rows.
func (c *Conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	// Convert NamedValue to positional args
	positionalArgs := make([]any, len(args))
	for _, arg := range args {
		if arg.Ordinal > 0 {
			positionalArgs[arg.Ordinal-1] = arg.Value
		}
	}

	// Parse the JSON query DSL
	q, err := ParseQuery(query)
	if err != nil {
		return nil, err
	}

	// Substitute parameters
	if err := q.SubstituteParams(positionalArgs); err != nil {
		return nil, err
	}

	// Execute based on operation
	return c.executeExec(ctx, q)
}

// executeQuery handles query operations (aggregate, find).
func (c *Conn) executeQuery(ctx context.Context, q *QueryDSL) (driver.Rows, error) {
	switch q.Operation {
	case OpEmpty:
		// Return empty result for dropped root selections (@add/@remove directives)
		return NewSingleValueRows([]byte("{}"), []string{"__root"}), nil
	case OpNull:
		// Return null result for skipped selections (@skip/@include with variable)
		// Wrap null in a JSON object with the field name
		result := fmt.Sprintf(`{"%s":null}`, q.FieldName)
		return NewSingleValueRows([]byte(result), []string{"__root"}), nil
	case OpIntrospectInfo:
		return c.introspectInfo(ctx, q)
	case OpIntrospectColumns:
		return c.introspectColumns(ctx, q)
	case OpIntrospectFuncs:
		return c.introspectFunctions(ctx, q)
	case OpAggregate:
		return c.executeAggregate(ctx, q)
	case OpMultiAggregate:
		return c.executeMultiAggregate(ctx, q)
	case OpInsertOne:
		// Handle insertOne as a query that returns the inserted document
		return c.executeInsertOneAsQuery(ctx, q)
	case OpInsertMany:
		// Handle insertMany as a query that returns the inserted documents
		return c.executeInsertManyAsQuery(ctx, q)
	case OpNestedInsert:
		// Handle nested insert (insert into multiple related collections)
		return c.executeNestedInsert(ctx, q)
	case OpUpdateOne:
		// Handle updateOne as a query that returns the updated document
		return c.executeUpdateOneAsQuery(ctx, q)
	case OpNestedUpdate:
		// Handle nested update (update multiple related collections)
		return c.executeNestedUpdate(ctx, q)
	case OpFind:
		return c.executeFind(ctx, q)
	case OpFindOne:
		return c.executeFindOne(ctx, q)
	default:
		return nil, fmt.Errorf("mongodriver: unsupported query operation: %s", q.Operation)
	}
}

// executeExec handles mutation operations (insert, update, delete).
func (c *Conn) executeExec(ctx context.Context, q *QueryDSL) (driver.Result, error) {
	switch q.Operation {
	case OpInsertOne:
		return c.executeInsertOne(ctx, q)
	case OpInsertMany:
		return c.executeInsertMany(ctx, q)
	case OpUpdateOne:
		return c.executeUpdateOne(ctx, q)
	case OpUpdateMany:
		return c.executeUpdateMany(ctx, q)
	case OpDeleteOne:
		return c.executeDeleteOne(ctx, q)
	case OpDeleteMany:
		return c.executeDeleteMany(ctx, q)
	default:
		return nil, fmt.Errorf("mongodriver: unsupported exec operation: %s", q.Operation)
	}
}

// Tx implements driver.Tx for MongoDB transactions.
type Tx struct {
	session *mongo.Session
	ctx     context.Context
}

// Commit commits the transaction.
func (t *Tx) Commit() error {
	defer t.session.EndSession(t.ctx)
	return t.session.CommitTransaction(t.ctx)
}

// Rollback aborts the transaction.
func (t *Tx) Rollback() error {
	defer t.session.EndSession(t.ctx)
	return t.session.AbortTransaction(t.ctx)
}

// Stmt implements driver.Stmt for MongoDB.
type Stmt struct {
	conn  *Conn
	query string
}

// Close closes the statement.
func (s *Stmt) Close() error {
	return nil
}

// NumInput returns the number of placeholder parameters.
func (s *Stmt) NumInput() int {
	return -1 // Unknown number of parameters
}

// Exec executes a query that doesn't return rows.
func (s *Stmt) Exec(args []driver.Value) (driver.Result, error) {
	namedArgs := make([]driver.NamedValue, len(args))
	for i, arg := range args {
		namedArgs[i] = driver.NamedValue{Ordinal: i + 1, Value: arg}
	}
	return s.conn.ExecContext(context.Background(), s.query, namedArgs)
}

// Query executes a query that returns rows.
func (s *Stmt) Query(args []driver.Value) (driver.Rows, error) {
	namedArgs := make([]driver.NamedValue, len(args))
	for i, arg := range args {
		namedArgs[i] = driver.NamedValue{Ordinal: i + 1, Value: arg}
	}
	return s.conn.QueryContext(context.Background(), s.query, namedArgs)
}
