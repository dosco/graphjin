package serv

import (
	"regexp"
	"strings"
)

// Exec-time error classifier. Pulls structured info (kind, table, column)
// out of dialect-specific error strings produced by the SQL drivers, so
// the augment layer can:
//   - emit a stable Kind field (consumers don't pattern-match prose)
//   - cross-check against the schema (a "column does not exist" error
//     where the column actually exists usually means the SQL emitter
//     dropped it from a CTE projection — point at fix_query_error rather
//     than describe_table)
//
// Coverage: postgres, mysql, mariadb, sqlite, oracle, mssql, snowflake,
// mongodb. Patterns derived from each driver's documented error formats.

// Kind constants. Stable strings — consumers may switch on them.
const (
	ExecKindColumnNotFound = "column_not_found"
	ExecKindTableNotFound  = "table_not_found"
	ExecKindTypeMismatch   = "type_mismatch"
	ExecKindSyntaxError    = "syntax_error"
	ExecKindPermission     = "permission_denied"
	ExecKindUnknown        = ""
)

// ExecErrorClass is the structured classification of a driver-level
// error message.
type ExecErrorClass struct {
	Kind   string
	Table  string // may be empty when not extractable
	Column string // may be empty when not extractable
}

// dialectPatterns maps each supported dbType to its per-kind regexes.
// Patterns capture the offending identifier in submatch group 1 (the
// extractor below normalizes "table.column" forms). Order within each
// dialect is significant — first match wins.
var dialectPatterns = map[string]map[string]*regexp.Regexp{
	"postgres": {
		ExecKindColumnNotFound: regexp.MustCompile(`(?i)column\s+"?([A-Za-z0-9_.]+)"?\s+does not exist`),
		ExecKindTableNotFound:  regexp.MustCompile(`(?i)relation\s+"([A-Za-z0-9_.]+)"\s+does not exist`),
		ExecKindTypeMismatch:   regexp.MustCompile(`(?i)invalid input syntax for (?:type )?(\S+):`),
		ExecKindSyntaxError:    regexp.MustCompile(`(?i)syntax error at or near\s+"([^"]+)"`),
		ExecKindPermission:     regexp.MustCompile(`(?i)permission denied for (?:relation|table|schema)\s+"?([A-Za-z0-9_.]+)"?`),
	},
	"mysql": {
		ExecKindColumnNotFound: regexp.MustCompile(`(?i)Unknown column\s+'([^']+)'`),
		ExecKindTableNotFound:  regexp.MustCompile(`(?i)Table\s+'(?:[^.']+\.)?([^']+)'\s+doesn't exist`),
		ExecKindTypeMismatch:   regexp.MustCompile(`(?i)Incorrect (?:integer|decimal|datetime|date|time|boolean) value:\s+'([^']+)'`),
		ExecKindSyntaxError:    regexp.MustCompile(`(?i)You have an error in your SQL syntax`),
		ExecKindPermission:     regexp.MustCompile(`(?i)Access denied|command denied to user`),
	},
	"mariadb": {
		ExecKindColumnNotFound: regexp.MustCompile(`(?i)Unknown column\s+'([^']+)'`),
		ExecKindTableNotFound:  regexp.MustCompile(`(?i)Table\s+'(?:[^.']+\.)?([^']+)'\s+doesn't exist`),
		ExecKindTypeMismatch:   regexp.MustCompile(`(?i)Incorrect (?:integer|decimal|datetime|date|time|boolean) value:\s+'([^']+)'`),
		ExecKindSyntaxError:    regexp.MustCompile(`(?i)You have an error in your SQL syntax`),
		ExecKindPermission:     regexp.MustCompile(`(?i)Access denied|command denied to user`),
	},
	"sqlite": {
		ExecKindColumnNotFound: regexp.MustCompile(`(?i)no such column:\s*([A-Za-z0-9_.]+)`),
		ExecKindTableNotFound:  regexp.MustCompile(`(?i)no such table:\s*([A-Za-z0-9_.]+)`),
		ExecKindTypeMismatch:   regexp.MustCompile(`(?i)datatype mismatch`),
		ExecKindSyntaxError:    regexp.MustCompile(`(?i)near\s+"([^"]+)":\s*syntax error`),
	},
	"oracle": {
		ExecKindColumnNotFound: regexp.MustCompile(`ORA-00904:\s*"?([^"]+)"?:\s*invalid identifier`),
		ExecKindTableNotFound:  regexp.MustCompile(`ORA-00942:\s*table or view does not exist`),
		ExecKindTypeMismatch:   regexp.MustCompile(`ORA-01722:\s*invalid number|ORA-01858:\s*a non-numeric character`),
		ExecKindSyntaxError:    regexp.MustCompile(`ORA-00936:\s*missing expression|ORA-00933:\s*SQL command not properly ended`),
		ExecKindPermission:     regexp.MustCompile(`ORA-00942|ORA-01031:\s*insufficient privileges`),
	},
	"mssql": {
		ExecKindColumnNotFound: regexp.MustCompile(`(?i)Invalid column name\s+'([^']+)'`),
		ExecKindTableNotFound:  regexp.MustCompile(`(?i)Invalid object name\s+'([^']+)'`),
		ExecKindTypeMismatch:   regexp.MustCompile(`(?i)Conversion failed when converting`),
		ExecKindSyntaxError:    regexp.MustCompile(`(?i)Incorrect syntax near\s+'([^']+)'`),
		ExecKindPermission:     regexp.MustCompile(`(?i)permission was denied|cannot find the (?:object|user)`),
	},
	"snowflake": {
		ExecKindColumnNotFound: regexp.MustCompile(`(?i)invalid identifier\s+'([^']+)'`),
		ExecKindTableNotFound:  regexp.MustCompile(`(?i)Object\s+'([^']+)'\s+does not exist or not authorized`),
		ExecKindTypeMismatch:   regexp.MustCompile(`(?i)Numeric value\s+'([^']+)'\s+is not recognized|Boolean value\s+'([^']+)'\s+is not recognized`),
		ExecKindSyntaxError:    regexp.MustCompile(`(?i)SQL compilation error.*syntax error`),
		ExecKindPermission:     regexp.MustCompile(`(?i)Insufficient privileges to operate on`),
	},
	"mongodb": {
		// MongoDB's failure model differs — the driver returns aggregation/projection errors
		// rather than column-not-exist. Patterns below cover the most common shapes we
		// surface up through the GraphJin MongoDB driver.
		ExecKindColumnNotFound: regexp.MustCompile(`(?i)field path\s+'([^']+)'\s+is invalid|FieldPath field names may not start with`),
		ExecKindTableNotFound:  regexp.MustCompile(`(?i)collection\s+(\S+)\s+(?:does not exist|not found)|ns not found`),
		ExecKindTypeMismatch:   regexp.MustCompile(`(?i)cannot apply\s+\$\w+\s+to\s+\S+|\$\w+ requires`),
	},
}

