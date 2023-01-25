package qcode

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/dosco/graphjin/core/v3/internal/graph"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
	"github.com/dosco/graphjin/core/v3/internal/util"
)

var errUserIDReq = errors.New("$user_id required for this query")

type MType uint8

const (
	MTInsert MType = iota + 1
	MTUpdate
	MTUpsert
	MTDelete
	MTConnect
	MTDisconnect
	MTNone
	MTKeyword
)

// const (
// 	CTConnect uint8 = 1 << iota
// 	CTDisconnect
// )

var insertTypes = map[string]MType{
	"connect": MTConnect,
	"find":    MTKeyword,
}

var updateTypes = map[string]MType{
	"where":      MTKeyword,
	"find":       MTKeyword,
	"connect":    MTConnect,
	"disconnect": MTDisconnect,
}

type Mutate struct {
	Field
	mData

	ID        int32
	ParentID  int32
	DependsOn map[int32]struct{}
	Type      MType
	// CType     uint8
	Key      string
	Path     []string
	Val      json.RawMessage
	Cols     []MColumn
	RCols    []MRColumn
	Ti       sdata.DBTable
	Rel      sdata.DBRel
	Where    Filter
	Multi    bool
	children []int32
	render   bool
}

type MColumn struct {
	Col       sdata.DBColumn
	FieldName string
	Alias     string
	Value     string
	Set       bool
}

type MRColumn struct {
	Col  sdata.DBColumn
	VCol sdata.DBColumn
}

type MTable struct {
	Ti sdata.DBTable
	// CType uint8
}

type mState struct {
	st *util.StackInf
	qc *QCode
	mt MType
	id int32
}

func (co *Compiler) compileMutation(qc *QCode,
	vmap map[string]json.RawMessage, role string,
) (err error) {
	if qc.ActionVar != "" {
		qc.ActionVal = vmap[qc.ActionVar]
	}

	var whereReq bool

	sel := &qc.Selects[0]
	m := Mutate{
		Field:    Field{Type: FieldTypeTable},
		ParentID: -1,
		Key:      sel.Table,
		Ti:       sel.Ti,
	}

	switch qc.SType {
	case QTInsert:
		m.Type = MTInsert
	case QTUpdate:
		m.Type = MTUpdate
		whereReq = true
	case QTUpsert:
		m.Type = MTUpsert
		whereReq = true
	case QTDelete:
		m.Type = MTDelete
		whereReq = true
	default:
		return errors.New("valid mutations: insert, update, upsert, delete'")
	}

	if whereReq && qc.Selects[0].Where.Exp == nil {
		return errors.New("where clause required")
	}

	if m.Type == MTDelete {
		m.render = true
		qc.Mutates = append(qc.Mutates, m)
		return nil
	}

	m.mData, err = parseMutationData(qc)
	if err != nil {
		return err
	}

	mutates := []Mutate{}
	mmap := map[int32]int32{-1: -1}
	mids := map[string][]int32{}

	st := util.NewStackInf()

	if m.Data.Type == graph.NodeList {
		for _, v := range co.processList(m) {
			st.Push(v)
		}
	} else {
		st.Push(m)
	}

	ms := mState{st: st, qc: qc, mt: m.Type, id: int32(st.Len() + 1)}

	for {
		if st.Len() == 0 {
			break
		}

		intf := st.Pop()
		item, ok := intf.(Mutate)

		if ok && item.render {
			id := int32(len(mutates))
			mmap[item.ID] = id
			mutates = append(mutates, item)
			continue
		}

		if err := co.newMutate(&ms, item, role); err != nil {
			return err
		}
	}

	for i := range mutates {
		m1 := &mutates[i]

		// Re-id all items to make array access easy.
		m1.ID = mmap[m1.ID]
		m1.ParentID = mmap[m1.ParentID]

		if m1.Type != MTNone {
			mids[m1.Ti.Name] = append(mids[m1.Ti.Name], m1.ID)
		}

		for id := range m1.DependsOn {
			delete(m1.DependsOn, id)
			m1.DependsOn[mmap[id]] = struct{}{}
		}

		for i, id := range m1.children {
			m1.children[i] = mmap[id]
		}
	}
	qc.MUnions = mids

	// Pull up children of MTNone to the depends-on of it's parent if applicable.
	for i := range mutates {
		m1 := &mutates[i]

		if len(mids[m1.Ti.Name]) > 1 {
			m1.Multi = true
		}

		if m1.Type == MTNone && m1.ParentID != -1 {
			p := &mutates[m1.ParentID]
			delete(p.DependsOn, m1.ID)

			for _, id := range m1.children {
				m2 := &mutates[id]
				if m2.Rel.Type == sdata.RelOneToMany {
					p.DependsOn[m2.ID] = struct{}{}
				}
			}
		}
	}
	qc.Mutates = mutates
	return nil
}

