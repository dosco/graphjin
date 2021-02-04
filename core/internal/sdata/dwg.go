package sdata

import (
	"fmt"

	"github.com/gobuffalo/flect"
	"gonum.org/v1/gonum/graph"
)

type TEdge struct {
	Type RelType

	LT, RT DBTable
	L, R   DBColumn

	graph.WeightedEdge
}

func (s *DBSchema) addNode(t DBTable) int64 {
	s.tables = append(s.tables, t)

	n := s.rg.NewNode()
	sn := nodeInfo{id: n.ID(), singular: true}
	pn := nodeInfo{id: n.ID(), singular: false}

	s.ni[(t.Schema + ":" + t.Singular)] = sn
	s.ni[(t.Schema + ":" + t.Plural)] = pn

	if _, ok := s.ni[(":" + t.Singular)]; !ok {
		s.ni[(":" + t.Singular)] = sn
	}

	if _, ok := s.ni[(":" + t.Plural)]; !ok {
		s.ni[(":" + t.Plural)] = pn
	}

	s.rg.AddNode(n)
	return n.ID()
}

func (s *DBSchema) addAliases(t DBTable, nodeID int64, aliases []string) {
	sn := nodeInfo{id: nodeID, singular: true}
	pn := nodeInfo{id: nodeID, singular: false}

	for _, al := range aliases {
		sk := flect.Singularize(al)
		pk := flect.Pluralize(al)

		if _, ok := s.ni[(":" + sk)]; !ok {
			s.ni[(":" + sk)] = sn
		}

		if _, ok := s.ni[(":" + pk)]; !ok {
			s.ni[(":" + pk)] = pn
		}

		if _, ok := s.ni[(t.Schema + ":" + sk)]; !ok {
			s.ni[(t.Schema + ":" + sk)] = sn
		}

		if _, ok := s.ni[(t.Schema + ":" + pk)]; !ok {
			s.ni[(t.Schema + ":" + pk)] = pn
		}
	}

}

type nodeKey struct {
	scheme, table, col string
	singular           bool
}

// public.product.owner_id -> public.user.id
func (s *DBSchema) addEdge(
	lti DBTable, lcol DBColumn,
	rti DBTable, rcol DBColumn,
	rt RelType) error {

	k1 := (lti.Schema + ":" + lti.Name)
	k2 := (rti.Schema + ":" + rti.Name)

	fn, ok := s.ni[k1]
	if !ok {
		return fmt.Errorf("addEdge: unknown node: %s", k1)
	}

	tn, ok := s.ni[k2]
	if !ok {
		return fmt.Errorf("addEdge: unknown node: %s", k2)
	}

	ln := s.rg.Node(fn.id)
	rn := s.rg.Node(tn.id)

	if rt == RelRecursive {
		s.re[ln.ID()] = TEdge{
			Type: rt,
			LT:   lti, RT: rti,
			L: lcol, R: rcol,
			WeightedEdge: s.rg.NewWeightedEdge(ln, rn, 1.0),
		}
		return nil
	}

	e := TEdge{
		Type: rt,
		LT:   lti, RT: rti,
		L: lcol, R: rcol,
		WeightedEdge: s.rg.NewWeightedEdge(ln, rn, 1.0),
	}
	s.rg.SetWeightedEdge(e)

	var rt2 RelType

	switch rt {
	case RelOneToOne:
		rt2 = RelOneToMany
	case RelOneToMany:
		rt2 = RelOneToOne
	default:
		return nil
	}

	e = TEdge{
		Type: rt2,
		LT:   rti, RT: lti,
		L: rcol, R: lcol,
		WeightedEdge: s.rg.NewWeightedEdge(rn, ln, 1.0),
	}
	s.rg.SetWeightedEdge(e)

	relT := getRelName(lcol.Name)
	if relT == "" {
		return nil
	}

	relT1 := flect.Singularize(relT)
	relT2 := flect.Pluralize(relT)

	alts := []nodeKey{
		{lti.Schema, lti.Singular, relT1, true},
		{lti.Schema, lti.Singular, relT2, false},
		{lti.Schema, lti.Plural, relT1, true},
		{lti.Schema, lti.Plural, relT2, false},
	}

	// register alternate right nodes
	for _, v := range alts {
		rn = s.rg.NewNode()
		n := nodeInfo{id: rn.ID(), singular: v.singular}

		k1 := (v.scheme + ":" + v.table + ":" + v.col)
		k2 := (":" + v.table + ":" + v.col)
		k3 := (":" + v.col)

		s.ni[k1] = n

		if _, ok := s.ni[k2]; !ok {
			s.ni[k2] = n
		}

		if _, ok := s.ni[k3]; !ok {
			s.ni[k3] = n
		}

		s.rg.AddNode(rn)

		e := TEdge{
			Type: rt,
			LT:   lti, RT: rti,
			L: lcol, R: rcol,
			WeightedEdge: s.rg.NewWeightedEdge(ln, rn, 2.0),
		}
		s.rg.SetWeightedEdge(e)

		if rt2 != RelNone {
			e := TEdge{
				Type: rt2,
				LT:   rti, RT: lti,
				L: rcol, R: lcol,
				WeightedEdge: s.rg.NewWeightedEdge(rn, ln, 2.0),
			}
			s.rg.SetWeightedEdge(e)
		}
	}
	return nil
}

