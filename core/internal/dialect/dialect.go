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
	
	// Mutation and Subscriptions
	SupportsReturning() bool
	SupportsWritableCTE() bool
	SupportsConflictUpdate() bool

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
	
	RenderSubscriptionUnbox(ctx Context, params []Param, renderInnerSQL func())

	// Linear Execution (for MySQL/SQLite)
	SupportsLinearExecution() bool
	RenderIDCapture(ctx Context, name string)
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


