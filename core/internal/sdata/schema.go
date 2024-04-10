//go:generate stringer -type=RelType -output=./gen_string.go

package sdata

import (
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/util"
)

type edgeInfo struct {
	nodeID  int32
	edgeIDs []int32
}

type nodeInfo struct {
	nodeID int32
}

type DBSchema struct {
	dbType            string                  // db type
	version           int                     // db version
	schemaName        string                  // db schema
	name              string                  // db name
	tables            []DBTable               // tables
	virtualTable      map[string]VirtualTable // for polymorphic relationships
	dbFunctions       map[string]DBFunction   // db functions
	tindex            map[string]nodeInfo     // table index
	aliasIndex        map[string]nodeInfo     // table alias index
	edgesIndex        map[string][]edgeInfo   // edges index
	allEdges          map[int32]TEdge         // all edges
	relationshipGraph *util.Graph             // relationship graph
}

type RelType int

const (
	RelNone RelType = iota
	RelOneToOne
	RelOneToMany
	RelPolymorphic
	RelRecursive
	RelEmbedded
	RelRemote
	RelSkip
)

type DBRelLeft struct {
	Ti  DBTable
	Col DBColumn
}

type DBRelRight struct {
	VTable string
	Ti     DBTable
	Col    DBColumn
}

type DBRel struct {
	Type  RelType
	Left  DBRelLeft
	Right DBRelRight
}

func NewDBSchemas(
	info *DBInfo,
	aliases map[string][]string,
) ([]*DBSchema, error) {
	var schemas []*DBSchema

	for _, schemaName := range info.Schemas {
		// TODO: Do I create a new DBSchema per schema or do I update this to work for multiple schemas?
		schema := &DBSchema{
			dbType:            info.Type,
			version:           info.Version,
			schemaName:        schemaName,
			name:              info.Name,
			virtualTable:      make(map[string]VirtualTable),
			dbFunctions:       make(map[string]DBFunction),
			tindex:            make(map[string]nodeInfo),
			aliasIndex:        make(map[string]nodeInfo),
			edgesIndex:        make(map[string][]edgeInfo),
			allEdges:          make(map[int32]TEdge),
			relationshipGraph: util.NewGraph(),
		}

		for _, t := range info.Tables {
			nid := schema.addNode(t)
			schema.addAliases(schema.tables[nid], nid, aliases[t.Name])
		}

		for _, t := range info.VTables {
			if err := schema.addVirtual(t); err != nil {
				return nil, err
			}
		}

		for _, t := range schema.tables {
			err := schema.addRels(t)
			if err != nil {
				return nil, err
			}
		}

		// add aliases to edge index by duplicating
		for t, al := range aliases {
			for _, alias := range al {
				if _, ok := schema.edgesIndex[alias]; ok {
					continue
				}
				if e, ok := schema.edgesIndex[t]; ok {
					schema.edgesIndex[alias] = e
				}
			}
		}

		// add some standard common functions into the schema
		for _, v := range funcList {
			info.Functions = append(info.Functions, DBFunction{
				Name:    v.name,
				Comment: v.desc,
				Type:    v.ftype,
				Agg:     true,
				Inputs:  []DBFuncParam{{ID: 0}},
			})
		}

		// add functions into the schema
		for k, f := range info.Functions {
			// don't include functions that return records
			// as those are considered selector functions
			if f.Type != "record" {
				schema.dbFunctions[f.Name] = info.Functions[k]
			}
		}

	}

	return schemas, nil
}

func (s *DBSchema) addRels(t DBTable) error {
	var err error
	switch t.Type {
	case "json", "jsonb":
		err = s.addJsonRel(t)
	case "virtual":
		err = s.addPolymorphicRel(t)
	case "remote":
		err = s.addRemoteRel(t)
	}

	if err != nil {
		return err
	}

	return s.addColumnRels(t)
}

func (s *DBSchema) addJsonRel(t DBTable) error {
	st, err := s.Find(t.SecondaryCol.Schema, t.SecondaryCol.Table)
	if err != nil {
		return err
	}

	sc, err := st.GetColumn(t.SecondaryCol.Name)
	if err != nil {
		return err
	}

	return s.addToGraph(t, t.PrimaryCol, st, sc, RelEmbedded)
}

func (s *DBSchema) addPolymorphicRel(t DBTable) error {
	pt, err := s.Find(t.PrimaryCol.FKeySchema, t.PrimaryCol.FKeyTable)
	if err != nil {
		return err
	}

	// pc, err := pt.GetColumn(t.PrimaryCol.FKeyCol)
	// if err != nil {
	// 	return err
	// }

	pc, err := pt.GetColumn(t.SecondaryCol.Name)
	if err != nil {
		return err
	}

	return s.addToGraph(t, t.PrimaryCol, pt, pc, RelPolymorphic)
}

func (s *DBSchema) addRemoteRel(t DBTable) error {
	pt, err := s.Find(t.PrimaryCol.FKeySchema, t.PrimaryCol.FKeyTable)
	if err != nil {
		return err
	}

	pc, err := pt.GetColumn(t.PrimaryCol.FKeyCol)
	if err != nil {
		return err
	}

	return s.addToGraph(t, t.PrimaryCol, pt, pc, RelRemote)
}

