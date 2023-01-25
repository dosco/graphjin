//nolint:errcheck
package psql

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

const (
	closeBlock = 500
)

type Param struct {
	Name      string
	Type      string
	IsArray   bool
	IsNotNull bool
}

type Metadata struct {
	ct     string
	poll   bool
	params []Param
	pindex map[string]int
}

type compilerContext struct {
	md     *Metadata
	w      *bytes.Buffer
	qc     *qcode.QCode
	isJSON bool
	*Compiler
}

type Variables map[string]json.RawMessage

type Config struct {
	Vars            map[string]string
	DBType          string
	DBVersion       int
	SecPrefix       []byte
	EnableCamelcase bool
}

type Compiler struct {
	svars           map[string]string
	ct              string // db type
	cv              int    // db version
	pf              []byte // security prefix
	enableCamelcase bool
}

func NewCompiler(conf Config) *Compiler {
	return &Compiler{
		svars:           conf.Vars,
		ct:              conf.DBType,
		cv:              conf.DBVersion,
		pf:              conf.SecPrefix,
		enableCamelcase: conf.EnableCamelcase,
	}
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

	w.WriteString(`/* action='` + qc.Name + `',controller='graphql',framework='graphjin' */ `)

	switch qc.Type {
	case qcode.QTQuery:
		co.CompileQuery(w, qc, &md)

	case qcode.QTSubscription:
		co.CompileQuery(w, qc, &md)

	case qcode.QTMutation:
		co.compileMutation(w, qc, &md)

	default:
		err = fmt.Errorf("unknown operation type %d", qc.Type)
	}

	return md, err
}

func (co *Compiler) CompileQuery(
	w *bytes.Buffer,
	qc *qcode.QCode,
	md *Metadata,
) {
	if qc.Type == qcode.QTSubscription {
		md.poll = true
	}

	md.ct = qc.Schema.DBType()

	st := NewIntStack()
	c := &compilerContext{
		md:       md,
		w:        w,
		qc:       qc,
		Compiler: co,
	}

	i := 0
	switch c.ct {
	case "mysql":
		c.w.WriteString(`SELECT json_object(`)
	default:
		c.w.WriteString(`SELECT jsonb_build_object(`)
	}
	if qc.Typename {
		c.w.WriteString(`'__typename', `)
		c.squoted(qc.Name)
		i++
	}

	for _, id := range qc.Roots {
		sel := &qc.Selects[id]

		if sel.SkipRender == qcode.SkipTypeDrop {
			continue
		}

		if i != 0 {
			c.w.WriteString(`, `)
		}

		switch sel.SkipRender {
		case qcode.SkipTypeUserNeeded, qcode.SkipTypeBlocked,
			qcode.SkipTypeNulled:

			c.w.WriteString(`'`)
			c.w.WriteString(sel.FieldName)
			c.w.WriteString(`', NULL`)

			if sel.Paging.Cursor {
				c.w.WriteString(`, '`)
				c.w.WriteString(sel.FieldName)
				c.w.WriteString(`_cursor', NULL`)
			}

		default:
			c.w.WriteString(`'`)
			c.w.WriteString(sel.FieldName)
			c.w.WriteString(`', __sj_`)
			int32String(c.w, sel.ID)
			c.w.WriteString(`.json`)

			// return the cursor for the this child selector as part of the parents json
			if sel.Paging.Cursor {
				c.w.WriteString(`, '`)
				c.w.WriteString(sel.FieldName)
				c.w.WriteString(`_cursor', `)

				c.w.WriteString(`__sj_`)
				int32String(c.w, sel.ID)
				c.w.WriteString(`.__cursor`)
			}

			st.Push(sel.ID + closeBlock)
			st.Push(sel.ID)
		}
		i++
	}

	// This helps multi-root work as well as return a null json value when
	// there are no rows found.

	c.w.WriteString(`) AS __root FROM ((SELECT true)) AS __root_x`)
	c.renderQuery(st, true)
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
				c.renderLateralJoin(sel, multi)
				c.renderPluralSelect(sel)
				c.renderSelect(sel)
			}

			for _, cid := range sel.Children {
				child := &c.qc.Selects[cid]

				if child.SkipRender != qcode.SkipTypeNone {
					continue
				}

				st.Push(child.ID + closeBlock)
				st.Push(child.ID)
			}

		} else {
			if sel.Type != qcode.SelTypeUnion {
				c.renderSelectClose(sel)
				c.renderLateralJoinClose(sel, multi)
			}
		}
	}
}

