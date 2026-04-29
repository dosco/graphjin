package serv

import (
	"encoding/json"
	"fmt"
	"strings"
)

// enhanceExecError augments an exec-time error with structured repair
// fields, a dialect-derived Kind, and — most importantly — a schema
// cross-check. When a "column does not exist" error names a column that
// actually exists on the table, the underlying cause is upstream (the
// SQL emitter dropped the column from a CTE projection), and the right
// next step is fix_query_error, NOT describe_table.
//
// Returns either:
//   - the original errMsg unchanged, if no useful classification could
//     be made and the legacy substring matcher in enhanceError doesn't
//     hit either;
//   - a JSON-encoded EnhancedError with structured fields populated.
//
// Augments rather than replaces — Message preserves the raw driver text
// so consumers that already parse it keep working.
func (ms *mcpServer) enhanceExecError(errMsg, currentTool string) string {
	if errMsg == "" {
		return errMsg
	}

	dbType := ms.execDBType()
	class := classifyExecError(dbType, errMsg)

	// If the dialect classifier didn't recognize the shape, fall back to
	// the legacy substring-based enhanceError. This keeps coverage for
	// errors that don't fit any per-dialect regex (driver bug strings,
	// network errors that bubble up from below the SQL layer, etc.).
	if class.Kind == ExecKindUnknown {
		return enhanceError(errMsg, currentTool)
	}

	enhanced := EnhancedError{
		Message: errMsg,
		Kind:    class.Kind,
		Table:   class.Table,
		Column:  class.Column,
	}

	switch class.Kind {
	case ExecKindColumnNotFound:
		ms.fillColumnNotFoundAugment(&enhanced, class)
	case ExecKindTableNotFound:
		enhanced.Suggestion = "Table not found. Check spelling and the database/schema namespace; some dialects are case-sensitive."
		enhanced.RelatedTool = "list_tables"
		enhanced.Hint = "If the table genuinely exists, the error may be from an alias or the role/permission layer."
	case ExecKindTypeMismatch:
		enhanced.Suggestion = "Value cannot be coerced to the column's type. Verify variable types and quoting."
		enhanced.RelatedTool = "describe_table"
		enhanced.Hint = "describe_table reports each column's type so you can match the literal/variable shape."
	case ExecKindSyntaxError:
		enhanced.Suggestion = "Database rejected the generated SQL as syntactically invalid. This usually means an upstream compiler issue rather than a user mistake."
		enhanced.RelatedTool = "fix_query_error"
		enhanced.Hint = "Pass this error and the original GraphQL to fix_query_error for a structured repair."
	case ExecKindPermission:
		enhanced.Suggestion = "Current role lacks permission. Check role configuration or use a saved query that's allowlisted."
		enhanced.RelatedTool = "audit_role_permissions"
	default:
		enhanced.Suggestion = "Unrecognized execution error class."
	}

	data, err := json.Marshal(enhanced)
	if err != nil {
		return errMsg
	}
	return string(data)
}

// fillColumnNotFoundAugment is the dialect-agnostic schema cross-check.
// When the named column actually exists on the named table, the error is
// a downstream artifact of SQL generation (most commonly: distinct +
// aggregate dropped the column from a CTE projection — see Stage 3
// compile-time rejection). When the column genuinely doesn't exist, the
// suggestion stays at describe_table.
func (ms *mcpServer) fillColumnNotFoundAugment(enhanced *EnhancedError, class ExecErrorClass) {
	if class.Column == "" {
		enhanced.Suggestion = "Column not found. Use describe_table to see available columns."
		enhanced.RelatedTool = "describe_table"
		return
	}

	exists, resolvedTable := ms.columnExistsAcrossSchema(class.Table, class.Column)
	if exists {
		enhanced.Table = resolvedTable
		enhanced.Hint = fmt.Sprintf(
			"Column '%s' DOES exist on '%s'. The driver-level 'does not exist' here is misleading — it usually means the SQL emitter dropped this column from a CTE projection (e.g. a distinct+aggregate GROUP BY collapsed it). Pass this error and the original GraphQL to fix_query_error for a structured repair instead of re-checking spelling.",
			class.Column, resolvedTable)
		enhanced.Suggestion = "Misleading error — column actually exists on the table. Likely an upstream query-shape issue, not a typo."
		enhanced.RelatedTool = "fix_query_error"
		return
	}

	enhanced.Suggestion = fmt.Sprintf(
		"Column '%s' not found%s. Use describe_table to see available columns.",
		class.Column, tableSuffix(class.Table))
	enhanced.RelatedTool = "describe_table"
}

// columnExistsAcrossSchema returns whether the column exists on the
// named table, plus the resolved (case-corrected) table name. When the
// extracted table name is empty (some dialects don't emit it), it walks
// every table in the engine looking for the column — a crude fallback,
// but it's only invoked once per error and the worst case is still
// O(tables × columns), which is small.
func (ms *mcpServer) columnExistsAcrossSchema(table, column string) (bool, string) {
	if ms == nil || ms.service == nil || ms.service.gj == nil {
		return false, ""
	}
	gj := ms.service.gj
	if !gj.SchemaReady() {
		return false, ""
	}

	if table != "" {
		if schema, err := gj.GetTableSchema(table); err == nil && schema != nil {
			for _, c := range schema.Columns {
				if strings.EqualFold(c.Name, column) {
					return true, schema.Name
				}
			}
		}
	}

	// Fallback: search across all tables. Useful when the dialect didn't
	// give us a table name in the error (oracle ORA-00904 sometimes
	// drops it; mongodb projections, etc.).
	for _, t := range gj.GetTables() {
		if schema, err := gj.GetTableSchema(t.Name); err == nil && schema != nil {
			for _, c := range schema.Columns {
				if strings.EqualFold(c.Name, column) {
					return true, schema.Name
				}
			}
		}
	}
	return false, ""
}

// execDBType returns the canonical dbType key for the default database.
// Empty string is treated as postgres by classifyExecError.
func (ms *mcpServer) execDBType() string {
	if ms == nil || ms.service == nil || ms.service.gj == nil {
		return ""
	}
	gj := ms.service.gj
	if _, dbtype, err := gj.DBForDatabase(gj.DefaultDatabase()); err == nil {
		return dbtype
	}
	return ""
}

func tableSuffix(table string) string {
	if table == "" {
		return ""
	}
	return " on table '" + table + "'"
}
