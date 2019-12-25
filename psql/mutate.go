//nolint:errcheck
package psql

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/dosco/super-graph/jsn"
	"github.com/dosco/super-graph/qcode"
	"github.com/dosco/super-graph/util"
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
	"connect":  itemConnect,
	"_connect": itemConnect,
}

var updateTypes = map[string]itemType{
	"connect":     itemConnect,
	"_connect":    itemConnect,
	"disconnect":  itemDisconnect,
	"_disconnect": itemDisconnect,
}

var noLimit = qcode.Paging{NoLimit: true}

func (co *Compiler) compileMutation(qc *qcode.QCode, w io.Writer, vars Variables) (uint32, error) {
	if len(qc.Selects) == 0 {
		return 0, errors.New("empty query")
	}

	c := &compilerContext{w, qc.Selects, co}
	root := &qc.Selects[0]

	ti, err := c.schema.GetTable(root.Name)
	if err != nil {
		return 0, err
	}

	switch qc.Type {
	case qcode.QTInsert:
		if _, err := c.renderInsert(qc, w, vars, ti); err != nil {
			return 0, err
		}

	case qcode.QTUpdate:
		if _, err := c.renderUpdate(qc, w, vars, ti); err != nil {
			return 0, err
		}

	case qcode.QTUpsert:
		if _, err := c.renderUpsert(qc, w, vars, ti); err != nil {
			return 0, err
		}

	case qcode.QTDelete:
		if _, err := c.renderDelete(qc, w, vars, ti); err != nil {
			return 0, err
		}

	default:
		return 0, errors.New("valid mutations are 'insert', 'update', 'upsert' and 'delete'")
	}

	root.Paging = noLimit
	root.DistinctOn = root.DistinctOn[:]
	root.OrderBy = root.OrderBy[:]
	root.Where = nil
	root.Args = nil

	return c.compileQuery(qc, w)
}

type kvitem struct {
	id    int32
	_type itemType
	key   string
	path  []string
	val   json.RawMessage
	ti    *DBTableInfo
	relCP *DBRel
	relPC *DBRel
	items []kvitem
}

type renitem struct {
	kvitem
	array bool
	data  map[string]json.RawMessage
}

