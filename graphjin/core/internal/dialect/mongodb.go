package dialect

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/graph"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// MongoDBDialect generates JSON query DSL instead of SQL.
// The JSON is parsed and executed by the mongodriver package.
type MongoDBDialect struct {
	EnableCamelcase bool
	pipelineDepth   int
	inPipeline      bool
	paramIndex      int
}

func (d *MongoDBDialect) Name() string {
	return "mongodb"
}

func (d *MongoDBDialect) QuoteIdentifier(s string) string {
	// MongoDB field names don't need quoting in JSON
	return s
}

// BindVar returns MongoDB parameter placeholder.
func (d *MongoDBDialect) BindVar(i int) string {
	return fmt.Sprintf("$%d", i)
}

func (d *MongoDBDialect) UseNamedParams() bool {
	return true
}

func (d *MongoDBDialect) SupportsLateral() bool {
	// MongoDB uses $lookup for joins, similar concept to lateral
	return true
}

func (d *MongoDBDialect) SupportsReturning() bool {
	// MongoDB returns documents after mutations
	return true
}

func (d *MongoDBDialect) SupportsWritableCTE() bool {
	return false // MongoDB doesn't have CTEs
}

func (d *MongoDBDialect) SupportsConflictUpdate() bool {
	return true // MongoDB upsert
}

func (d *MongoDBDialect) SupportsSubscriptionBatching() bool {
	return true // MongoDB change streams
}

func (d *MongoDBDialect) SupportsLinearExecution() bool {
	return false
}

// RenderJSONRoot starts the JSON query structure
func (d *MongoDBDialect) RenderJSONRoot(ctx Context, sel *qcode.Select) {
	if sel == nil {
		ctx.WriteString(`{"operation":"aggregate","collection":"unknown","pipeline":[`)
		d.inPipeline = true
		d.pipelineDepth = 0
		return
	}
	ctx.WriteString(`{"operation":"aggregate","collection":"`)
	ctx.WriteString(sel.Table)
	ctx.WriteString(`","pipeline":[`)
	d.inPipeline = true
	d.pipelineDepth = 0
}

func (d *MongoDBDialect) RenderJSONSelect(ctx Context, sel *qcode.Select) {
	// In MongoDB, we use $project stage instead of SELECT
	if d.pipelineDepth > 0 {
		ctx.WriteString(`,`)
	}
	ctx.WriteString(`{"$project":{`)
	d.renderProjectFields(ctx, sel)
	ctx.WriteString(`}}`)
	d.pipelineDepth++
}

func (d *MongoDBDialect) renderProjectFields(ctx Context, sel *qcode.Select) {
	first := true
	for _, f := range sel.Fields {
		if !first {
			ctx.WriteString(`,`)
		}
		ctx.WriteString(`"`)
		ctx.WriteString(f.Col.Name)
		ctx.WriteString(`":1`)
		first = false
	}
	// Always include _id
	if first {
		ctx.WriteString(`"_id":1`)
	}
}

func (d *MongoDBDialect) RenderJSONPlural(ctx Context, sel *qcode.Select) {
	// For plural results, we just close the aggregate
	// The driver will return results as an array
}

func (d *MongoDBDialect) RenderLateralJoin(ctx Context, sel *qcode.Select, multi bool) {
	// MongoDB uses $lookup for joins
	if sel.Rel.Type == sdata.RelNone && !multi {
		return
	}

	if d.pipelineDepth > 0 {
		ctx.WriteString(`,`)
	}

	rel := sel.Rel

	ctx.WriteString(`{"$lookup":{`)
	ctx.WriteString(`"from":"`)
	ctx.WriteString(sel.Table)
	ctx.WriteString(`","localField":"`)

	// Determine local and foreign fields based on relationship
	switch rel.Type {
	case sdata.RelOneToOne, sdata.RelOneToMany:
		ctx.WriteString(rel.Right.Col.Name)
		ctx.WriteString(`","foreignField":"`)
		ctx.WriteString(rel.Left.Col.Name)
	default:
		ctx.WriteString("_id")
		ctx.WriteString(`","foreignField":"`)
		ctx.WriteString(sel.Table + "_id")
	}

	ctx.WriteString(`","as":"`)
	ctx.WriteString(sel.FieldName)
	ctx.WriteString(`"}}`)
	d.pipelineDepth++
}

func (d *MongoDBDialect) RenderJoinTables(ctx Context, sel *qcode.Select) {
	// MongoDB doesn't have traditional JOIN tables
}

func (d *MongoDBDialect) RenderCursorCTE(ctx Context, sel *qcode.Select) {
	// MongoDB handles cursors differently - using $skip/$limit
}

func (d *MongoDBDialect) RenderLimit(ctx Context, sel *qcode.Select) {
	if sel.Paging.NoLimit {
		return
	}

	// Add $skip first if there's an offset
	if sel.Paging.Offset > 0 || sel.Paging.OffsetVar != "" {
		if d.pipelineDepth > 0 {
			ctx.WriteString(`,`)
		}
		ctx.WriteString(`{"$skip":`)
		if sel.Paging.OffsetVar != "" {
			ctx.AddParam(Param{Name: sel.Paging.OffsetVar, Type: "integer"})
		} else {
			ctx.WriteString(strconv.Itoa(int(sel.Paging.Offset)))
		}
		ctx.WriteString(`}`)
		d.pipelineDepth++
	}

	// Add $limit
	if d.pipelineDepth > 0 {
		ctx.WriteString(`,`)
	}
	ctx.WriteString(`{"$limit":`)
	if sel.Paging.LimitVar != "" {
		ctx.AddParam(Param{Name: sel.Paging.LimitVar, Type: "integer"})
	} else {
		ctx.WriteString(strconv.Itoa(int(sel.Paging.Limit)))
	}
	ctx.WriteString(`}`)
	d.pipelineDepth++
}

func (d *MongoDBDialect) RenderOrderBy(ctx Context, sel *qcode.Select) {
	if len(sel.OrderBy) == 0 {
		return
	}

	if d.pipelineDepth > 0 {
		ctx.WriteString(`,`)
	}
	ctx.WriteString(`{"$sort":{`)

	for i, ob := range sel.OrderBy {
		if i != 0 {
			ctx.WriteString(`,`)
		}
		ctx.WriteString(`"`)
		ctx.WriteString(ob.Col.Name)
		ctx.WriteString(`":`)

		switch ob.Order {
		case qcode.OrderAsc, qcode.OrderAscNullsFirst, qcode.OrderAscNullsLast:
			ctx.WriteString(`1`)
		case qcode.OrderDesc, qcode.OrderDescNullsFirst, qcode.OrderDescNullsLast:
			ctx.WriteString(`-1`)
		default:
			ctx.WriteString(`1`)
		}
	}
	ctx.WriteString(`}}`)
	d.pipelineDepth++
}

func (d *MongoDBDialect) RenderDistinctOn(ctx Context, sel *qcode.Select) {
	// MongoDB uses $group for distinct
	if len(sel.DistinctOn) == 0 {
		return
	}

	if d.pipelineDepth > 0 {
		ctx.WriteString(`,`)
	}
	ctx.WriteString(`{"$group":{"_id":{`)
	for i, col := range sel.DistinctOn {
		if i != 0 {
			ctx.WriteString(`,`)
		}
		ctx.WriteString(`"`)
		ctx.WriteString(col.Name)
		ctx.WriteString(`":"$`)
		ctx.WriteString(col.Name)
		ctx.WriteString(`"`)
	}
	ctx.WriteString(`}}}`)
	d.pipelineDepth++
}

func (d *MongoDBDialect) RenderFromEdge(ctx Context, sel *qcode.Select) {
	// MongoDB doesn't have FROM clause in same sense
}

func (d *MongoDBDialect) RenderJSONPath(ctx Context, table, col string, path []string) {
	// MongoDB uses dot notation for nested fields
	ctx.WriteString(`"`)
	ctx.WriteString(col)
	for _, p := range path {
		ctx.WriteString(`.`)
		ctx.WriteString(p)
	}
	ctx.WriteString(`"`)
}

func (d *MongoDBDialect) RenderList(ctx Context, ex *qcode.Exp) {
	ctx.WriteString(`[`)
	for i, v := range ex.Right.ListVal {
		if i != 0 {
			ctx.WriteString(`,`)
		}
		d.RenderLiteral(ctx, v, ex.Right.ListType)
	}
	ctx.WriteString(`]`)
}

func (d *MongoDBDialect) RenderOp(op qcode.ExpOp) (string, error) {
	// Map GraphJin operators to MongoDB operators
	switch op {
	case qcode.OpEquals:
		return "$eq", nil
	case qcode.OpNotEquals:
		return "$ne", nil
	case qcode.OpGreaterThan:
		return "$gt", nil
	case qcode.OpGreaterOrEquals:
		return "$gte", nil
	case qcode.OpLesserThan:
		return "$lt", nil
	case qcode.OpLesserOrEquals:
		return "$lte", nil
	case qcode.OpIn:
		return "$in", nil
	case qcode.OpNotIn:
		return "$nin", nil
	case qcode.OpLike:
		return "$regex", nil
	case qcode.OpILike:
		return "$regex", nil // with "i" option
	case qcode.OpIsNull:
		return "$eq", nil // check for null
	case qcode.OpIsNotNull:
		return "$ne", nil // check not null
	case qcode.OpAnd:
		return "$and", nil
	case qcode.OpOr:
		return "$or", nil
	case qcode.OpNot:
		return "$not", nil
	default:
		return "", fmt.Errorf("mongodb: unsupported operator %d", op)
	}
}

func (d *MongoDBDialect) RenderValPrefix(ctx Context, ex *qcode.Exp) bool {
	return false
}

func (d *MongoDBDialect) RenderTsQuery(ctx Context, ti sdata.DBTable, ex *qcode.Exp) {
	// MongoDB full-text search uses $text operator
	ctx.WriteString(`{"$text":{"$search":`)
	ctx.AddParam(Param{Name: ex.Right.Val, Type: "text"})
	ctx.WriteString(`}}`)
}

func (d *MongoDBDialect) RenderSearchRank(ctx Context, sel *qcode.Select, f qcode.Field) {
	// MongoDB uses $meta textScore
	ctx.WriteString(`{"$meta":"textScore"}`)
}

func (d *MongoDBDialect) RenderSearchHeadline(ctx Context, sel *qcode.Select, f qcode.Field) {
	// MongoDB doesn't have direct headline equivalent
}

func (d *MongoDBDialect) RenderValVar(ctx Context, ex *qcode.Exp, val string) bool {
	return false
}