type mData struct {
	Data   *graph.Node
	IsJSON bool
	Array  bool
}

func parseDataValue(qc *QCode, actionVal *graph.Node, isJSON bool) (mData, error) {
	var md mData
	md.Data = actionVal

	// md, err := parseMutationData(qc, actionVal)
	// if err != nil {
	// 	return md, err
	// }

	if isJSON {
		md.IsJSON = isJSON
	}
	return md, nil
}

func parseMutationData(qc *QCode) (mData, error) {
	var md mData
	var err error

	av := qc.actionArg.Val
	switch av.Type {
	case graph.NodeVar:
		if len(qc.ActionVal) == 0 {
			return md, fmt.Errorf("variable not found: %s", av.Val)
		}
		md.Data, err = graph.ParseArgValue(string(qc.ActionVal), true)
		if err != nil {
			return md, err
		}
		md.IsJSON = true

	default:
		md.Data = av
	}
	return md, nil
}

// TODO: Handle cases where a column name matches the child table name
// the child path needs to be exluded in the json sent to insert or update

func (co *Compiler) newMutate(ms *mState, m Mutate, role string) error {
	trv := co.getRole(role, m.Ti.Schema, m.Ti.Name, m.Key)
	data := m.Data

	items, err := co.processNestedMutations(ms, &m, data, trv)
	if err != nil {
		return err
	}

	if err := co.addTablesAndColumns(&m, items, data, trv); err != nil {
		return err
	}

	m.render = true

	// For inserts order the children according to
	// the creation order required by the parent-to-child
	// relationships. For example users need to be created
	// before the products they own.

	// For updates the order defined in the query must be
	// the order used.
	switch m.Type {
	case MTInsert:
		for _, v := range items {
			if v.Rel.Type == sdata.RelOneToOne {
				ms.st.Push(v)
			}
		}
		ms.st.Push(m)
		for _, v := range items {
			if v.Rel.Type == sdata.RelOneToMany {
				ms.st.Push(v)
			}
		}

	case MTUpdate:
		for _, v := range items {
			ms.st.Push(v)
		}
		ms.st.Push(m)

	case MTUpsert:
		ms.st.Push(m)

	case MTNone:
		for _, v := range items {
			ms.st.Push(v)
		}
		ms.st.Push(m)
	}
	return nil
}

