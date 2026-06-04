package cassandradriver

import (
	"context"
	"database/sql/driver"
	"fmt"
	"sort"
	"strings"
)

// The 12-column contract GraphJin's schema discovery scans (see
// core/internal/introspection scanDiscoveredColumnRows).
var introspectColumnCols = []string{
	"table_schema", "table_name", "column_name", "data_type",
	"is_nullable", "is_primary_key", "is_unique_key", "is_array",
	"is_fulltext", "fkey_schema", "fkey_table", "fkey_column",
}

func (c *Conn) executor() Executor {
	if c.exec != nil {
		return c.exec
	}
	return newGocqlExecutor(c.session)
}

func (c *Conn) targetKeyspace(q *QueryDSL) string {
	if q.Keyspace != "" {
		return q.Keyspace
	}
	return c.keyspace
}

// introspect serves GraphJin's boot-time schema discovery against system_schema.
func (c *Conn) introspect(ctx context.Context, q *QueryDSL) (driver.Rows, error) {
	switch q.Operation {
	case OpIntrospectInfo:
		return c.introspectInfo(ctx, q)
	case OpIntrospectColumns:
		return c.introspectColumns(ctx, q)
	case OpIntrospectKeys:
		return c.introspectKeys(ctx, q)
	default:
		return nil, fmt.Errorf("cassandradriver: unsupported introspection operation %q", q.Operation)
	}
}

// introspectInfo returns {version, schema, name} — the keyspace is both schema and name.
func (c *Conn) introspectInfo(ctx context.Context, q *QueryDSL) (driver.Rows, error) {
	ks := c.targetKeyspace(q)
	rs, err := c.executor().Query(ctx, Statement{
		CQL: "SELECT release_version FROM system.local", Idempotent: true,
	})
	if err != nil {
		return nil, err
	}
	version := 0
	if len(rs.Rows) > 0 {
		if v, ok := rs.Rows[0]["release_version"].(string); ok {
			fmt.Sscanf(v, "%d", &version)
		}
	}
	return NewColumnRows([]string{"version", "schema", "name"}, [][]any{{version, ks, ks}}), nil
}

// schemaColumn is one row of system_schema.columns.
type schemaColumn struct {
	table      string
	column     string
	kind       string // partition_key | clustering | regular | static
	position   int
	cqlType    string
	clustOrder string
}

func (c *Conn) fetchSchemaColumns(ctx context.Context, ks string) ([]schemaColumn, error) {
	rs, err := c.executor().Query(ctx, Statement{
		CQL: "SELECT table_name, column_name, kind, position, type, clustering_order " +
			"FROM system_schema.columns WHERE keyspace_name = ?",
		Args:       []any{ks},
		Idempotent: true,
	})
	if err != nil {
		return nil, err
	}
	out := make([]schemaColumn, 0, len(rs.Rows))
	for _, r := range rs.Rows {
		out = append(out, schemaColumn{
			table:      asString(r["table_name"]),
			column:     asString(r["column_name"]),
			kind:       asString(r["kind"]),
			position:   asInt(r["position"]),
			cqlType:    asString(r["type"]),
			clustOrder: asString(r["clustering_order"]),
		})
	}
	return out, nil
}

// introspectColumns emits the 12-column contract from system_schema.columns.
func (c *Conn) introspectColumns(ctx context.Context, q *QueryDSL) (driver.Rows, error) {
	ks := c.targetKeyspace(q)
	scols, err := c.fetchSchemaColumns(ctx, ks)
	if err != nil {
		return nil, err
	}

	rows := make([][]any, 0, len(scols))
	for _, sc := range scols {
		isKey := sc.kind == "partition_key" || sc.kind == "clustering"
		dataType, isArray := cqlTypeToSQL(sc.cqlType)
		rows = append(rows, []any{
			ks,        // table_schema
			sc.table,  // table_name
			sc.column, // column_name
			dataType,  // data_type
			isKey,     // is_nullable -> NotNull: keys are never null
			isKey,     // is_primary_key
			isKey,     // is_unique_key (approximation; cardinality derived from key roles)
			isArray,   // is_array
			false,     // is_fulltext
			"",        // fkey_schema
			"",        // fkey_table
			"",        // fkey_column
		})
	}
	return NewColumnRows(introspectColumnCols, rows), nil
}

// introspectKeys returns partition/clustering roles so core can populate
// DBTable.PartitionKeys / ClusteringKeys / clustering order.
func (c *Conn) introspectKeys(ctx context.Context, q *QueryDSL) (driver.Rows, error) {
	ks := c.targetKeyspace(q)
	scols, err := c.fetchSchemaColumns(ctx, ks)
	if err != nil {
		return nil, err
	}
	// Only key columns, ordered by table then key position.
	keys := make([]schemaColumn, 0, len(scols))
	for _, sc := range scols {
		if sc.kind == "partition_key" || sc.kind == "clustering" {
			keys = append(keys, sc)
		}
	}
	sort.SliceStable(keys, func(i, j int) bool {
		if keys[i].table != keys[j].table {
			return keys[i].table < keys[j].table
		}
		return keys[i].position < keys[j].position
	})

	cols := []string{"table_name", "column_name", "kind", "key_position", "clustering_order"}
	rows := make([][]any, 0, len(keys))
	for _, sc := range keys {
		rows = append(rows, []any{sc.table, sc.column, sc.kind, sc.position, sc.clustOrder})
	}
	return NewColumnRows(cols, rows), nil
}

// cqlTypeToSQL maps a CQL type to a GraphJin SQL type, returning whether the
// column is a collection (list/set → array).
func cqlTypeToSQL(cqlType string) (sqlType string, isArray bool) {
	t := normType(cqlType)
	if inner, ok := genericArgs(t, "list"); ok {
		st, _ := cqlTypeToSQL(inner)
		return st, true
	}
	if inner, ok := genericArgs(t, "set"); ok {
		st, _ := cqlTypeToSQL(inner)
		return st, true
	}
	if _, ok := genericArgs(t, "map"); ok {
		return "json", false
	}
	if _, ok := genericArgs(t, "tuple"); ok {
		return "json", false
	}
	switch t {
	case "text", "varchar", "ascii":
		return "text", false
	case "uuid", "timeuuid":
		return "uuid", false
	case "inet":
		return "text", false
	case "boolean":
		return "boolean", false
	case "tinyint", "smallint", "int":
		return "integer", false
	case "bigint", "counter":
		return "bigint", false
	case "float":
		return "real", false
	case "double":
		return "double precision", false
	case "decimal", "varint":
		return "numeric", false
	case "blob":
		return "bytea", false
	case "timestamp":
		return "timestamptz", false
	case "date":
		return "date", false
	case "time":
		return "time", false
	default:
		// frozen<...> already unwrapped by normType; unknown/UDT → json.
		if strings.Contains(t, "<") || strings.Contains(t, " ") {
			return "json", false
		}
		return "text", false
	}
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func asInt(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int32:
		return int(x)
	case int64:
		return int(x)
	case float64:
		return int(x)
	default:
		return 0
	}
}
