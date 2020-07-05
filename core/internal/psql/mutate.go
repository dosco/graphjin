//nolint:errcheck
package psql

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/dosco/super-graph/core/internal/qcode"
	"github.com/dosco/super-graph/core/internal/util"
	"github.com/dosco/super-graph/jsn"
)

type itemType int

const (
	itemInsert itemType = iota + 1
	itemUpdate
	itemConnect
	itemDisconnect
	itemUnion
)

var insertTypes = map[string]itemType{
	"connect": itemConnect,
}

var updateTypes = map[string]itemType{
	"connect":    itemConnect,
	"disconnect": itemDisconnect,
}

var noLimit = qcode.Paging{NoLimit: true}

func (co *Compiler) compileMutation(w io.Writer, qc *qcode.QCode, vars Variables) (Metadata, error) {
	md := Metadata{}

	if len(qc.Selects) == 0 {
		return md, errors.New("empty query")
	}

	c := &compilerContext{md, w, qc.Selects, co}
	root := &qc.Selects[0]

	ti, err := c.schema.GetTableInfoB(root.Name)
	if err != nil {
		return c.md, err
	}

	switch qc.Type {
	case qcode.QTInsert:
		if _, err := c.renderInsert(w, qc, vars, ti, false); err != nil {
			return c.md, err
		}

	case qcode.QTUpdate:
		if _, err := c.renderUpdate(w, qc, vars, ti); err != nil {
			return c.md, err
		}

	case qcode.QTUpsert:
		if _, err := c.renderUpsert(w, qc, vars, ti); err != nil {
			return c.md, err
		}

	case qcode.QTDelete:
		if _, err := c.renderDelete(w, qc, vars, ti); err != nil {
			return c.md, err
		}

	default:
		return c.md, errors.New("valid mutations are 'insert', 'update', 'upsert' and 'delete'")
	}

	root.Paging = noLimit
	root.DistinctOn = root.DistinctOn[:]
	root.OrderBy = root.OrderBy[:]
	root.Where = nil
	root.Args = nil

	return co.compileQueryWithMetadata(w, qc, vars, c.md)
}

type kvitem struct {
	id     int32
	_type  itemType
	_ctype int
	key    string
	path   []string
	val    json.RawMessage
	data   map[string]json.RawMessage
	array  bool
	ti     *DBTableInfo
	relCP  *DBRel
	relPC  *DBRel
	items  []kvitem
}

type renitem struct {
	kvitem
	array bool
	data  map[string]json.RawMessage
}

// TODO: Handle cases where a column name matches the child table name
// the child path needs to be exluded in the json sent to insert or update

