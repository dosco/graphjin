package qcode

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/graph"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
	"github.com/dosco/graphjin/core/v3/internal/util"
)

func (co *Compiler) compileFields(
	st *util.StackInt32,
	op *graph.Operation,
	qc *QCode,
	sel *Select,
	field graph.Field,
	tr trval,
	role string,
) (err error) {
	sel.Fields = make([]Field, 0, len(field.Children))
	sel.BCols = make([]Column, 0, len(field.Children))

	if err = co.compileChildColumns(st, op, qc, sel, field, tr, role); err != nil {
		return
	}

	if err = validateSelector(qc, sel, tr); err != nil {
		return
	}

	if err = co.addColumns(qc, sel); err != nil {
		return
	}

	co.addOrderByColumns(sel)

	// Inject __gj_id field for cache tracking if enabled
	if co.c.EnableCacheTracking && qc.Type == QTQuery {
		co.addCacheTrackingField(sel)
	}

	return nil
}

func (co *Compiler) compileChildColumns(
	st *util.StackInt32,
	op *graph.Operation,
	qc *QCode,
	sel *Select,
	gf graph.Field,
	tr trval,
	role string,
) (err error) {
	var aggExists bool
	var id int32

	for _, cid := range gf.Children {
		field := Field{ID: id, ParentID: sel.ID, Type: FieldTypeCol}
		f := op.Fields[cid]

		name := co.ParseName(f.Name)

		if f.Alias != "" {
			field.FieldName = f.Alias
		} else {
			field.FieldName = f.Name
		}

		// these are all remote fields we use
		// these later to strip the response json
		if sel.Rel.Type == sdata.RelRemote {
			sel.Fields = append(sel.Fields, field)
			continue
		}

		if len(f.Children) != 0 {
			val := f.ID | (sel.ID << 16)
			st.Push(val)
			continue
		}

		switch {
		case name == "__typename":
			sel.Typename = true
			continue

		case strings.HasSuffix(name, "_cursor"):
			continue
		}

		var isCol, isFunc bool
		var fn Function

		field.Col, isCol = sel.Ti.ColumnExists(name)

		if !isCol {
			fn, isFunc, err = co.isFunction(sel, name, f)
			if err != nil {
				return err
			}
		}

		switch {
		case isCol:
		case isFunc:
			field.Type = FieldTypeFunc
			field.Func = fn.Func
			field.Args = fn.Args
			aggExists = fn.Agg
		default:
			return fmt.Errorf("field '%s' is not a column or a function", name)
		}

		if err := co.compileFieldDirectives(sel, &field, f.Directives, role); err != nil {
			return err
		}

		if err := co.compileFieldArgs(sel, &field, f.Args, role); err != nil {
			return err
		}

		if field.Col.Blocked {
			return fmt.Errorf("column: '%s.%s.%s' blocked",
				field.Col.Schema,
				field.Col.Table,
				field.Col.Name)
		}

		if field.SkipRender == SkipTypeDrop {
			continue
		}

		// this is needed cause recursive selects cannot have functions
		// in them so we need to render the function a level above
		// and therefore the column to run to aggregation function
		// on should be included in the base columns
		if isFunc && fn.Agg && sel.Rel.Type == sdata.RelRecursive {
			sel.addBaseCol(Column{Col: fn.Args[0].Col})
		}
		sel.addField(field)
		id++
	}

	if aggExists {
		sel.GroupCols = true
		// Remove injected __gj_id from BCols and Fields — including the
		// primary key in GROUP BY makes every group unique (count always 1).
		sel.removeCacheTrackingField()
	}
	return nil
}

func newArgs(sel *Select, f sdata.DBFunction, arg graph.Arg) (args []Arg, err error) {
	node := arg.Val
	for i, argNode := range node.Children {
		var a Arg
		a, err = parseArg(argNode, f, i)
		if err != nil {
			return
		}
		switch argNode.Type {
		case graph.NodeLabel:
			a.Type = ArgTypeCol
			a.Col, err = sel.Ti.GetColumn(argNode.Val)
		case graph.NodeVar:
			a.Type = ArgTypeVar
			fallthrough
		default:
			a.Val = argNode.Val
		}
		if err != nil {
			return
		}
		args = append(args, a)
	}
	return
}

func parseArg(arg *graph.Node, f sdata.DBFunction, index int) (a Arg, err error) {
	argName := arg.Name
	if numArgKeyRe.MatchString(argName) {
		var n int
		argName = argName[1:]
		n, err = strconv.Atoi(argName)
		if err != nil {
			err = fmt.Errorf("db function %s: invalid key: %s", f.Name, arg.Name)
			return
		}
		if n != index {
			err = fmt.Errorf("db function %s: invalid key order: %s", f.Name, arg.Name)
			return
		}
		a = Arg{DType: f.Inputs[n].Type}
		return
	}

	var input sdata.DBFuncParam
	input, err = f.GetInput(argName)
	if err != nil {
		err = fmt.Errorf("db function %s: %w", f.Name, err)
	}
	a = Arg{Name: arg.Name, DType: input.Type}
	return
}

