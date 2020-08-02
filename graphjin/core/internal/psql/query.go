//nolint:errcheck
package psql

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dosco/super-graph/core/internal/qcode"
	"github.com/dosco/super-graph/core/internal/sdata"
	"github.com/dosco/super-graph/core/internal/util"
)

const (
	closeBlock = 500
)

type Param struct {
	Name    string
	Type    string
	IsArray bool
}

type Metadata struct {
	Poll        bool
	remoteCount int
	params      []Param
	pindex      map[string]int
}

type compilerContext struct {
	md Metadata
	w  *bytes.Buffer
	qc *qcode.QCode
	*Compiler
}

type Variables map[string]json.RawMessage

type Config struct {
	Vars map[string]string
}

type Compiler struct {
	vars map[string]string
}

func NewCompiler(conf Config) *Compiler {
	return &Compiler{vars: conf.Vars}
}

func (co *Compiler) CompileEx(qc *qcode.QCode) (Metadata, []byte, error) {
	var w bytes.Buffer

	if metad, err := co.Compile(&w, qc); err != nil {
		return metad, nil, err
	} else {
		return metad, w.Bytes(), nil
	}
}

func (co *Compiler) Compile(w *bytes.Buffer, qc *qcode.QCode) (Metadata, error) {
	var err error
	var md Metadata

	if qc == nil {
		return md, fmt.Errorf("qcode is nil")
	}

	switch qc.Type {
	case qcode.QTQuery:
		md = co.CompileQuery(w, qc, md)

	case qcode.QTSubscription:
		md.Poll = true
		md = co.CompileQuery(w, qc, md)

	case qcode.QTMutation:
		md = co.compileMutation(w, qc, md)

	default:
		err = fmt.Errorf("Unknown operation type %d", qc.Type)
	}

	return md, err
}

func (co *Compiler) CompileQuery(
	w *bytes.Buffer,
	qc *qcode.QCode,
	metad Metadata) Metadata {

	st := NewIntStack()
	c := compilerContext{
		md:       metad,
		w:        w,
		qc:       qc,
		Compiler: co,
	}

	i := 0
	c.w.WriteString(`SELECT jsonb_build_object(`)
	for _, id := range qc.Roots {
		if i != 0 {
			c.w.WriteString(`, `)
		}
		sel := &qc.Selects[id]

		switch sel.SkipRender {
		case qcode.SkipTypeUserNeeded:
			c.w.WriteString(`'`)
			c.w.WriteString(sel.FieldName)
			c.w.WriteString(`', NULL`)

			if sel.Paging.Cursor {
				c.w.WriteString(`, '`)
				c.w.WriteString(sel.FieldName)
				c.w.WriteString(`_cursor', NULL`)
			}

		case qcode.SkipTypeRemote:
			c.md.remoteCount++
			fallthrough

		default:
			c.w.WriteString(`'`)
			c.w.WriteString(sel.FieldName)
			c.w.WriteString(`', "__sj_`)
			int32String(c.w, sel.ID)
			c.w.WriteString(`"."json"`)

			// return the cursor for the this child selector as part of the parents json
			if sel.Paging.Cursor {
				c.w.WriteString(`, '`)
				c.w.WriteString(sel.FieldName)
				c.w.WriteString(`_cursor', `)

				c.w.WriteString(`"__sj_`)
				int32String(c.w, sel.ID)
				c.w.WriteString(`"."cursor"`)
			}

			st.Push(sel.ID + closeBlock)
			st.Push(sel.ID)
		}
		i++
	}

	// if len(qc.Roots) == 1 {
	// 	c.w.WriteString(`) AS "__root" FROM (`)
	// 	c.renderQuery(st, false)
	// 	c.w.WriteString(`) AS "__sj_0"`)
	// } else {
	// 	c.w.WriteString(`) AS "__root" FROM (VALUES(true)) AS "__root_x"`)
	// 	c.renderQuery(st, true)
	// }

	// This helps multi-root work as well as return a null json value when
	// there are no rows found.

	c.w.WriteString(`) AS "__root" FROM (VALUES(true)) AS "__root_x"`)
	c.renderQuery(st, true)

	return c.md
}

