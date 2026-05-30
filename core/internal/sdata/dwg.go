package sdata

import (
	"errors"
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/util"
)

var (
	ErrFromEdgeNotFound   = errors.New("from edge not found")
	ErrToEdgeNotFound     = errors.New("to edge not found")
	ErrPathNotFound       = errors.New("path not found")
	ErrPathSearchLimit    = errors.New("path search limit reached")
	ErrThoughNodeNotFound = errors.New("though node not found")
)

// FKCandidate describes one foreign key option when an A→B relationship
// has multiple FKs and disambiguation is required.
type FKCandidate struct {
	Column           string
	TargetTable      string
	TargetColumn     string
	IsComposite      bool
	CompositeColumns []string
}

// AmbiguousPathError is returned when a direct (single-hop) relationship
// between two tables has multiple distinct foreign keys and the caller did
// not provide a @through hint to disambiguate.
type AmbiguousPathError struct {
	From, To   string
	Candidates []FKCandidate
}

func (e *AmbiguousPathError) Error() string {
	cols := make([]string, len(e.Candidates))
	for i, c := range e.Candidates {
		cols[i] = c.Column
	}
	return fmt.Sprintf("ambiguous relationship %s -> %s: multiple foreign keys (%s)",
		e.From, e.To, strings.Join(cols, ", "))
}

// TEdge represents a table edge for the graph
type TEdge struct {
	From, To, Weight int32

	Type       RelType
	LT, RT     DBTable
	L, R       DBColumn
	CName      string
	name       string
	ExtraPairs []ColPair // Additional column pairs for composite FKs
}

// addNode adds a table node to the graph
func (s *DBSchema) addNode(t DBTable) int32 {
	s.tables = append(s.tables, t)
	n := s.relationshipGraph.AddNode()

	s.tindex[(t.Schema + ":" + t.Name)] = nodeInfo{n}
	s.nameIndex[t.Name] = append(s.nameIndex[t.Name], n)
	return n
}

// addAliases adds table aliases to the graph
func (s *DBSchema) addAliases(t DBTable, nodeID int32, aliases []string) {
	for _, al := range aliases {
		s.tindex[(t.Schema + ":" + al)] = nodeInfo{nodeID}
		s.nameIndex[al] = append(s.nameIndex[al], nodeID)
		s.tableAliasIndex[al] = nodeInfo{nodeID}
	}
}

// GetAliases returns a map of table aliases
func (s *DBSchema) GetAliases() map[string]DBTable {
	ts := make(map[string]DBTable)

	for name, n := range s.tableAliasIndex {
		ts[name] = s.tables[int(n.nodeID)]
	}
	return ts
}

// IsAlias checks if a table is an alias
func (s *DBSchema) IsAlias(name string) bool {
	_, ok := s.tableAliasIndex[name]
	return ok
}

// Building the graph
// 1. AddNode is used to add tables nodes to the graph
// 2. addEdge creates relationships between schema:table -> fk_schema:fk_table
// 3. addEdge creates relationships between fk_schema:fk_table:column_name -> schema:table
// 4. addEdge creates relationships between :fk_table:column_name -> schema:table
// 5. addEdge creates relationships between :column_name -> schema:table
//
// Note 1: `_id` or `id_` is stripped from the column name to use as a graph key
// in the case where that then matches a real table name will result in conflict.