func (d *MongoDBDialect) RenderValArrayColumn(ctx Context, ex *qcode.Exp, table string, pid int32) {
}

func (d *MongoDBDialect) RenderArray(ctx Context, items []string) {
	ctx.WriteString(`[`)
	for i, item := range items {
		if i != 0 {
			ctx.WriteString(`,`)
		}
		ctx.WriteString(`"`)
		ctx.WriteString(item)
		ctx.WriteString(`"`)
	}
	ctx.WriteString(`]`)
}

func (d *MongoDBDialect) RenderLiteral(ctx Context, val string, valType qcode.ValType) {
	switch valType {
	case qcode.ValNum:
		ctx.WriteString(val)
	case qcode.ValBool:
		ctx.WriteString(val)
	case qcode.ValStr:
		ctx.WriteString(`"`)
		ctx.WriteString(escapeJSONString(val))
		ctx.WriteString(`"`)
	default:
		ctx.WriteString(`"`)
		ctx.WriteString(escapeJSONString(val))
		ctx.WriteString(`"`)
	}
}

func (d *MongoDBDialect) RenderBooleanEqualsTrue(ctx Context, paramName string) {
	ctx.WriteString(`{"`)
	ctx.WriteString(paramName)
	ctx.WriteString(`":true}`)
}

func (d *MongoDBDialect) RenderBooleanNotEqualsTrue(ctx Context, paramName string) {
	ctx.WriteString(`{"`)
	ctx.WriteString(paramName)
	ctx.WriteString(`":{"$ne":true}}`)
}

func (d *MongoDBDialect) RenderJSONField(ctx Context, fieldName string, tableAlias string, colName string, isNull bool, isJSON bool) {
	ctx.WriteString(`"`)
	ctx.WriteString(fieldName)
	ctx.WriteString(`":"$`)
	ctx.WriteString(colName)
	ctx.WriteString(`"`)
}

func (d *MongoDBDialect) RenderRootTerminator(ctx Context) {
	// Close the pipeline array and root object
	ctx.WriteString(`]}`)
}

func (d *MongoDBDialect) RenderBaseTable(ctx Context) {
	// MongoDB doesn't have dual table concept
}

func (d *MongoDBDialect) RenderJSONRootField(ctx Context, key string, val func()) {
	ctx.WriteString(`"`)
	ctx.WriteString(key)
	ctx.WriteString(`":`)
	val()
}

func (d *MongoDBDialect) RenderTableName(ctx Context, sel *qcode.Select, schema, table string) {
	ctx.WriteString(table)
}

func (d *MongoDBDialect) RenderTableAlias(ctx Context, alias string) {
	// MongoDB doesn't use table aliases in same way
}

func (d *MongoDBDialect) RenderLateralJoinClose(ctx Context, alias string) {
	// MongoDB $lookup is self-contained
}

func (d *MongoDBDialect) RenderInlineChild(ctx Context, renderer InlineChildRenderer, psel, sel *qcode.Select) {
	// MongoDB handles nested documents inline
	renderer.RenderDefaultInlineChild(sel)
}

func (d *MongoDBDialect) RenderChildCursor(ctx Context, renderChild func()) {
	renderChild()
}

func (d *MongoDBDialect) RenderChildValue(ctx Context, sel *qcode.Select, renderChild func()) {
	renderChild()
}

// Mutation methods

func (d *MongoDBDialect) RenderMutationCTE(ctx Context, m *qcode.Mutate, renderBody func()) {
	renderBody()
}

func (d *MongoDBDialect) RenderMutationInput(ctx Context, qc *qcode.QCode) {
}

func (d *MongoDBDialect) RenderMutationPostamble(ctx Context, qc *qcode.QCode) {
}

func (d *MongoDBDialect) RenderInsert(ctx Context, m *qcode.Mutate, values func()) {
	ctx.WriteString(`{"operation":"insertOne","collection":"`)
	ctx.WriteString(m.Ti.Name)
	ctx.WriteString(`","document":{`)
	values()
	ctx.WriteString(`}}`)
}

func (d *MongoDBDialect) RenderUpdate(ctx Context, m *qcode.Mutate, set func(), from func(), where func()) {
	ctx.WriteString(`{"operation":"updateMany","collection":"`)
	ctx.WriteString(m.Ti.Name)
	ctx.WriteString(`","filter":{`)
	where()
	ctx.WriteString(`},"update":{"$set":{`)
	set()
	ctx.WriteString(`}}}`)
}

func (d *MongoDBDialect) RenderDelete(ctx Context, m *qcode.Mutate, where func()) {
	ctx.WriteString(`{"operation":"deleteMany","collection":"`)
	ctx.WriteString(m.Ti.Name)
	ctx.WriteString(`","filter":{`)
	where()
	ctx.WriteString(`}}`)
}

func (d *MongoDBDialect) RenderUpsert(ctx Context, m *qcode.Mutate, insert func(), updateSet func()) {
	ctx.WriteString(`{"operation":"updateOne","collection":"`)
	ctx.WriteString(m.Ti.Name)
	ctx.WriteString(`","filter":{`)
	// The filter would be based on unique key
	ctx.WriteString(`},"update":{"$set":{`)
	updateSet()
	ctx.WriteString(`}},"options":{"upsert":true}}`)
}

func (d *MongoDBDialect) RenderReturning(ctx Context, m *qcode.Mutate) {
	// MongoDB returns documents automatically
}

func (d *MongoDBDialect) RenderAssign(ctx Context, col string, val string) {
	ctx.WriteString(`"`)
	ctx.WriteString(col)
	ctx.WriteString(`":`)
	ctx.WriteString(val)
}

func (d *MongoDBDialect) RenderCast(ctx Context, val func(), typ string) {
	val()
}

func (d *MongoDBDialect) RenderTryCast(ctx Context, val func(), typ string) {
	val()
}

func (d *MongoDBDialect) RenderSubscriptionUnbox(ctx Context, params []Param, innerSQL string) {
	// MongoDB change streams
	ctx.WriteString(innerSQL)
}

// Linear execution methods (not supported for MongoDB)

func (d *MongoDBDialect) RenderIDCapture(ctx Context, varName string) {}
func (d *MongoDBDialect) RenderVar(ctx Context, name string)          {}
func (d *MongoDBDialect) RenderSetup(ctx Context)                     {}
func (d *MongoDBDialect) RenderBegin(ctx Context)                     {}
func (d *MongoDBDialect) RenderTeardown(ctx Context)                  {}
func (d *MongoDBDialect) RenderVarDeclaration(ctx Context, name, typeName string) {
}

func (d *MongoDBDialect) RenderMutateToRecordSet(ctx Context, m *qcode.Mutate, n int, renderRoot func()) {
	renderRoot()
}

func (d *MongoDBDialect) RenderSetSessionVar(ctx Context, name, value string) bool {
	return false
}

func (d *MongoDBDialect) RenderLinearInsert(ctx Context, m *qcode.Mutate, qc *qcode.QCode, varName string, renderColVal func(qcode.MColumn)) {
}

func (d *MongoDBDialect) RenderLinearUpdate(ctx Context, m *qcode.Mutate, qc *qcode.QCode, varName string, renderColVal func(qcode.MColumn), renderWhere func()) {
}

func (d *MongoDBDialect) RenderLinearConnect(ctx Context, m *qcode.Mutate, qc *qcode.QCode, varName string, renderFilter func()) {
}

func (d *MongoDBDialect) RenderLinearDisconnect(ctx Context, m *qcode.Mutate, qc *qcode.QCode, varName string, renderFilter func()) {
}

func (d *MongoDBDialect) ModifySelectsForMutation(qc *qcode.QCode) {}

func (d *MongoDBDialect) RenderQueryPrefix(ctx Context, qc *qcode.QCode) {}

func (d *MongoDBDialect) SplitQuery(query string) []string {
	return []string{query}
}

// Role statement methods

func (d *MongoDBDialect) RoleSelectPrefix() string {
	return ""
}

func (d *MongoDBDialect) RoleLimitSuffix() string {
	return ""
}

func (d *MongoDBDialect) RoleDummyTable() string {
	return ""
}

func (d *MongoDBDialect) TransformBooleanLiterals(match string) string {
	return match
}

// Driver behavior methods

func (d *MongoDBDialect) RequiresJSONAsString() bool {
	return false // MongoDB handles JSON natively
}

func (d *MongoDBDialect) RequiresLowercaseIdentifiers() bool {
	return false
}

func (d *MongoDBDialect) RequiresBooleanAsInt() bool {
	return false
}

// Recursive CTE methods (not supported)

func (d *MongoDBDialect) RequiresRecursiveKeyword() bool {
	return false
}

func (d *MongoDBDialect) RequiresRecursiveCTEColumnList() bool {
	return false
}

func (d *MongoDBDialect) RenderRecursiveOffset(ctx Context) {}

func (d *MongoDBDialect) RenderRecursiveLimit1(ctx Context) {}

func (d *MongoDBDialect) WrapRecursiveSelect() bool {
	return false
}

func (d *MongoDBDialect) RenderRecursiveAnchorWhere(ctx Context, psel *qcode.Select, ti sdata.DBTable, pkCol string) bool {
	return false
}

// JSON null field methods

func (d *MongoDBDialect) RenderJSONNullField(ctx Context, fieldName string) {
	ctx.WriteString(`"`)
	ctx.WriteString(fieldName)
	ctx.WriteString(`":null`)
}

func (d *MongoDBDialect) RenderJSONNullCursorField(ctx Context, fieldName string) {
	d.RenderJSONNullField(ctx, fieldName)
}

func (d *MongoDBDialect) RenderJSONRootSuffix(ctx Context) {
	// No suffix needed for MongoDB
}

// Array operations

func (d *MongoDBDialect) RenderArraySelectPrefix(ctx Context) {
	ctx.WriteString(`[`)
}

func (d *MongoDBDialect) RenderArraySelectSuffix(ctx Context) {
	ctx.WriteString(`]`)
}

func (d *MongoDBDialect) RenderArrayAggPrefix(ctx Context, distinct bool) {
	ctx.WriteString(`{"$push":`)
}

func (d *MongoDBDialect) RenderArrayRemove(ctx Context, col string, val func()) {
	ctx.WriteString(`{"$pull":{"`)
	ctx.WriteString(col)
	ctx.WriteString(`":`)
	val()
	ctx.WriteString(`}}`)
}

// Column rendering

func (d *MongoDBDialect) RequiresJSONQueryWrapper() bool {
	return false
}

