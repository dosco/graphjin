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

// CrossDBRel represents a cross-database foreign key relationship.
// These are stored separately from the graph because they connect tables
// across different databases — the target table doesn't exist in this schema's
// graph. Resolution happens at runtime via database_join.go.
type CrossDBRel struct {
	SourceTable  DBTable  // local table containing the FK column
	SourceCol    DBColumn // local FK column
	TargetDB     string   // remote database name
	TargetSchema string   // remote schema
	TargetTable  string   // remote table name
	TargetCol    string   // remote column name
	IsOneToOne   bool     // true if target col is PK/unique
}

type DBSchema struct {
	dbType            string                  // db type
	version           int                     // db version
	schema            string                  // db schema
	name              string                  // db name
	tables            []DBTable               // tables
	virtualTables     map[string]VirtualTable // for polymorphic relationships
	dbFunctions       map[string]DBFunction   // db functions
	tindex            map[string]nodeInfo     // table index (schema:name → node)
	nameIndex         map[string][]int32      // name-only index (name → nodeIDs) for cross-schema fallback
	tableAliasIndex   map[string]nodeInfo     // table alias index
	edgesIndex        map[string][]edgeInfo   // edges index
	allEdges          map[int32]TEdge         // all edges
	relationshipGraph *util.Graph             // relationship graph
	crossDBRels       []CrossDBRel            // cross-database FK relationships
	compositeFKs      []CompositeFKInfo       // composite FK metadata (Postgres only)
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
	// RelDatabaseJoin represents a cross-database relationship (multi-database support).
	// Similar to RelRemote but for in-process database joins rather than HTTP calls.
	RelDatabaseJoin
)

// DBRelLeft represents database information
type DBRelLeft struct {
	Ti  DBTable
	Col DBColumn
}

// DBRelRight represents a database relationship
type DBRelRight struct {
	VTable string
	Ti     DBTable
	Col    DBColumn
}

// DBRel represents a database relationship
type DBRel struct {
	Type       RelType
	Left       DBRelLeft
	Right      DBRelRight
	ExtraPairs []ColPair // Additional column pairs for composite FKs
}

// IsCrossDatabase returns true if this relationship crosses database boundaries.
// This is used to determine if a join needs to be executed as a database join
// rather than a SQL join.
func (r *DBRel) IsCrossDatabase() bool {
	leftDB := r.Left.Ti.Database
	rightDB := r.Right.Ti.Database

	// If either is empty, they're in the default/same database
	if leftDB == "" || rightDB == "" {
		return false
	}

	return leftDB != rightDB
}

