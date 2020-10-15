package qcode

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/dosco/super-graph/core/internal/graph"
	"github.com/dosco/super-graph/core/internal/sdata"
	"github.com/dosco/super-graph/core/internal/util"
	"github.com/dosco/super-graph/jsn"
)

type MType int8

const (
	MTInsert MType = iota + 1
	MTUpdate
	MTUpsert
	MTDelete
	MTConnect
	MTDisconnect
	MTNone
)

var insertTypes = map[string]MType{
	"connect": MTConnect,
}

var updateTypes = map[string]MType{
	"connect":    MTConnect,
	"disconnect": MTDisconnect,
}

type Mutate struct {
	ID    int32
	Type  MType
	CType int
	Key   string
	Path  []string
	Val   json.RawMessage
	Data  map[string]json.RawMessage
	Array bool

	Cols   []MColumn
	RCols  []MRColumn
	Tables []MTable
	Ti     sdata.DBTableInfo
	RelCP  sdata.DBRel
	RelPC  sdata.DBRel
	Items  []Mutate
	Multi  bool
	MID    int32
	render bool
}

type MColumn struct {
	Col       sdata.DBColumn
	FieldName string
	Value     string
}

type MRColumn struct {
	Col   sdata.DBColumn
	VCol  sdata.DBColumn
	CType int
}

type MTable struct {
	Ti    sdata.DBTableInfo
	CType int
}

func (co *Compiler) compileMutation(qc *QCode, op *graph.Operation, role string) error {
	var ok bool

	sel := &qc.Selects[0]
	m := Mutate{Key: sel.Table, Ti: sel.Ti}

	var whereReq bool

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

	tm := make(map[string]int32)
	st := util.NewStackInf()
	st.Push(m)

	for {
		if st.Len() == 0 {
			break
		}

		intf := st.Pop()

		if item, ok := intf.(Mutate); ok && item.render {
			item.MID = tm[item.Ti.Name]
			qc.Mutates = append(qc.Mutates, item)
			tm[item.Ti.Name]++

		} else if err := co.newMutate(st, item, role); err != nil {
			return err
		}
	}

	for i, v := range qc.Mutates {
		if c, ok := tm[v.Ti.Name]; ok && c > 1 {
			qc.Mutates[i].Multi = true
		}
	}

	qc.MCounts = tm

	return nil
}

// TODO: Handle cases where a column name matches the child table name
// the child path needs to be exluded in the json sent to insert or update

func (co *Compiler) newMutate(st *util.StackInf, m Mutate, role string) error {
	var err error
	tr := co.getRole(role, m.Key)

	if m.Data == nil {
		if m.Data, m.Array, err = jsn.Tree(m.Val); err != nil {
			return err
		}
	}

	id := m.ID + 1
	m.Items = make([]Mutate, 0, len(m.Data))
	m.render = true

	for k, v := range m.Data {
		if v[0] != '{' && v[0] != '[' {
			continue
		}

		// Get child-to-parent relationship
		relCP, err := co.s.GetRel(k, m.Key, "")
		if err != nil {
			var ty MType
			var ok bool

			switch m.Type {
			case MTInsert:
				ty, ok = insertTypes[k]
			case MTUpdate:
				ty, ok = updateTypes[k]
			}

			if ok {
				m1 := m
				m1.Type = ty
				m1.ID = id
				m1.Val = v

				m.Items = append(m.Items, m1)
				m.Type = MTNone
				m.render = false
				id++
			}

			// Get parent-to-child relationship
		} else if relPC, err := co.s.GetRel(m.Key, k, ""); err == nil {
			ti, err := co.s.GetTableInfo(k)
			if err != nil {
				return err
			}

			m1 := Mutate{
				ID:    id,
				Type:  m.Type,
				Key:   k,
				Val:   v,
				Path:  append(m.Path, k),
				Ti:    ti,
				RelCP: relCP,
				RelPC: relPC,
			}

			if v[0] == '{' {
				m1.Data, m1.Array, err = jsn.Tree(v)
				if err != nil {
					return err
				}
				if v1, ok := m1.Data["connect"]; ok && (v1[0] == '{' || v1[0] == '[') {
					m1.CType |= (1 << MTConnect)
				}
				if v1, ok := m1.Data["disconnect"]; ok && (v1[0] == '{' || v1[0] == '[') {
					m1.CType |= (1 << MTDisconnect)
				}
			}

			m.Items = append(m.Items, m1)
			id++
		}
	}

	// Add columns, relationship columns and tables needed.
	if m, err = addTablesAndColumns(m, tr); err != nil {
		return err
	}

	// For inserts order the children according to
	// the creation order required by the parent-to-child
	// relationships. For example users need to be created
	// before the products they own.

	// For updates the order defined in the query must be
	// the order used.
	switch m.Type {
	case MTInsert:
		for _, v := range m.Items {
			if v.RelPC.Type == sdata.RelOneToMany {
				st.Push(v)
			}
		}
		st.Push(m)
		for _, v := range m.Items {
			if v.RelPC.Type == sdata.RelOneToOne {
				st.Push(v)
			}
		}

	case MTUpdate:
		for _, v := range m.Items {
			if !(v.CType != 0 && v.RelPC.Type == sdata.RelOneToOne) {
				st.Push(v)
			}
		}
		st.Push(m)
		for _, v := range m.Items {
			if v.CType != 0 && v.RelPC.Type == sdata.RelOneToOne {
				st.Push(v)
			}
		}

	case MTUpsert:
		st.Push(m)

	case MTNone:
		for _, v := range m.Items {
			st.Push(v)
		}
	}

	return nil
}