func (c *compilerContext) renderQuery(st *IntStack, multi bool) {
	for {
		var sel *qcode.Select
		var open bool

		if st.Len() == 0 {
			break
		}

		id := st.Pop()
		if id < closeBlock {
			sel = &c.qc.Selects[id]
			open = true
		} else {
			sel = &c.qc.Selects[(id - closeBlock)]
		}

		if open {
			if sel.Type != qcode.SelTypeUnion {
				if sel.Rel != nil || multi {
					c.renderLateralJoin()
				}
				c.renderPluralSelect(sel)
				c.renderSelect(sel)
			}

			for _, cid := range sel.Children {
				child := &c.qc.Selects[cid]

				if child.SkipRender == qcode.SkipTypeRemote {
					c.md.remoteCount++
					continue

				} else if child.SkipRender != qcode.SkipTypeNone {
					continue
				}

				st.Push(child.ID + closeBlock)
				st.Push(child.ID)
			}

		} else {
			if sel.Type != qcode.SelTypeUnion {
				c.w.WriteString(`)`)
				aliasWithID(c.w, "__sr", sel.ID)

				if !sel.Singular {
					c.w.WriteString(`)`)
					aliasWithID(c.w, "__sj", sel.ID)
				}
				if sel.Rel != nil || multi {
					c.renderLateralJoinClose(sel)
				}
			}

			if sel.Type != qcode.SelTypeMember {
				for _, v := range sel.Args {
					v.Free()
				}
			}
		}
	}
}

func (c *compilerContext) renderPluralSelect(sel *qcode.Select) {
	if sel.Singular {
		return
	}
	c.w.WriteString(`SELECT coalesce(jsonb_agg("__sj_`)
	int32String(c.w, sel.ID)
	c.w.WriteString(`"."json"), '[]') as "json"`)

	// Build the cursor value string
	if sel.Paging.Cursor {
		c.w.WriteString(`, CONCAT_WS(','`)
		for i := 0; i < len(sel.OrderBy); i++ {
			c.w.WriteString(`, max("__cur_`)
			int32String(c.w, int32(i))
			c.w.WriteString(`")`)
		}
		c.w.WriteString(`) as "cursor"`)
	}

	c.w.WriteString(` FROM (`)
}

func (c *compilerContext) renderSelect(sel *qcode.Select) {

	c.w.WriteString(`SELECT to_jsonb("__sr_`)
	int32String(c.w, sel.ID)
	c.w.WriteString(`".*) `)

	// Exclude the cusor values from the the generated json object since
	// we manually use these values to build the cursor string
	// Notice the `- '__cur_` its' what excludes fields in `to_jsonb`
	if sel.Paging.Cursor {
		for i := range sel.OrderBy {
			c.w.WriteString(`- '__cur_`)
			int32String(c.w, int32(i))
			c.w.WriteString(`' `)
		}
	}

	c.w.WriteString(`AS "json" `)

	// We manually insert the cursor values into row we're building outside
	// of the generated json object so they can be used higher up in the sql.
	if sel.Paging.Cursor {
		for i := range sel.OrderBy {
			c.w.WriteString(`, "__cur_`)
			int32String(c.w, int32(i))
			c.w.WriteString(`"`)
		}
	}

	c.w.WriteString(`FROM (SELECT `)
	c.renderColumns(sel)

	// This is how we get the values to use to build the cursor.
	if sel.Paging.Cursor {
		for i, ob := range sel.OrderBy {
			c.w.WriteString(`, LAST_VALUE(`)
			colWithTableID(c.w, sel.Table, sel.ID, ob.Col.Name)
			c.w.WriteString(`) OVER() AS "__cur_`)
			int32String(c.w, int32(i))
			c.w.WriteString(`"`)
		}
	}

	c.w.WriteString(` FROM (`)
	c.renderBaseSelect(sel)
	c.w.WriteString(`)`)
	aliasWithID(c.w, sel.Table, sel.ID)
}

func (c *compilerContext) renderLateralJoin() {
	c.w.WriteString(` LEFT OUTER JOIN LATERAL (`)
}

func (c *compilerContext) renderLateralJoinClose(sel *qcode.Select) {
	c.w.WriteString(`)`)
	aliasWithID(c.w, "__sj", sel.ID)
	c.w.WriteString(` ON true`)
}

func (c *compilerContext) renderJoinTables(rel *sdata.DBRel) {
	if rel != nil && rel.Type == sdata.RelOneToManyThrough {
		c.renderJoin(rel)
	}
}