func (c *compilerContext) handleKVItem(st *util.Stack, item kvitem) error {
	data, array, err := jsn.Tree(item.val)
	if err != nil {
		return err
	}

	var unionize bool
	id := item.id + 1

	item.items = make([]kvitem, 0, len(data))

	for k, v := range data {
		if v[0] != '{' && v[0] != '[' {
			continue
		}
		if _, ok := item.ti.ColMap[k]; ok {
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

		} else {
			ti, err := c.schema.GetTable(k)
			if err != nil {
				return err
			}
			// Get parent-to-child relationship
			relPC, err := c.schema.GetRel(item.key, k)
			if err != nil {
				return err
			}

			item.items = append(item.items, kvitem{
				id:    id,
				_type: item._type,
				key:   k,
				val:   v,
				path:  append(item.path, k),
				ti:    ti,
				relCP: relCP,
				relPC: relPC,
			})
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

func renderInsertUpdateColumns(w io.Writer,
	qc *qcode.QCode,
	jt map[string]json.RawMessage,
	ti *DBTableInfo,
	skipcols map[string]struct{},
	values bool) (uint32, error) {

	root := &qc.Selects[0]

	n := 0
	for _, cn := range ti.Columns {
		if _, ok := skipcols[cn.Name]; ok {
			continue
		}
		if _, ok := jt[cn.Key]; !ok {
			continue
		}
		if _, ok := root.PresetMap[cn.Key]; ok {
			continue
		}
		if len(root.Allowed) != 0 {
			if _, ok := root.Allowed[cn.Key]; !ok {
				continue
			}
		}
		if n != 0 {
			io.WriteString(w, `, `)
		}

		if values {
			colWithTable(w, "t", cn.Name)
		} else {
			quoted(w, cn.Name)
		}
		n++
	}

	for i := range root.PresetList {
		cn := root.PresetList[i]
		col, ok := ti.ColMap[cn]
		if !ok {
			continue
		}
		if _, ok := skipcols[col.Name]; ok {
			continue
		}
		if i != 0 || n != 0 {
			io.WriteString(w, `, `)
		}

		if values {
			io.WriteString(w, `'`)
			io.WriteString(w, root.PresetMap[cn])
			io.WriteString(w, `' :: `)
			io.WriteString(w, col.Type)

		} else {
			quoted(w, cn)
		}
	}
	return 0, nil
}

func (c *compilerContext) renderUpsert(qc *qcode.QCode, w io.Writer,
	vars Variables, ti *DBTableInfo) (uint32, error) {
	root := &qc.Selects[0]

	upsert, ok := vars[qc.ActionVar]
	if !ok {
		return 0, fmt.Errorf("Variable '%s' not defined", qc.ActionVar)
	}

	if ti.PrimaryCol == nil {
		return 0, fmt.Errorf("no primary key column found")
	}

	jt, _, err := jsn.Tree(upsert)
	if err != nil {
		return 0, err
	}

	if _, err := c.renderInsert(qc, w, vars, ti); err != nil {
		return 0, err
	}

	io.WriteString(c.w, ` ON CONFLICT (`)
	i := 0

	for _, cn := range ti.Columns {
		if _, ok := jt[cn.Key]; !ok {
			continue
		}

		if col, ok := ti.ColMap[cn.Key]; !ok || !(col.UniqueKey || col.PrimaryKey) {
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

	if root.Where != nil {
		io.WriteString(c.w, ` WHERE `)

		if err := c.renderWhere(root, ti); err != nil {
			return 0, err
		}
	}

	io.WriteString(c.w, ` DO UPDATE SET `)

	i = 0
	for _, cn := range ti.Columns {
		if _, ok := jt[cn.Key]; !ok {
			continue
		}
		if i != 0 {
			io.WriteString(c.w, `, `)
		}
		io.WriteString(c.w, cn.Name)
		io.WriteString(c.w, ` = EXCLUDED.`)
		io.WriteString(c.w, cn.Name)
		i++
	}

	io.WriteString(c.w, ` RETURNING *) `)

	return 0, nil
}

func quoted(w io.Writer, identifier string) {
	io.WriteString(w, `"`)
	io.WriteString(w, identifier)
	io.WriteString(w, `"`)
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

func (c *compilerContext) renderConnectStmt(qc *qcode.QCode, w io.Writer,
	item renitem) error {

	rel := item.relPC

	renderCteName(c.w, item.kvitem)
	io.WriteString(c.w, ` AS (`)

	// Render either select or update sql based on parent-to-child
	// relationship
	switch rel.Type {
	case RelOneToOne:
		io.WriteString(c.w, `SELECT * FROM `)
		quoted(c.w, item.ti.Name)
		io.WriteString(c.w, ` WHERE `)
		if err := renderKVItemWhere(c.w, item.kvitem); err != nil {
			return err
		}
		io.WriteString(c.w, ` LIMIT 1`)

	case RelOneToMany:
		// UPDATE films SET kind = 'Dramatic' WHERE kind = 'Drama';
		io.WriteString(c.w, `UPDATE `)
		quoted(c.w, item.ti.Name)
		io.WriteString(c.w, ` SET `)
		quoted(c.w, rel.Right.Col)
		io.WriteString(c.w, ` = `)
		colWithTable(c.w, rel.Left.Table, rel.Left.Col)
		io.WriteString(c.w, ` WHERE `)
		if err := renderKVItemWhere(c.w, item.kvitem); err != nil {
			return err
		}

	default:
		return fmt.Errorf("unsuppported relationship %s", rel)
	}

	io.WriteString(c.w, ` RETURNING *)`)

	return nil

}

func (c *compilerContext) renderDisconnectStmt(qc *qcode.QCode, w io.Writer,
	item renitem) error {

	renderCteName(c.w, item.kvitem)
	io.WriteString(c.w, ` AS (`)

	io.WriteString(c.w, `UPDATE `)
	quoted(c.w, item.ti.Name)
	io.WriteString(c.w, ` SET `)
	quoted(c.w, item.relPC.Right.Col)
	io.WriteString(c.w, ` = NULL `)
	io.WriteString(c.w, ` WHERE `)

	// Render either select or update sql based on parent-to-child
	// relationship
	switch item.relPC.Type {
	case RelOneToOne:
		if err := renderRelEquals(c.w, item.relPC); err != nil {
			return err
		}

	case RelOneToMany:
		if err := renderRelEquals(c.w, item.relPC); err != nil {
			return err
		}

		io.WriteString(c.w, ` AND `)

		if err := renderKVItemWhere(c.w, item.kvitem); err != nil {
			return err
		}

	default:
		return fmt.Errorf("unsuppported relationship %s", item.relPC)
	}

	io.WriteString(c.w, ` RETURNING *)`)

	return nil
}

func renderKVItemWhere(w io.Writer, item kvitem) error {
	return renderWhereFromJSON(w, item.val)
}

func renderWhereFromJSON(w io.Writer, val []byte) error {
	var kv map[string]json.RawMessage
	if err := json.Unmarshal(val, &kv); err != nil {
		return err
	}
	i := 0
	for k, v := range kv {
		if i != 0 {
			io.WriteString(w, ` AND `)
		}
		quoted(w, k)
		io.WriteString(w, ` = '`)
		switch v[0] {
		case '"':
			w.Write(v[1 : len(v)-1])
		default:
			w.Write(v)
		}
		io.WriteString(w, `'`)
		i++
	}
	return nil
}

func renderRelEquals(w io.Writer, rel *DBRel) error {
	switch rel.Type {
	case RelOneToOne:
		colWithTable(w, rel.Left.Table, rel.Left.Col)
		io.WriteString(w, ` = `)
		colWithTable(w, rel.Right.Table, rel.Right.Col)

	case RelOneToMany:
		colWithTable(w, rel.Right.Table, rel.Right.Col)
		io.WriteString(w, ` = `)
		colWithTable(w, rel.Left.Table, rel.Left.Col)
	}

	return nil
}

func renderCteName(w io.Writer, item kvitem) error {
	io.WriteString(w, `"`)
	io.WriteString(w, item.ti.Name)
	if item._type == itemConnect || item._type == itemDisconnect {
		io.WriteString(w, `_`)
		int2string(w, item.id)
	}
	io.WriteString(w, `"`)
	return nil
}
