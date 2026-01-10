package dialect

import (
	"fmt"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)


type Param struct {
	Name        string
	Type        string
	IsArray     bool
	IsNotNull   bool
	WrapInArray bool
}

type Context interface {
	Write(s string) (int, error)
	WriteString(s string) (int, error) // io.StringWriter
	AddParam(p Param) string

	// Helpers commonly used by dialects
	Quote(s string)
	ColWithTable(table, col string)
	RenderJSONFields(sel *qcode.Select)
	IsTableMutated(table string) bool
	RenderExp(ti sdata.DBTable, ex *qcode.Exp)
}

// InlineChildRenderer is passed to dialects for rendering inline children
// It provides callbacks to compiler methods that dialects need
type InlineChildRenderer interface {
	RenderTable(sel *qcode.Select, schema, table string, alias bool)
	RenderJoin(join qcode.Join)
	RenderLimit(sel *qcode.Select)
	RenderOrderBy(sel *qcode.Select)
	RenderWhereExp(psel, sel *qcode.Select, ex interface{})
	RenderInlineChild(psel, sel *qcode.Select)
	RenderDefaultInlineChild(sel *qcode.Select) // For dialects that want to use the default implementation
	GetChild(id int32) *qcode.Select
	ColWithTable(table, col string)
	Quoted(s string)
	Squoted(s string)
	RenderExp(ti sdata.DBTable, ex *qcode.Exp)
	GetConfigVar(name string) (string, bool) // Returns config var value and whether it exists
	GetSecPrefix() string
	GetRootWithCursor() *qcode.Select // Returns first root select with cursor pagination
}

