package cassandradriver

import (
	"database/sql/driver"
	"io"
)

// SingleValueRows returns one row with one JSON value — the shape GraphJin reads
// for a root selection.
type SingleValueRows struct {
	value    []byte
	columns  []string
	consumed bool
}

// NewSingleValueRows wraps a JSON payload as a single-column, single-row result.
func NewSingleValueRows(value []byte, columns []string) *SingleValueRows {
	return &SingleValueRows{value: value, columns: columns}
}

func (r *SingleValueRows) Columns() []string { return r.columns }
func (r *SingleValueRows) Close() error      { return nil }

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

// ColumnRows returns multi-column rows — used by schema introspection to hand
// GraphJin the table/column metadata contract.
type ColumnRows struct {
	columns []string
	data    [][]any
	index   int
}

// NewColumnRows creates multi-column rows.
func NewColumnRows(columns []string, data [][]any) *ColumnRows {
	return &ColumnRows{columns: columns, data: data}
}

func (r *ColumnRows) Columns() []string { return r.columns }
func (r *ColumnRows) Close() error      { return nil }

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