// NewDBSchema creates a new database schema
func NewDBSchema(
	info *DBInfo,
	aliases map[string][]string,
) (*DBSchema, error) {
	schema := &DBSchema{
		dbType:            info.Type,
		version:           info.Version,
		schema:            info.Schema,
		name:              info.Name,
		virtualTables:     make(map[string]VirtualTable),
		dbFunctions:       make(map[string]DBFunction),
		tindex:            make(map[string]nodeInfo),
		nameIndex:         make(map[string][]int32),
		tableAliasIndex:   make(map[string]nodeInfo),
		edgesIndex:        make(map[string][]edgeInfo),
		allEdges:          make(map[int32]TEdge),
		relationshipGraph: util.NewGraph(),
		compositeFKs:      info.CompositeFKs,
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

	return schema, nil
}

// addRels adds relationships to the schema
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

// addJsonRel adds a json relationship to the schema
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

// addPolymorphicRel adds a polymorphic relationship to the schema
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

// addRemoteRel adds a remote relationship to the schema
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

// addColumnRels adds column relationships to the schema
func (s *DBSchema) addColumnRels(t DBTable) error {
	var err error

	// Build lookup for composite FK columns belonging to this table.
	// Key: column name → constraint name
	compositeCols := make(map[string]string)
	for _, cfk := range s.compositeFKs {
		if cfk.Schema == t.Schema && cfk.Table == t.Name {
			for _, col := range cfk.LocalCols {
				compositeCols[col] = cfk.ConstraintName
			}
		}
	}

	// Track which composite FK constraints have already had their primary edge added.
	// Key: constraint name → list of edge IDs (forward + reverse in s.allEdges)
	compositeEdges := make(map[string][]int32)

	for _, c := range t.Columns {
		if c.FKeyTable == "" {
			continue
		}

		if c.FKeySchema == "" {
			c.FKeySchema = t.Schema
		}

		if c.FKeyCol == "" {
			continue
		}

		// Cross-database FK: store as metadata rather than adding to the graph.
		// The target table lives in another database and doesn't exist as a
		// graph node. Path resolution for these is handled by FindCrossDBPath.
		if c.FKeyDatabase != "" {
			s.crossDBRels = append(s.crossDBRels, CrossDBRel{
				SourceTable:  t,
				SourceCol:    c,
				TargetDB:     c.FKeyDatabase,
				TargetSchema: c.FKeySchema,
				TargetTable:  c.FKeyTable,
				TargetCol:    c.FKeyCol,
				IsOneToOne:   c.FKeyIsUnique,
			})
			continue
		}

		v, ok := s.tindex[(c.FKeySchema + ":" + c.FKeyTable)]
		if !ok {
			return fmt.Errorf("foreign key table not found: %s.%s", c.FKeySchema, c.FKeyTable)
		}
		ft := s.tables[v.nodeID]

		fc, ok := ft.getColumn(c.FKeyCol)
		if !ok {
			return fmt.Errorf("foreign key column not found: %s.%s", c.FKeyTable, c.FKeyCol)
		}

		// Check if this column belongs to a composite FK
		if conName, isComposite := compositeCols[c.Name]; isComposite {
			if edgeIDs, already := compositeEdges[conName]; already {
				// Subsequent column of the composite FK — attach to all existing edges
				// (forward and reverse, with correct column orientation)
				for _, eid := range edgeIDs {
					edge := s.allEdges[eid]
					if edge.L.Table == c.Table {
						// Forward edge: local → foreign
						edge.ExtraPairs = append(edge.ExtraPairs, ColPair{L: c, R: fc})
					} else {
						// Reverse edge: foreign → local
						edge.ExtraPairs = append(edge.ExtraPairs, ColPair{L: fc, R: c})
					}
					s.allEdges[eid] = edge
				}
				continue
			}
			// First column of the composite FK — fall through to add edge normally,
			// then record the edge IDs
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

		// If this was the first column of a composite FK, record the edge IDs
		// (both forward and reverse edges)
		if conName, isComposite := compositeCols[c.Name]; isComposite {
			k1 := t.Schema + ":" + t.Name
			k2 := c.FKeySchema + ":" + c.FKeyTable
			fn := s.tindex[k1].nodeID
			tn := s.tindex[k2].nodeID
			var edgeIDs []int32
			for eid, edge := range s.allEdges {
				if (edge.From == fn && edge.To == tn && edge.L.Name == c.Name) ||
					(edge.From == tn && edge.To == fn && edge.R.Name == c.Name) {
					edgeIDs = append(edgeIDs, eid)
				}
			}
			compositeEdges[conName] = edgeIDs
		}
	}
	return nil
}

// FindCrossDBPath checks cross-database FK metadata for a relationship between
// two tables identified by their unqualified names (as used in GraphQL queries).
// Returns a synthetic TPath if found, without requiring the target table to be
// a node in the graph.
func (s *DBSchema) FindCrossDBPath(childName, parentName string) (TPath, bool) {
	for _, rel := range s.crossDBRels {
		// Forward: source table is the parent (has the FK), target is the child
		// e.g. job_crew.employee_id → ats:employees.id
		// GraphQL: { job_crew { employees { ... } } }
		// FindPath is called as FindPath("employees", "job_crew")
		if rel.SourceTable.Name == parentName && rel.TargetTable == childName {
			relType := RelOneToMany
			if rel.IsOneToOne {
				relType = RelOneToOne
			}
			return TPath{
				Rel: relType,
				LT:  rel.SourceTable,
				LC:  rel.SourceCol,
				RT: DBTable{
					Name:     rel.TargetTable,
					Schema:   rel.TargetSchema,
					Database: rel.TargetDB,
				},
				RC: DBColumn{
					Name:     rel.TargetCol,
					Schema:   rel.TargetSchema,
					Table:    rel.TargetTable,
					Database: rel.TargetDB,
				},
			}, true
		}
		// Reverse: child has the FK, parent is the remote target
		if rel.TargetTable == parentName && rel.SourceTable.Name == childName {
			relType := RelOneToOne
			if rel.IsOneToOne {
				relType = RelOneToMany
			}
			return TPath{
				Rel: relType,
				LT: DBTable{
					Name:     rel.TargetTable,
					Schema:   rel.TargetSchema,
					Database: rel.TargetDB,
				},
				LC: DBColumn{
					Name:     rel.TargetCol,
					Schema:   rel.TargetSchema,
					Table:    rel.TargetTable,
					Database: rel.TargetDB,
				},
				RT:  rel.SourceTable,
				RC:  rel.SourceCol,
			}, true
		}
	}
	return TPath{}, false
}

// GetCrossDBRels returns all cross-database relationships in the schema.
func (s *DBSchema) GetCrossDBRels() []CrossDBRel {
	return s.crossDBRels
}

// addVirtual adds a virtual table to the schema
func (s *DBSchema) addVirtual(vt VirtualTable) error {
	s.virtualTables[vt.Name] = vt

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

// GetTables returns a table from the schema
func (s *DBSchema) GetTables() []DBTable {
	return s.tables
}

// RelNode represents a relationship node
type RelNode struct {
	Name  string
	Type  RelType
	Table DBTable
}

// GetFirstDegree returns the first degree relationships of a table
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

// GetSecondDegree returns the second degree relationships of a table
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

// getRelNodes returns the relationship nodes
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

// getColumn returns a column from a table
func (ti *DBTable) getColumn(name string) (DBColumn, bool) {
	var c DBColumn
	if i, ok := ti.colMap[name]; ok {
		return ti.Columns[i], true
	}
	return c, false
}

// GetColumn returns a column from a table
func (ti *DBTable) GetColumn(name string) (DBColumn, error) {
	c, ok := ti.getColumn(name)
	if ok {
		return c, nil
	}
	return c, fmt.Errorf("column: '%s.%s' not found", ti.Name, name)
}

// ColumnExists returns true if a column exists in a table
func (ti *DBTable) ColumnExists(name string) (DBColumn, bool) {
	return ti.getColumn(name)
}

// GetFunction returns a function from the schema
func (s *DBSchema) GetFunctions() map[string]DBFunction {
	return s.dbFunctions
}

// GetRelName returns the relationship name
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

// DBType returns the database type
func (s *DBSchema) DBType() string {
	return s.dbType
}

// DBVersion returns the database version
func (s *DBSchema) DBVersion() int {
	return s.version
}

// DBSchema returns the database schema
func (s *DBSchema) DBSchema() string {
	return s.schema
}

// DBName returns the database name
func (s *DBSchema) DBName() string {
	return s.name
}
