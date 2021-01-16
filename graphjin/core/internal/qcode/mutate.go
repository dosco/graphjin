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

const (
	CTConnect uint8 = 1 << iota
	CTDisconnect
)

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
	ID       int32
	ParentID int32
	//Children  []int32
	DependsOn map[int32]struct{}
	Type      MType
	CType     uint8
	Key       string
	Path      []string
	Val       json.RawMessage
	Data      map[string]json.RawMessage
	Array     bool
	Cols      []MColumn
	RCols     []MRColumn
	Ti        sdata.DBTableInfo
	Rel       sdata.DBRel
	Multi     bool
	items     []Mutate
	render    bool
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
	Ti    sdata.DBTableInfo
	CType uint8
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

			if item.Type != MTNone {
				mids[item.Ti.Name] = append(mids[item.Ti.Name], id)
			}
			continue
		}
		if err := co.newMutate(item, m.Type, st); err != nil {
			return err
		}
	}
	qc.MUnions = mids

	for i := range mutates {
		m1 := &mutates[i]
		err := co.addTablesAndColumns(m1, co.getRole(role, m1.Key))
		if err != nil {
			return err
		}

		// Re-id all items to make array access easy.
		m1.ID = mmap[m1.ID]
		m1.ParentID = mmap[m1.ParentID]

		for id := range m1.DependsOn {
			delete(m1.DependsOn, id)
			nid := mmap[id]
			if mutates[nid].Type != MTNone {
				m1.DependsOn[nid] = struct{}{}
			}
		}

		if len(mids[m1.Ti.Name]) > 1 {
			m1.Multi = true
		}

		if m1.Type == MTNone {
			for n := range m1.items {
				m2 := &m1.items[n]
				m2.ID = mmap[m2.ID]
				m2.ParentID = mmap[m2.ParentID]
			}
		}
	}

	for i := range mutates {
		m1 := &mutates[i]
		if m1.Type != MTNone {
			continue
		}

		for n := range m1.items {
			m2 := &m1.items[n]
			if m2.Rel.Type == sdata.RelOneToMany {
				p := &mutates[m1.ParentID]
				p.DependsOn[m2.ID] = struct{}{}
			}
		}
	}
	qc.Mutates = mutates

	return nil
}

// TODO: Handle cases where a column name matches the child table name
// the child path needs to be exluded in the json sent to insert or update

func (co *Compiler) newMutate(m Mutate, mt MType, st *util.StackInf) error {
	var m1 Mutate

	m.items = make([]Mutate, 0, len(m.Data))
	m.render = true
	id := m.ID

	for k, v := range m.Data {
		if v[0] != '{' && v[0] != '[' {
			continue
		}

		// Get child-to-parent relationship
		rel, err := co.s.GetRel(k, m.Key, "")
		if err != nil {
			var ty MType
			var ok bool

			switch mt {
			case MTInsert:
				ty, ok = insertTypes[k]
			case MTUpdate:
				ty, ok = updateTypes[k]
			}

			if ok && ty != MTKeyword {
				id++

				m1 = Mutate{
					ID:       id,
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

			} else if _, err := m.Ti.GetColumn(k); err != nil {
				return err
			} else {
				continue
			}

			// Get parent-to-child relationship
		} else {
			ti, err := co.s.GetTableInfo(k, m.Key)
			if err != nil {
				return err
			}

			id++
			m1 = Mutate{
				ID:       id,
				ParentID: m.ID,
				Type:     m.Type,
				Key:      k,
				Val:      v,
				Path:     append(m.Path, k),
				Ti:       ti,
				Rel:      rel,
			}
		}

		if err = processDirectives(&m1); err != nil {
			return err
		}
		m.items = append(m.items, m1)
	}

	// For inserts order the children according to
	// the creation order required by the parent-to-child
	// relationships. For example users need to be created
	// before the products they own.

	// For updates the order defined in the query must be
	// the order used.

	switch m.Type {
	case MTInsert:
		for _, v := range m.items {
			if v.Rel.Type == sdata.RelOneToOne {
				st.Push(v)
			}
		}
		st.Push(m)
		for _, v := range m.items {
			if v.Rel.Type == sdata.RelOneToMany {
				st.Push(v)
			}
		}

	case MTUpdate:
		for _, v := range m.items {
			st.Push(v)
		}
		st.Push(m)

	case MTUpsert:
		st.Push(m)

	case MTNone:
		for _, v := range m.items {
			st.Push(v)
		}
		st.Push(m)
	}

	return nil
}

func processDirectives(m *Mutate) error {
	var err error

	if m.Val[0] != '{' {
		return nil
	}

	m.Data, m.Array, err = jsn.Tree(m.Val)
	if err != nil {
		return err
	}

	if v1, ok := m.Data["connect"]; ok && (v1[0] == '{' || v1[0] == '[') {
		m.CType |= CTConnect
	}
	if v1, ok := m.Data["disconnect"]; ok && (v1[0] == '{' || v1[0] == '[') {
		m.CType |= CTDisconnect
	}

	if m.Rel.Type == sdata.RelRecursive {
		var find string

		if v1, ok := m.Data["find"]; !ok {
			find = `"child"`
		} else {
			find = string(v1)
		}

		switch find {
		case `"child"`, `"children"`:
			m.Rel.Type = sdata.RelOneToOne

		case `"parent"`, `"parents"`:
			m.Rel.Type = sdata.RelOneToMany
			m.Rel = flipRel(m.Rel)

		}
	}

	return nil
}

func (co *Compiler) addTablesAndColumns(m *Mutate, tr trval) error {
	var err error

	m.DependsOn = make(map[int32]struct{})
	cm := make(map[string]struct{})

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
		for _, v := range m.items {
			if v.Rel.Type == sdata.RelOneToMany {
				if v.Type != MTNone {
					m.DependsOn[v.ID] = struct{}{}
				}
				m.RCols = append(m.RCols, MRColumn{
					Col:  v.Rel.Right.Col,
					VCol: v.Rel.Left.Col,
				})
				cm[v.Rel.Right.Col.Name] = struct{}{}
			}
		}

	case MTUpdate:
		if m.Rel.Type == sdata.RelOneToOne {
			m.DependsOn[m.ParentID] = struct{}{}
			m.RCols = append(m.RCols, MRColumn{
				Col:  m.Rel.Left.Col,
				VCol: m.Rel.Right.Col,
			})
			cm[m.Rel.Left.Col.Name] = struct{}{}
		}

	default:
		if m.Type != MTNone && m.Rel.Type == sdata.RelOneToOne {
			m.DependsOn[m.ParentID] = struct{}{}
		}
	}
	if m.Cols, err = getColumnsFromJSON(m, tr, cm); err != nil {
		return err
	}

	return nil
}

func getColumnsFromJSON(m *Mutate, tr trval, cm map[string]struct{}) ([]MColumn, error) {
	var cols []MColumn

	for k, v := range tr.getPresets(m.Type) {
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