func (c *compilerContext) handleKVItem(st *util.Stack, item kvitem) error {
	var data map[string]json.RawMessage
	var array bool
	var err error

	if item.data == nil {
		data, array, err = jsn.Tree(item.val)
		if err != nil {
			return err
		}
	} else {
		data, array = item.data, item.array
	}

	var unionize bool
	id := item.id + 1

	item.items = make([]kvitem, 0, len(data))

	for k, v := range data {
		if v[0] != '{' && v[0] != '[' {
			continue
		}

		// Get child-to-parent relationship
		relCP, err := c.schema.GetRel(k, item.key)
		if err != nil {
			var ty itemType
			var ok bool

			switch item._type {
			case itemInsert:
				ty, ok = insertTypes[k]
			case itemUpdate:
				ty, ok = updateTypes[k]
			}

			if ok {
				unionize = true
				item1 := item
				item1._type = ty
				item1.id = id
				item1.val = v

				item.items = append(item.items, item1)
				id++
			}

			// Get parent-to-child relationship
		} else if relPC, err := c.schema.GetRel(item.key, k); err == nil {
			ti, err := c.schema.GetTableInfo(k)
			if err != nil {
				return err
			}

			item1 := kvitem{
				id:    id,
				_type: item._type,
				key:   k,
				val:   v,
				path:  append(item.path, k),
				ti:    ti,
				relCP: relCP,
				relPC: relPC,
			}

			if v[0] == '{' {
				item1.data, item1.array, err = jsn.Tree(v)
				if err != nil {
					return err
				}
				if v1, ok := item1.data["connect"]; ok && (v1[0] == '{' || v1[0] == '[') {
					item1._ctype |= (1 << itemConnect)
				}
				if v1, ok := item1.data["disconnect"]; ok && (v1[0] == '{' || v1[0] == '[') {
					item1._ctype |= (1 << itemDisconnect)
				}
			}

			item.items = append(item.items, item1)
			id++
		}
	}

	if unionize {
		item._type = itemUnion
	}

	// For inserts order the children according to
	// the creation order required by the parent-to-child
	// relationships. For example users need to be created
	// before the products they own.

	// For updates the order defined in the query must be
	// the order used.
	switch item._type {
	case itemInsert:
		for _, v := range item.items {
			if v.relPC.Type == RelOneToMany {
				st.Push(v)
			}
		}
		st.Push(renitem{kvitem: item, array: array, data: data})
		for _, v := range item.items {
			if v.relPC.Type == RelOneToOne {
				st.Push(v)
			}
		}

	case itemUpdate:
		for _, v := range item.items {
			if !(v._ctype > 0 && v.relPC.Type == RelOneToOne) {
				st.Push(v)
			}
		}
		st.Push(renitem{kvitem: item, array: array, data: data})
		for _, v := range item.items {
			if v._ctype > 0 && v.relPC.Type == RelOneToOne {
				st.Push(v)
			}
		}

	case itemUnion:
		st.Push(renitem{kvitem: item, array: array, data: data})
		for _, v := range item.items {
			st.Push(v)
		}

	default:
		for _, v := range item.items {
			st.Push(v)
		}
		st.Push(renitem{kvitem: item, array: array, data: data})
	}

	return nil
}

func (c *compilerContext) renderUnionStmt(w io.Writer, item renitem) error {
	var connect, disconnect bool

	// Render only for parent-to-child relationship of one-to-many
	if item.relPC.Type != RelOneToMany {
		return nil
	}

	for _, v := range item.items {
		if v._type == itemConnect {
			connect = true
		} else if v._type == itemDisconnect {
			disconnect = true
		}
		if connect && disconnect {
			break
		}
	}

	if connect {
		io.WriteString(w, `, `)
		if connect && disconnect {
			renderCteNameWithSuffix(w, item.kvitem, "c")
		} else {
			quoted(w, item.ti.Name)
		}
		io.WriteString(w, ` AS ( UPDATE `)
		quoted(w, item.ti.Name)
		io.WriteString(w, ` SET `)
		quoted(w, item.relPC.Right.Col)
		io.WriteString(w, ` = `)

		// When setting the id of the connected table in a one-to-many setting
		// we always overwrite the value including for array columns
		colWithTable(w, item.relPC.Left.Table, item.relPC.Left.Col)

		io.WriteString(w, ` FROM `)
		quoted(w, item.relPC.Left.Table)
		io.WriteString(w, ` WHERE`)

		i := 0
		for _, v := range item.items {
			if v._type == itemConnect {
				if i != 0 {
					io.WriteString(w, ` OR (`)
				} else {
					io.WriteString(w, ` (`)
				}
				if err := renderWhereFromJSON(w, v, "connect", v.val); err != nil {
					return err
				}
				io.WriteString(w, `)`)
				i++
			}
		}
		io.WriteString(w, ` RETURNING `)
		quoted(w, item.ti.Name)
		io.WriteString(w, `.*)`)
	}

	if disconnect {
		io.WriteString(w, `, `)
		if connect && disconnect {
			renderCteNameWithSuffix(w, item.kvitem, "d")
		} else {
			quoted(w, item.ti.Name)
		}
		io.WriteString(w, ` AS ( UPDATE `)
		quoted(w, item.ti.Name)
		io.WriteString(w, ` SET `)
		quoted(w, item.relPC.Right.Col)
		io.WriteString(w, ` = `)

		if item.relPC.Right.Array {
			io.WriteString(w, ` array_remove(`)
			quoted(w, item.relPC.Right.Col)
			io.WriteString(w, `, `)
			colWithTable(w, item.relPC.Left.Table, item.relPC.Left.Col)
			io.WriteString(w, `)`)

		} else {
			io.WriteString(w, ` NULL`)
		}

		io.WriteString(w, ` FROM `)
		quoted(w, item.relPC.Left.Table)
		io.WriteString(w, ` WHERE`)

		i := 0
		for _, v := range item.items {
			if v._type == itemDisconnect {
				if i != 0 {
					io.WriteString(w, ` OR (`)
				} else {
					io.WriteString(w, ` (`)
				}
				if err := renderWhereFromJSON(w, v, "disconnect", v.val); err != nil {
					return err
				}
				io.WriteString(w, `)`)
				i++
			}
		}
		io.WriteString(w, ` RETURNING `)
		quoted(w, item.ti.Name)
		io.WriteString(w, `.*)`)
	}

	if connect && disconnect {
		io.WriteString(w, `, `)
		quoted(w, item.ti.Name)
		io.WriteString(w, ` AS (`)
		io.WriteString(w, `SELECT * FROM `)
		renderCteNameWithSuffix(w, item.kvitem, "c")
		io.WriteString(w, ` UNION ALL `)
		io.WriteString(w, `SELECT * FROM `)
		renderCteNameWithSuffix(w, item.kvitem, "d")
		io.WriteString(w, `)`)
	}

	return nil
}