func (c *compilerContext) renderJoin(rel *sdata.DBRel) {
	c.w.WriteString(` LEFT OUTER JOIN "`)
	c.w.WriteString(rel.Through.ColL.Table)
	c.w.WriteString(`" ON ((`)
	colWithTable(c.w, rel.Through.ColL.Table, rel.Through.ColL.Name)
	c.w.WriteString(`) = (`)
	colWithTable(c.w, rel.Left.Col.Table, rel.Left.Col.Name)
	c.w.WriteString(`))`)
}

func (c *compilerContext) renderBaseSelect(sel *qcode.Select) {
	c.renderCursorCTE(sel)
	c.w.WriteString(`SELECT `)
	c.renderDistinctOn(sel)
	c.renderBaseColumns(sel)
	c.renderFrom(sel)
	c.renderJoinTables(sel.Rel)
	c.renderRelWhere(sel)
	c.renderGroupBy(sel)
	c.renderOrderBy(sel)

	switch {
	case sel.Paging.NoLimit:
		break

	case sel.Singular:
		c.w.WriteString(` LIMIT ('1') :: integer`)

	default:
		c.w.WriteString(` LIMIT ('`)
		int32String(c.w, sel.Paging.Limit)
		c.w.WriteString(`') :: integer`)
	}

	if sel.Paging.Offset != 0 {
		c.w.WriteString(` OFFSET ('`)
		int32String(c.w, sel.Paging.Offset)
		c.w.WriteString(`') :: integer`)
	}
}

func (c *compilerContext) renderFrom(sel *qcode.Select) {
	c.w.WriteString(` FROM `)

	if sel.Rel != nil && sel.Rel.Type == sdata.RelEmbedded {
		// jsonb_to_recordset('[{"a":1,"b":[1,2,3],"c":"bar"}, {"a":2,"b":[1,2,3],"c":"bar"}]') as x(a int, b text, d text);

		c.w.WriteString(`"`)
		c.w.WriteString(sel.Rel.Left.Col.Table)
		c.w.WriteString(`", `)

		c.w.WriteString(sel.Ti.Type)
		c.w.WriteString(`_to_recordset(`)
		colWithTable(c.w, sel.Rel.Left.Col.Table, sel.Rel.Right.Col.Name)
		c.w.WriteString(`) AS `)

		c.w.WriteString(`"`)
		c.w.WriteString(sel.Ti.Name)
		c.w.WriteString(`"`)

		c.w.WriteString(`(`)
		for i, col := range sel.Ti.Columns {
			if i != 0 {
				c.w.WriteString(`, `)
			}
			c.w.WriteString(col.Name)
			c.w.WriteString(` `)
			c.w.WriteString(col.Type)
		}
		c.w.WriteString(`)`)

	} else {
		c.w.WriteString(`"`)
		c.w.WriteString(sel.Table)
		c.w.WriteString(`"`)
	}

	if sel.Paging.Cursor {
		c.w.WriteString(`, "__cur"`)
	}
}

func (c *compilerContext) renderCursorCTE(sel *qcode.Select) {
	if !sel.Paging.Cursor {
		return
	}
	c.w.WriteString(`WITH "__cur" AS (SELECT `)
	for i, ob := range sel.OrderBy {
		if i != 0 {
			c.w.WriteString(`, `)
		}
		c.w.WriteString(`a[`)
		int32String(c.w, int32(i+1))
		c.w.WriteString(`] :: `)
		c.w.WriteString(ob.Col.Type)
		c.w.WriteString(` as `)
		quoted(c.w, ob.Col.Name)
	}
	c.w.WriteString(` FROM string_to_array(`)
	c.md.renderParam(c.w, Param{Name: "cursor", Type: "text"})
	c.w.WriteString(`, ',') as a) `)
}

func (c *compilerContext) renderRelWhere(sel *qcode.Select) {
	var pid int32

	if sel.Rel == nil && sel.Where.Exp == nil {
		return
	}

	if sel.Type == qcode.SelTypeMember {
		pid = sel.UParentID
	} else {
		pid = sel.ParentID
	}

	c.w.WriteString(` WHERE (`)

	if sel.Rel != nil {
		c.renderRel(sel.Ti, sel.Rel, pid)
	}

	if sel.Rel != nil && sel.Where.Exp != nil {
		c.w.WriteString(` AND `)
	}

	if sel.Where.Exp != nil {
		c.renderExp(c.qc.Schema, sel.Ti, sel.Where.Exp, false)
	}

	c.w.WriteString(`)`)
}