func (s *DBSchema) addToGraph(
	lti DBTable, lcol DBColumn,
	rti DBTable, rcol DBColumn,
	rt RelType,
) ([]int32, error) {
	var err error

	var rt2 RelType
	k1 := (lti.Schema + ":" + lti.Name)
	k2 := (rti.Schema + ":" + rti.Name)

	fn, ok := s.tindex[k1]
	if !ok {
		return nil, fmt.Errorf("addEdge: unknown node: %s", k1)
	}

	tn, ok := s.tindex[k2]
	if !ok {
		return nil, fmt.Errorf("addEdge: unknown node: %s", k2)
	}

	ln := fn.nodeID
	rn := tn.nodeID

	var weight int32 = 1
	relT := GetRelName(lcol.Name)

	switch rt {
	case RelOneToOne:
		rt2 = RelOneToMany
		relT = nonConflictingRelName(lti, lcol, relT, rti.Name)
	case RelOneToMany:
		rt2 = RelOneToOne
		relT = nonConflictingRelName(lti, lcol, relT, rti.Name)
	case RelPolymorphic:
		rt2 = rt
		relT = rti.Name
		weight = 15
	case RelEmbedded:
		rt2 = rt
		relT = rti.Name
		weight = 5
	case RelRecursive:
		rt2 = rt
		weight = 10
	case RelRemote:
		weight = 8
		relT = rti.Name
	default:
		return nil, nil
	}

	var edgeID1, edgeID2 int32

	// Add edge from table -> foreign key table
	e1 := TEdge{
		From:   ln,
		To:     rn,
		Weight: weight,
		Type:   rt,
		LT:     lti, RT: rti,
		L: lcol, R: rcol,
		CName: lcol.Name,
	}

	if edgeID1, err = s.addEdge(lti.Name, e1, true); err != nil {
		return nil, err
	}

	// Add reverse edge from parent table -> column_name
	e2 := TEdge{
		From:   rn,
		To:     ln,
		Weight: weight,
		Type:   rt2,
		LT:     rti, RT: lti,
		L: rcol, R: lcol,
		CName: lcol.Name,
	}

	if edgeID2, err = s.addEdge(relT, e2, true); err != nil {
		return nil, err
	}

	if err := s.relationshipGraph.UpdateEdge(ln, rn, edgeID1, edgeID2); err != nil {
		return nil, err
	}

	if err := s.relationshipGraph.UpdateEdge(rn, ln, edgeID2, edgeID1); err != nil {
		return nil, err
	}

	edgeIDs := []int32{edgeID1, edgeID2}
	if rti.Name != relT {
		edgeID, err := s.addEdge(rti.Name, e2, false)
		if err != nil {
			return nil, err
		}
		edgeIDs = append(edgeIDs, edgeID)
	}

	// fmt.Printf("1. (%s, %d) %s.%s (%d) -> %s.%s (%d) == %s\n", lti.Name, e1.ID(), lti.Name, lcol.Name, ln.ID(), rti.Name, rcol.Name, rn.ID(), rt.String())
	// fmt.Printf("2. (%s, %d) %s.%s (%d) -> %s.%s (%d) == %s\n", rti.Name, e2.ID(), rti.Name, rcol.Name, rn.ID(), lti.Name, lcol.Name, ln.ID(), rt2.String())
	// fmt.Printf("3. (%s, %d) %s.%s (%d) -> %s.%s (%d) == %s\n", relT, e2.ID(), rti.Name, rcol.Name, rn.ID(), lti.Name, lcol.Name, ln.ID(), rt2.String())
	// fmt.Println("-----")
	return edgeIDs, nil
}

func nonConflictingRelName(t DBTable, c DBColumn, name, fallback string) string {
	if _, ok := t.getColumn(name); !ok {
		return name
	}
	if hasMultipleFKsToTable(t, c) {
		return name
	}
	if _, ok := t.getColumn(fallback); !ok {
		return fallback
	}
	return name
}

func hasMultipleFKsToTable(t DBTable, c DBColumn) bool {
	if c.FKeyTable == "" {
		return false
	}
	var n int
	for _, col := range t.Columns {
		if col.FKeySchema == c.FKeySchema && col.FKeyTable == c.FKeyTable {
			n++
			if n > 1 {
				return true
			}
		}
	}
	return false
}

// addEdge creates a relationship between two tables
func (s *DBSchema) addEdge(name string, edge TEdge, inSchema bool,
) (int32, error) {
	// add edge to graph
	edgeID, err := s.relationshipGraph.AddEdge(edge.From, edge.To,
		edge.Weight, edge.CName)
	if err != nil {
		return -1, err
	}

	ei := edgeInfo{nodeID: edge.From, edgeIDs: []int32{edgeID}}
	s.addEdgeInfo(name, ei)

	if inSchema {
		edge.name = name
	}
	s.allEdges[edgeID] = edge

	return edgeID, nil
}