func (c *compilerContext) renderInsertUpdateColumns(
	qc *qcode.QCode,
	jt map[string]json.RawMessage,
	ti *DBTableInfo,
	skipcols map[string]struct{},
	isValues bool) (uint32, error) {

	root := &qc.Selects[0]
	renderedCol := false

	n := 0
	for _, cn := range ti.Columns {
		if _, ok := skipcols[cn.Name]; ok {
			continue
		}
		if _, ok := jt[cn.Key]; !ok {
			continue
		}
		if cn.Blocked {
			return 0, fmt.Errorf("insert: column '%s' blocked", cn.Name)
		}
		if _, ok := root.PresetMap[cn.Key]; ok {
			continue
		}
		if err := ColumnAccess(ti, root, cn.Name, true); err != nil {
			return 0, err
		}
		if n != 0 {
			io.WriteString(c.w, `, `)
		}

		if isValues {
			io.WriteString(c.w, `CAST( i.j ->>`)
			io.WriteString(c.w, `'`)
			io.WriteString(c.w, cn.Name)
			io.WriteString(c.w, `' AS `)
			io.WriteString(c.w, cn.Type)
			io.WriteString(c.w, `)`)
		} else {
			quoted(c.w, cn.Name)
		}

		if !renderedCol {
			renderedCol = true
		}
		n++
	}

	for i, pcol := range root.PresetList {
		col, err := ti.GetColumn(pcol)
		if err != nil {
			return 0, fmt.Errorf("insert presets: %w", err)
		}
		if _, ok := skipcols[col.Name]; ok {
			continue
		}

		if i != 0 || n != 0 {
			io.WriteString(c.w, `, `)
		}

		if isValues {
			val := root.PresetMap[col.Name]
			var v string

			if len(val) > 1 && val[0] == '$' {
				vn := val[1:]
				if v1, ok := c.vars[vn]; ok {
					v = v1
				} else {
					v = val
				}
			} else {
				v = val
			}

			switch {
			case len(v) > 1 && v[0] == '$':
				c.md.renderParam(c.w, Param{Name: v[1:], Type: col.Type})

			case strings.HasPrefix(v, "sql:"):
				io.WriteString(c.w, `(`)
				c.md.RenderVar(c.w, v[4:])
				io.WriteString(c.w, `)`)

			default:
				squoted(c.w, v)
			}

			io.WriteString(c.w, ` :: `)
			io.WriteString(c.w, col.Type)

		} else {
			quoted(c.w, col.Name)
		}

		if !renderedCol {
			renderedCol = true
		}
	}

	if len(skipcols) != 0 && renderedCol {
		io.WriteString(c.w, `, `)
	}
	return 0, nil
}