func (d *MongoDBDialect) RequiresNullOnEmptySelect() bool {
	return false
}

// Helper to escape JSON strings
func escapeJSONString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}

// CompileFullMutation implements FullMutationCompiler interface.
// It generates the complete JSON mutation DSL for MongoDB, bypassing SQL generation.
func (d *MongoDBDialect) CompileFullMutation(ctx Context, qc *qcode.QCode) bool {
	if len(qc.Mutates) == 0 {
		return false
	}

	// Collect all root-level mutations (ParentID == -1) of the same type
	// This handles inline bulk inserts where each array element becomes a separate Mutate
	var rootMutations []*qcode.Mutate
	for i := range qc.Mutates {
		m := &qc.Mutates[i]
		if m.ParentID != -1 {
			continue
		}
		switch m.Type {
		case qcode.MTInsert, qcode.MTUpdate, qcode.MTDelete, qcode.MTUpsert:
			rootMutations = append(rootMutations, m)
		}
	}

	if len(rootMutations) == 0 {
		return false
	}

	// For inline bulk inserts: multiple root mutations of type MTInsert
	if len(rootMutations) > 1 && rootMutations[0].Type == qcode.MTInsert {
		d.renderInsertManyMutation(ctx, qc, rootMutations)
		return true
	}

	// Check if there are child mutations (nested inserts into related tables)
	// Note: MTConnect for FK relationships (different tables) are handled in renderInsertMutation
	// Only MTConnect for recursive relationships (same table) should trigger nested insert
	hasChildMutations := false
	for i := range qc.Mutates {
		m := &qc.Mutates[i]
		if m.ParentID != -1 {
			if m.Type == qcode.MTInsert {
				hasChildMutations = true
				break
			}
			// For connect operations, only include recursive connects (same table)
			if m.Type == qcode.MTConnect {
				// Find parent mutation to compare tables
				for j := range qc.Mutates {
					parent := &qc.Mutates[j]
					if parent.ID == m.ParentID && parent.Ti.Name == m.Ti.Name {
						hasChildMutations = true
						break
					}
				}
				if hasChildMutations {
					break
				}
			}
		}
	}

	// Single mutation - use existing logic
	rootMutate := rootMutations[0]

	// For nested inserts, generate multi-collection operation
	if hasChildMutations && rootMutate.Type == qcode.MTInsert {
		d.renderNestedInsertMutation(ctx, qc, rootMutate)
		return true
	}

	switch rootMutate.Type {
	case qcode.MTInsert:
		d.renderInsertMutation(ctx, qc, rootMutate)
	case qcode.MTUpdate:
		d.renderUpdateMutation(ctx, qc, rootMutate)
	case qcode.MTDelete:
		d.renderDeleteMutation(ctx, qc, rootMutate)
	case qcode.MTUpsert:
		d.renderUpsertMutation(ctx, qc, rootMutate)
	}

	return true
}

// renderInsertMutation generates a MongoDB insertOne operation
func (d *MongoDBDialect) renderInsertMutation(ctx Context, qc *qcode.QCode, m *qcode.Mutate) {
	ctx.WriteString(`{"operation":"insertOne","collection":"`)
	ctx.WriteString(m.Ti.Name)
	ctx.WriteString(`"`)

	// Check if we have a single variable (ActionVar) or individual field variables
	if qc.ActionVar != "" {
		// Case 1: Single variable - use raw_document with parameter placeholder
		ctx.WriteString(`,"raw_document":"`)
		ctx.AddParam(Param{Name: qc.ActionVar, Type: "json"})
		ctx.WriteString(`"`)

		// Also output presets separately - driver will merge them into the document
		d.renderPresets(ctx, m)
	} else {
		// Case 2: Individual field variables - build document inline
		ctx.WriteString(`,"document":{`)
		d.renderInsertDocument(ctx, m)
		ctx.WriteString(`}`)
	}

	// Find connect operations and add metadata for runtime transformation
	// Two types of connects:
	// 1. Array column connects: categories.connect.id -> category_ids (array)
	// 2. FK connects: owner.connect.id -> owner_id (single value)
	for i := range qc.Mutates {
		cm := &qc.Mutates[i]
		if cm.Type != qcode.MTConnect || cm.ParentID != m.ID {
			continue
		}
		// Check if this is an array column connect or FK connect
		if cm.Rel.Right.Col.Array {
			// Array column connect: categories.connect.id -> category_ids
			ctx.WriteString(`,"connect_column":"`)
			ctx.WriteString(cm.Rel.Right.Col.Name)
			ctx.WriteString(`"`)
		} else if cm.Rel.Type == sdata.RelOneToOne || cm.Rel.Type == sdata.RelOneToMany {
			// FK connect: owner.connect.id -> owner_id
			// The field name (e.g., "owner") is in cm.Path[0], not cm.Key
			// cm.Key is "connect" for connect operations
			if len(cm.Path) > 0 {
				ctx.WriteString(`,"fk_connect":{"path":"`)
				ctx.WriteString(cm.Path[0]) // "owner"
				ctx.WriteString(`","column":"`)
				ctx.WriteString(cm.Rel.Right.Col.Name) // "owner_id"
				ctx.WriteString(`"}`)
			}
		}
	}

	// Add field_name for result wrapping
	var rootSel *qcode.Select
	if len(qc.Roots) > 0 {
		rootSel = &qc.Selects[qc.Roots[0]]
		ctx.WriteString(`,"field_name":"`)
		ctx.WriteString(rootSel.FieldName)
		ctx.WriteString(`"`)
	}

	// Add return_pipeline for fetching related data after insert
	// This is the aggregation pipeline to run after the insert to get the response
	if rootSel != nil && (len(rootSel.Fields) > 0 || len(rootSel.Children) > 0) {
		ctx.WriteString(`,"return_pipeline":[`)

		// Generate $lookup stages for children (relationships)
		pipelineDepth := 0
		for _, childID := range rootSel.Children {
			child := &qc.Selects[childID]
			if child.SkipRender != qcode.SkipTypeNone {
				continue
			}
			if pipelineDepth > 0 {
				ctx.WriteString(`,`)
			}
			d.renderLookupStage(ctx, rootSel, child)
			pipelineDepth++
		}

		// Add $project stage for field selection
		if pipelineDepth > 0 {
			ctx.WriteString(`,`)
		}
		d.renderProjectStageWithChildren(ctx, rootSel, qc)

		ctx.WriteString(`]`)
	}

	ctx.WriteString(`}`)
}

// renderInsertManyMutation generates a MongoDB insertMany operation for inline bulk inserts
func (d *MongoDBDialect) renderInsertManyMutation(ctx Context, qc *qcode.QCode, mutations []*qcode.Mutate) {
	if len(mutations) == 0 {
		return
	}

	m := mutations[0] // Use first for collection name and return pipeline
	ctx.WriteString(`{"operation":"insertMany","collection":"`)
	ctx.WriteString(m.Ti.Name)
	ctx.WriteString(`","documents":[`)

	// Build each document from the mutations
	for i, mut := range mutations {
		if i > 0 {
			ctx.WriteString(`,`)
		}
		ctx.WriteString(`{`)
		d.renderInsertDocument(ctx, mut)
		ctx.WriteString(`}`)
	}

	ctx.WriteString(`]`)

	// Add field_name for result wrapping
	var rootSel *qcode.Select
	if len(qc.Roots) > 0 {
		rootSel = &qc.Selects[qc.Roots[0]]
		ctx.WriteString(`,"field_name":"`)
		ctx.WriteString(rootSel.FieldName)
		ctx.WriteString(`"`)
	}

	// Add return_pipeline for fetching related data after insert
	if rootSel != nil && (len(rootSel.Fields) > 0 || len(rootSel.Children) > 0 || len(rootSel.OrderBy) > 0) {
		ctx.WriteString(`,"return_pipeline":[`)

		// Add $sort stage if there's ordering
		pipelineDepth := 0
		if len(rootSel.OrderBy) > 0 {
			d.renderSortStage(ctx, rootSel)
			pipelineDepth++
		}

		// Add $project stage for field selection
		if pipelineDepth > 0 {
			ctx.WriteString(`,`)
		}
		d.renderProjectStageWithChildren(ctx, rootSel, qc)

		ctx.WriteString(`]`)
	}

	ctx.WriteString(`}`)
}