// classifyExecError parses an exec-time error string into a structured
// kind plus extracted identifiers. dbType uses the same canonical names
// as core/internal/sdata (postgres/mysql/mariadb/sqlite/oracle/mssql/
// snowflake/mongodb). Empty dbType is treated as postgres.
func classifyExecError(dbType, errMsg string) ExecErrorClass {
	if errMsg == "" {
		return ExecErrorClass{Kind: ExecKindUnknown}
	}
	patterns, ok := dialectPatterns[normalizeDBType(dbType)]
	if !ok {
		return ExecErrorClass{Kind: ExecKindUnknown}
	}

	// Iterate kinds in a stable order — most specific first. Postgres
	// for instance can emit both "column does not exist" and a syntax
	// error in the same message; column wins because it's the more
	// actionable signal.
	order := []string{
		ExecKindColumnNotFound,
		ExecKindTableNotFound,
		ExecKindTypeMismatch,
		ExecKindSyntaxError,
		ExecKindPermission,
	}
	for _, kind := range order {
		re, has := patterns[kind]
		if !has {
			continue
		}
		m := re.FindStringSubmatch(errMsg)
		if m == nil {
			continue
		}
		var ident string
		if len(m) > 1 {
			ident = m[1]
		}
		table, column := splitIdentTableColumn(ident)
		// For ColumnNotFound the captured ident may be just the column;
		// for TableNotFound it's the table.
		if kind == ExecKindTableNotFound {
			return ExecErrorClass{Kind: kind, Table: stripAliasSuffix(ident)}
		}
		return ExecErrorClass{
			Kind:   kind,
			Table:  stripAliasSuffix(table),
			Column: column,
		}
	}
	return ExecErrorClass{Kind: ExecKindUnknown}
}

// splitIdentTableColumn handles "table.column" and "schema.table.column"
// forms emitted by various drivers. When only a single identifier is
// present, it's treated as the column name.
func splitIdentTableColumn(ident string) (table, column string) {
	if ident == "" {
		return "", ""
	}
	parts := strings.Split(ident, ".")
	switch len(parts) {
	case 1:
		return "", parts[0]
	case 2:
		return parts[0], parts[1]
	default:
		// schema.table.column — keep last two
		return parts[len(parts)-2], parts[len(parts)-1]
	}
}

// stripAliasSuffix removes GraphJin's per-select alias suffix
// (e.g. "salesorderdetail_0" -> "salesorderdetail"). The compiler tags
// every select with a numeric index so its emitted CTEs don't collide;
// when a Postgres error references "salesorderdetail_0.salesorderid"
// the underlying table is salesorderdetail.
func stripAliasSuffix(name string) string {
	if name == "" {
		return name
	}
	i := strings.LastIndex(name, "_")
	if i <= 0 || i == len(name)-1 {
		return name
	}
	suf := name[i+1:]
	for _, r := range suf {
		if r < '0' || r > '9' {
			return name
		}
	}
	return name[:i]
}

// normalizeDBType maps configured dbType values to the canonical key used
// in dialectPatterns. Keeps "" → postgres (the historical default) and
// also accepts "pg" / "postgresql" aliases.
func normalizeDBType(dbType string) string {
	switch strings.ToLower(strings.TrimSpace(dbType)) {
	case "", "postgres", "postgresql", "pg":
		return "postgres"
	case "mysql":
		return "mysql"
	case "mariadb":
		return "mariadb"
	case "sqlite", "sqlite3":
		return "sqlite"
	case "oracle":
		return "oracle"
	case "mssql", "sqlserver":
		return "mssql"
	case "snowflake":
		return "snowflake"
	case "mongodb", "mongo":
		return "mongodb"
	default:
		return dbType
	}
}