func (co *Compiler) processNestedMutations(ms *mState, m *Mutate, data *graph.Node, trv trval) ([]Mutate, error) {
	var ml []Mutate
	var md mData
	var err error

	items := make([]Mutate, 0, len(data.Children))

	for i := range data.Children {
		v := data.Children[i]

		if md, err = parseDataValue(ms.qc, v, m.IsJSON); err != nil {
			return nil, err
		}

		if md.Data.Type != graph.NodeObj && md.Data.Type != graph.NodeList {
			continue
		}

		k := co.ParseName(v.Name)

		// Get child-to-parent relationship
		paths, err := co.FindPath(k, m.Key, "")
		// no relationship found must be a keyword
		if err != nil {
			var ty MType
			var ok bool

			switch ms.mt {
			case MTInsert:
				ty, ok = insertTypes[k]
			case MTUpdate:
				ty, ok = updateTypes[k]
			}

			if ok && ty != MTKeyword {
				ml = []Mutate{{
					mData:    md,
					ID:       ms.id,
					ParentID: m.ParentID,
					Type:     ty,
					Key:      k,
					//	Val:      v,
					Path:   append(m.Path, k),
					Ti:     m.Ti,
					Rel:    m.Rel,
					render: true,
				}}
				m.Type = MTNone

			} else if ok && ty == MTKeyword {
				continue
			} else if _, err := m.Ti.GetColumn(k); err != nil {
				return nil, err
			} else {
				// valid column so return
				continue
			}

			// is a related to parent so we need to mutate the related table
		} else {
			rel := sdata.PathToRel(paths[0])
			ti := rel.Left.Ti

			if rel.Type != sdata.RelRecursive &&
				ms.id == 1 && ti.Name == ms.qc.Selects[0].Ti.Name {
				return nil, fmt.Errorf("remove json root '%s' from '%s' data", k, ms.qc.SType)
			}

			ml = []Mutate{{
				mData:    md,
				ID:       ms.id,
				ParentID: m.ID,
				Type:     m.Type,
				Key:      k,
				// Val:      v,
				Path: append(m.Path, k),
				Ti:   ti,
				Rel:  rel,
			}}
		}

		if err = co.processDirectives(ms, &ml[0], md.Data, trv); err != nil {
			return nil, err
		}

		if md.Data.Type == graph.NodeList {
			ml = co.processList(ml[0])
		}

		for _, v := range ml {
			items = append(items, v)
			m.children = append(m.children, v.ID)
			ms.id++
		}
	}

	return items, nil
}

func (co *Compiler) processList(m Mutate) []Mutate {
	if m.IsJSON {
		m.Array = m.Data.Type == graph.NodeList
		m.Data = m.Data.Children[0]
		return []Mutate{m}
	}

	var mList []Mutate
	for i := range m.Data.Children {
		m1 := m
		m1.Data = m.Data.Children[i]
		m1.Array = m1.Data.Type == graph.NodeList
		m1.ID += int32(i)
		mList = append(mList, m1)
	}
	return mList
}

func (co *Compiler) processDirectives(ms *mState, m *Mutate, data *graph.Node, trv trval) error {
	var filterNode *graph.Node
	var err error

	switch {
	case m.Type == MTConnect, m.Type == MTDisconnect:
		filterNode = data

	case m.Type == MTUpdate && m.Rel.Type == sdata.RelOneToOne:
		if v, ok := data.CMap["where"]; ok {
			filterNode = v
		} else {
			_, ok1 := data.CMap["connect"]
			_, ok2 := data.CMap["disconnect"]
			if !ok1 && ok2 {
				return errors.New("missing argument: where")
			}
		}
	}

	if filterNode != nil {
		st := util.NewStackInf()
		nu := false

		node := &graph.Node{
			Type:     filterNode.Type,
			Children: filterNode.Children,
			CMap:     filterNode.CMap,
		}

		if m.Where.Exp, nu, err = co.compileBaseExpNode(
			"",
			m.Ti,
			st,
			node,
			m.IsJSON); err != nil {
			return err
		}
		if nu && trv.role == "anon" {
			return errUserIDReq
		}
		if nu = addFilters(ms.qc, &m.Where, trv); nu && trv.role == "anon" {
			return errUserIDReq
		}
	}

	if m.Rel.Type == sdata.RelRecursive {
		var find string

		if v1, ok := data.CMap["find"]; !ok {
			if ms.mt == MTInsert {
				find = "child"
			} else {
				find = "parent"
			}
		} else {
			find = string(v1.Val)
		}

		switch find {
		case "child", "children":
			m.Rel.Type = sdata.RelOneToOne

		case "parent", "parents":
			if ms.mt == MTInsert {
				return fmt.Errorf("a new '%s' cannot have a parent", m.Key)
			}
			m.Rel.Type = sdata.RelOneToMany
			m.Rel = flipRel(m.Rel)
		}
	}

	return nil
}