// renderNestedInsertMutation generates a nested_insert operation for inserting into multiple related collections.
func (d *MongoDBDialect) renderNestedInsertMutation(ctx Context, qc *qcode.QCode, rootMutate *qcode.Mutate) {
	ctx.WriteString(`{"operation":"nested_insert","root_collection":"`)
	ctx.WriteString(rootMutate.Ti.Name)
	ctx.WriteString(`","root_mutate_id":`)
	ctx.WriteString(strconv.Itoa(int(rootMutate.ID)))

	// Build topologically sorted list of mutations based on DependsOn
	// Filter to only include inserts and recursive connects (same table as parent)
	allSortedMutates := d.topologicalSortMutates(qc.Mutates)

	// Build a map of mutation IDs to mutations for parent lookup
	mutateMap := make(map[int32]*qcode.Mutate)
	for i := range qc.Mutates {
		mutateMap[qc.Mutates[i].ID] = &qc.Mutates[i]
	}

	// Filter mutations: include inserts and recursive connects only
	var filteredMutates []*qcode.Mutate
	for _, m := range allSortedMutates {
		if m.Type == qcode.MTInsert {
			filteredMutates = append(filteredMutates, m)
		} else if m.Type == qcode.MTConnect && m.ParentID != -1 {
			// Only include recursive connects (same table as parent)
			parent := mutateMap[m.ParentID]
			if parent != nil && parent.Ti.Name == m.Ti.Name {
				filteredMutates = append(filteredMutates, m)
			}
		}
	}

	ctx.WriteString(`,"inserts":[`)
	for i, m := range filteredMutates {
		if i > 0 {
			ctx.WriteString(`,`)
		}
		d.renderNestedInsertItem(ctx, qc, m)
	}
	ctx.WriteString(`]`)

	// Check if all inserts are in the same collection (recursive-only mutation)
	// For recursive mutations, we return ALL inserted/connected documents as an array
	// But if there are any FK connects (connects to different tables), this is NOT recursive-only
	allSameCollection := true
	for _, m := range filteredMutates {
		if m.Ti.Name != rootMutate.Ti.Name {
			allSameCollection = false
			break
		}
	}
	// Also check for FK connects - if any exist, this is NOT a recursive-only mutation
	if allSameCollection {
		for i := range qc.Mutates {
			cm := &qc.Mutates[i]
			if cm.Type == qcode.MTConnect && cm.Ti.Name != rootMutate.Ti.Name {
				allSameCollection = false
				break
			}
		}
	}
	if allSameCollection && len(filteredMutates) > 1 {
		ctx.WriteString(`,"all_same_collection":true`)
	}

	// Add FK connect values for the root document
	// FK connects (like product.connect, commenter.connect) set FK values on the root document
	// We extract the connect ID from each FK connect mutation and set it on the root document
	fkConnectValues := make([]struct {
		column string
		value  string
	}, 0)
	for i := range qc.Mutates {
		cm := &qc.Mutates[i]
		if cm.Type != qcode.MTConnect || cm.ParentID != rootMutate.ID {
			continue
		}
		// Skip recursive connects (same table) - they're handled in inserts array
		if cm.Ti.Name == rootMutate.Ti.Name {
			continue
		}
		// FK connect: owner.connect.id -> owner_id
		if cm.Rel.Type == sdata.RelOneToOne || cm.Rel.Type == sdata.RelOneToMany {
			// Extract the ID value from the Where clause
			if cm.Where.Exp != nil && cm.Where.Exp.Op == qcode.OpEquals {
				column := cm.Rel.Right.Col.Name
				value := cm.Where.Exp.Right.Val
				fkConnectValues = append(fkConnectValues, struct {
					column string
					value  string
				}{column, value})
			}
		}
	}

	if len(fkConnectValues) > 0 {
		ctx.WriteString(`,"fk_values":{`)
		for i, fkv := range fkConnectValues {
			if i > 0 {
				ctx.WriteString(`,`)
			}
			ctx.WriteString(`"`)
			ctx.WriteString(fkv.column)
			ctx.WriteString(`":`)
			ctx.WriteString(fkv.value)
		}
		ctx.WriteString(`}`)
	}

	// Add field_name for result wrapping
	var rootSel *qcode.Select
	if len(qc.Roots) > 0 {
		rootSel = &qc.Selects[qc.Roots[0]]
		ctx.WriteString(`,"field_name":"`)
		ctx.WriteString(rootSel.FieldName)
		ctx.WriteString(`"`)

		// Add singular flag for @object directive
		if rootSel.Singular {
			ctx.WriteString(`,"singular":true`)
		}
	}

	// Add return_pipeline for fetching related data after all inserts
	if rootSel != nil && (len(rootSel.Fields) > 0 || len(rootSel.Children) > 0) {
		ctx.WriteString(`,"return_pipeline":[`)

		// Generate $lookup stages for children (relationships)
		pipelineDepth := 0
		for _, childID := range rootSel.Children {
			child := &qc.Selects[childID]
			if child.SkipRender != qcode.SkipTypeNone {
				continue
			}
			if pipelineDepth > 0 {
				ctx.WriteString(`,`)
			}
			d.renderLookupStage(ctx, rootSel, child)
			pipelineDepth++
		}

		// Add $project stage for field selection
		if pipelineDepth > 0 {
			ctx.WriteString(`,`)
		}
		d.renderProjectStageWithChildren(ctx, rootSel, qc)

		ctx.WriteString(`]`)
	}

	ctx.WriteString(`}`)
}

// renderNestedInsertItem renders a single insert item for nested mutations.
func (d *MongoDBDialect) renderNestedInsertItem(ctx Context, qc *qcode.QCode, m *qcode.Mutate) {
	ctx.WriteString(`{"collection":"`)
	ctx.WriteString(m.Ti.Name)
	ctx.WriteString(`","id":`)
	ctx.WriteString(strconv.Itoa(int(m.ID)))
	ctx.WriteString(`,"parent_id":`)
	ctx.WriteString(strconv.Itoa(int(m.ParentID)))

	// Mark connect operations - these should UPDATE existing records, not INSERT
	if m.Type == qcode.MTConnect {
		ctx.WriteString(`,"is_connect":true`)
	}

	// Add relationship info for FK linking
	if m.ParentID != -1 && m.Rel.Type != sdata.RelNone {
		ctx.WriteString(`,"rel_type":"`)
		switch m.Rel.Type {
		case sdata.RelOneToOne:
			ctx.WriteString(`one_to_one`)
		case sdata.RelOneToMany:
			ctx.WriteString(`one_to_many`)
		default:
			ctx.WriteString(`other`)
		}
		ctx.WriteString(`"`)

		// In sdata, Left/Right semantics depend on relationship type:
		// - RelOneToOne: Left = FK side, Right = PK side
		// - RelOneToMany: Left = PK side, Right = FK side
		var fkCol string
		var fkOnParent bool

		if m.Rel.Type == sdata.RelOneToOne {
			// OneToOne: Left is FK side, Right is PK side
			fkCol = m.Rel.Left.Col.Name
			fkOnParent = m.Rel.Left.Ti.Name != m.Ti.Name
		} else {
			// OneToMany and others: Left is PK side, Right is FK side
			fkCol = m.Rel.Right.Col.Name
			fkOnParent = m.Rel.Right.Ti.Name != m.Ti.Name
		}

		if fkCol == "id" {
			fkCol = "_id"
		}
		ctx.WriteString(`,"fk_col":"`)
		ctx.WriteString(fkCol)
		ctx.WriteString(`"`)
		ctx.WriteString(`,"fk_on_parent":`)
		if fkOnParent {
			ctx.WriteString(`true`)
		} else {
			ctx.WriteString(`false`)
		}
	}

	// Add document data
	ctx.WriteString(`,"document":{`)
	if m.Type == qcode.MTConnect {
		// For connect operations, extract ID from Where clause
		d.renderConnectDocument(ctx, m)
	} else {
		d.renderInsertDocument(ctx, m)
	}
	ctx.WriteString(`}`)

	ctx.WriteString(`}`)
}

// renderConnectDocument renders the document for connect operations.
// Connect operations have the ID in m.Where.Exp instead of m.Cols.
func (d *MongoDBDialect) renderConnectDocument(ctx Context, m *qcode.Mutate) {
	// Extract ID from the Where clause (e.g., connect: { id: 6 })
	if m.Where.Exp != nil {
		d.renderConnectExpression(ctx, m.Where.Exp, true)
	}
}

// renderConnectExpression extracts values from the where expression for connect.
func (d *MongoDBDialect) renderConnectExpression(ctx Context, exp *qcode.Exp, first bool) bool {
	if exp == nil {
		return first
	}

	// Handle AND expressions
	if exp.Op == qcode.OpAnd {
		for _, child := range exp.Children {
			first = d.renderConnectExpression(ctx, child, first)
		}
		return first
	}

	// Handle equality expression (id = value)
	// Exp has Left.Col for the column and Right.Val for the value
	if exp.Op == qcode.OpEquals && exp.Left.Col.Name != "" {
		if !first {
			ctx.WriteString(`,`)
		}
		colName := exp.Left.Col.Name
		if colName == "_id" {
			colName = "id"
		}
		ctx.WriteString(`"`)
		ctx.WriteString(colName)
		ctx.WriteString(`":`)
		ctx.WriteString(exp.Right.Val)
		return false
	}

	return first
}

// topologicalSortMutates sorts mutations for MongoDB nested inserts.
// The order is determined by FK location:
// - If FK is on parent: child must be inserted first (to get child ID for parent's FK)
// - If FK is on child: parent must be inserted first (to get parent ID for child's FK)
func (d *MongoDBDialect) topologicalSortMutates(mutates []qcode.Mutate) []*qcode.Mutate {
	// Build parent-child relationships and FK info
	type mutateInfo struct {
		m          *qcode.Mutate
		fkOnParent bool // true if FK is on parent table
	}

	mutateMap := make(map[int32]*mutateInfo)
	for i := range mutates {
		m := &mutates[i]
		// Determine if FK is on parent
		fkOnParent := false
		if m.ParentID != -1 && m.Rel.Type != sdata.RelNone {
			// In sdata, Left/Right semantics depend on relationship type:
			// - RelOneToOne: Left = FK side, Right = PK side
			// - RelOneToMany: Left = PK side, Right = FK side
			if m.Rel.Type == sdata.RelOneToOne {
				fkOnParent = m.Rel.Left.Ti.Name != m.Ti.Name
			} else {
				fkOnParent = m.Rel.Right.Ti.Name != m.Ti.Name
			}
		}
		mutateMap[m.ID] = &mutateInfo{m: m, fkOnParent: fkOnParent}
	}

	// Track which mutations have been added to result
	added := make(map[int32]bool)
	var result []*qcode.Mutate

	// Repeatedly find mutations whose dependencies are satisfied
	// Dependency: if FK is on parent, child goes first; if FK is on child, parent goes first
	for len(result) < len(mutates) {
		foundOne := false
		for i := range mutates {
			m := &mutates[i]
			if added[m.ID] {
				continue
			}

			canAdd := true

			if m.ParentID == -1 {
				// Root mutation - check if any children need to go first
				for j := range mutates {
					child := &mutates[j]
					if child.ParentID == m.ID {
						childInfo := mutateMap[child.ID]
						if childInfo != nil && childInfo.fkOnParent {
							// FK is on parent (this mutation), child must go first
							if !added[child.ID] {
								canAdd = false
								break
							}
						}
					}
				}
			} else {
				// Child mutation - check if parent needs to go first
				info := mutateMap[m.ID]
				if info != nil && !info.fkOnParent {
					// FK is on child (this mutation), parent must go first
					if !added[m.ParentID] {
						canAdd = false
					}
				}
				// If FK is on parent, no dependency on parent - this child can go first
			}

			if canAdd {
				result = append(result, m)
				added[m.ID] = true
				foundOne = true
			}
		}

		// If no progress was made, there's a cycle - just add remaining in order
		if !foundOne {
			for i := range mutates {
				m := &mutates[i]
				if !added[m.ID] {
					result = append(result, m)
					added[m.ID] = true
				}
			}
			break
		}
	}

	return result
}

// renderUpdateMutation generates a MongoDB updateOne operation
func (d *MongoDBDialect) renderUpdateMutation(ctx Context, qc *qcode.QCode, m *qcode.Mutate) {
	ctx.WriteString(`{"operation":"updateOne","collection":"`)
	ctx.WriteString(m.Ti.Name)
	ctx.WriteString(`","filter":{`)

	// Render where clause for the filter
	if m.Where.Exp != nil {
		d.renderExpression(ctx, m.Where.Exp)
	}

	ctx.WriteString(`},"update":{"$set":{`)

	first := true
	for _, col := range m.Cols {
		if !first {
			ctx.WriteString(`,`)
		}
		colName := col.Col.Name
		if colName == "id" {
			colName = "_id"
		}
		ctx.WriteString(`"`)
		ctx.WriteString(colName)
		ctx.WriteString(`":`)

		if col.Value != "" {
			ctx.WriteString(`"`)
			ctx.WriteString(col.Value)
			ctx.WriteString(`"`)
		} else {
			ctx.WriteString(`null`)
		}
		first = false
	}

	ctx.WriteString(`}}}`)
}

