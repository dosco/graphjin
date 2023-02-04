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

type TEdge struct {
	From, To, Weight int32

	Type   RelType
	LT, RT DBTable
	L, R   DBColumn
	CName  string
	name   string
}

func (s *DBSchema) addNode(t DBTable) int32 {
	s.tables = append(s.tables, t)
	n := s.rg.AddNode()

	s.tindex[(t.Schema + ":" + t.Name)] = nodeInfo{n}
	return n
}

func (s *DBSchema) addAliases(t DBTable, nodeID int32, aliases []string) {
	for _, al := range aliases {
		s.tindex[(t.Schema + ":" + al)] = nodeInfo{nodeID}
		s.ai[al] = nodeInfo{nodeID}
	}
}

func (s *DBSchema) GetAliases() map[string]DBTable {
	ts := make(map[string]DBTable)

	for name, n := range s.ai {
		ts[name] = s.tables[int(n.nodeID)]
	}
	return ts
}

func (s *DBSchema) IsAlias(name string) bool {
	_, ok := s.ai[name]
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

	if err := s.rg.UpdateEdge(ln, rn, edgeID1, edgeID2); err != nil {
		return err
	}

	if err := s.rg.UpdateEdge(rn, ln, edgeID2, edgeID1); err != nil {
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

func (s *DBSchema) addEdge(name string, edge TEdge, inSchema bool,
) (int32, error) {
	// add edge to graph
	edgeID, err := s.rg.AddEdge(edge.From, edge.To,
		edge.Weight, edge.CName)
	if err != nil {
		return -1, err
	}

	ei := edgeInfo{nodeID: edge.From, edgeIDs: []int32{edgeID}}
	s.addEdgeInfo(name, ei)

	if inSchema {
		edge.name = name
	}
	s.ae[edgeID] = edge

	return edgeID, nil
}

func (s *DBSchema) addEdgeInfo(k string, ei edgeInfo) {
	if eiList, ok := s.ei[k]; ok {
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
			s.ei[k][i].edgeIDs = edgeIDs
			return
		}
	}
	s.ei[k] = append(s.ei[k], ei)
}

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

type TPath struct {
	Rel RelType
	LT  DBTable
	LC  DBColumn
	RT  DBTable
	RC  DBColumn
}

func (s *DBSchema) FindPath(from, to, through string) ([]TPath, error) {
	fl, ok := s.ei[from]
	if !ok {
		return nil, ErrFromEdgeNotFound
	}

	tl, ok := s.ei[to]
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
		edge := s.ae[eid]
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

type graphResult struct {
	from, to edgeInfo
	edges    []int32
}

func (s *DBSchema) between(from, to []edgeInfo, through string) (res graphResult, err error) {
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

func (s *DBSchema) pickPath(from, to edgeInfo, through string) (res graphResult, err error) {
	res.from = from
	res.to = to

	fn := from.nodeID
	tn := to.nodeID
	paths := s.rg.AllPaths(fn, tn)

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

func (s *DBSchema) pickEdges(path []int32, from, to edgeInfo) (edges []int32, allFound bool) {
	pathLen := len(path)
	peID := int32(-2) // must be -2 so does not match default -1

	for i := 1; i < pathLen; i++ {
		fn := path[i-1]
		tn := path[i]
		lines := s.rg.GetEdges(fn, tn)

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

func pickLine(lines []util.Edge, ei edgeInfo, peID int32) *util.Edge {
	for _, v := range lines {
		for _, eid := range ei.edgeIDs {
			if v.ID == eid && v.OppID != peID {
				return &v
			}
		}
	}
	return nil
}

func PathToRel(p TPath) DBRel {
	return DBRel{
		Type:  p.Rel,
		Left:  DBRelLeft{Ti: p.LT, Col: p.LC},
		Right: DBRelRight{Ti: p.RT, Col: p.RC},
	}
}

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

func (s *DBSchema) PrintLines(lines []util.Edge) {
	for _, v := range lines {
		e := s.ae[v.ID]
		f := s.tables[e.From]
		t := s.tables[e.To]

		fmt.Printf("- (EdgeID: %d, OppEdge: %d, W:%d, N:%s) %s TableID:%d -> %s TableID:%d\n",
			v.ID, v.OppID, v.Weight, e.name, f.Name, e.From, t.Name, e.To)
	}
	fmt.Println("---")
}

func (s *DBSchema) PrintEdgeInfo(e edgeInfo) {
	t := s.tables[e.nodeID]
	fmt.Printf("-- EdgeInfo %s %+v\n", t.Name, e.edgeIDs)

	// for _, id := range e.edgeIDs {
	// 	e := s.ae[id]
	// }
}

func (tp *TPath) String() string {
	return fmt.Sprintf("(%s) %s ==> %s ==> (%s) %s",
		tp.LT.String(), tp.LC.String(),
		tp.Rel.String(),
		tp.RT.String(), tp.RC.String())
}