type TInfo struct {
	IsSingular bool
	DBTable
}

func (s *DBSchema) Find(schema, name string) (TInfo, error) {
	var t TInfo
	v, ok := s.ni[(schema + ":" + name)]
	if !ok {
		return t, fmt.Errorf("table not found: %s.%s", schema, name)
	}
	n := s.tables[v.id]

	t.IsSingular = v.singular
	t.DBTable = n
	return t, nil
}

type TPath struct {
	Rel RelType
	LTi TInfo
	L   DBColumn
	RTi TInfo
	R   DBColumn
}

func (s *DBSchema) FindPath(s1, from, s2, to string) ([]TPath, error) {
	f, ok := s.ni[(s2 + ":" + to + ":" + from)]
	if !ok {
		f, ok = s.ni[(s1 + ":" + from)]
	}
	if !ok {
		return nil, fmt.Errorf("table not found: %s.%s", s1, from)
	}

	t, ok := s.ni[(s1 + ":" + from + ":" + to)]
	if !ok {
		t, ok = s.ni[(s2 + ":" + to)]
	}
	if !ok {
		return nil, fmt.Errorf("table not found: %s.%s", s2, to)
	}

	// fmt.Printf("> %s.%s (%d) -> %s.%s (%d)\n",
	// 	s1, from, f.id,
	// 	s2, to, t.id)

	nodes, _, _ := s.sp.Between(f.id, t.id)
	//fmt.Printf("> weight: %f, unique: %t, nodes: %d\n", w, u, len(nodes))

	if len(nodes) == 0 {
		return nil, fmt.Errorf("no relationship found: %s.%s -> %s.%s", s1, from, s2, to)
	}

	if len(nodes) == 1 {
		edge, ok := s.re[nodes[0].ID()]
		if !ok {
			return nil, fmt.Errorf("no recursive relationship found: %s.%s", s1, from)
		}
		return []TPath{{
			Rel: edge.Type,
			LTi: TInfo{IsSingular: f.singular, DBTable: edge.LT},
			L:   edge.L,
			RTi: TInfo{IsSingular: t.singular, DBTable: edge.RT},
			R:   edge.R,
		}}, nil
	}

	path := []TPath{}
	for i := 1; i < len(nodes); i++ {
		fn := nodes[i-1]
		tn := nodes[i]

		//var e graph.Line
		e := s.rg.WeightedEdge(fn.ID(), tn.ID())
		if e == nil {
			return nil, fmt.Errorf("invalid edge: %d -> %d", fn.ID(), tn.ID())
		}
		edge := e.(TEdge)

		path = append(path, TPath{
			Rel: edge.Type,
			LTi: TInfo{IsSingular: f.singular, DBTable: edge.LT},
			L:   edge.L,
			RTi: TInfo{IsSingular: t.singular, DBTable: edge.RT},
			R:   edge.R,
		})
	}
	return path, nil
}

func PathToRel(p TPath) DBRel {
	return DBRel{
		Type:  p.Rel,
		Left:  DBRelLeft{Ti: p.LTi, Col: p.L},
		Right: DBRelRight{Ti: p.RTi, Col: p.R},
	}
}
