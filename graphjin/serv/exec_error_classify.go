package serv

import (
	"regexp"
	"strings"
)

// Exec-time error classifier: dialect-specific error strings → {Kind, Table, Column}. Covers all 8 supported dialects.

const (
	ExecKindColumnNotFound = "column_not_found"
	ExecKindTableNotFound  = "table_not_found"
	ExecKindTypeMismatch   = "type_mismatch"
	ExecKindSyntaxError    = "syntax_error"
	ExecKindPermission     = "permission_denied"
	ExecKindUnknown        = ""
)

// ExecErrorClass is the parsed shape of a driver-level error.
type ExecErrorClass struct {
	Kind   string
	Table  string
	Column string
}

// dialectPatterns: per-dbType per-kind regex; first match wins, capture group 1 is the offending identifier.
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
		// MongoDB exposes projection/aggregation errors instead of column-not-exist.
		ExecKindColumnNotFound: regexp.MustCompile(`(?i)field path\s+'([^']+)'\s+is invalid|FieldPath field names may not start with`),
		ExecKindTableNotFound:  regexp.MustCompile(`(?i)collection\s+(\S+)\s+(?:does not exist|not found)|ns not found`),
		ExecKindTypeMismatch:   regexp.MustCompile(`(?i)cannot apply\s+\$\w+\s+to\s+\S+|\$\w+ requires`),
	},
}

// classifyExecError parses a driver error into {Kind, Table, Column}; empty dbType defaults to postgres.
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

// splitIdentTableColumn parses "col" / "table.col" / "schema.table.col"; single token treated as column.
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
		return parts[len(parts)-2], parts[len(parts)-1]
	}
}

// stripAliasSuffix unwinds the per-select numeric suffix (salesorderdetail_0 → salesorderdetail).
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

// normalizeDBType maps configured dbType values (incl. aliases like pg/sqlserver) to the canonical dialectPatterns key.
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
