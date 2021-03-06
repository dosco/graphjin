package qcode

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/dosco/graphjin/core/internal/graph"
	"github.com/dosco/graphjin/core/internal/sdata"
	"github.com/dosco/graphjin/core/internal/util"
	"github.com/dosco/graphjin/internal/jsn"
)

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
	ID        int32
	ParentID  int32
	DependsOn map[int32]struct{}
	Type      MType
	// CType     uint8
	Key      string
	Path     []string
	Val      json.RawMessage
	Data     map[string]json.RawMessage
	Array    bool
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
	Value     string
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

func (co *Compiler) compileMutation(qc *QCode, op *graph.Operation, role string) error {
	var err error
	var ok bool
	var whereReq bool

	sel := &qc.Selects[0]
	m := Mutate{ParentID: -1, Key: sel.Table, Ti: sel.Ti}

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

	if m.Val, ok = qc.Vars[qc.ActionVar]; !ok {
		return fmt.Errorf("variable not defined: %s", qc.ActionVar)
	}

	if m.Data, m.Array, err = jsn.Tree(m.Val); err != nil {
		return err
	}

	mutates := []Mutate{}
	mmap := map[int32]int32{-1: -1}
	mids := map[string][]int32{}

	st := util.NewStackInf()
	st.Push(m)
	ms := mState{st: st, qc: qc, mt: m.Type, id: (m.ID + 1)}

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

		if m1.Type == MTNone {
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

// TODO: Handle cases where a column name matches the child table name
// the child path needs to be exluded in the json sent to insert or update

func (co *Compiler) newMutate(ms *mState, m Mutate, role string) error {
	var m1 Mutate
	items := make([]Mutate, 0, len(m.Data))
	trv := co.getRole(role, m.Ti.Schema, m.Ti.Name, m.Key)

	for k, v := range m.Data {
		if v[0] != '{' && v[0] != '[' {
			continue
		}

		// Get child-to-parent relationship
		// rel, err := co.s.GetRel(k, m.Key, "")
		paths, err := co.s.FindPath(k, m.Key, "")
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
				m1 = Mutate{
					ID:       ms.id,
					ParentID: m.ParentID,
					Type:     ty,
					Key:      k,
					Val:      v,
					Path:     append(m.Path, k),
					Ti:       m.Ti,
					Rel:      m.Rel,
					render:   true,
				}
				m.Type = MTNone

			} else if ok && ty == MTKeyword {
				continue
			} else if _, err := m.Ti.GetColumn(k); err != nil {
				return err
			} else {
				// valid column so return
				continue
			}

		} else {
			rel := sdata.PathToRel(paths[0])
			ti := rel.Left.Ti

			m1 = Mutate{
				ID:       ms.id,
				ParentID: m.ID,
				Type:     m.Type,
				Key:      k,
				Val:      v,
				Path:     append(m.Path, k),
				Ti:       ti,
				Rel:      rel,
			}
		}

		if err = co.processDirectives(ms, &m1, trv); err != nil {
			return err
		}

		items = append(items, m1)
		m.children = append(m.children, m1.ID)
		ms.id++
	}

	err := co.addTablesAndColumns(&m, items, trv)
	if err != nil {
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

func (co *Compiler) processDirectives(ms *mState, m *Mutate, trv trval) error {
	var err error

	if m.Val[0] != '{' {
		return nil
	}

	m.Data, m.Array, err = jsn.Tree(m.Val)
	if err != nil {
		return err
	}

	var filterVal string

	if m.Type == MTConnect || m.Type == MTDisconnect {
		filterVal = string(m.Val)
	}

	if m.Type == MTUpdate && m.Rel.Type == sdata.RelOneToOne {
		_, connect := m.Data["connect"]
		_, disconnect := m.Data["disconnect"]

		if v, ok := m.Data["where"]; ok {
			filterVal = string(v)
		} else if !connect && !disconnect {
			return errors.New("missing argument: where")
		}
	}

	if filterVal != "" {
		m.Where.Exp, _, err = compileFilter(
			co.s,
			m.Ti,
			[]string{filterVal},
			true)

		if err != nil {
			return err
		}
		addFilters(ms.qc, &m.Where, trv)
	}

	if m.Rel.Type == sdata.RelRecursive {
		var find string

		if v1, ok := m.Data["find"]; !ok {
			if ms.mt == MTInsert {
				find = `"child"`
			} else {
				find = `"parent"`
			}
		} else {
			find = string(v1)
		}

		switch find {
		case `"child"`, `"children"`:
			m.Rel.Type = sdata.RelOneToOne

		case `"parent"`, `"parents"`:
			if ms.mt == MTInsert {
				return fmt.Errorf("a new '%s' cannot have a parent", m.Key)
			}
			m.Rel.Type = sdata.RelOneToMany
			m.Rel = flipRel(m.Rel)
		}
	}

	return nil
}

func (co *Compiler) addTablesAndColumns(m *Mutate, items []Mutate, trv trval) error {
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

	if m.Cols, err = getColumnsFromJSON(m, trv, cm); err != nil {
		return err
	}

	return nil
}

func getColumnsFromJSON(m *Mutate, trv trval, cm map[string]struct{}) ([]MColumn, error) {
	var cols []MColumn

	for k, v := range trv.getPresets(m.Type) {
		if _, ok := cm[k]; ok {
			continue
		}

		col, err := m.Ti.GetColumn(k)
		if err != nil {
			return nil, err
		}

		cols = append(cols, MColumn{Col: col, FieldName: k, Value: v})
		cm[k] = struct{}{}
	}

	for i, col := range m.Ti.Columns {
		k := col.Name

		if _, ok := cm[k]; ok {
			continue
		}

		if _, ok := m.Data[k]; !ok {
			continue
		}

		if m.Ti.Blocked {
			return nil, fmt.Errorf("column blocked: %s", k)
		}

		cols = append(cols, MColumn{Col: m.Ti.Columns[i], FieldName: k})
	}

	// TODO: This is faster than the above
	// but randomized maps in go make testing harder
	// put this back in once we have integration testing

	// for k, _ := range m.Data {
	// if _, ok := cm[k]; ok {
	// 	continue
	// }

	// 	col, err := m.Ti.GetColumn(k)
	// 	if err != nil {
	// 		return nil, err
	// 	}

	// 	cols = append(cols, MColumn{Col: col, FieldName: k})
	// }

	return cols, nil
}

func flipRel(rel sdata.DBRel) sdata.DBRel {
	rc := rel.Right.Col
	rel.Right.Col = rel.Left.Col
	rel.Left.Col = rc
	return rel
}