func (co *Compiler) addOrderByColumns(sel *Select) {
	// For warehouse databases (Snowflake, BigQuery), ORDER BY columns don't
	// need to be in the inner SELECT list when:
	//   - No cursor pagination (no LAST_VALUE extraction from outer query)
	//   - No DISTINCT ON (SQL requires ORDER BY cols in SELECT with DISTINCT)
	//   - No GROUP BY (ORDER BY cols must be aggregated or grouped)
	//   - Not a recursive query (CTE column list must match SELECT)
	// This avoids scanning extra columns that only serve the ORDER BY clause,
	// which matters for cost on columnar stores where you pay per byte scanned.
	skipProjection := !sel.Paging.Cursor &&
		len(sel.DistinctOn) == 0 &&
		!sel.GroupCols &&
		sel.Rel.Type != sdata.RelRecursive &&
		isWarehouseDB(co.s.DBType())

	if skipProjection {
		return
	}
	for _, ob := range sel.OrderBy {
		sel.addBaseCol(Column{Col: ob.Col})
	}
}

// isWarehouseDB returns true for columnar/warehouse databases where
// projecting fewer columns directly reduces scan cost and credits.
func isWarehouseDB(dbType string) bool {
	switch dbType {
	case "snowflake", "bigquery":
		return true
	default:
		return false
	}
}

func (co *Compiler) addColumns(qc *QCode, sel *Select) error {
	var rel sdata.DBRel

	switch {
	case len(sel.Joins) == 0:
		rel = sel.Rel
	case sel.Joins[0].Local:
		return nil
	default:
		rel = sel.Joins[0].Rel
	}
	if err := co.addRelColumns(qc, sel, rel); err != nil {
		return err
	}

	// co.addFuncColumns(qc, sel)
	return nil
}

func (co *Compiler) addRelColumns(qc *QCode, sel *Select, rel sdata.DBRel) error {
	var psel *Select

	if sel.ParentID != -1 {
		psel = &qc.Selects[sel.ParentID]
	} else {
		return nil
	}

	switch rel.Type {
	case sdata.RelNone:
		return nil

	case sdata.RelOneToOne, sdata.RelOneToMany:
		psel.addBaseCol(Column{Col: rel.Right.Col})
		// Composite FK: add extra pair columns to parent's base columns
		for _, pair := range rel.ExtraPairs {
			psel.addBaseCol(Column{Col: pair.R})
		}

	case sdata.RelEmbedded:
		psel.addBaseCol(Column{Col: rel.Right.Col})

	case sdata.RelRemote:
		f := Field{Type: FieldTypeCol, Col: rel.Right.Col, FieldName: rel.Left.Col.Name}
		psel.addField(f)
		sel.SkipRender = SkipTypeRemote

	case sdata.RelDatabaseJoin:
		// Cross-database join: add the foreign key column to parent for ID extraction,
		// and mark this select to be handled by the database join execution path.
		// Use a synthetic placeholder name (__%s_db_join) so it's unique and matches
		// what databaseJoinFieldIds() searches for during result stitching.
		placeholderName := fmt.Sprintf("__%s_db_join", sel.FieldName)
		f := Field{Type: FieldTypeCol, Col: rel.Right.Col, FieldName: placeholderName}
		psel.addField(f)
		sel.SkipRender = SkipTypeDatabaseJoin
		sel.Database = sel.Ti.Database

	case sdata.RelPolymorphic:
		typeCol := rel.Left.Col
		typeCol.Name = rel.Left.Col.FKeyCol

		psel.addBaseCol(Column{Col: rel.Left.Col})
		psel.addBaseCol(Column{Col: typeCol})

	case sdata.RelRecursive:
		sel.addBaseCol(Column{Col: rel.Left.Col})
		sel.addBaseCol(Column{Col: rel.Right.Col})
	}
	return nil
}

// orderByClusterKeys prepends Snowflake clustering key columns to the ORDER BY
// list when cursor pagination is active and the user hasn't specified an explicit
// ORDER BY. This aligns the cursor seek with Snowflake's micro-partition layout
// for better scan performance.
func (co *Compiler) orderByClusterKeys(sel *Select) {
	if co.s.DBType() != "snowflake" {
		return
	}
	if len(sel.Ti.ClusteringKeys) == 0 {
		return
	}
	// Only inject clustering ORDER BY when the user hasn't specified their own
	if len(sel.OrderBy) > 0 {
		return
	}

	for _, ckName := range sel.Ti.ClusteringKeys {
		cid, ok := sel.Ti.GetColumnIndex(ckName)
		if !ok {
			continue
		}
		col := sel.Ti.Columns[cid]
		sel.addBaseCol(Column{Col: col})
		sel.OrderBy = append(sel.OrderBy, OrderBy{Col: col, Order: sel.order})
	}
}