// renderDeleteMutation generates a MongoDB deleteOne operation
func (d *MongoDBDialect) renderDeleteMutation(ctx Context, qc *qcode.QCode, m *qcode.Mutate) {
	ctx.WriteString(`{"operation":"deleteOne","collection":"`)
	ctx.WriteString(m.Ti.Name)
	ctx.WriteString(`","filter":{`)

	if m.Where.Exp != nil {
		d.renderExpression(ctx, m.Where.Exp)
	}

	ctx.WriteString(`}}`)
}

// renderUpsertMutation generates a MongoDB updateOne operation with upsert: true
func (d *MongoDBDialect) renderUpsertMutation(ctx Context, qc *qcode.QCode, m *qcode.Mutate) {
	ctx.WriteString(`{"operation":"updateOne","collection":"`)
	ctx.WriteString(m.Ti.Name)
	ctx.WriteString(`","filter":{`)

	if m.Where.Exp != nil {
		d.renderExpression(ctx, m.Where.Exp)
	}

	ctx.WriteString(`},"update":{"$set":{`)

	first := true
	for _, col := range m.Cols {
		if !first {
			ctx.WriteString(`,`)
		}
		colName := col.Col.Name
		if colName == "id" {
			colName = "_id"
		}
		ctx.WriteString(`"`)
		ctx.WriteString(colName)
		ctx.WriteString(`":`)

		if col.Value != "" {
			ctx.WriteString(`"`)
			ctx.WriteString(col.Value)
			ctx.WriteString(`"`)
		} else {
			ctx.WriteString(`null`)
		}
		first = false
	}

	ctx.WriteString(`}},"options":{"upsert":true}}`)
}

// renderInsertDocument builds the document for insert mutations with individual field variables
func (d *MongoDBDialect) renderInsertDocument(ctx Context, m *qcode.Mutate) {
	first := true
	for _, col := range m.Cols {
		if !first {
			ctx.WriteString(`,`)
		}
		colName := col.Col.Name
		if colName == "id" {
			colName = "_id"
		}
		ctx.WriteString(`"`)
		ctx.WriteString(colName)
		ctx.WriteString(`":`)

		if col.Set {
			// Preset value (e.g., owner_id: "$user_id")
			if col.Value != "" && col.Value[0] == '$' {
				ctx.WriteString(`"`)
				ctx.AddParam(Param{Name: col.Value[1:], Type: col.Col.Type})
				ctx.WriteString(`"`)
			} else {
				ctx.WriteString(`"`)
				ctx.WriteString(col.Value)
				ctx.WriteString(`"`)
			}
		} else if m.Data != nil && m.Data.CMap != nil {
			// Get value from parsed mutation data
			field := m.Data.CMap[col.FieldName]
			if field == nil {
				ctx.WriteString(`null`)
			} else if field.Type == graph.NodeVar {
				// Variable reference - add parameter placeholder
				ctx.WriteString(`"`)
				ctx.AddParam(Param{Name: field.Val, Type: col.Col.Type})
				ctx.WriteString(`"`)
			} else {
				// Literal value - render directly
				d.renderGraphNodeValue(ctx, field)
			}
		} else {
			ctx.WriteString(`null`)
		}
		first = false
	}
}

// renderPresets outputs preset values that need to be merged with raw_document
func (d *MongoDBDialect) renderPresets(ctx Context, m *qcode.Mutate) {
	hasPresets := false
	for _, col := range m.Cols {
		if col.Set {
			hasPresets = true
			break
		}
	}

	if !hasPresets {
		return
	}

	ctx.WriteString(`,"presets":{`)
	first := true
	for _, col := range m.Cols {
		if !col.Set {
			continue
		}
		if !first {
			ctx.WriteString(`,`)
		}
		colName := col.Col.Name
		if colName == "id" {
			colName = "_id"
		}
		ctx.WriteString(`"`)
		ctx.WriteString(colName)
		ctx.WriteString(`":`)

		if col.Value != "" && col.Value[0] == '$' {
			// Parameter reference (e.g., "$user_id")
			ctx.WriteString(`"`)
			ctx.AddParam(Param{Name: col.Value[1:], Type: col.Col.Type})
			ctx.WriteString(`"`)
		} else {
			// Literal value
			ctx.WriteString(`"`)
			ctx.WriteString(col.Value)
			ctx.WriteString(`"`)
		}
		first = false
	}
	ctx.WriteString(`}`)
}

// renderGraphNodeValue renders a graph.Node value as JSON
func (d *MongoDBDialect) renderGraphNodeValue(ctx Context, node *graph.Node) {
	switch node.Type {
	case graph.NodeStr:
		ctx.WriteString(`"`)
		ctx.WriteString(node.Val)
		ctx.WriteString(`"`)
	case graph.NodeNum:
		ctx.WriteString(node.Val)
	case graph.NodeBool:
		ctx.WriteString(node.Val)
	case graph.NodeObj:
		ctx.WriteString(`{`)
		first := true
		for k, v := range node.CMap {
			if !first {
				ctx.WriteString(`,`)
			}
			ctx.WriteString(`"`)
			ctx.WriteString(k)
			ctx.WriteString(`":`)
			d.renderGraphNodeValue(ctx, v)
			first = false
		}
		ctx.WriteString(`}`)
	case graph.NodeList:
		ctx.WriteString(`[`)
		for i, child := range node.Children {
			if i > 0 {
				ctx.WriteString(`,`)
			}
			d.renderGraphNodeValue(ctx, child)
		}
		ctx.WriteString(`]`)
	default:
		ctx.WriteString(`null`)
	}
}

// CompileFullQuery implements FullQueryCompiler interface.
// It generates the complete JSON query DSL for MongoDB, bypassing SQL generation.
func (d *MongoDBDialect) CompileFullQuery(ctx Context, qc *qcode.QCode) bool {
	if len(qc.Roots) == 0 {
		return false
	}

	// Handle multiple roots with multi_aggregate operation
	if len(qc.Roots) > 1 {
		ctx.WriteString(`{"operation":"multi_aggregate","queries":[`)
		for i, rootID := range qc.Roots {
			if i > 0 {
				ctx.WriteString(`,`)
			}
			sel := &qc.Selects[rootID]
			d.renderAggregateQuery(ctx, qc, sel)
		}
		ctx.WriteString(`]}`)
		return true
	}

	// Single root - use standard aggregate operation
	rootID := qc.Roots[0]
	sel := &qc.Selects[rootID]
	d.renderAggregateQuery(ctx, qc, sel)

	return true
}

// renderAggregateQuery generates a single aggregate query for a root selection
func (d *MongoDBDialect) renderAggregateQuery(ctx Context, qc *qcode.QCode, sel *qcode.Select) {
	// Start the JSON query
	ctx.WriteString(`{"operation":"aggregate","collection":"`)
	ctx.WriteString(sel.Table)
	ctx.WriteString(`","field_name":"`)
	ctx.WriteString(sel.FieldName)
	ctx.WriteString(`"`)

	// Include singular flag for proper result wrapping
	if sel.Singular {
		ctx.WriteString(`,"singular":true`)
	}

	ctx.WriteString(`,"pipeline":[`)

	pipelineDepth := 0

	// Add $match stage if there's a filter
	if sel.Where.Exp != nil {
		d.renderMatchStage(ctx, sel.Where.Exp)
		pipelineDepth++
	}

	// Add $lookup stages for each child (related table)
	for _, childID := range sel.Children {
		child := &qc.Selects[childID]
		if child.SkipRender != qcode.SkipTypeNone {
			continue
		}
		if pipelineDepth > 0 {
			ctx.WriteString(`,`)
		}
		d.renderLookupStageWithQC(ctx, sel, child, qc)
		pipelineDepth++
	}

	// Add $sort stage if there's ordering
	if len(sel.OrderBy) > 0 {
		if pipelineDepth > 0 {
			ctx.WriteString(`,`)
		}
		d.renderSortStage(ctx, sel)
		pipelineDepth++
	}

	// Add $skip stage if there's an offset
	if sel.Paging.Offset > 0 || sel.Paging.OffsetVar != "" {
		if pipelineDepth > 0 {
			ctx.WriteString(`,`)
		}
		ctx.WriteString(`{"$skip":`)
		if sel.Paging.OffsetVar != "" {
			ctx.WriteString(`"`)
			ctx.AddParam(Param{Name: sel.Paging.OffsetVar, Type: "integer"})
			ctx.WriteString(`"`)
		} else {
			ctx.WriteString(strconv.Itoa(int(sel.Paging.Offset)))
		}
		ctx.WriteString(`}`)
		pipelineDepth++
	}

	// Add $limit stage
	if !sel.Paging.NoLimit && (sel.Paging.Limit > 0 || sel.Paging.LimitVar != "") {
		if pipelineDepth > 0 {
			ctx.WriteString(`,`)
		}
		ctx.WriteString(`{"$limit":`)
		if sel.Paging.LimitVar != "" {
			ctx.WriteString(`"`)
			ctx.AddParam(Param{Name: sel.Paging.LimitVar, Type: "integer"})
			ctx.WriteString(`"`)
		} else {
			ctx.WriteString(strconv.Itoa(int(sel.Paging.Limit)))
		}
		ctx.WriteString(`}`)
		pipelineDepth++
	}

	// Add $project stage for field selection (including children)
	if len(sel.Fields) > 0 || len(sel.Children) > 0 {
		if pipelineDepth > 0 {
			ctx.WriteString(`,`)
		}
		d.renderProjectStageWithChildren(ctx, sel, qc)
		pipelineDepth++
	}

	// Close pipeline array and root object
	ctx.WriteString(`]}`)
}

// renderLookupStage generates a $lookup stage for joining a related collection
func (d *MongoDBDialect) renderLookupStage(ctx Context, parent, child *qcode.Select) {
	d.renderLookupStageWithQC(ctx, parent, child, nil)
}