func (c *compilerContext) renderPluralSelect(sel *qcode.Select) {
	if sel.Singular {
		return
	}

	c.w.WriteString(`SELECT `)
	if sel.FieldFilter.Exp != nil {
		c.w.WriteString(`(CASE WHEN `)
		c.renderExp(sel.Ti, sel.FieldFilter.Exp, false)
		c.w.WriteString(` THEN (`)
	}

	switch c.ct {
	case "mysql":
		c.w.WriteString(`CAST(COALESCE(json_arrayagg(__sj_`)
		int32String(c.w, sel.ID)
		c.w.WriteString(`.json), '[]') AS JSON)`)

	default:
		c.w.WriteString(`COALESCE(jsonb_agg(__sj_`)
		int32String(c.w, sel.ID)
		c.w.WriteString(`.json), '[]')`)
	}

	if sel.FieldFilter.Exp != nil {
		c.w.WriteString(`) ELSE null END)`)
	}
	c.w.WriteString(` AS json`)

	// Build the cursor value string
	if sel.Paging.Cursor {
		c.w.WriteString(`, CONCAT('`)
		c.w.Write(c.pf)
		c.w.WriteString(`', CONCAT_WS(',', `)
		int32String(c.w, int32(sel.ID))

		for i := 0; i < len(sel.OrderBy); i++ {
			c.w.WriteString(`, MAX(__cur_`)
			int32String(c.w, int32(i))
			c.w.WriteString(`)`)
			// 	c.w.WriteString(`, CAST(MAX(__cur_`)
			// 	int32String(c.w, int32(i))
			// 	c.w.WriteString(`) AS CHAR(20))`)
		}
		c.w.WriteString(`)) as __cursor`)
	}

	c.w.WriteString(` FROM (`)
}

func (c *compilerContext) renderSelect(sel *qcode.Select) {
	switch c.ct {
	case "mysql":
		c.w.WriteString(`SELECT json_object(`)
		c.renderJSONFields(sel)
		c.w.WriteString(`) `)
	default:
		c.w.WriteString(`SELECT to_jsonb(__sr_`)
		int32String(c.w, sel.ID)
		c.w.WriteString(`.*) `)

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
	}
	c.w.WriteString(`AS json `)

	// We manually insert the cursor values into row we're building outside
	// of the generated json object so they can be used higher up in the sql.
	if sel.Paging.Cursor {
		for i := range sel.OrderBy {
			c.w.WriteString(`, __cur_`)
			int32String(c.w, int32(i))
			c.w.WriteString(` `)
		}
	}

	c.w.WriteString(`FROM (SELECT `)
	c.renderColumns(sel)

	// This is how we get the values to use to build the cursor.
	if sel.Paging.Cursor {
		for i, ob := range sel.OrderBy {
			c.w.WriteString(`, LAST_VALUE(`)
			c.colWithTableID(sel.Table, sel.ID, ob.Col.Name)
			c.w.WriteString(`) OVER() AS __cur_`)
			int32String(c.w, int32(i))
		}
	}

	c.w.WriteString(` FROM (`)
	if sel.Rel.Type == sdata.RelRecursive {
		c.renderRecursiveBaseSelect(sel)
	} else {
		c.renderBaseSelect(sel)
	}
	c.w.WriteString(`)`)
	c.aliasWithID(sel.Table, sel.ID)
}

func (c *compilerContext) renderSelectClose(sel *qcode.Select) {
	c.w.WriteString(`)`)
	c.aliasWithID("__sr", sel.ID)

	if !sel.Singular {
		c.w.WriteString(`)`)
		c.aliasWithID("__sj", sel.ID)
	}
}

func (c *compilerContext) renderLateralJoin(sel *qcode.Select, multi bool) {
	if sel.Rel.Type == sdata.RelNone && !multi {
		return
	}
	c.w.WriteString(` LEFT OUTER JOIN LATERAL (`)
}

func (c *compilerContext) renderLateralJoinClose(sel *qcode.Select, multi bool) {
	if sel.Rel.Type == sdata.RelNone && !multi {
		return
	}
	c.w.WriteString(`)`)
	c.aliasWithID(`__sj`, sel.ID)
	c.w.WriteString(` ON true`)
}

func (c *compilerContext) renderJoinTables(sel *qcode.Select) {
	for _, join := range sel.Joins {
		c.renderJoin(join)
	}
	if c.ct != "mysql" {
		c.renderPostgreaOnlyJoinTables(sel)
	}
}

func (c *compilerContext) renderPostgreaOnlyJoinTables(sel *qcode.Select) {
	for _, ob := range sel.OrderBy {
		if ob.Var != "" {
			c.renderJoinForOrderByList(ob)
		}
	}
}

func (c *compilerContext) renderJoin(join qcode.Join) {
	c.w.WriteString(` INNER JOIN `)
	c.w.WriteString(join.Rel.Left.Ti.Name)
	c.w.WriteString(` ON ((`)
	c.renderExp(join.Rel.Left.Ti, join.Filter, false)
	c.w.WriteString(`))`)
}

