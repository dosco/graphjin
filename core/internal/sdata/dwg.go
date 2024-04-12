package sdata

import (
	"errors"
	"fmt"

	"github.com/dosco/graphjin/core/v3/internal/util"
)

var (
	ErrFromEdgeNotFound   = errors.New("from edge not found")
	ErrToEdgeNotFound     = errors.New("to edge not found")
	ErrPathNotFound       = errors.New("path not found")
	ErrThoughNodeNotFound = errors.New("though node not found")
)

// TEdge represents a table edge for the graph
type TEdge struct {
	From, To, Weight int32

	Type   RelType
	LT, RT DBTable
	L, R   DBColumn
	CName  string
	name   string
}

// addNode adds a table node to the graph
func (s *DBSchema) addNode(t DBTable) int32 {
	s.tables = append(s.tables, t)
	n := s.relationshipGraph.AddNode()

	s.tindex[(t.Schema + ":" + t.Name)] = NodeInfo{n}
	return n
}

// addAliases adds table aliases to the graph
func (s *DBSchema) addAliases(t DBTable, nodeID int32, aliases []string) {
	for _, al := range aliases {
		s.tindex[(t.Schema + ":" + al)] = NodeInfo{nodeID}
		s.tableAliasIndex[al] = NodeInfo{nodeID}
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
) error {
	var err error

	var rt2 RelType
	k1 := (lti.Schema + ":" + lti.Name)
	k2 := (rti.Schema + ":" + rti.Name)

	fn, ok := s.tindex[k1]
	if !ok {
		return fmt.Errorf("addEdge: unknown node: %s", k1)
	}

	tn, ok := s.tindex[k2]
	if !ok {
		return fmt.Errorf("addEdge: unknown node: %s", k2)
	}

	ln := fn.nodeID
	rn := tn.nodeID

	var weight int32 = 1
	relT := GetRelName(lcol.Name)

	switch rt {
	case RelOneToOne:
		rt2 = RelOneToMany
	case RelOneToMany:
		rt2 = RelOneToOne
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
		return nil
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
		return err
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
		return err
	}

	if err := s.relationshipGraph.UpdateEdge(ln, rn, edgeID1, edgeID2); err != nil {
		return err
	}

	if err := s.relationshipGraph.UpdateEdge(rn, ln, edgeID2, edgeID1); err != nil {
		return err
	}

	if rti.Name != relT {
		if _, err := s.addEdge(rti.Name, e2, false); err != nil {
			return err
		}
	}

	// fmt.Printf("1. (%s, %d) %s.%s (%d) -> %s.%s (%d) == %s\n", lti.Name, e1.ID(), lti.Name, lcol.Name, ln.ID(), rti.Name, rcol.Name, rn.ID(), rt.String())
	// fmt.Printf("2. (%s, %d) %s.%s (%d) -> %s.%s (%d) == %s\n", rti.Name, e2.ID(), rti.Name, rcol.Name, rn.ID(), lti.Name, lcol.Name, ln.ID(), rt2.String())
	// fmt.Printf("3. (%s, %d) %s.%s (%d) -> %s.%s (%d) == %s\n", relT, e2.ID(), rti.Name, rcol.Name, rn.ID(), lti.Name, lcol.Name, ln.ID(), rt2.String())
	// fmt.Println("-----")
	return nil
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

	ei := EdgeInfo{nodeID: edge.From, edgeIDs: []int32{edgeID}}
	s.addEdgeInfo(name, ei)

	if inSchema {
		edge.name = name
	}
	s.allEdges[edgeID] = edge

	return edgeID, nil
}

// addEdgeInfo adds edge info to the index
func (s *DBSchema) addEdgeInfo(k string, ei EdgeInfo) {
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

// Find returns a table by schema and name
func (s *DBSchema) Find(schema, name string) (DBTable, error) {
	var t DBTable

	if schema == "" {
		schema = s.DBSchema()
	}

	v, ok := s.tindex[(schema + ":" + name)]
	if !ok {
		return t, fmt.Errorf("table not found: %s.%s", schema, name)
	}

	return s.tables[v.nodeID], nil
}

// TPath represents a table path
type TPath struct {
	Rel RelType
	LT  DBTable
	LC  DBColumn
	RT  DBTable
	RC  DBColumn
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

	res, err := s.between(fl, tl, through)
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
			Rel: edge.Type,
			LT:  edge.LT,
			LC:  edge.L,
			RT:  edge.RT,
			RC:  edge.R,
		})
	}
	if len(path) == 0 {
		return nil, ErrPathNotFound
	}
	return path, nil
}

// GraphResult represents a graph result
type GraphResult struct {
	from, to EdgeInfo
	edges    []int32
}

