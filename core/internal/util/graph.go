package util

import (
	"fmt"
)

type Edge struct {
	ID     int32
	OppID  int32
	Weight int32
	Name   string
}

type Graph struct {
	edgeID int32
	edges  map[[2]int32][]Edge
	graph  [][]int32
}

func NewGraph() *Graph {
	return &Graph{edges: make(map[[2]int32][]Edge)}
}

func (g *Graph) AddNode() int32 {
	id := int32(len(g.graph))
	g.graph = append(g.graph, []int32{})
	return id
}

func (g *Graph) AddEdge(from, to, weight int32, name string) (int32, error) {
	nl := int32(len(g.graph))
	if from >= nl {
		return -1, fmt.Errorf("from node %d does not exist", from)
	}

	if to >= nl {
		return -1, fmt.Errorf("to node %d does not exist", to)
	}

	id := g.edgeID
	g.edgeID++

	e := [2]int32{from, to}
	_, edgeExists := g.edges[e]
	g.edges[e] = append(g.edges[e], Edge{
		ID:     id,
		OppID:  -1,
		Weight: weight,
		Name:   name,
	})

	if !edgeExists {
		g.graph[from] = append(g.graph[from], to)
	}
	return id, nil
}

func (g *Graph) UpdateEdge(
	from, to, edgeID, oppEdgeID int32,
) error {
	ek := [2]int32{from, to}
	edges, ok := g.edges[ek]
	if !ok {
		return fmt.Errorf("not edges found: %v", ek)
	}

	for i, e := range edges {
		if e.ID == edgeID {
			e.OppID = oppEdgeID
			g.edges[ek][i] = e
			return nil
		}
	}
	return fmt.Errorf("edge not found: %d", edgeID)
}

func (g *Graph) GetEdges(from, to int32) []Edge {
	return g.edges[[2]int32{from, to}]
}

func (g *Graph) AllPaths(from, to int32) [][]int32 {
	var paths [][]int32
	var limit int

	h := newHeap()
	h.push(path{weight: 0, parent: from, nodes: []int32{from}})
	visited := make(map[[2]int32]struct{})

	for len(*h.paths) > 0 {
		if limit > 3000 {
			return paths
		}
		limit++

		// Find the nearest unvisited node
		p := h.pop()
		pnode := p.nodes[len(p.nodes)-1]

		if _, ok := visited[[2]int32{p.parent, pnode}]; ok {
			continue
		}

		if pnode == to && len(p.nodes) > 1 {
			for _, v := range paths {
				if equals(v, p.nodes) {
					return paths
				}
			}
			paths = append(paths, p.nodes)
			continue
		}

		for _, e := range g.graph[pnode] {
			if _, ok := p.visited[e]; ok && e != to {
				continue
			}

			if _, ok := visited[[2]int32{pnode, e}]; ok {
				continue
			}

			// We calculate cost so far and add in the weight (cost) of this edge.
			p1 := path{
				weight:  p.weight + 1,
				parent:  pnode,
				nodes:   append(append([]int32{}, p.nodes...), e),
				visited: make(map[int32]struct{}),
			}
			for _, v := range p1.nodes {
				p1.visited[v] = struct{}{}
			}
			h.push(p1)
		}
	}
	return paths
}

func (g *Graph) Connections(n int32) []int32 {
	return g.graph[n]
}

func equals(a, b []int32) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	return true
}