func (c *compilerContext) renderJoinForOrderByList(ob qcode.OrderBy) {
	c.w.WriteString(` JOIN (SELECT id ::` + ob.Col.Type + `, ord FROM json_array_elements_text(`)
	c.renderParam(Param{Name: ob.Var, Type: ob.Col.Type})
	c.w.WriteString(`) WITH ORDINALITY a(id, ord)) AS _gj_ob_` + ob.Col.Name + `(id, ord) USING (id)`)
}

func (c *compilerContext) renderBaseSelect(sel *qcode.Select) {
	c.renderCursorCTE(sel)
	c.w.WriteString(`SELECT `)
	c.renderDistinctOn(sel)
	c.renderBaseColumns(sel)
	c.renderFrom(sel)
	c.renderJoinTables(sel)
	c.renderFromCursor(sel)
	c.renderWhere(sel)
	c.renderGroupBy(sel)
	c.renderOrderBy(sel)
	c.renderLimit(sel)
}

func (c *compilerContext) renderLimit(sel *qcode.Select) {
	switch c.ct {
	case "mysql":
		c.renderMysqlLimit(sel)
	default:
		c.renderDefaultLimit(sel)
	}
}

func (c *compilerContext) renderDefaultLimit(sel *qcode.Select) {
	switch {
	case sel.Paging.NoLimit:
		break

	case sel.Singular:
		c.w.WriteString(` LIMIT 1`)

	case sel.Paging.LimitVar != "":
		c.w.WriteString(` LIMIT LEAST(`)
		c.renderParam(Param{Name: sel.Paging.LimitVar, Type: "integer"})
		c.w.WriteString(`, `)
		int32String(c.w, sel.Paging.Limit)
		c.w.WriteString(`)`)

	default:
		c.w.WriteString(` LIMIT `)
		int32String(c.w, sel.Paging.Limit)
	}

	switch {
	case sel.Paging.OffsetVar != "":
		c.w.WriteString(` OFFSET `)
		c.renderParam(Param{Name: sel.Paging.OffsetVar, Type: "integer"})

	case sel.Paging.Offset != 0:
		c.w.WriteString(` OFFSET `)
		int32String(c.w, sel.Paging.Offset)
	}
}

func (c *compilerContext) renderMysqlLimit(sel *qcode.Select) {
	c.w.WriteString(` LIMIT `)

	switch {
	case sel.Paging.OffsetVar != "":
		c.renderParam(Param{Name: sel.Paging.OffsetVar, Type: "integer"})
		c.w.WriteString(`, `)

	case sel.Paging.Offset != 0:
		int32String(c.w, sel.Paging.Offset)
		c.w.WriteString(`, `)
	}

	switch {
	case sel.Paging.NoLimit:
		c.w.WriteString(`18446744073709551610`)

	case sel.Singular:
		c.w.WriteString(`1`)

	default:
		int32String(c.w, sel.Paging.Limit)
	}
}

func (c *compilerContext) renderFrom(sel *qcode.Select) {
	c.w.WriteString(` FROM `)

	if c.qc.Type == qcode.QTMutation {
		c.quoted(sel.Table)
		return
	}

	if sel.Ti.Type == "function" {
		c.renderTableFunction(sel)
		return
	}

	switch sel.Rel.Type {
	case sdata.RelEmbedded:
		c.w.WriteString(sel.Rel.Left.Col.Table)
		c.w.WriteString(`, `)

		switch c.ct {
		case "mysql":
			c.renderJSONTable(sel)
		default:
			c.renderSelectToRecordSet(sel)
		}

	default:
		c.table(sel.Ti.Schema, sel.Ti.Name, true)
	}
}

func (c *compilerContext) renderFromCursor(sel *qcode.Select) {
	if sel.Paging.Cursor {
		c.w.WriteString(`, __cur`)
	}
}

func (c *compilerContext) renderJSONTable(sel *qcode.Select) {
	c.w.WriteString(`JSON_TABLE(`)
	c.colWithTable(sel.Rel.Left.Col.Table, sel.Rel.Left.Col.Name)
	c.w.WriteString(`, "$[*]" COLUMNS(`)

	for i, col := range sel.Ti.Columns {
		if i != 0 {
			c.w.WriteString(`, `)
		}
		c.w.WriteString(col.Name)
		c.w.WriteString(` `)
		c.w.WriteString(col.Type)
		c.w.WriteString(` PATH "$.`)
		c.w.WriteString(col.Name)
		c.w.WriteString(`" ERROR ON ERROR`)
	}
	c.w.WriteString(`)) AS`)
	c.quoted(sel.Table)
}