func (c *compilerContext) renderUpsert(
	w io.Writer, qc *qcode.QCode, vars Variables, ti *DBTableInfo) (uint32, error) {

	root := &qc.Selects[0]
	upsert, ok := vars[qc.ActionVar]
	if !ok {
		return 0, fmt.Errorf("variable '%s' not defined", qc.ActionVar)
	}
	if len(upsert) == 0 {
		return 0, fmt.Errorf("variable '%s' is empty", qc.ActionVar)
	}

	if ti.PrimaryCol == nil {
		return 0, fmt.Errorf("no primary key column found")
	}

	jt, _, err := jsn.Tree(upsert)
	if err != nil {
		return 0, err
	}

	if _, err := c.renderInsert(w, qc, vars, ti, true); err != nil {
		return 0, err
	}

	io.WriteString(c.w, ` ON CONFLICT (`)
	i := 0

	for _, cn := range ti.Columns {
		if _, ok := jt[cn.Key]; !ok {
			continue
		}
		if cn.Blocked {
			return 0, fmt.Errorf("upsert: column '%s' blocked", cn.Name)
		}
		if !cn.UniqueKey || !cn.PrimaryKey {
			continue
		}

		if i != 0 {
			io.WriteString(c.w, `, `)
		}
		io.WriteString(c.w, cn.Name)
		i++
	}
	if i == 0 {
		io.WriteString(c.w, ti.PrimaryCol.Name)
	}
	io.WriteString(c.w, `)`)

	io.WriteString(c.w, ` DO UPDATE SET `)

	i = 0
	for _, cn := range ti.Columns {
		if _, ok := jt[cn.Key]; !ok {
			continue
		}
		if cn.Blocked {
			return 0, fmt.Errorf("upsert: column '%s' blocked", cn.Name)
		}
		if i != 0 {
			io.WriteString(c.w, `, `)
		}
		io.WriteString(c.w, cn.Name)
		io.WriteString(c.w, ` = EXCLUDED.`)
		io.WriteString(c.w, cn.Name)
		i++
	}

	if root.Where != nil {
		io.WriteString(c.w, ` WHERE `)

		if err := c.renderWhere(root, ti); err != nil {
			return 0, err
		}
	}

	io.WriteString(c.w, ` RETURNING *) `)

	return 0, nil
}

func (c *compilerContext) renderConnectStmt(qc *qcode.QCode, w io.Writer,
	item renitem) error {

	rel := item.relPC

	if rel == nil {
		return errors.New("invalid connect value")
	}

	// Render only for parent-to-child relationship of one-to-one
	// For this to work the child needs to found first so it's primary key
	// can be set in the related column on the parent object.
	// Eg. Create product and connect a user to it.
	if rel.Type != RelOneToOne {
		return nil
	}

	io.WriteString(w, `, "_x_`)
	io.WriteString(c.w, item.ti.Name)
	io.WriteString(c.w, `" AS (SELECT `)

	if rel.Left.Array {
		io.WriteString(w, `array_agg(DISTINCT `)
		quoted(w, rel.Right.Col)
		io.WriteString(w, `) AS `)
		quoted(w, rel.Right.Col)

	} else {
		quoted(w, rel.Right.Col)

	}

	io.WriteString(c.w, ` FROM "_sg_input" i,`)
	quoted(c.w, item.ti.Name)

	io.WriteString(c.w, ` WHERE `)
	if err := renderWhereFromJSON(c.w, item.kvitem, "connect", item.kvitem.val); err != nil {
		return err
	}
	io.WriteString(c.w, ` LIMIT 1)`)

	return nil
}

