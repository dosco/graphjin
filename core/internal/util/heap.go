package util

import (
	hp "container/heap"
)

type Path struct {
	weight  int32
	parent  int32
	nodes   []int32
	visited map[int32]struct{}
}

type MinPath []Path

func (h MinPath) Len() int           { return len(h) }
func (h MinPath) Less(i, j int) bool { return h[i].weight < h[j].weight }
func (h MinPath) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *MinPath) Push(x interface{}) {
	*h = append(*h, x.(Path))
}

func (h *MinPath) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

type Heap struct {
	paths *MinPath
}

func newHeap() *Heap {
	return &Heap{paths: &MinPath{}}
}

func (h *Heap) push(p Path) {
	hp.Push(h.paths, p)
}

func (h *Heap) pop() Path {
	i := hp.Pop(h.paths)
	return i.(Path)
}
