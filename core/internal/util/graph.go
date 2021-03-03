package util

import (
	"fmt"
)

type Edge struct {
	ID, Weight int32
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

func (g *Graph) AddEdge(from, to, weight int32) (int32, error) {
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
	g.edges[e] = append(g.edges[e], Edge{ID: id, Weight: weight})

	if !edgeExists {
		g.graph[from] = append(g.graph[from], to)
	}
	return id, nil
}

func (g *Graph) GetEdges(from, to int32) []Edge {
	return g.edges[[2]int32{from, to}]
}

func (g *Graph) AllPaths(from, to int32) [][]int32 {
	var paths [][]int32

	h := newHeap()
	h.push(path{weight: 0, nodes: []int32{from}})
	visited := make(map[[2]int32]struct{})

	for len(*h.paths) > 0 {
		// Find the nearest unvisited node
		p := h.pop()
		node := p.nodes[len(p.nodes)-1]

		if _, ok := visited[[2]int32{p.parent, node}]; ok {
			continue
		}

		if node == to && len(p.nodes) > 1 {
			paths = append(paths, p.nodes)
			continue
		}

		for _, e := range g.graph[node] {
			if _, ok := p.visited[e]; ok {
				continue
			}
			if _, ok := visited[[2]int32{node, e}]; !ok {
				// We calculate cost so far and add in the weight (cost) of this edge.
				p1 := path{
					weight:  p.weight + 1,
					parent:  node,
					nodes:   append(append([]int32{}, p.nodes...), e),
					visited: make(map[int32]struct{}),
				}
				for _, v := range p1.nodes {
					p1.visited[v] = struct{}{}
				}
				h.push(p1)
			}
		}
	}

	return paths
}