func (c *compilerContext) renderDisconnectStmt(qc *qcode.QCode, w io.Writer,
	item renitem) error {

	rel := item.relPC

	// Render only for parent-to-child relationship of one-to-one
	// For this to work the child needs to found first so it's
	// null value can beset in the related column on the parent object.
	// Eg. Update product and diconnect the user from it.
	if rel.Type != RelOneToOne {
		return nil
	}
	io.WriteString(w, `, "_x_`)
	io.WriteString(c.w, item.ti.Name)
	io.WriteString(c.w, `" AS (`)

	if rel.Right.Array {
		io.WriteString(c.w, `SELECT `)
		quoted(w, rel.Right.Col)
		io.WriteString(c.w, ` FROM "_sg_input" i,`)
		quoted(c.w, item.ti.Name)
		io.WriteString(c.w, ` WHERE `)
		if err := renderWhereFromJSON(c.w, item.kvitem, "connect", item.kvitem.val); err != nil {
			return err
		}
		io.WriteString(c.w, ` LIMIT 1))`)

	} else {
		io.WriteString(c.w, `SELECT * FROM (VALUES(NULL::`)
		io.WriteString(w, rel.Right.col.Type)
		io.WriteString(c.w, `)) AS LOOKUP(`)
		quoted(w, rel.Right.Col)
		io.WriteString(c.w, `))`)
	}

	return nil
}

func renderWhereFromJSON(w io.Writer, item kvitem, key string, val []byte) error {
	var kv map[string]json.RawMessage
	ti := item.ti

	if err := json.Unmarshal(val, &kv); err != nil {
		return err
	}
	i := 0
	for k, v := range kv {
		col, err := ti.GetColumn(k)
		if err != nil {
			return err
		}
		if i != 0 {
			io.WriteString(w, ` AND `)
		}

		if v[0] == '[' {
			colWithTable(w, ti.Name, k)

			if col.Array {
				io.WriteString(w, ` && `)
			} else {
				io.WriteString(w, ` = `)
			}

			io.WriteString(w, `ANY((select a::`)
			io.WriteString(w, col.Type)

			io.WriteString(w, ` AS list from json_array_elements_text(`)
			renderPathJSON(w, item, key, k)
			io.WriteString(w, `::json) AS a))`)

		} else if col.Array {
			io.WriteString(w, `(`)
			renderPathJSON(w, item, key, k)
			io.WriteString(w, `)::`)
			io.WriteString(w, col.Type)

			io.WriteString(w, ` = ANY(`)
			colWithTable(w, ti.Name, k)
			io.WriteString(w, `)`)

		} else {
			colWithTable(w, ti.Name, k)

			io.WriteString(w, `= (`)
			renderPathJSON(w, item, key, k)
			io.WriteString(w, `)::`)
			io.WriteString(w, col.Type)
		}

		i++
	}
	return nil
}

func renderPathJSON(w io.Writer, item kvitem, key1, key2 string) {
	io.WriteString(w, `(i.j->`)
	joinPath(w, item.path)
	io.WriteString(w, `->'`)
	io.WriteString(w, key1)
	io.WriteString(w, `'->>'`)
	io.WriteString(w, key2)
	io.WriteString(w, `')`)
}

func renderCteName(w io.Writer, item kvitem) error {
	io.WriteString(w, `"`)
	io.WriteString(w, item.ti.Name)
	if item._type == itemConnect || item._type == itemDisconnect {
		io.WriteString(w, `_`)
		int32String(w, item.id)
	}
	io.WriteString(w, `"`)
	return nil
}

func renderCteNameWithSuffix(w io.Writer, item kvitem, suffix string) error {
	io.WriteString(w, `"`)
	io.WriteString(w, item.ti.Name)
	io.WriteString(w, `_`)
	io.WriteString(w, suffix)
	io.WriteString(w, `"`)
	return nil
}

func joinPath(w io.Writer, path []string) {
	for i := range path {
		if i != 0 {
			io.WriteString(w, `->`)
		}
		io.WriteString(w, `'`)
		io.WriteString(w, path[i])
		io.WriteString(w, `'`)
	}
}
