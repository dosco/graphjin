package serv

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/v3"
)

// enhanceExecError augments a driver error with structured repair fields and a schema cross-check; falls back to enhanceError when unclassifiable.
func (ms *mcpServer) enhanceExecError(errMsg, currentTool string) string {
	if errMsg == "" {
		return errMsg
	}

	dbType := ms.execDBType()
	class := classifyExecError(dbType, errMsg)
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
		enhanced.RelatedTool = "query_catalog"
		enhanced.Hint = "Search query_catalog(where: { kind: { eq: \"table\" } }) to verify the table item, database, schema, and role-visible discovery surface."
	case ExecKindTypeMismatch:
		enhanced.Suggestion = "Value cannot be coerced to the column's type. Verify variable types and quoting."
		enhanced.RelatedTool = "query_catalog"
		enhanced.Hint = "Search query_catalog(where: { kind: { eq: \"column\" } }) for the column item so you can match the literal or variable shape."
	case ExecKindSyntaxError:
		enhanced.Suggestion = "Database rejected the generated SQL as syntactically invalid. This usually means an upstream compiler issue rather than a user mistake."
		enhanced.RelatedTool = "query_catalog"
		enhanced.Hint = "Use the graphjin_repair error extension and catalog rows to repair the original GraphQL."
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

func (ms *mcpServer) errorInfo(query, errMsg, currentTool string) ErrorInfo {
	info := ErrorInfo{Message: ms.enhanceExecError(errMsg, currentTool)}
	if repair := core.BuildGraphJinErrorRepair(query, errMsg); repair.Known() {
		info.Extensions = map[string]any{"graphjin_repair": repair}
	}
	return info
}

func (ms *mcpServer) errorInfoFromCoreError(query string, err core.Error, currentTool string) ErrorInfo {
	info := ErrorInfo{
		Message:    ms.enhanceExecError(err.Message, currentTool),
		Extensions: err.Extensions,
	}
	if len(info.Extensions) == 0 {
		if repair := core.BuildGraphJinErrorRepair(query, err.Message); repair.Known() {
			info.Extensions = map[string]any{"graphjin_repair": repair}
		}
	}
	return info
}

// fillColumnNotFoundAugment cross-checks against the schema and reframes the error when the column actually exists.
func (ms *mcpServer) fillColumnNotFoundAugment(enhanced *EnhancedError, class ExecErrorClass) {
	if class.Column == "" {
		enhanced.Suggestion = "Column not found. Use query_catalog(where: { kind: { eq: \"column\" } }) to see available columns."
		enhanced.RelatedTool = "query_catalog"
		return
	}

	exists, resolvedTable := ms.columnExistsAcrossSchema(class.Table, class.Column)
	if exists {
		enhanced.Table = resolvedTable
		enhanced.Hint = fmt.Sprintf(
			"Column '%s' DOES exist on '%s'. The driver-level 'does not exist' here is misleading — it usually means the SQL emitter dropped this column from a CTE projection (e.g. a distinct+aggregate GROUP BY collapsed it). Use the graphjin_repair error extension for structured repair guidance instead of re-checking spelling.",
			class.Column, resolvedTable)
		enhanced.Suggestion = "Misleading error — column actually exists on the table. Likely an upstream query-shape issue, not a typo."
		enhanced.RelatedTool = "query_catalog"
		return
	}

	enhanced.Suggestion = fmt.Sprintf(
		"Column '%s' not found%s. Use query_catalog(where: { kind: { eq: \"column\" } }) to see available columns.",
		class.Column, tableSuffix(class.Table))
	enhanced.RelatedTool = "query_catalog"
}

// columnExistsAcrossSchema reports whether the column exists on the named table; falls back to a full scan when table is empty.
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