type Dialect interface {
	Name() string

	RenderLimit(ctx Context, sel *qcode.Select)
	RenderJSONRoot(ctx Context, sel *qcode.Select) 
	RenderJSONSelect(ctx Context, sel *qcode.Select)
	RenderJSONPlural(ctx Context, sel *qcode.Select)
	RenderLateralJoin(ctx Context, sel *qcode.Select, multi bool)
	RenderJoinTables(ctx Context, sel *qcode.Select)
	RenderCursorCTE(ctx Context, sel *qcode.Select)
	RenderOrderBy(ctx Context, sel *qcode.Select)
	RenderDistinctOn(ctx Context, sel *qcode.Select)
    RenderFromEdge(ctx Context, sel *qcode.Select) // For embedded/JSONTable vs RecordSet

	RenderJSONPath(ctx Context, table, col string, path []string)
	RenderList(ctx Context, ex *qcode.Exp)
	RenderOp(op qcode.ExpOp) (string, error)
	RenderValPrefix(ctx Context, ex *qcode.Exp) bool
	RenderTsQuery(ctx Context, ti sdata.DBTable, ex *qcode.Exp)
	RenderSearchRank(ctx Context, sel *qcode.Select, f qcode.Field)
	RenderSearchHeadline(ctx Context, sel *qcode.Select, f qcode.Field)
	RenderValVar(ctx Context, ex *qcode.Exp, val string) bool
	RenderValArrayColumn(ctx Context, ex *qcode.Exp, table string, pid int32)
	RenderArray(ctx Context, items []string)
	RenderLiteral(ctx Context, val string, valType qcode.ValType)
	RenderBooleanEqualsTrue(ctx Context, paramName string)
	RenderBooleanNotEqualsTrue(ctx Context, paramName string)
	RenderJSONField(ctx Context, fieldName string, tableAlias string, colName string, isNull bool, isJSON bool)
	RenderRootTerminator(ctx Context)
	RenderBaseTable(ctx Context)
	RenderJSONRootField(ctx Context, key string, val func())
	RenderTableName(ctx Context, sel *qcode.Select, schema, table string)
	RenderTableAlias(ctx Context, alias string)
	RenderLateralJoinClose(ctx Context, alias string)

	// Parameter Handling
	BindVar(i int) string
	UseNamedParams() bool
	SupportsLateral() bool
	
	// Identifier quoting - each dialect uses different quote characters
	QuoteIdentifier(s string) string
	
	// Inline child rendering for dialects without LATERAL support
	// renderer provides callbacks to compiler methods
	RenderInlineChild(ctx Context, renderer InlineChildRenderer, psel, sel *qcode.Select)
	RenderChildCursor(ctx Context, renderChild func())
	RenderChildValue(ctx Context, sel *qcode.Select, renderChild func())

	
	// Mutation and Subscriptions
	SupportsReturning() bool
	SupportsWritableCTE() bool
	SupportsConflictUpdate() bool
	SupportsSubscriptionBatching() bool

	RenderMutationCTE(ctx Context, m *qcode.Mutate, renderBody func())
	RenderMutationInput(ctx Context, qc *qcode.QCode)
	RenderMutationPostamble(ctx Context, qc *qcode.QCode)

	RenderInsert(ctx Context, m *qcode.Mutate, values func())
	RenderUpdate(ctx Context, m *qcode.Mutate, set func(), from func(), where func())
	RenderDelete(ctx Context, m *qcode.Mutate, where func())
	RenderUpsert(ctx Context, m *qcode.Mutate, insert func(), updateSet func())
	RenderReturning(ctx Context, m *qcode.Mutate)
	RenderAssign(ctx Context, col string, val string)
	RenderCast(ctx Context, val func(), typ string)
	RenderTryCast(ctx Context, val func(), typ string)
	
	RenderSubscriptionUnbox(ctx Context, params []Param, innerSQL string)

	// Linear Execution (for MySQL/SQLite)
	SupportsLinearExecution() bool
	RenderIDCapture(ctx Context, varName string)
	RenderVar(ctx Context, name string)
	RenderSetup(ctx Context)
	RenderBegin(ctx Context)
	RenderTeardown(ctx Context)
	RenderVarDeclaration(ctx Context, name, typeName string)
	RenderMutateToRecordSet(ctx Context, m *qcode.Mutate, n int, renderRoot func())
	RenderSetSessionVar(ctx Context, name, value string) bool
	
	RenderLinearInsert(ctx Context, m *qcode.Mutate, qc *qcode.QCode, varName string, renderColVal func(qcode.MColumn))
	RenderLinearUpdate(ctx Context, m *qcode.Mutate, qc *qcode.QCode, varName string, renderColVal func(qcode.MColumn), renderWhere func())
	RenderLinearConnect(ctx Context, m *qcode.Mutate, qc *qcode.QCode, varName string, renderFilter func())
	RenderLinearDisconnect(ctx Context, m *qcode.Mutate, qc *qcode.QCode, varName string, renderFilter func())

	ModifySelectsForMutation(qc *qcode.QCode)
	RenderQueryPrefix(ctx Context, qc *qcode.QCode)
	SplitQuery(query string) []string

	// Role Statement rendering (moves db-specific code from core/rolestmt.go)
	// These return strings since they're used outside the psql compiler context
	RoleSelectPrefix() string             // "SELECT TOP 1 (CASE" vs "SELECT (CASE"
	RoleLimitSuffix() string              // Close with/without LIMIT 1
	RoleDummyTable() string               // Database-specific dummy table
	TransformBooleanLiterals(match string) string   // "true"→"1" for MSSQL

	// Driver Behavior (moves db-specific code from core/args.go and core/core.go)
	RequiresJSONAsString() bool          // Oracle/MSSQL need JSON as string
	RequiresLowercaseIdentifiers() bool  // Oracle needs lowercase schemas
	RequiresBooleanAsInt() bool          // Oracle needs bool as 1/0 (PL/SQL BOOLEAN can't be used in SQL)

	// Recursive CTE Syntax (moves db-specific code from psql/recur.go)
	RequiresRecursiveKeyword() bool      // Oracle doesn't use RECURSIVE
	RequiresRecursiveCTEColumnList() bool // Oracle requires explicit column alias list
	RenderRecursiveOffset(ctx Context)   // OFFSET 1 vs LIMIT -1 OFFSET 1 vs LIMIT 1, MAX
	RenderRecursiveLimit1(ctx Context)   // LIMIT 1 vs FETCH FIRST 1 ROWS ONLY
	WrapRecursiveSelect() bool           // SQLite needs extra SELECT * FROM (...)
	// RenderRecursiveAnchorWhere renders the WHERE clause for recursive CTE anchor
	// Returns true if it handled the WHERE rendering, false to use default correlation
	// For Oracle/MSSQL: inline parent's WHERE expression (no outer scope correlation)
	// For Postgres/MySQL: return false to use default outer scope correlation
	RenderRecursiveAnchorWhere(ctx Context, psel *qcode.Select, ti sdata.DBTable, pkCol string) bool

	// JSON Null Fields (moves db-specific code from psql/query.go)
	RenderJSONNullField(ctx Context, fieldName string)       // NULL field syntax
	RenderJSONNullCursorField(ctx Context, fieldName string) // NULL cursor field syntax
	RenderJSONRootSuffix(ctx Context)                        // FOR JSON PATH for MSSQL, empty for others

	// Array Operations (moves db-specific code from psql/mutate.go)
	RenderArraySelectPrefix(ctx Context)                     // ARRAY(SELECT vs (SELECT JSON_ARRAYAGG(
	RenderArraySelectSuffix(ctx Context)                     // ) vs ))
	RenderArrayAggPrefix(ctx Context, distinct bool)         // ARRAY_AGG vs json_group_array vs JSON_ARRAYAGG
	RenderArrayRemove(ctx Context, col string, val func())   // array_remove vs JSON_REMOVE

	// Column rendering (moves db-specific code from psql/columns.go)
	RequiresJSONQueryWrapper() bool     // MariaDB needs JSON_QUERY wrapper for inline children
	RequiresNullOnEmptySelect() bool    // MySQL/SQLite/MariaDB need NULL when no columns rendered
}

