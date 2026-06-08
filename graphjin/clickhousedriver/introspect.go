package clickhousedriver

import (
	"context"
	"database/sql/driver"
	"fmt"
)

// The 12-column contract GraphJin's schema discovery scans (see
// core/internal/introspection scanDiscoveredColumnRows). Column 5 is consumed as
// NotNull.
var introspectColumnCols = []string{
	"table_schema", "table_name", "column_name", "data_type",
	"is_nullable", "is_primary_key", "is_unique_key", "is_array",
	"is_fulltext", "fkey_schema", "fkey_table", "fkey_column",
}

func (c *Conn) targetDatabase(q *QueryDSL) string {
	if q.Database != "" {
		return q.Database
	}
	return c.database
}

// introspect serves GraphJin's boot-time schema discovery against system tables.
func (c *Conn) introspect(ctx context.Context, q *QueryDSL) (driver.Rows, error) {
	switch q.Operation {
	case OpIntrospectInfo:
		return c.introspectInfo(ctx, q)
	case OpIntrospectColumns:
		return c.introspectColumns(ctx, q)
	default:
		return nil, fmt.Errorf("clickhousedriver: unsupported introspection operation %q", q.Operation)
	}
}

// introspectInfo returns {version, schema, name}; the database is both schema and name.
func (c *Conn) introspectInfo(ctx context.Context, q *QueryDSL) (driver.Rows, error) {
	db := c.targetDatabase(q)
	rs, err := c.executor().Query(ctx, Statement{SQL: "SELECT version() AS v"})
	if err != nil {
		return nil, err
	}
	version := 0
	if len(rs.Rows) > 0 {
		fmt.Sscanf(asString(rs.Rows[0]["v"]), "%d", &version)
	}
	return NewColumnRows([]string{"version", "schema", "name"}, [][]any{{version, db, db}}), nil
}

// introspectColumns emits the 12-column contract from system.columns. ClickHouse
// has no FKs; is_in_primary_key (MergeTree PK prefix) stands in for primary/unique
// so GraphJin gets a cursor tie-breaker column.
func (c *Conn) introspectColumns(ctx context.Context, q *QueryDSL) (driver.Rows, error) {
	db := c.targetDatabase(q)
	rs, err := c.executor().Query(ctx, Statement{
		SQL: "SELECT c.database AS table_schema, c.table AS table_name, c.name AS column_name, " +
			"c.type AS data_type, c.is_in_primary_key AS is_pk, c.position AS position " +
			"FROM system.columns AS c " +
			"INNER JOIN system.tables AS t ON t.database = c.database AND t.name = c.table " +
			"WHERE c.database = ? " +
			"ORDER BY c.table, c.position",
		Args: []any{db},
	})
	if err != nil {
		return nil, err
	}

	rows := make([][]any, 0, len(rs.Rows))
	for _, r := range rs.Rows {
		dataType, isArray, isNullable := chTypeToSQL(asString(r["data_type"]))
		isPK := asBool(r["is_pk"])
		rows = append(rows, []any{
			asString(r["table_schema"]),
			asString(r["table_name"]),
			asString(r["column_name"]),
			dataType,
			!isNullable, // is_nullable column carries NotNull
			isPK,        // is_primary_key
			isPK,        // is_unique_key (approximation; CH PK is not unique)
			isArray,
			false, // is_fulltext
			"",    // fkey_schema
			"",    // fkey_table
			"",    // fkey_column
		})
	}
	return NewColumnRows(introspectColumnCols, rows), nil
}