// renderLookupStageWithQC is like renderLookupStage but with access to qc for grandchildren
func (d *MongoDBDialect) renderLookupStageWithQC(ctx Context, parent, child *qcode.Select, qc *qcode.QCode) {
	// Check if this is an embedded JSON table (RelEmbedded)
	if child.Rel.Type == sdata.RelEmbedded {
		d.renderEmbeddedJSONStage(ctx, parent, child, qc)
		return
	}

	// Check for M2M via join table (sel.Joins contains intermediate tables)
	if len(child.Joins) > 0 {
		d.renderM2MLookupViaJoinTable(ctx, parent, child, qc)
		return
	}

	ctx.WriteString(`{"$lookup":{`)
	ctx.WriteString(`"from":"`)
	ctx.WriteString(child.Table)
	ctx.WriteString(`"`)

	// Determine local and foreign fields based on relationship
	// rel.Left = referenced table (users), rel.Right = table with FK (products)
	// For products->owner (users): localField=owner_id (from products), foreignField=_id (from users)
	rel := child.Rel

	var localField, foreignField string
	var isLocalArray, isForeignArray bool

	switch rel.Type {
	case sdata.RelOneToOne, sdata.RelOneToMany:
		// rel.Right = table with FK (e.g., products.owner_id)
		// rel.Left = referenced table (e.g., users.id)
		// We need to determine which side is local (parent) vs foreign (child)
		if rel.Right.Ti.Name == parent.Table {
			// FK is on parent: products -> owner lookup (products.owner_id -> users._id)
			localField = rel.Right.Col.Name  // owner_id (FK on parent)
			foreignField = rel.Left.Col.Name // id (PK on child)
			isLocalArray = rel.Right.Col.Array
			isForeignArray = rel.Left.Col.Array
		} else {
			// FK is on child: users -> products lookup (users._id <- products.owner_id)
			localField = rel.Left.Col.Name    // id (PK on parent)
			foreignField = rel.Right.Col.Name // owner_id (FK on child)
			isLocalArray = rel.Left.Col.Array
			isForeignArray = rel.Right.Col.Array
		}
		if localField == "id" {
			localField = "_id"
		}
		if foreignField == "id" {
			foreignField = "_id"
		}
	default:
		// Default: assume parent._id -> child.parent_id
		localField = "_id"
		foreignField = parent.Table + "_id"
	}

	// Use $lookup with pipeline to select only requested fields and apply aliases
	ctx.WriteString(`,"let":{"joinValue":"$`)
	ctx.WriteString(localField)
	ctx.WriteString(`"},"pipeline":[{"$match":{"$expr":{`)

	// For array columns, use $in instead of $eq
	// - If localField is an array (e.g., category_ids), check if foreignField is IN the array
	// - If foreignField is an array (reverse lookup), check if localField is IN that array
	if isLocalArray {
		// Forward array lookup: products.category_ids -> categories._id
		// Check if category._id is IN the category_ids array
		ctx.WriteString(`"$in":["$`)
		ctx.WriteString(foreignField)
		ctx.WriteString(`","$$joinValue"]`)
	} else if isForeignArray {
		// Reverse array lookup: categories._id -> products.category_ids
		// Check if the category ID is IN products.category_ids
		ctx.WriteString(`"$in":["$$joinValue","$`)
		ctx.WriteString(foreignField)
		ctx.WriteString(`"]`)
	} else {
		// Standard scalar lookup: use $eq
		ctx.WriteString(`"$eq":["$`)
		ctx.WriteString(foreignField)
		ctx.WriteString(`","$$joinValue"]`)
	}
	ctx.WriteString(`}}}`)

	// Add $project stage within the pipeline to select only requested fields
	if len(child.Fields) > 0 {
		// Track if we're outputting an id field to determine _id handling
		// Skip function fields - MongoDB doesn't support SQL-style aggregations
		hasIdField := false
		for _, f := range child.Fields {
			if f.Type == qcode.FieldTypeFunc {
				continue
			}
			if f.FieldName == "id" || f.Col.Name == "id" {
				hasIdField = true
				break
			}
		}

		ctx.WriteString(`,{"$project":{`)
		// Only exclude _id if we're not including id field
		// If we're including id, we'll rename it and translateIDFieldsBack will handle conversion
		first := true
		if !hasIdField {
			ctx.WriteString(`"_id":0`)
			first = false
		}
		for _, f := range child.Fields {
			// Skip function fields - MongoDB doesn't support SQL-style aggregations
			if f.Type == qcode.FieldTypeFunc {
				continue
			}
			if !first {
				ctx.WriteString(`,`)
			}
			// Use alias if present, otherwise use column name
			outputName := f.FieldName
			colName := f.Col.Name

			// Translate id -> _id for MongoDB (both source and output)
			// The translateIDFieldsBack in pipeline.go will convert _id back to id
			if colName == "id" {
				colName = "_id"
			}
			if outputName == "id" {
				outputName = "_id"
			}
			ctx.WriteString(`"`)
			ctx.WriteString(outputName)
			ctx.WriteString(`":"$`)
			ctx.WriteString(colName)
			ctx.WriteString(`"`)
			first = false
		}
		ctx.WriteString(`}}`)
	}

	// Add $sort stage if there's ordering, or default sort by _id for consistent results
	if len(child.OrderBy) > 0 {
		ctx.WriteString(`,{"$sort":{`)
		for i, ob := range child.OrderBy {
			if i > 0 {
				ctx.WriteString(`,`)
			}
			colName := ob.Col.Name
			if colName == "id" {
				colName = "_id"
			}
			ctx.WriteString(`"`)
			ctx.WriteString(colName)
			ctx.WriteString(`":`)
			if ob.Order == qcode.OrderDesc {
				ctx.WriteString(`-1`)
			} else {
				ctx.WriteString(`1`)
			}
		}
		ctx.WriteString(`}}`)
	} else {
		// Default sort by _id for consistent ordering
		ctx.WriteString(`,{"$sort":{"_id":1}}`)
	}

	// Add $limit stage for nested queries
	if !child.Paging.NoLimit && (child.Paging.Limit > 0 || child.Paging.LimitVar != "") {
		ctx.WriteString(`,{"$limit":`)
		if child.Paging.LimitVar != "" {
			ctx.WriteString(`"`)
			ctx.AddParam(Param{Name: child.Paging.LimitVar, Type: "integer"})
			ctx.WriteString(`"`)
		} else {
			ctx.WriteString(strconv.Itoa(int(child.Paging.Limit)))
		}
		ctx.WriteString(`}`)
	}

	ctx.WriteString(`],"as":"`)
	ctx.WriteString(child.FieldName)
	ctx.WriteString(`"}}`)
}

// renderM2MLookupViaJoinTable handles many-to-many relationships via join tables
// e.g., products -> purchases -> users (customer)
func (d *MongoDBDialect) renderM2MLookupViaJoinTable(ctx Context, parent, child *qcode.Select, qc *qcode.QCode) {
	// child.Joins[0].Rel = relationship from parent to join table
	// child.Rel = relationship from join table to target table
	joinRel := child.Joins[0].Rel
	targetRel := child.Rel

	// Determine the join table name
	// joinRel.Left = join table (purchases), joinRel.Right = parent table (products) with FK
	joinTable := joinRel.Left.Ti.Name
	targetTable := child.Table

	// FK from join table pointing to parent (e.g., purchases.product_id -> products._id)
	// joinRel.Left.Col is the FK column in the join table that links to parent
	parentToJoinFK := joinRel.Left.Col.Name
	if parentToJoinFK == "id" {
		parentToJoinFK = "_id"
	}

	// FK from join table pointing to target (e.g., purchases.customer_id -> users._id)
	// targetRel.Right has the FK column on the join table side
	joinToTargetFK := targetRel.Right.Col.Name
	if joinToTargetFK == "id" {
		joinToTargetFK = "_id"
	}

	// Target PK (e.g., users._id)
	targetPK := targetRel.Left.Col.Name
	if targetPK == "id" {
		targetPK = "_id"
	}

	// Generate nested $lookup
	ctx.WriteString(`{"$lookup":{`)
	ctx.WriteString(`"from":"`)
	ctx.WriteString(joinTable)
	ctx.WriteString(`"`)
	ctx.WriteString(`,"let":{"parentId":"$_id"}`)
	ctx.WriteString(`,"pipeline":[`)

	// Match join table records where FK matches parent ID
	ctx.WriteString(`{"$match":{"$expr":{"$eq":["$`)
	ctx.WriteString(parentToJoinFK)
	ctx.WriteString(`","$$parentId"]}}}`)

	// Nested lookup to target table
	ctx.WriteString(`,{"$lookup":{"from":"`)
	ctx.WriteString(targetTable)
	ctx.WriteString(`"`)
	ctx.WriteString(`,"localField":"`)
	ctx.WriteString(joinToTargetFK)
	ctx.WriteString(`"`)
	ctx.WriteString(`,"foreignField":"`)
	ctx.WriteString(targetPK)
	ctx.WriteString(`"`)
	ctx.WriteString(`,"as":"_target"}}`)

	// Unwind and replace root with target
	ctx.WriteString(`,{"$unwind":"$_target"}`)
	ctx.WriteString(`,{"$replaceRoot":{"newRoot":"$_target"}}`)

	// Add $project for requested fields if specified
	// Note: mongodriver's translateFieldsInMap converts "id" -> "_id" in keys,
	// and translateIDFieldsBack converts "_id" -> "id" in results.
	// So we should NOT rename _id to id here - just include/exclude fields.
	if len(child.Fields) > 0 {
		ctx.WriteString(`,{"$project":{`)

		// Check if id field is requested
		hasIdField := false
		for _, f := range child.Fields {
			if f.Col.Name == "id" {
				hasIdField = true
				break
			}
		}

		first := true
		// Exclude _id unless id is requested
		if !hasIdField {
			ctx.WriteString(`"_id":0`)
			first = false
		}

		for _, f := range child.Fields {
			if !first {
				ctx.WriteString(`,`)
			}
			first = false

			colName := f.Col.Name
			if colName == "id" {
				// Include _id (will be renamed to id by translateIDFieldsBack)
				ctx.WriteString(`"_id":1`)
			} else {
				ctx.WriteString(`"`)
				ctx.WriteString(f.FieldName)
				ctx.WriteString(`":1`)
			}
		}
		ctx.WriteString(`}}`)
	}

	ctx.WriteString(`]`)
	ctx.WriteString(`,"as":"`)
	ctx.WriteString(child.FieldName)
	ctx.WriteString(`"}}`)
}