func (co *Compiler) orderByIDCol(sel *Select) error {
	if sel.Ti.PrimaryCol.Name == "" {
		return fmt.Errorf("table requires primary key: %s", sel.Ti.Name)
	}

	for _, pkCol := range sel.Ti.PrimaryCols {
		sel.addBaseCol(Column{Col: pkCol})

		already := false
		for _, ob := range sel.OrderBy {
			if ob.Col.Name == pkCol.Name {
				already = true
				break
			}
		}
		if !already {
			sel.OrderBy = append(sel.OrderBy, OrderBy{Col: pkCol, Order: sel.order})
		}
	}
	return nil
}

func validateSelector(qc *QCode, sel *Select, tr trval) error {
	for _, f := range sel.Fields {
		if err := validateField(qc, f, tr); err != nil {
			return err
		}
	}
	return nil
}

func validateField(qc *QCode, f Field, tr trval) error {
	switch f.Type {
	case FieldTypeCol:
		if !tr.columnAllowed(qc, f.Col.Name) {
			return validateErr(tr, f.Col.Name, "db column blocked")
		}
	case FieldTypeFunc:
		if tr.isFuncsBlocked() {
			return validateErr(tr, f.Func.Name, "all db functions blocked")
		}
		if len(f.Args) != 0 && !tr.columnAllowed(qc, f.Args[0].Col.Name) {
			return validateErr(tr, f.Args[0].Col.Name, "db column blocked")
		}
	}

	return nil
}

func validateErr(tr trval, name, msg string) error {
	return fmt.Errorf("%s: %s (role: '%s')", msg, name, tr.role)
}

func (sel *Select) addField(f Field) {
	if f.Type == FieldTypeCol && sel.bcolExists(f.Col.Name) == -1 {
		sel.BCols = append(sel.BCols, Column{Col: f.Col, FieldName: f.FieldName})
	}
	if sel.fieldExists(f.FieldName) == -1 {
		sel.Fields = append(sel.Fields, f)
	}
}

func (sel *Select) addBaseCol(col Column) {
	if sel.bcolExists(col.Col.Name) == -1 {
		sel.BCols = append(sel.BCols, col)
	}
}

// removeCacheTrackingField strips the __gj_id column injected by
// addCacheTrackingField. When aggregation functions are present the PK
// must not appear in SELECT or GROUP BY — otherwise every group is
// unique and counts are always 1. This applies to all database dialects.
func (sel *Select) removeCacheTrackingField() {
	for i := len(sel.BCols) - 1; i >= 0; i-- {
		if sel.BCols[i].FieldName == "__gj_id" {
			sel.BCols = append(sel.BCols[:i], sel.BCols[i+1:]...)
		}
	}
	for i := len(sel.Fields) - 1; i >= 0; i-- {
		if sel.Fields[i].FieldName == "__gj_id" {
			sel.Fields = append(sel.Fields[:i], sel.Fields[i+1:]...)
		}
	}
}

func (sel *Select) fieldExists(name string) int {
	for i, c := range sel.Fields {
		if strings.EqualFold(c.FieldName, name) {
			return i
		}
	}
	return -1
}

func (sel *Select) bcolExists(name string) int {
	for i, c := range sel.BCols {
		if strings.EqualFold(c.Col.Name, name) {
			return i
		}
	}
	return -1
}

// addCacheTrackingField injects __gj_id field with primary key for cache row tracking
func (co *Compiler) addCacheTrackingField(sel *Select) {
	// Skip if table has no primary key
	pk := sel.Ti.PrimaryCol
	if pk.Name == "" {
		return
	}

	// Skip if __gj_id already exists
	for _, f := range sel.Fields {
		if f.FieldName == "__gj_id" {
			return
		}
	}

	// For single PK, skip if PK column is already requested
	if !sel.Ti.HasCompositePK() {
		for _, f := range sel.Fields {
			if f.Type == FieldTypeCol && strings.EqualFold(f.Col.Name, pk.Name) {
				return
			}
		}
	}

	// Add the injected field (uses first PK column; cache_response.go extracts the value)
	field := Field{
		ID:        int32(len(sel.Fields)),
		ParentID:  sel.ID,
		Type:      FieldTypeCol,
		Col:       pk,
		FieldName: "__gj_id",
	}

	sel.Fields = append(sel.Fields, field)

	// Also add all PK columns to base columns
	for _, pkCol := range sel.Ti.PrimaryCols {
		if sel.bcolExists(pkCol.Name) == -1 {
			sel.BCols = append(sel.BCols, Column{Col: pkCol, FieldName: "__gj_id"})
		}
	}
}