func (co *Compiler) addTablesAndColumns(m *Mutate, items []Mutate, data *graph.Node, trv trval) error {
	var err error
	cm := make(map[string]struct{})

	if m.DependsOn == nil {
		m.DependsOn = make(map[int32]struct{})
	}

	switch m.Type {
	case MTInsert:
		// Render columns and values needed to connect current table and the parent table
		// TODO: check if needed
		if m.Rel.Type == sdata.RelOneToOne {
			m.DependsOn[m.ParentID] = struct{}{}
			m.RCols = append(m.RCols, MRColumn{
				Col:  m.Rel.Left.Col,
				VCol: m.Rel.Right.Col,
			})
			cm[m.Rel.Left.Col.Name] = struct{}{}
		}

		// Render columns and values needed by the children of the current level
		// Render child foreign key columns if child-to-parent
		// relationship is one-to-many
		for _, v := range items {
			if v.Rel.Type == sdata.RelOneToMany {
				m.DependsOn[v.ID] = struct{}{}
				m.RCols = append(m.RCols, MRColumn{
					Col:  v.Rel.Right.Col,
					VCol: v.Rel.Left.Col,
				})
				cm[v.Rel.Right.Col.Name] = struct{}{}
			}
		}

	case MTUpdate:
		if m.Rel.Type == sdata.RelOneToMany {
			m.DependsOn[m.ParentID] = struct{}{}
			m.RCols = append(m.RCols, MRColumn{
				Col:  m.Rel.Left.Col,
				VCol: m.Rel.Right.Col,
			})
			cm[m.Rel.Left.Col.Name] = struct{}{}
		}

		if m.Rel.Type == sdata.RelOneToOne {
			m.DependsOn[m.ParentID] = struct{}{}
		}

	default:
		if m.Rel.Type == sdata.RelOneToOne {
			m.DependsOn[m.ParentID] = struct{}{}
		}

		for i, v := range items {
			if v.Rel.Type == sdata.RelOneToOne {
				if v.DependsOn == nil {
					items[i].DependsOn = make(map[int32]struct{})
				}
				items[i].DependsOn[m.ParentID] = struct{}{}
			}
		}
	}

	if m.Cols, err = co.getColumnsFromData(m, data, trv, cm); err != nil {
		return err
	}

	return nil
}

func (co *Compiler) getColumnsFromData(m *Mutate, data *graph.Node, trv trval, cm map[string]struct{}) ([]MColumn, error) {
	var cols []MColumn

	for k, v := range trv.getPresets(m.Type) {
		k1 := k
		k := co.ParseName(k)

		if _, ok := cm[k]; ok {
			continue
		}

		col, err := m.Ti.GetColumn(k)
		if err != nil {
			return nil, err
		}

		cols = append(cols, MColumn{Col: col, FieldName: k1, Alias: k, Value: v, Set: true})
		cm[k] = struct{}{}
	}

	/*
		for i, col := range m.Ti.Columns {
			k := col.Name

			if _, ok := cm[k]; ok {
				continue
			}

			if _, ok := data.CMap[k]; !ok {
				continue
			}

			if col.Blocked {
				return nil, fmt.Errorf("column blocked: %s", k)
			}

			cols = append(cols, MColumn{Col: m.Ti.Columns[i], FieldName: k})
		}
	*/

	// TODO: This is faster than the above
	// but randomized maps in go make testing harder
	// put this back in once we have integration testing

	for k := range data.CMap {
		k1 := k
		k := co.ParseName(k)

		if _, ok := cm[k]; ok {
			continue
		}

		col, ok := m.Ti.ColumnExists(k)
		if !ok {
			continue
		}

		if col.Blocked {
			return nil, fmt.Errorf("column blocked: %s", k)
		}

		cols = append(cols, MColumn{Col: col, FieldName: k1, Alias: k})
	}

	return cols, nil
}

func flipRel(rel sdata.DBRel) sdata.DBRel {
	rc := rel.Right.Col
	rel.Right.Col = rel.Left.Col
	rel.Left.Col = rc
	return rel
}