func addTablesAndColumns(m Mutate, tr trval) (Mutate, error) {
	var err error
	cm := make(map[string]struct{})

	switch m.Type {
	case MTInsert:
		// Render columns and values needed to connect current table and the parent table
		if m.RelCP.Type == sdata.RelOneToOne {
			m.Tables = append(m.Tables, MTable{Ti: m.RelPC.Left.Ti})
			m.RCols = append(m.RCols, MRColumn{
				Col:  m.RelCP.Left.Col,
				VCol: m.RelCP.Right.Col,
			})
			cm[m.RelCP.Left.Col.Name] = struct{}{}
		}
		// Render columns and values needed to connect parent level with it's children
		// this is for when the parent actually depends on the child level
		// the order of the table rendering if handled upstream
		if len(m.Items) == 0 {
			// TODO: Commenting this out since I suspect this code path is not required
			// if m.RelPC.Type == sdata.RelOneToMany {
			// 	m.Tables = append(m.Tables, MTable{Ti: m.RelPC.Left.Ti})
			// 	m.RCols = append(m.RCols, MRColumn{
			// 		Col:  m.RelCP.Right.Col,
			// 		VCol: m.RelCP.Left.Col,
			// 	})
			// 	cm[m.RelCP.Right.Col.Name] = struct{}{}
			// }
		} else {
			// Render columns and values needed by the children of the current level
			// Render child foreign key columns if child-to-parent
			// relationship is one-to-many
			for _, v := range m.Items {
				if v.RelCP.Type == sdata.RelOneToMany {
					m.Tables = append(m.Tables, MTable{Ti: v.RelCP.Left.Ti, CType: v.CType})
					m.RCols = append(m.RCols, MRColumn{
						Col:   v.RelCP.Right.Col,
						VCol:  v.RelCP.Left.Col,
						CType: v.CType,
					})
					cm[v.RelCP.Right.Col.Name] = struct{}{}
				}
			}
		}

	case MTUpdate:
		// Render tables needed to set values if child-to-parent
		// relationship is one-to-many
		for _, v := range m.Items {
			if v.CType != 0 && v.RelCP.Type == sdata.RelOneToMany {
				m.Tables = append(m.Tables, MTable{Ti: v.RelCP.Left.Ti, CType: v.CType})
				m.RCols = append(m.RCols, MRColumn{
					Col:   v.RelCP.Right.Col,
					VCol:  v.RelCP.Left.Col,
					CType: v.CType,
				})
				cm[v.RelCP.Right.Col.Name] = struct{}{}
			}
		}
	}

	if m.Cols, err = getColumnsFromJSON(m, tr, cm); err != nil {
		return m, err
	}

	return m, nil
}

func getColumnsFromJSON(m Mutate, tr trval, cm map[string]struct{}) ([]MColumn, error) {
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