func (c *compilerContext) renderRel(ti *sdata.DBTableInfo, rel *sdata.DBRel, pid int32) {
	c.w.WriteString(`((`)

	switch rel.Type {
	case sdata.RelOneToOne, sdata.RelOneToMany:
		//fmt.Fprintf(w, `(("%s"."%s") = ("%s_%d"."%s"))`,
		//c.sel.Name, rel.Left.Col, c.parent.Name, c.parent.ID, rel.Right.Col)

		switch {
		case !rel.Left.Col.Array && rel.Right.Col.Array:
			colWithTable(c.w, rel.Left.Col.Table, rel.Left.Col.Name)
			c.w.WriteString(`) = any (`)
			colWithTableID(c.w, rel.Right.Col.Table, pid, rel.Right.Col.Name)

		case rel.Left.Col.Array && !rel.Right.Col.Array:
			colWithTableID(c.w, rel.Right.Col.Table, pid, rel.Right.Col.Name)
			c.w.WriteString(`) = any (`)
			colWithTable(c.w, rel.Left.Col.Table, rel.Left.Col.Name)

		default:
			colWithTable(c.w, rel.Left.Col.Table, rel.Left.Col.Name)
			c.w.WriteString(`) = (`)
			colWithTableID(c.w, rel.Right.Col.Table, pid, rel.Right.Col.Name)
		}

	case sdata.RelOneToManyThrough:
		// This requires the through table to be joined onto this select
		//fmt.Fprintf(w, `(("%s"."%s") = ("%s"."%s"))`,
		//c.sel.Name, rel.Left.Col, rel.Through, rel.Right.Col)

		switch {
		case !rel.Left.Col.Array && rel.Right.Col.Array:
			colWithTable(c.w, rel.Left.Col.Table, rel.Left.Col.Name)
			c.w.WriteString(`) = any (`)
			colWithTable(c.w, rel.Through.ColR.Table, rel.Through.ColR.Name)

		case rel.Left.Col.Array && !rel.Right.Col.Array:
			colWithTable(c.w, rel.Through.ColR.Table, rel.Through.ColR.Name)
			c.w.WriteString(`) = any (`)
			colWithTable(c.w, rel.Left.Col.Table, rel.Left.Col.Name)

		default:
			colWithTable(c.w, rel.Through.ColR.Table, rel.Through.ColR.Name)
			c.w.WriteString(`) = (`)
			colWithTableID(c.w, rel.Right.Col.Table, pid, rel.Right.Col.Name)
		}

	case sdata.RelEmbedded:
		colWithTable(c.w, rel.Left.Col.Table, rel.Left.Col.Name)
		c.w.WriteString(`) = (`)
		colWithTableID(c.w, rel.Left.Col.Table, pid, rel.Left.Col.Name)

	case sdata.RelPolymorphic:
		colWithTable(c.w, ti.Name, rel.Right.Col.Name)
		c.w.WriteString(`) = (`)
		colWithTableID(c.w, rel.Left.Col.Table, pid, rel.Left.Col.Name)
		c.w.WriteString(`) AND (`)
		colWithTableID(c.w, rel.Left.Col.Table, pid, rel.Right.VTable)
		c.w.WriteString(`) = (`)
		squoted(c.w, ti.Name)
	}
	c.w.WriteString(`))`)
}

func (c *compilerContext) renderExp(schema *sdata.DBSchema, ti *sdata.DBTableInfo, ex *qcode.Exp, skipNested bool) {
	st := util.NewStackInf()
	st.Push(ex)

	for {
		if st.Len() == 0 {
			break
		}

		intf := st.Pop()

		switch val := intf.(type) {
		case int32:
			switch val {
			case '(':
				c.w.WriteString(`(`)
			case ')':
				c.w.WriteString(`)`)
			}

		case qcode.ExpOp:
			switch val {
			case qcode.OpAnd:
				c.w.WriteString(` AND `)
			case qcode.OpOr:
				c.w.WriteString(` OR `)
			case qcode.OpNot:
				c.w.WriteString(`NOT `)
			case qcode.OpFalse:
				c.w.WriteString(`false`)
			}

		case *qcode.Exp:
			switch val.Op {
			case qcode.OpFalse:
				st.Push(val.Op)

			case qcode.OpAnd, qcode.OpOr:
				st.Push(')')
				for i := len(val.Children) - 1; i >= 0; i-- {
					st.Push(val.Children[i])
					if i > 0 {
						st.Push(val.Op)
					}
				}
				st.Push('(')

			case qcode.OpNot:
				st.Push(val.Children[0])
				st.Push(qcode.OpNot)

			default:
				if !skipNested && len(val.Rels) != 0 {
					c.w.WriteString(`EXISTS `)
					c.renderNestedWhere(schema, ti, val)
				} else {
					c.renderOp(schema, ti, val)
				}
			}
		}
	}
}

