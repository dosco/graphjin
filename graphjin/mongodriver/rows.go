package mongodriver

import (
	"database/sql/driver"
	"encoding/json"
	"io"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// Rows implements driver.Rows for MongoDB query results.
type Rows struct {
	cursor  *mongo.Cursor
	columns []string
	closed  bool
}

// NewRows creates a new Rows from a MongoDB cursor.
func NewRows(cursor *mongo.Cursor, columns []string) *Rows {
	return &Rows{
		cursor:  cursor,
		columns: columns,
	}
}

// Columns returns the column names.
func (r *Rows) Columns() []string {
	return r.columns
}

// Close closes the rows iterator.
func (r *Rows) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	if r.cursor != nil {
		return r.cursor.Close(nil)
	}
	return nil
}

// Next moves to the next row.
// For GraphJin, we return the entire document as JSON in a single column.
func (r *Rows) Next(dest []driver.Value) error {
	if r.cursor == nil || !r.cursor.Next(nil) {
		return io.EOF
	}

	var doc bson.M
	if err := r.cursor.Decode(&doc); err != nil {
		return err
	}

	// Convert to JSON bytes - GraphJin expects JSON as the result
	jsonBytes, err := json.Marshal(doc)
	if err != nil {
		return err
	}

	// Return as single JSON column
	if len(dest) > 0 {
		dest[0] = jsonBytes
	}

	return nil
}

// SingleValueRows returns a single row with a single JSON value.
// Used for aggregate results that need to be wrapped.
type SingleValueRows struct {
	value    []byte
	columns  []string
	consumed bool
}

// NewSingleValueRows creates rows that return a single JSON value.
func NewSingleValueRows(value []byte, columns []string) *SingleValueRows {
	return &SingleValueRows{
		value:   value,
		columns: columns,
	}
}

// Columns returns column names.
func (r *SingleValueRows) Columns() []string {
	return r.columns
}

// Close closes the rows.
func (r *SingleValueRows) Close() error {
	return nil
}

// Next returns the single value.
func (r *SingleValueRows) Next(dest []driver.Value) error {
	if r.consumed {
		return io.EOF
	}
	r.consumed = true
	if len(dest) > 0 {
		dest[0] = r.value
	}
	return nil
}

// ColumnRows returns schema introspection results as multiple columns.
// Used for returning table/column metadata to GraphJin.
type ColumnRows struct {
	data    [][]any
	columns []string
	index   int
}

// NewColumnRows creates rows with multiple columns per row.
func NewColumnRows(columns []string, data [][]any) *ColumnRows {
	return &ColumnRows{
		columns: columns,
		data:    data,
		index:   0,
	}
}

// Columns returns column names.
func (r *ColumnRows) Columns() []string {
	return r.columns
}

// Close closes the rows.
func (r *ColumnRows) Close() error {
	return nil
}

// Next moves to the next row.
func (r *ColumnRows) Next(dest []driver.Value) error {
	if r.index >= len(r.data) {
		return io.EOF
	}

	row := r.data[r.index]
	for i := 0; i < len(dest) && i < len(row); i++ {
		dest[i] = row[i]
	}
	r.index++
	return nil
}
