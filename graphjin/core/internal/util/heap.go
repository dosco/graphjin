package util

import (
	hp "container/heap"
)

type path struct {
	weight  int32
	parent  int32
	nodes   []int32
	visited map[int32]struct{}
}

type minPath []path

func (h minPath) Len() int           { return len(h) }
func (h minPath) Less(i, j int) bool { return h[i].weight < h[j].weight }
func (h minPath) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *minPath) Push(x interface{}) {
	*h = append(*h, x.(path))
}

func (h *minPath) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

type heap struct {
	paths *minPath
}

func newHeap() *heap {
	return &heap{paths: &minPath{}}
}

func (h *heap) push(p path) {
	hp.Push(h.paths, p)
}

func (h *heap) pop() path {
	i := hp.Pop(h.paths)
	return i.(path)
}
