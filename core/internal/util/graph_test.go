package util_test

import (
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/assert"
	"github.com/dosco/graphjin/core/v3/internal/util"
)

// nolint:errcheck
func TestGraph1(t *testing.T) {
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
	assert.Equals(t, paths, [][]int32{
		{6, 1, 0},
		{6, 3, 2, 0},
		{6, 3, 2, 1, 0},
		{6, 5, 4, 3, 2, 0},
		{6, 5, 4, 3, 2, 1, 0},
	})

	paths = g.AllPaths(b, b)
	assert.Equals(t, paths, [][]int32{
		{1, 1},
		{1, 0, 1},
		{1, 0, 2, 1},
		{1, 0, 3, 2, 1},
	})

	edges := g.GetEdges(b, b)
	assert.Equals(t, edges, []util.Edge{{13, 2, "test"}})
}

/*
func TestGraph2(t *testing.T) {
	rand.Seed(time.Now().Unix())
	g := util.NewGraph()

	var nodes []int32
	for i := 0; i < 100; i++ {
		nodes = append(nodes, g.AddNode())
	}
	n := len(nodes)

	for i := 0; i < 50; i++ {
		a := nodes[rand.Intn(n-1)]
		b := nodes[rand.Intn(n-1)]
		w := int32(rand.Intn(3))
		name := fmt.Sprintf("node%d->node%d", a, b)
		g.AddEdge(a, b, w, name)
	}

	for i := 0; i < 1000; i++ {
		a := nodes[rand.Intn(n-1)]
		b := nodes[rand.Intn(n-1)]
		paths := g.AllPaths(b, a)
		if len(paths) == 0 {
			continue
		}
		fmt.Printf("Path From: %d -> %d\n", b, a)
		for _, p := range paths {
			fmt.Printf("- %d %v\n", len(p), p)
			for i := 1; i < len(p); i++ {
				a1 := p[(i - 1)]
				b1 := p[(i)]
				edges := g.GetEdges(a1, b1)
				fmt.Printf("*** %v\n", edges)
			}
		}
		fmt.Println("---")
		// assert.ElementsMatch(t, paths, [][]int32{
		// 	{6, 1, 0},
		// 	{6, 3, 2, 0},
		// 	{6, 3, 2, 1, 0},
		// 	{6, 5, 4, 3, 2, 0},
		// 	{6, 5, 4, 3, 2, 1, 0},
		// })
	}
	// edges := g.GetEdges(b, b)
	// assert.ElementsMatch(t, edges, []util.Edge{{13, 2, "test"}})
}
*/