// renderProjectStageWithChildren renders $project including child field names
func (d *MongoDBDialect) renderProjectStageWithChildren(ctx Context, sel *qcode.Select, qc *qcode.QCode) {
	ctx.WriteString(`{"$project":{`)
	first := true

	// Check if id field is requested (skip function fields)
	hasIdField := false
	for _, f := range sel.Fields {
		if f.Type != qcode.FieldTypeFunc && f.Col.Name == "id" {
			hasIdField = true
			break
		}
	}

	// Exclude _id if not requested (MongoDB returns it by default)
	if !hasIdField {
		ctx.WriteString(`"_id":0`)
		first = false
	}

	// Add parent fields (skip function fields - MongoDB doesn't support SQL-style aggregations)
	for _, f := range sel.Fields {
		if f.Type == qcode.FieldTypeFunc {
			continue
		}
		if !first {
			ctx.WriteString(`,`)
		}
		colName := f.Col.Name
		if colName == "id" {
			colName = "_id"
		}
		ctx.WriteString(`"`)
		ctx.WriteString(colName)
		ctx.WriteString(`":1`)
		first = false
	}

	// Add child fields (from $lookup)
	for _, childID := range sel.Children {
		child := &qc.Selects[childID]
		if child.SkipRender != qcode.SkipTypeNone {
			continue
		}
		if !first {
			ctx.WriteString(`,`)
		}
		// For singular relationships (e.g., owner), extract first element
		if child.Singular {
			ctx.WriteString(`"`)
			ctx.WriteString(child.FieldName)
			ctx.WriteString(`":{"$arrayElemAt":["$`)
			ctx.WriteString(child.FieldName)
			ctx.WriteString(`",0]}`)
		} else {
			ctx.WriteString(`"`)
			ctx.WriteString(child.FieldName)
			ctx.WriteString(`":1`)
		}
		first = false
	}

	ctx.WriteString(`}}`)
}

// renderMatchStage renders a $match pipeline stage from an expression
func (d *MongoDBDialect) renderMatchStage(ctx Context, exp *qcode.Exp) {
	ctx.WriteString(`{"$match":{`)
	d.renderExpression(ctx, exp)
	ctx.WriteString(`}}`)
}

// renderExpression renders a filter expression in MongoDB query format
func (d *MongoDBDialect) renderExpression(ctx Context, exp *qcode.Exp) {
	if exp == nil {
		return
	}

	switch exp.Op {
	case qcode.OpAnd:
		if len(exp.Children) > 0 {
			ctx.WriteString(`"$and":[`)
			for i, child := range exp.Children {
				if i > 0 {
					ctx.WriteString(`,`)
				}
				ctx.WriteString(`{`)
				d.renderExpression(ctx, child)
				ctx.WriteString(`}`)
			}
			ctx.WriteString(`]`)
		}
	case qcode.OpOr:
		if len(exp.Children) > 0 {
			ctx.WriteString(`"$or":[`)
			for i, child := range exp.Children {
				if i > 0 {
					ctx.WriteString(`,`)
				}
				ctx.WriteString(`{`)
				d.renderExpression(ctx, child)
				ctx.WriteString(`}`)
			}
			ctx.WriteString(`]`)
		}
	case qcode.OpNot:
		// MongoDB's $not cannot be a top-level operator. Use $nor instead.
		// $nor: [{...}] means "match documents where none of the conditions are true"
		if len(exp.Children) > 0 {
			ctx.WriteString(`"$nor":[{`)
			d.renderExpression(ctx, exp.Children[0])
			ctx.WriteString(`}]`)
		}
	case qcode.OpSelectExists:
		// Related table filtering: e.g., owner: { id: { eq: 3 } }
		// Transform to FK column filtering: owner_id: { eq: 3 }
		if len(exp.Joins) > 0 && len(exp.Children) > 0 {
			join := exp.Joins[0]
			rel := join.Rel

			// Get FK column from relationship
			// For products->owner: rel.Right = products.owner_id (FK), rel.Left = users.id (PK)
			fkColName := rel.Right.Col.Name
			if fkColName == "id" {
				fkColName = "_id"
			}

			// Render the child expression using the FK column
			// For $or/$and, these need to be at top level with FK column in each condition
			d.renderSelectExistsWithFK(ctx, exp.Children[0], fkColName)
		}
	case qcode.OpTsQuery:
		// MongoDB full-text search uses $text operator
		// Note: MongoDB's $text returns all documents matching any token, sorted by relevance
		ctx.WriteString(`"$text":{"$search":"`)
		ctx.AddParam(Param{Name: exp.Right.Val, Type: "text"})
		ctx.WriteString(`"}`)
	default:
		// Simple comparison: field op value
		colName := exp.Left.Col.Name
		if colName == "" {
			colName = exp.Left.ColName
		}

		// Translate "id" to "_id" for MongoDB
		if colName == "id" {
			colName = "_id"
		}

		ctx.WriteString(`"`)
		ctx.WriteString(colName)
		// Add JSON path using dot notation if present
		if len(exp.Left.Path) > 0 {
			for _, p := range exp.Left.Path {
				ctx.WriteString(`.`)
				ctx.WriteString(p)
			}
		}
		ctx.WriteString(`":`)

		d.renderComparisonValue(ctx, exp)
	}
}

// renderComparisonValue renders the right side of a comparison
func (d *MongoDBDialect) renderComparisonValue(ctx Context, exp *qcode.Exp) {
	switch exp.Op {
	case qcode.OpEquals:
		d.renderValue(ctx, exp)
	case qcode.OpNotEquals:
		ctx.WriteString(`{"$ne":`)
		d.renderValue(ctx, exp)
		ctx.WriteString(`}`)
	case qcode.OpGreaterThan:
		ctx.WriteString(`{"$gt":`)
		d.renderValue(ctx, exp)
		ctx.WriteString(`}`)
	case qcode.OpGreaterOrEquals:
		ctx.WriteString(`{"$gte":`)
		d.renderValue(ctx, exp)
		ctx.WriteString(`}`)
	case qcode.OpLesserThan:
		ctx.WriteString(`{"$lt":`)
		d.renderValue(ctx, exp)
		ctx.WriteString(`}`)
	case qcode.OpLesserOrEquals:
		ctx.WriteString(`{"$lte":`)
		d.renderValue(ctx, exp)
		ctx.WriteString(`}`)
	case qcode.OpIn, qcode.OpHasInCommon:
		// OpIn: scalar field matches any value in list
		// OpHasInCommon: array field has any element matching values in list
		// MongoDB's $in handles both cases with the same syntax
		ctx.WriteString(`{"$in":`)
		if exp.Right.ValType == qcode.ValList {
			// Static list of values
			ctx.WriteString(`[`)
			for i, v := range exp.Right.ListVal {
				if i > 0 {
					ctx.WriteString(`,`)
				}
				d.renderLiteralValue(ctx, v, exp.Right.ListType)
			}
			ctx.WriteString(`]`)
		} else if exp.Right.Val != "" {
			// Variable reference for list operations
			// Note: setListVal in qcode doesn't set ValType for variables,
			// but sets Val to the variable name
			ctx.WriteString(`"`)
			ctx.AddParam(Param{Name: exp.Right.Val, Type: "json", IsArray: true})
			ctx.WriteString(`"`)
		} else {
			// Fallback
			d.renderValue(ctx, exp)
		}
		ctx.WriteString(`}`)
	case qcode.OpNotIn:
		ctx.WriteString(`{"$nin":[`)
		for i, v := range exp.Right.ListVal {
			if i > 0 {
				ctx.WriteString(`,`)
			}
			d.renderLiteralValue(ctx, v, exp.Right.ListType)
		}
		ctx.WriteString(`]}`)
	case qcode.OpLike:
		ctx.WriteString(`{"$regex":"`)
		// Convert SQL LIKE pattern to regex
		pattern := strings.ReplaceAll(exp.Right.Val, "%", ".*")
		pattern = strings.ReplaceAll(pattern, "_", ".")
		ctx.WriteString(escapeJSONString(pattern))
		ctx.WriteString(`"}`)
	case qcode.OpILike:
		ctx.WriteString(`{"$regex":"`)
		pattern := strings.ReplaceAll(exp.Right.Val, "%", ".*")
		pattern = strings.ReplaceAll(pattern, "_", ".")
		ctx.WriteString(escapeJSONString(pattern))
		ctx.WriteString(`","$options":"i"}`)
	case qcode.OpRegex:
		ctx.WriteString(`{"$regex":`)
		d.renderValue(ctx, exp)
		ctx.WriteString(`}`)
	case qcode.OpIRegex:
		ctx.WriteString(`{"$regex":`)
		d.renderValue(ctx, exp)
		ctx.WriteString(`,"$options":"i"}`)
	case qcode.OpIsNull:
		ctx.WriteString(`null`)
	case qcode.OpIsNotNull:
		ctx.WriteString(`{"$ne":null}`)
	default:
		d.renderValue(ctx, exp)
	}
}

// renderValue renders a value from an expression
func (d *MongoDBDialect) renderValue(ctx Context, exp *qcode.Exp) {
	switch exp.Right.ValType {
	case qcode.ValVar:
		// This is a parameter reference - wrap in quotes for valid JSON
		// The driver will substitute the actual value
		ctx.WriteString(`"`)
		ctx.AddParam(Param{Name: exp.Right.Val, Type: "any"})
		ctx.WriteString(`"`)
	case qcode.ValNum:
		ctx.WriteString(exp.Right.Val)
	case qcode.ValBool:
		ctx.WriteString(exp.Right.Val)
	case qcode.ValStr:
		ctx.WriteString(`"`)
		ctx.WriteString(escapeJSONString(exp.Right.Val))
		ctx.WriteString(`"`)
	default:
		// Default: treat as string
		ctx.WriteString(`"`)
		ctx.WriteString(escapeJSONString(exp.Right.Val))
		ctx.WriteString(`"`)
	}
}

// renderLiteralValue renders a literal value
func (d *MongoDBDialect) renderLiteralValue(ctx Context, val string, valType qcode.ValType) {
	switch valType {
	case qcode.ValNum:
		ctx.WriteString(val)
	case qcode.ValBool:
		ctx.WriteString(val)
	default:
		ctx.WriteString(`"`)
		ctx.WriteString(escapeJSONString(val))
		ctx.WriteString(`"`)
	}
}

