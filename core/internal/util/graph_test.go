package util_test

import (
	"testing"

	"github.com/dosco/graphjin/core/internal/util"
	"github.com/tj/assert"
)

//nolint: errcheck
func TestGraph(t *testing.T) {
	g := util.NewGraph()

	a := g.AddNode() // 0
	b := g.AddNode() // 1
	c := g.AddNode() // 2
	d := g.AddNode() // 3
	e := g.AddNode() // 4
	f := g.AddNode() // 5
	h := g.AddNode() // 6

	g.AddEdge(a, b, 1, "test")
	g.AddEdge(b, a, 2, "test")
	g.AddEdge(a, c, 1, "test")
	g.AddEdge(d, c, 4, "test")
	g.AddEdge(c, b, 1, "test")

	g.AddEdge(h, f, 1, "test")
	g.AddEdge(f, e, 1, "test")
	g.AddEdge(e, d, 1, "test")
	g.AddEdge(h, d, 1, "test")
	g.AddEdge(h, b, 1, "test")
	g.AddEdge(c, a, 3, "test")
	g.AddEdge(a, d, 1, "test")
	g.AddEdge(d, c, 1, "test")
	g.AddEdge(b, b, 2, "test")

	paths := g.AllPaths(h, a)
	assert.ElementsMatch(t, paths, [][]int32{
		{6, 1, 0},
		{6, 3, 2, 0},
		{6, 3, 2, 1, 0},
		{6, 5, 4, 3, 2, 0},
		{6, 5, 4, 3, 2, 1, 0},
	})

	paths = g.AllPaths(b, b)
	assert.ElementsMatch(t, paths, [][]int32{
		{1, 1},
		{1, 0, 1},
		{1, 0, 2, 1},
		{1, 0, 3, 2, 1},
	})

	edges := g.GetEdges(b, b)
	assert.ElementsMatch(t, edges, []util.Edge{{13, 2, "test"}})
}
