package dialect

import (
	"fmt"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

// MariaDBDialect embeds MySQLDialect and provides MariaDB-specific behavior.
// MariaDB is a fork of MySQL and is largely compatible, but may have
// differences in JSON functions, version detection, and other features.
type MariaDBDialect struct {
	MySQLDialect
	DBVersion int
}

func (d *MariaDBDialect) Name() string {
	return "mariadb"
}

// SupportsLateral returns false for MariaDB.
// While MariaDB has some LATERAL support since 10.6, it does NOT support the
// LEFT OUTER JOIN LATERAL syntax that MySQL 8+ uses (see MDEV-33018).
// Instead, we use inline subqueries like SQLite.
func (d *MariaDBDialect) SupportsLateral() bool {
	return false
}

// SupportsReturning returns true for MariaDB 10.5+ which added RETURNING clause support.
func (d *MariaDBDialect) SupportsReturning() bool {
	return false
}

// RenderReturning renders the RETURNING clause for MariaDB 10.5+.
// MariaDB supports RETURNING * syntax similar to PostgreSQL.
func (d *MariaDBDialect) RenderReturning(ctx Context, m *qcode.Mutate) {
	if d.DBVersion >= 1050 {
		ctx.WriteString(` RETURNING *`)
	}
}

// RenderJSONPlural renders JSON array aggregation for MariaDB.
// Since MariaDB doesn't support LATERAL joins, we use inline subqueries
// and aggregate the "json" column from the inner query.
// Unlike MySQL, we don't CAST AS JSON since MariaDB doesn't support that syntax.
func (d *MariaDBDialect) RenderJSONPlural(ctx Context, sel *qcode.Select) {
	ctx.WriteString(`COALESCE(json_arrayagg(` + "`json`" + `), '[]')`)
}

// RenderJSONRootField wraps nested JSON values with JSON_QUERY to prevent
// MariaDB from double-escaping them. Since MariaDB treats JSON as LONGTEXT,
// json_object would otherwise escape the nested JSON as a string.
// Exception: __typename is a string literal, not JSON, so skip JSON_QUERY for it.
func (d *MariaDBDialect) RenderJSONRootField(ctx Context, key string, val func()) {
	ctx.WriteString(`'`)
	ctx.WriteString(key)
	if key == "__typename" {
		// __typename is a string literal, not JSON - don't wrap with JSON_QUERY
		ctx.WriteString(`', `)
		val()
	} else {
		ctx.WriteString(`', JSON_QUERY(`)
		val()
		ctx.WriteString(`, '$')`)
	}
}

// RenderValPrefix handles value prefix rendering for MariaDB.
// Unlike MySQL, MariaDB does not support CAST(... AS JSON), so we use
// JSON_QUERY to ensure the value is treated as JSON.
func (d *MariaDBDialect) RenderValPrefix(ctx Context, ex *qcode.Exp) bool {
	// Handle JSON key operations
	if ex.Op == qcode.OpHasKey ||
		ex.Op == qcode.OpHasKeyAny ||
		ex.Op == qcode.OpHasKeyAll {
		var optype string
		switch ex.Op {
		case qcode.OpHasKey, qcode.OpHasKeyAny:
			optype = "'one'"
		case qcode.OpHasKeyAll:
			optype = "'all'"
		}
		ctx.WriteString("JSON_CONTAINS_PATH(")
		ctx.ColWithTable(ex.Left.Col.Table, ex.Left.Col.Name)
		ctx.WriteString(", " + optype)
		for i := range ex.Right.ListVal {
			ctx.WriteString(`, '$.` + ex.Right.ListVal[i] + `'`)
		}
		ctx.WriteString(") = 1")
		return true
	}

	// Handle IN/NOT IN with JSON array variable
	// For scalar columns (integers, strings), pass the column value directly to JSON_CONTAINS
	// MariaDB will auto-convert scalar values to JSON when needed
	if ex.Right.ValType == qcode.ValVar &&
		(ex.Op == qcode.OpIn || ex.Op == qcode.OpNotIn) {
		if ex.Op == qcode.OpNotIn {
			ctx.WriteString(`NOT `)
		}
		ctx.WriteString(`JSON_CONTAINS(`)
		ctx.AddParam(Param{Name: ex.Right.Val, Type: ex.Left.Col.Type, IsArray: true})
		ctx.WriteString(`, `)
		ctx.ColWithTable(ex.Left.Col.Table, ex.Left.Col.Name)
		ctx.WriteString(`)`)
		return true
	}
	return false
}

// RenderValArrayColumn handles array value columns for MariaDB.
// Unlike MySQL, MariaDB does not support CAST(... AS JSON), so we
// rely on the column already containing valid JSON text.
func (d *MariaDBDialect) RenderValArrayColumn(ctx Context, ex *qcode.Exp, table string, pid int32) {
	ctx.WriteString(`SELECT _gj_jt.* FROM `)
	ctx.WriteString(`(SELECT `)

	t := table
	if pid >= 0 {
		t = fmt.Sprintf("%s_%d", table, pid)
	}
	ctx.ColWithTable(t, ex.Right.Col.Name)

	ctx.WriteString(` as ids) j, `)
	ctx.WriteString(`JSON_TABLE(j.ids, "$[*]" COLUMNS(`)
	ctx.WriteString(ex.Right.Col.Name)
	ctx.WriteString(` `)
	ctx.WriteString(ex.Left.Col.Type)
	ctx.WriteString(` PATH "$" ERROR ON ERROR)) AS _gj_jt`)
}

// MariaDB 10.6+ uses the same LEFT OUTER JOIN LATERAL syntax as MySQL 8+,
// so we inherit RenderLateralJoin and RenderLateralJoinClose from MySQLDialect.

// RenderTableAlias renders a table alias for MariaDB.
// Unlike MySQL, MariaDB requires stricter whitespace around identifiers.
func (d *MariaDBDialect) RenderTableAlias(ctx Context, alias string) {
	ctx.WriteString(` AS `)
	ctx.Quote(alias)
	ctx.WriteString(` `) // Trailing space needed for MariaDB SQL parser
}

// RenderCast handles type casting for MariaDB.
// MariaDB doesn't support CAST(... AS JSON) or CAST(... AS LONGTEXT),
// so we need to map these to supported types.
func (d *MariaDBDialect) RenderCast(ctx Context, val func(), typ string) {
	// MariaDB CAST supports: BINARY, CHAR, DATE, DATETIME, DECIMAL, NCHAR, SIGNED, TIME, UNSIGNED
	// Note: JSON and LONGTEXT are NOT supported as CAST targets in MariaDB
	switch typ {
	case "json", "longtext", "varchar", "character varying", "text", "string":
		// MariaDB treats JSON as LONGTEXT (string). No need to cast.
		val()
		return
	default:
		ctx.WriteString(`CAST(`)
		val()
		ctx.WriteString(` AS `)
	}

	switch typ {
	case "int", "integer", "int4", "int8", "bigint", "smallint":
		ctx.WriteString("SIGNED")
	case "boolean", "bool":
		ctx.WriteString("UNSIGNED")
	case "float", "double", "numeric", "real":
		ctx.WriteString("DECIMAL(65,30)")
	case "timestamp", "timestamptz", "timestamp without time zone", "timestamp with time zone":
		ctx.WriteString("DATETIME")
	case "date":
		ctx.WriteString("DATE")
	case "time", "timetz":
		ctx.WriteString("TIME")
	default:
		ctx.WriteString(typ)
	}
	ctx.WriteString(`)`)
}

// RenderJSONPath renders a JSON path extraction for MariaDB.
// MariaDB uses JSON_EXTRACT(col, '$.path') and we wrap it in JSON_UNQUOTE to get text.
// We do NOT use ->> operator to avoid syntax issues in older versions or specific contexts,
// even though 10.2+ supports it.
func (d *MariaDBDialect) RenderJSONPath(ctx Context, table, col string, path []string) {
	ctx.WriteString(`JSON_UNQUOTE(JSON_EXTRACT(`)
	ctx.ColWithTable(table, col)
	ctx.WriteString(`, '$.`)
	for i, p := range path {
		if i > 0 {
			ctx.WriteString(`.`)
		}
		ctx.WriteString(p)
	}
	ctx.WriteString(`'))`)
}

func (d *MariaDBDialect) RenderTableName(ctx Context, sel *qcode.Select, schema, table string) {
	if schema != "" {
		ctx.Quote(schema)
		ctx.WriteString(`.`)
	}
	ctx.Quote(table)
}

func (d *MariaDBDialect) ModifySelectsForMutation(qc *qcode.QCode) {}

func (d *MariaDBDialect) RenderQueryPrefix(ctx Context, qc *qcode.QCode) {}

func (d *MariaDBDialect) SplitQuery(query string) (parts []string) { return []string{query} }