func (c *compilerContext) renderNestedWhere(
	schema *sdata.DBSchema, ti *sdata.DBTableInfo, ex *qcode.Exp) {
	for i, rel := range ex.Rels {
		if i != 0 {
			c.w.WriteString(` AND `)
		}

		c.w.WriteString(`(SELECT 1 FROM `)
		c.w.WriteString(rel.Left.Col.Table)
		c.renderJoinTables(rel)
		c.w.WriteString(` WHERE `)
		c.renderRel(ti, rel, -1)
		c.w.WriteString(` AND (`)
		c.renderExp(schema, rel.Left.Col.Ti, ex, true)
		c.w.WriteString(`)`)
	}

	for i := 0; i < len(ex.Rels); i++ {
		c.w.WriteString(`)`)
	}
}

func (c *compilerContext) renderOp(schema *sdata.DBSchema, ti *sdata.DBTableInfo, ex *qcode.Exp) {
	if ex.Op == qcode.OpNop {
		return
	}

	if ex.Col != nil {
		c.w.WriteString(`((`)
		if ex.Type == qcode.ValRef && ex.Op == qcode.OpIsNull {
			colWithTable(c.w, ex.Table, ex.Col.Name)
		} else {
			colWithTable(c.w, ti.Name, ex.Col.Name)
		}
		c.w.WriteString(`) `)
	}

	switch ex.Op {
	case qcode.OpEquals:
		c.w.WriteString(`=`)
	case qcode.OpNotEquals:
		c.w.WriteString(`!=`)
	case qcode.OpNotDistinct:
		c.w.WriteString(`IS NOT DISTINCT FROM`)
	case qcode.OpDistinct:
		c.w.WriteString(`IS DISTINCT FROM`)
	case qcode.OpGreaterOrEquals:
		c.w.WriteString(`>=`)
	case qcode.OpLesserOrEquals:
		c.w.WriteString(`<=`)
	case qcode.OpGreaterThan:
		c.w.WriteString(`>`)
	case qcode.OpLesserThan:
		c.w.WriteString(`<`)
	case qcode.OpIn:
		c.w.WriteString(`= ANY`)
	case qcode.OpNotIn:
		c.w.WriteString(`!= ANY`)
	case qcode.OpLike:
		c.w.WriteString(`LIKE`)
	case qcode.OpNotLike:
		c.w.WriteString(`NOT LIKE`)
	case qcode.OpILike:
		c.w.WriteString(`ILIKE`)
	case qcode.OpNotILike:
		c.w.WriteString(`NOT ILIKE`)
	case qcode.OpSimilar:
		c.w.WriteString(`SIMILAR TO`)
	case qcode.OpNotSimilar:
		c.w.WriteString(`NOT SIMILAR TO`)
	case qcode.OpContains:
		c.w.WriteString(`@>`)
	case qcode.OpContainedIn:
		c.w.WriteString(`<@`)
	case qcode.OpHasKey:
		c.w.WriteString(`?`)
	case qcode.OpHasKeyAny:
		c.w.WriteString(`?|`)
	case qcode.OpHasKeyAll:
		c.w.WriteString(`?&`)
	case qcode.OpIsNull:
		if strings.EqualFold(ex.Val, "true") {
			c.w.WriteString(`IS NULL)`)
		} else {
			c.w.WriteString(`IS NOT NULL)`)
		}
		return

	case qcode.OpTsQuery:
		//fmt.Fprintf(w, `(("%s") @@ websearch_to_tsquery('%s'))`, c.ti.TSVCol, val.Val)
		c.w.WriteString(`((`)
		colWithTable(c.w, ti.Name, ti.TSVCol.Name)
		if ti.Schema.DBVersion() >= 110000 {
			c.w.WriteString(`) @@ websearch_to_tsquery(`)
		} else {
			c.w.WriteString(`) @@ to_tsquery(`)
		}
		c.md.renderParam(c.w, Param{Name: ex.Val, Type: "text"})
		c.w.WriteString(`))`)
		return
	}

	switch {
	case ex.Type == qcode.ValList:
		c.renderList(ex)
	default:
		c.renderVal(ex, c.vars)
	}

	c.w.WriteString(`)`)
}