// renderSelectExistsWithFK renders the child expression for OpSelectExists,
// using the FK column name for each condition.
// For $or/$and, these need to be at top level with FK column in each condition.
// e.g., owner: { id: { or: [ { eq: 2 }, { eq: 3 } ] } }
// becomes: "$or":[{"owner_id":2},{"owner_id":3}]
func (d *MongoDBDialect) renderSelectExistsWithFK(ctx Context, exp *qcode.Exp, fkColName string) {
	switch exp.Op {
	case qcode.OpOr:
		ctx.WriteString(`"$or":[`)
		for i, child := range exp.Children {
			if i > 0 {
				ctx.WriteString(`,`)
			}
			ctx.WriteString(`{"`)
			ctx.WriteString(fkColName)
			ctx.WriteString(`":`)
			d.renderComparisonValue(ctx, child)
			ctx.WriteString(`}`)
		}
		ctx.WriteString(`]`)
	case qcode.OpAnd:
		ctx.WriteString(`"$and":[`)
		for i, child := range exp.Children {
			if i > 0 {
				ctx.WriteString(`,`)
			}
			ctx.WriteString(`{"`)
			ctx.WriteString(fkColName)
			ctx.WriteString(`":`)
			d.renderComparisonValue(ctx, child)
			ctx.WriteString(`}`)
		}
		ctx.WriteString(`]`)
	default:
		// Simple comparison: "fk_col": value
		ctx.WriteString(`"`)
		ctx.WriteString(fkColName)
		ctx.WriteString(`":`)
		d.renderComparisonValue(ctx, exp)
	}
}

// renderSortStage renders a $sort pipeline stage
func (d *MongoDBDialect) renderSortStage(ctx Context, sel *qcode.Select) {
	// Check if we need list-based ordering (order by position in array)
	hasListOrder := false
	for _, ob := range sel.OrderBy {
		if ob.Var != "" {
			hasListOrder = true
			break
		}
	}

	// If we have list-based ordering, first add $addFields stage to compute positions
	if hasListOrder {
		ctx.WriteString(`{"$addFields":{`)
		first := true
		for _, ob := range sel.OrderBy {
			if ob.Var != "" {
				if !first {
					ctx.WriteString(`,`)
				}
				first = false
				colName := ob.Col.Name
				if colName == "id" {
					colName = "_id"
				}
				// Add computed field: "__sort_pos_colname": { "$indexOfArray": [$list, "$colname"] }
				ctx.WriteString(`"__sort_pos_`)
				ctx.WriteString(ob.Col.Name)
				ctx.WriteString(`":{"$indexOfArray":["`)
				ctx.AddParam(Param{Name: ob.Var, Type: "json", IsArray: true})
				ctx.WriteString(`","$`)
				ctx.WriteString(colName)
				ctx.WriteString(`"]}`)
			}
		}
		ctx.WriteString(`}},`)
	}

	// Now render $sort stage
	ctx.WriteString(`{"$sort":{`)
	for i, ob := range sel.OrderBy {
		if i > 0 {
			ctx.WriteString(`,`)
		}
		ctx.WriteString(`"`)
		if ob.Var != "" {
			// Use computed position field for list-based ordering
			ctx.WriteString(`__sort_pos_`)
			ctx.WriteString(ob.Col.Name)
		} else {
			colName := ob.Col.Name
			// Translate "id" to "_id"
			if colName == "id" {
				colName = "_id"
			}
			ctx.WriteString(colName)
		}
		ctx.WriteString(`":`)
		switch ob.Order {
		case qcode.OrderDesc, qcode.OrderDescNullsFirst, qcode.OrderDescNullsLast:
			ctx.WriteString(`-1`)
		default:
			ctx.WriteString(`1`)
		}
	}
	ctx.WriteString(`}}`)
}

// renderProjectStage renders a $project pipeline stage
func (d *MongoDBDialect) renderProjectStage(ctx Context, sel *qcode.Select) {
	ctx.WriteString(`{"$project":{`)
	for i, f := range sel.Fields {
		if i > 0 {
			ctx.WriteString(`,`)
		}
		colName := f.Col.Name
		// Translate "id" to "_id"
		if colName == "id" {
			colName = "_id"
		}
		ctx.WriteString(`"`)
		ctx.WriteString(colName)
		ctx.WriteString(`":1`)
	}
	ctx.WriteString(`}}`)
}

// renderEmbeddedJSONStage handles JSON virtual tables (RelEmbedded).
// The data is already embedded in the parent document as an array.
// We need to:
// 1. $unwind the embedded array
// 2. $lookup for any FK relationships within elements (to a temp field)
// 3. $addFields to merge the lookup result into the embedded element
// 4. $unwind the merged arrays (single element for FK)
// 5. $group back to reconstruct the array
func (d *MongoDBDialect) renderEmbeddedJSONStage(ctx Context, parent, child *qcode.Select, qc *qcode.QCode) {
	// The embedded array field name comes from the relationship
	// rel.Left.Col.Name is the JSON column name in the parent table
	embeddedField := child.Rel.Left.Col.Name // e.g., "category_counts"

	// Step 1: $unwind the embedded array
	ctx.WriteString(`{"$unwind":{"path":"$`)
	ctx.WriteString(embeddedField)
	ctx.WriteString(`","preserveNullAndEmptyArrays":true}}`)

	// Step 2 & 3: $lookup for FK relationships and merge into embedded element
	if qc != nil {
		for _, grandchildID := range child.Children {
			grandchild := &qc.Selects[grandchildID]
			if grandchild.SkipRender != qcode.SkipTypeNone {
				continue
			}
			// Generate temp field name for lookup result
			tempField := "_temp_" + grandchild.FieldName

			// $lookup to temp field
			ctx.WriteString(`,`)
			d.renderNestedLookupForEmbedded(ctx, embeddedField, grandchild, tempField)

			// $addFields to merge temp field into embedded element
			ctx.WriteString(`,{"$addFields":{"`)
			ctx.WriteString(embeddedField)
			ctx.WriteString(`.`)
			ctx.WriteString(grandchild.FieldName)
			ctx.WriteString(`":{"$arrayElemAt":["$`)
			ctx.WriteString(tempField)
			ctx.WriteString(`",0]}}}`)

			// Clean up temp field
			ctx.WriteString(`,{"$project":{"`)
			ctx.WriteString(tempField)
			ctx.WriteString(`":0}}`)
		}
	}

	// Step 4: $group back to reconstruct the document
	// We use $mergeObjects to collect all parent fields with $first
	ctx.WriteString(`,{"$group":{"_id":"$_id"`)

	// Include parent fields with $first accumulator
	// Note: in $group, $first is an accumulator that returns first value from group
	for _, f := range parent.Fields {
		colName := f.Col.Name
		// Skip id field - we'll use _id from group
		if colName == "id" {
			continue
		}
		ctx.WriteString(`,"`)
		ctx.WriteString(f.FieldName)
		ctx.WriteString(`":{"$first":"$`)
		ctx.WriteString(colName)
		ctx.WriteString(`"}`)
	}

	// Push the embedded field back as array
	ctx.WriteString(`,"`)
	ctx.WriteString(embeddedField)
	ctx.WriteString(`":{"$push":"$`)
	ctx.WriteString(embeddedField)
	ctx.WriteString(`"}}}`)

	// Add $addFields to rename _id back to id if needed
	hasIdField := false
	for _, f := range parent.Fields {
		if f.Col.Name == "id" {
			hasIdField = true
			break
		}
	}
	if hasIdField {
		ctx.WriteString(`,{"$addFields":{"id":"$_id"}}`)
	}

	// Step 5: Final $project to select only requested fields from embedded elements
	if qc != nil && (len(child.Fields) > 0 || len(child.Children) > 0) {
		ctx.WriteString(`,{"$addFields":{"`)
		ctx.WriteString(embeddedField)
		ctx.WriteString(`":{"$map":{"input":"$`)
		ctx.WriteString(embeddedField)
		ctx.WriteString(`","as":"elem","in":{`)

		first := true
		// Include requested child fields
		for _, f := range child.Fields {
			if !first {
				ctx.WriteString(`,`)
			}
			ctx.WriteString(`"`)
			ctx.WriteString(f.FieldName)
			ctx.WriteString(`":"$$elem.`)
			ctx.WriteString(f.Col.Name)
			ctx.WriteString(`"`)
			first = false
		}

		// Include grandchildren (looked up relationships)
		for _, grandchildID := range child.Children {
			grandchild := &qc.Selects[grandchildID]
			if grandchild.SkipRender != qcode.SkipTypeNone {
				continue
			}
			if !first {
				ctx.WriteString(`,`)
			}
			ctx.WriteString(`"`)
			ctx.WriteString(grandchild.FieldName)
			ctx.WriteString(`":"$$elem.`)
			ctx.WriteString(grandchild.FieldName)
			ctx.WriteString(`"`)
			first = false
		}

		ctx.WriteString(`}}}}}`)
	}
}

// renderNestedLookupForEmbedded generates a $lookup for FK relationships within embedded JSON elements
func (d *MongoDBDialect) renderNestedLookupForEmbedded(ctx Context, embeddedField string, grandchild *qcode.Select, tempField string) {
	rel := grandchild.Rel

	// Get the FK column from the embedded element
	// rel.Right.Col.Name is the FK column in the embedded element (e.g., "category_id")
	// rel.Left.Col.Name is the referenced column (e.g., "id" -> "_id")
	fkField := rel.Right.Col.Name // e.g., "category_id"
	refField := rel.Left.Col.Name // e.g., "id"
	if refField == "id" {
		refField = "_id"
	}

	// Use $lookup with pipeline for field selection
	ctx.WriteString(`{"$lookup":{"from":"`)
	ctx.WriteString(grandchild.Table) // e.g., "categories"
	ctx.WriteString(`","let":{"fkValue":"$`)
	ctx.WriteString(embeddedField)
	ctx.WriteString(`.`)
	ctx.WriteString(fkField)
	ctx.WriteString(`"},"pipeline":[{"$match":{"$expr":{"$eq":["$`)
	ctx.WriteString(refField)
	ctx.WriteString(`","$$fkValue"]}}}`)

	// Add $project for field selection
	if len(grandchild.Fields) > 0 {
		ctx.WriteString(`,{"$project":{"_id":0`)
		for _, f := range grandchild.Fields {
			colName := f.Col.Name
			if colName == "id" {
				colName = "_id"
			}
			ctx.WriteString(`,"`)
			ctx.WriteString(f.FieldName)
			ctx.WriteString(`":"$`)
			ctx.WriteString(colName)
			ctx.WriteString(`"`)
		}
		ctx.WriteString(`}}`)
	}

	// Write to temp field (not dotted path)
	ctx.WriteString(`],"as":"`)
	ctx.WriteString(tempField)
	ctx.WriteString(`"}}`)
}