// between finds a path between two tables
func (s *DBSchema) between(from, to []EdgeInfo, through string) (res GraphResult, err error) {
	// TODO: picking a path
	// 1. first look for a direct edge to other table
	// 2. then find shortest path using relevant edges

	for _, f := range from {
		for _, t := range to {
			res, err = s.pickPath(f, t, through)
			if err == ErrPathNotFound {
				continue
			} else {
				return
			}
		}
	}
	return res, ErrPathNotFound
}

// pickPath picks a path between two tables
func (s *DBSchema) pickPath(from, to EdgeInfo, through string) (res GraphResult, err error) {
	res.from = from
	res.to = to

	fn := from.nodeID
	tn := to.nodeID
	paths := s.relationshipGraph.AllPaths(fn, tn)

	if through != "" {
		paths, err = s.pickThroughPath(paths, through)
		if err != nil {
			return
		}
	}

	for _, path := range paths {
		edges, ok := s.pickEdges(path, from, to)
		if ok {
			res.edges = edges
			return
		}
	}
	return res, ErrPathNotFound
}

// pickEdges picks edges between two tables
func (s *DBSchema) pickEdges(path []int32, from, to EdgeInfo) (edges []int32, allFound bool) {
	pathLen := len(path)
	peID := int32(-2) // must be -2 so does not match default -1

	for i := 1; i < pathLen; i++ {
		fn := path[i-1]
		tn := path[i]
		lines := s.relationshipGraph.GetEdges(fn, tn)

		// s.PrintLines(lines)

		switch {
		case i == 1:
			if v := pickLine(lines, from, peID); v != nil {
				edges = append(edges, v.ID)
				peID = v.ID
			} else {
				return
			}

		case i == (pathLen - 1):
			if v := pickLine(lines, to, peID); v != nil {
				edges = append(edges, v.ID)
				peID = v.ID

			} else {
				v := minWeightedLine(lines, peID)
				edges = append(edges, v.ID)
				peID = v.ID
			}

		default:
			v := minWeightedLine(lines, peID)
			edges = append(edges, v.ID)
			peID = v.ID
		}
	}
	allFound = true
	return
}

// pickThroughPath picks a path through a node
func (s *DBSchema) pickThroughPath(paths [][]int32, through string) ([][]int32, error) {
	var npaths [][]int32

	if len(paths) == 1 && len(paths[0]) == 2 {
		return paths, nil
	}

	v, ok := s.tindex[(s.DBSchema() + ":" + through)]
	if !ok {
		return nil, ErrThoughNodeNotFound
	}

	for i := range paths {
		for j := range paths[i] {
			if paths[i][j] == v.nodeID {
				npaths = append(npaths, paths[i])
			}
		}
	}
	return npaths, nil
}

// pickLine picks a line between two tables
func pickLine(lines []util.Edge, ei EdgeInfo, peID int32) *util.Edge {
	for _, v := range lines {
		for _, eid := range ei.edgeIDs {
			if v.ID == eid && v.OppID != peID {
				return &v
			}
		}
	}
	return nil
}

// PathToRel converts a table path to a relationship
func PathToRel(p TPath) DBRel {
	return DBRel{
		Type:  p.Rel,
		Left:  DBRelLeft{Ti: p.LT, Col: p.LC},
		Right: DBRelRight{Ti: p.RT, Col: p.RC},
	}
}

// minWeightedLine returns the line with the minimum weight
func minWeightedLine(lines []util.Edge, peID int32) *util.Edge {
	var min int32 = 100
	var line *util.Edge

	for i, v := range lines {
		if v.Weight < min && v.OppID != peID {
			min = v.Weight
			line = &lines[i]
		}
	}

	if line == nil && len(lines) != 0 {
		return &lines[0]
	}

	return line
}

// PrintLines prints the graph lines
func (s *DBSchema) PrintLines(lines []util.Edge) {
	for _, v := range lines {
		e := s.allEdges[v.ID]
		f := s.tables[e.From]
		t := s.tables[e.To]

		fmt.Printf("- (EdgeID: %d, OppEdge: %d, W:%d, N:%s) %s TableID:%d -> %s TableID:%d\n",
			v.ID, v.OppID, v.Weight, e.name, f.Name, e.From, t.Name, e.To)
	}
	fmt.Println("---")
}

// PrintEdgeInfo prints edge info
func (s *DBSchema) PrintEdgeInfo(e EdgeInfo) {
	t := s.tables[e.nodeID]
	fmt.Printf("-- EdgeInfo %s %+v\n", t.Name, e.edgeIDs)

	// for _, id := range e.edgeIDs {
	// 	e := s.ae[id]
	// }
}

// String returns a string representation of a table path
func (tp *TPath) String() string {
	return fmt.Sprintf("(%s) %s ==> %s ==> (%s) %s",
		tp.LT.String(), tp.LC.String(),
		tp.Rel.String(),
		tp.RT.String(), tp.RC.String())
}