func (c *compilerContext) renderSelectToRecordSet(sel *qcode.Select) {
	// jsonb_to_recordset('[{"a":1,"b":[1,2,3],"c":"bar"}, {"a":2,"b":[1,2,3],"c":"bar"}]') as x(a int, b text, d text);
	c.w.WriteString(sel.Ti.Type)
	c.w.WriteString(`_to_recordset(`)
	c.colWithTable(sel.Rel.Left.Col.Table, sel.Rel.Left.Col.Name)
	c.w.WriteString(`) AS `)
	c.quoted(sel.Table)

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
}

func (c *compilerContext) renderCursorCTE(sel *qcode.Select) {
	if !sel.Paging.Cursor {
		return
	}
	c.w.WriteString(`WITH __cur AS (SELECT `)
	switch c.ct {
	case "mysql":
		for i, ob := range sel.OrderBy {
			if i != 0 {
				c.w.WriteString(`, `)
			}
			c.w.WriteString(`NULLIF(SUBSTRING_INDEX(SUBSTRING_INDEX(a.i, ',', `)
			int32String(c.w, int32(i+2))
			c.w.WriteString(`), ',', -1), '') AS `)
			// c.w.WriteString(ob.Col.Type)
			c.quoted(ob.Col.Name)
		}
		c.w.WriteString(` FROM ((SELECT `)
		c.renderParam(Param{Name: "cursor", Type: "text"})
		c.w.WriteString(` AS i)) AS a) `)

	default:
		for i, ob := range sel.OrderBy {
			if i != 0 {
				c.w.WriteString(`, `)
			}
			c.w.WriteString(`a[`)
			int32String(c.w, int32((i + 2)))
			c.w.WriteString(`] :: `)
			c.w.WriteString(ob.Col.Type)
			c.w.WriteString(` AS `)
			c.quoted(ob.Col.Name)
		}
		c.w.WriteString(` FROM STRING_TO_ARRAY(`)
		c.renderParam(Param{Name: "cursor", Type: "text"})
		c.w.WriteString(`, ',') AS a) `)

		// c.w.WriteString(`WHERE CAST(a.a[1] AS integer) = `)
		// int32String(c.w, sel.ID)
		// c.w.WriteString(`) `)
	}
}

func (c *compilerContext) renderWhere(sel *qcode.Select) {
	if sel.Rel.Type == sdata.RelNone && sel.Where.Exp == nil {
		return
	}

	c.w.WriteString(` WHERE `)
	c.renderExp(sel.Ti, sel.Where.Exp, false)
}

func (c *compilerContext) renderGroupBy(sel *qcode.Select) {
	if !sel.GroupCols || len(sel.BCols) == 0 {
		return
	}
	c.w.WriteString(` GROUP BY `)
	for i, col := range sel.BCols {
		if i != 0 {
			c.w.WriteString(`, `)
		}
		c.colWithTable(sel.Table, col.Col.Name)
	}
}

func (c *compilerContext) renderOrderBy(sel *qcode.Select) {
	if len(sel.OrderBy) == 0 {
		return
	}
	c.w.WriteString(` ORDER BY `)

	for i, ob := range sel.OrderBy {
		if i != 0 {
			c.w.WriteString(`, `)
		}
		if ob.KeyVar != "" && ob.Key != "" {
			c.w.WriteString(` CASE WHEN `)
			c.renderParam(Param{Name: ob.KeyVar, Type: "text"})
			c.w.WriteString(` = `)
			c.squoted(ob.Key)
			c.w.WriteString(` THEN `)
		}
		if ob.Var != "" {
			switch c.ct {
			case "mysql":
				c.renderOrderByList(ob)
			default:
				c.colWithTable(`_gj_ob_`+ob.Col.Name, "ord")
			}
		} else {
			c.colWithTable(ob.Col.Table, ob.Col.Name)
		}
		if ob.KeyVar != "" && ob.Key != "" {
			c.w.WriteString(` END `)
		}

		switch ob.Order {
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

func (c *compilerContext) renderOrderByList(ob qcode.OrderBy) {
	switch c.ct {
	case "mysql":
		c.w.WriteString(`FIND_IN_SET(`)
		c.colWithTable(ob.Col.Table, ob.Col.Name)
		c.w.WriteString(`, (SELECT GROUP_CONCAT(id) FROM JSON_TABLE(`)
		c.renderParam(Param{Name: ob.Var, Type: "text"})
		c.w.WriteString(`, '$[*]' COLUMNS (id ` + ob.Col.Type + ` PATH '$')) AS a))`)
	default:
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
		c.colWithTable(sel.Table, col.Name)
	}
	c.w.WriteString(`) `)
}