// addEdgeInfo adds edge info to the index
func (s *DBSchema) addEdgeInfo(k string, ei edgeInfo) {
	if eiList, ok := s.edgesIndex[k]; ok {
		for i, v := range eiList {
			if v.nodeID != ei.nodeID {
				continue
			}
			for _, eid := range v.edgeIDs {
				if eid == ei.edgeIDs[0] {
					return
				}
			}
			edgeIDs := append(v.edgeIDs, ei.edgeIDs[0])
			s.edgesIndex[k][i].edgeIDs = edgeIDs
			return
		}
	}
	s.edgesIndex[k] = append(s.edgesIndex[k], ei)
}

// Find returns a table by schema and name. If an exact schema:name match
// is not found, it falls back to searching across all discovered schemas.
// When multiple schemas contain the same table name, the default schema
// is preferred. If the table exists in multiple non-default schemas,
// an error listing the available schemas is returned.
func (s *DBSchema) Find(schema, name string) (DBTable, error) {
	var t DBTable

	if schema == "" {
		schema = s.DBSchema()
	}

	// Fast path: exact schema:name match
	if v, ok := s.tindex[(schema + ":" + name)]; ok {
		return s.tables[v.nodeID], nil
	}

	// Fallback: search across all schemas by name
	nodeIDs, ok := s.nameIndex[name]
	if !ok || len(nodeIDs) == 0 {
		return t, fmt.Errorf("table not found: %s.%s", schema, name)
	}

	// Single match: unambiguous, return it
	if len(nodeIDs) == 1 {
		return s.tables[nodeIDs[0]], nil
	}

	// Multiple matches: prefer the default schema
	defSchema := s.DBSchema()
	if v, ok := s.tindex[(defSchema + ":" + name)]; ok {
		return s.tables[v.nodeID], nil
	}

	// Multiple matches, none in default schema: report ambiguity
	schemas := make([]string, 0, len(nodeIDs))
	for _, nid := range nodeIDs {
		schemas = append(schemas, s.tables[nid].Schema)
	}
	return t, fmt.Errorf(
		"table '%s' found in multiple schemas: %s (use schema prefix to disambiguate)",
		name, strings.Join(schemas, ", "))
}

// TPath represents a table path
type TPath struct {
	Rel        RelType
	LT         DBTable
	LC         DBColumn
	RT         DBTable
	RC         DBColumn
	ExtraPairs []ColPair // Additional column pairs for composite FKs
}

// FindPath returns a path between two tables
func (s *DBSchema) FindPath(from, to, through string) ([]TPath, error) {
	fl, ok := s.edgesIndex[from]
	if !ok {
		return nil, ErrFromEdgeNotFound
	}

	tl, ok := s.edgesIndex[to]
	if !ok {
		return nil, ErrToEdgeNotFound
	}

	res, err := s.resolvePath(fl, tl, pathOptions{
		kind:    pathThroughTable,
		through: through,
	})
	if err != nil {
		return nil, err
	}

	// fmt.Printf("> %s (%d) -> %s (%d)\n",
	// 	from, res.from.nodeID,
	// 	to, res.to.nodeID)

	path := []TPath{}
	for _, eid := range res.edges {
		edge := s.allEdges[eid]
		path = append(path, TPath{
			Rel:        edge.Type,
			LT:         edge.LT,
			LC:         edge.L,
			RT:         edge.RT,
			RC:         edge.R,
			ExtraPairs: edge.ExtraPairs,
		})
	}
	if len(path) == 0 {
		return nil, ErrPathNotFound
	}
	return path, nil
}

// graphResult represents a graph result
type graphResult struct {
	from, to edgeInfo
	edges    []int32
}

// FindPathByColumn returns a path between two tables, disambiguating by the FK
// column name when the two tables have multiple foreign keys between them.
func (s *DBSchema) FindPathByColumn(from, to, col string) ([]TPath, error) {
	fl, ok := s.edgesIndex[from]
	if !ok {
		return nil, ErrFromEdgeNotFound
	}
	tl, ok := s.edgesIndex[to]
	if !ok {
		return nil, ErrToEdgeNotFound
	}

	res, err := s.resolvePath(fl, tl, pathOptions{
		kind:    pathThroughColumn,
		through: col,
	})
	if err != nil {
		return nil, err
	}

	path := []TPath{}
	for _, eid := range res.edges {
		edge := s.allEdges[eid]
		path = append(path, TPath{
			Rel:        edge.Type,
			LT:         edge.LT,
			LC:         edge.L,
			RT:         edge.RT,
			RC:         edge.R,
			ExtraPairs: edge.ExtraPairs,
		})
	}
	if len(path) == 0 {
		return nil, ErrPathNotFound
	}
	return path, nil
}