func (c *compilerContext) renderGroupBy(sel *qcode.Select) {
	if !sel.GroupCols {
		return
	}
	c.w.WriteString(` GROUP BY `)

	for i, col := range sel.Cols {
		if i != 0 {
			c.w.WriteString(`, `)
		}
		colWithTable(c.w, sel.Ti.Name, col.Col.Name)
	}
}

func (c *compilerContext) renderOrderBy(sel *qcode.Select) {
	if len(sel.OrderBy) == 0 {
		return
	}
	c.w.WriteString(` ORDER BY `)
	for i, col := range sel.OrderBy {
		if i != 0 {
			c.w.WriteString(`, `)
		}
		colWithTable(c.w, sel.Ti.Name, col.Col.Name)

		switch col.Order {
		case qcode.OrderAsc:
			c.w.WriteString(` ASC`)
		case qcode.OrderDesc:
			c.w.WriteString(` DESC`)
		case qcode.OrderAscNullsFirst:
			c.w.WriteString(` ASC NULLS FIRST`)
		case qcode.OrderDescNullsFirst:
			c.w.WriteString(` DESC NULLLS FIRST`)
		case qcode.OrderAscNullsLast:
			c.w.WriteString(` ASC NULLS LAST`)
		case qcode.OrderDescNullsLast:
			c.w.WriteString(` DESC NULLS LAST`)
		}
	}
}

func (c *compilerContext) renderDistinctOn(sel *qcode.Select) {
	if len(sel.DistinctOn) == 0 {
		return
	}
	c.w.WriteString(`DISTINCT ON (`)
	for i, col := range sel.DistinctOn {
		if i != 0 {
			c.w.WriteString(`, `)
		}
		colWithTable(c.w, sel.Ti.Name, col.Name)
	}
	c.w.WriteString(`) `)
}

func (c *compilerContext) renderList(ex *qcode.Exp) {
	c.w.WriteString(` (ARRAY[`)
	for i := range ex.ListVal {
		if i != 0 {
			c.w.WriteString(`, `)
		}
		switch ex.ListType {
		case qcode.ValBool, qcode.ValNum:
			c.w.WriteString(ex.ListVal[i])
		case qcode.ValStr:
			c.w.WriteString(`'`)
			c.w.WriteString(ex.ListVal[i])
			c.w.WriteString(`'`)
		}
	}
	c.w.WriteString(`])`)
}

func (c *compilerContext) renderVal(ex *qcode.Exp, vars map[string]string) {
	c.w.WriteString(` `)

	switch ex.Type {
	case qcode.ValVar:
		val, ok := vars[ex.Val]
		switch {
		case ok && strings.HasPrefix(val, "sql:"):
			c.w.WriteString(`(`)
			c.md.RenderVar(c.w, val[4:])
			c.w.WriteString(`)`)

		case ok:
			squoted(c.w, val)

		case ex.Op == qcode.OpIn || ex.Op == qcode.OpNotIn:
			c.w.WriteString(`(ARRAY(SELECT json_array_elements_text(`)
			c.md.renderParam(c.w, Param{Name: ex.Val, Type: ex.Col.Type, IsArray: true})
			c.w.WriteString(`))`)
			c.w.WriteString(` :: `)
			c.w.WriteString(ex.Col.Type)
			c.w.WriteString(`[])`)
			return

		default:
			c.md.renderParam(c.w, Param{Name: ex.Val, Type: ex.Col.Type, IsArray: false})
		}

	case qcode.ValRef:
		colWithTable(c.w, ex.Table, ex.Col.Name)

	default:
		squoted(c.w, ex.Val)
	}

	c.w.WriteString(` :: `)
	c.w.WriteString(ex.Col.Type)
}