func (s *DBSchema) addColumnRels(t DBTable) error {
	var err error

	for _, c := range t.Columns {
		if c.FKeyTable == "" {
			continue
		}

		if c.FKeySchema == "" {
			c.FKeySchema = t.Schema
		}

		v, ok := s.tindex[(c.FKeySchema + ":" + c.FKeyTable)]
		if !ok {
			return fmt.Errorf("foreign key table not found: %s.%s", c.FKeySchema, c.FKeyTable)
		}
		ft := s.tables[v.nodeID]

		if c.FKeyCol == "" {
			continue
		}

		fc, ok := ft.getColumn(c.FKeyCol)
		if !ok {
			return fmt.Errorf("foreign key column not found: %s.%s", c.FKeyTable, c.FKeyCol)
		}

		var rt RelType

		switch {
		case c.FKRecursive: // t.Name == c.FKeyTable:
			rt = RelRecursive
		case fc.UniqueKey:
			rt = RelOneToOne
		default:
			rt = RelOneToMany
		}

		if err = s.addToGraph(t, c, ft, fc, rt); err != nil {
			return err
		}
	}
	return nil
}

func (s *DBSchema) addVirtual(vt VirtualTable) error {
	s.virtualTable[vt.Name] = vt

	for _, t := range s.tables {
		idCol, ok := t.getColumn(vt.IDColumn)
		if !ok {
			continue
		}

		typeCol, ok := t.getColumn(vt.TypeColumn)
		if !ok {
			continue
		}

		isRecursive := (typeCol.Schema == t.Schema &&
			typeCol.Table == t.Name)

		col1 := DBColumn{
			ID:          -1,
			Schema:      t.Schema,
			Table:       t.Name,
			Name:        idCol.Name,
			Type:        idCol.Type,
			FKeySchema:  typeCol.Schema,
			FKeyTable:   typeCol.Table,
			FKeyCol:     typeCol.Name,
			FKRecursive: isRecursive,
		}

		fIDCol, ok := t.getColumn(vt.FKeyColumn)
		if !ok {
			continue
		}

		col2 := DBColumn{
			ID:     -1,
			Schema: t.Schema,
			Table:  t.Name,
			Name:   fIDCol.Name,
		}

		pt := DBTable{
			Name:         vt.Name,
			Schema:       t.Schema,
			Type:         "virtual",
			PrimaryCol:   col1,
			SecondaryCol: col2,
		}
		s.addNode(pt)
	}

	return nil
}

func (s *DBSchema) GetTables() []DBTable {
	return s.tables
}

type RelNode struct {
	Name  string
	Type  RelType
	Table DBTable
}

func (s *DBSchema) GetFirstDegree(t DBTable) (items []RelNode, err error) {
	currNode, ok := s.tindex[(t.Schema + ":" + t.Name)]
	if !ok {
		return nil, fmt.Errorf("table not found: %s", t.String())
	}
	relatedNodes := s.relationshipGraph.Connections(currNode.nodeID)
	for _, id := range relatedNodes {
		v := s.getRelNodes(id, currNode.nodeID)
		items = append(items, v...)
	}
	return
}

func (s *DBSchema) GetSecondDegree(t DBTable) (items []RelNode, err error) {
	currNode, ok := s.tindex[(t.Schema + ":" + t.Name)]
	if !ok {
		return nil, fmt.Errorf("table not found: %s", t.String())
	}

	relatedNodes1 := s.relationshipGraph.Connections(currNode.nodeID)
	for _, id := range relatedNodes1 {
		relatedNodes2 := s.relationshipGraph.Connections(id)
		for _, id1 := range relatedNodes2 {
			v := s.getRelNodes(id1, id)
			items = append(items, v...)
		}
	}
	return
}

func (s *DBSchema) getRelNodes(fromID, toID int32) (items []RelNode) {
	edges := s.relationshipGraph.GetEdges(fromID, toID)
	for _, e := range edges {
		e1 := s.allEdges[e.ID]
		if e1.name == "" {
			continue
		}
		item := RelNode{Name: e1.name, Type: e1.Type, Table: e1.LT}
		items = append(items, item)
	}
	return
}

func (ti *DBTable) getColumn(name string) (DBColumn, bool) {
	var c DBColumn
	if i, ok := ti.colMap[name]; ok {
		return ti.Columns[i], true
	}
	return c, false
}

func (ti *DBTable) GetColumn(name string) (DBColumn, error) {
	c, ok := ti.getColumn(name)
	if ok {
		return c, nil
	}
	return c, fmt.Errorf("column: '%s.%s' not found", ti.Name, name)
}

func (ti *DBTable) ColumnExists(name string) (DBColumn, bool) {
	return ti.getColumn(name)
}

func (s *DBSchema) GetFunctions() map[string]DBFunction {
	return s.dbFunctions
}

func GetRelName(colName string) string {
	cn := colName

	if strings.HasSuffix(cn, "_id") {
		return colName[:len(colName)-3]
	}

	if strings.HasSuffix(cn, "_ids") {
		return colName[:len(colName)-4]
	}

	if strings.HasPrefix(cn, "id_") {
		return colName[3:]
	}

	if strings.HasPrefix(cn, "ids_") {
		return colName[4:]
	}

	return cn
}

func (s *DBSchema) DBType() string {
	return s.dbType
}

func (s *DBSchema) DBVersion() int {
	return s.version
}

func (s *DBSchema) DBSchema() string {
	return s.schemaName
}

func (s *DBSchema) DBName() string {
	return s.name
}