// FullQueryCompiler is an optional interface that dialects can implement
// to handle entire query compilation themselves (bypassing SQL generation).
// This is used by MongoDB which generates JSON query DSL, not SQL.
type FullQueryCompiler interface {
	// CompileFullQuery generates the complete query output.
	// Returns true if it handled the compilation, false to use default SQL generation.
	CompileFullQuery(ctx Context, qc *qcode.QCode) bool
}

// FullMutationCompiler is an optional interface that dialects can implement
// to handle entire mutation compilation themselves (bypassing SQL generation).
// This is used by MongoDB which generates JSON mutation DSL, not SQL.
type FullMutationCompiler interface {
	// CompileFullMutation generates the complete mutation output.
	// Returns true if it handled the compilation, false to use default SQL generation.
	CompileFullMutation(ctx Context, qc *qcode.QCode) bool
}

func GenericRenderMutationPostamble(ctx Context, qc *qcode.QCode) {
	for k, cids := range qc.MUnions {
		if len(cids) < 2 {
			continue
		}
		ctx.WriteString(`, `)
		ctx.Quote(k)
		ctx.WriteString(` AS (`)

		i := 0
		for _, id := range cids {
			m := qc.Mutates[id]
			if m.Rel.Type == sdata.RelOneToMany &&
				(m.Type == qcode.MTConnect || m.Type == qcode.MTDisconnect) {
				continue
			}
			if i != 0 {
				ctx.WriteString(` UNION ALL `)
			}
			ctx.WriteString(`SELECT * FROM `)
			
			if m.Multi {
				ctx.WriteString(m.Ti.Name)
				ctx.WriteString(`_`)
				ctx.WriteString(fmt.Sprintf("%d", m.ID))
			} else {
				ctx.Quote(m.Ti.Name)
			}
			i++
		}

		ctx.WriteString(`)`)
	}
}