func (s *DBSchema) detectDirectAmbiguityFromLines(lines []util.Edge, from edgeInfo) []FKCandidate {
	// Dedup forward+reverse halves of the same FK constraint without
	// silencing genuine multi-FK ambiguity. Two edges in the same bucket
	// are "the same FK" iff they're each other's opposites — true only
	// for self-referential FKs (recursive comments → comments). For
	// non-recursive multi-FK (orders → users with two distinct FKs), the
	// edges in either bucket are NOT each other's opposites — their
	// OppIDs reference edges in the OTHER bucket. We track edge IDs as
	// we iterate; subsequent edges that name an already-seen OppID are
	// the recursive-pair we want to drop.
	bucketSeen := make(map[int32]bool)
	colSeen := make(map[string]struct{})
	var cands []FKCandidate

	for _, v := range lines {
		edge, ok := s.allEdges[v.ID]
		if !ok {
			continue
		}
		// Self-referential dedup: if this edge's OppID is already in the
		// bucket, we've already counted its constraint.
		if v.OppID != -1 && bucketSeen[v.OppID] {
			continue
		}
		bucketSeen[v.ID] = true

		// Anchor: if the caller's edgeInfo lists specific edges, only
		// count those. Empty edgeIDs means "no anchor filter" (e.g. when
		// pickPath was called from FindPath against a name index that
		// matched the bucket directly).
		if len(from.edgeIDs) > 0 {
			anchored := false
			for _, eid := range from.edgeIDs {
				if eid == v.ID || eid == v.OppID {
					anchored = true
					break
				}
			}
			if !anchored {
				continue
			}
		}

		// Resolve the FK side of the edge: for forward edges L is the
		// FK column; for reverse edges R is the FK column. The FK column
		// is the one whose owning DBColumn declares FKeyTable.
		var fkCol DBColumn
		var refCol string
		switch {
		case edge.L.FKeyTable != "":
			fkCol = edge.L
			refCol = edge.R.Name
		case edge.R.FKeyTable != "":
			fkCol = edge.R
			refCol = edge.L.Name
		default:
			// Neither side declares an FK — skip (ambiguous edges from
			// non-FK relationships like polymorphic / virtual tables).
			continue
		}

		if _, dup := colSeen[fkCol.Name]; dup {
			continue
		}
		colSeen[fkCol.Name] = struct{}{}

		c := FKCandidate{
			Column:       fkCol.Name,
			TargetTable:  fkCol.FKeyTable,
			TargetColumn: refCol,
		}
		if len(edge.ExtraPairs) > 0 {
			c.IsComposite = true
			cols := make([]string, 0, len(edge.ExtraPairs)+1)
			cols = append(cols, fkCol.Name)
			for _, p := range edge.ExtraPairs {
				if p.L.FKeyTable != "" {
					cols = append(cols, p.L.Name)
				} else if p.R.FKeyTable != "" {
					cols = append(cols, p.R.Name)
				}
			}
			c.CompositeColumns = cols
		}
		cands = append(cands, c)
	}
	return cands
}

// PathToRel converts a table path to a relationship
func PathToRel(p TPath) DBRel {
	return DBRel{
		Type:       p.Rel,
		Left:       DBRelLeft{Ti: p.LT, Col: p.LC},
		Right:      DBRelRight{Ti: p.RT, Col: p.RC},
		ExtraPairs: p.ExtraPairs,
	}
}

// String returns a string representation of a table path
func (tp *TPath) String() string {
	return fmt.Sprintf("(%s) %s ==> %s ==> (%s) %s",
		tp.LT.String(), tp.LC.String(),
		tp.Rel.String(),
		tp.RT.String(), tp.RC.String())
}
