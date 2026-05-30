package sdata

import "sort"

func (s *DBSchema) buildReachability() {
	n := len(s.tables)
	if n == 0 {
		return
	}
	words := (n + 63) / 64
	s.reachability = make([][]uint64, n)

	for src := 0; src < n; src++ {
		bits := make([]uint64, words)
		seen := make([]bool, n)
		queue := make([]int32, 0, n)

		for _, next := range s.relationshipGraph.Connections(int32(src)) {
			if next < 0 || int(next) >= n {
				continue
			}
			if !seen[next] {
				seen[next] = true
				queue = append(queue, next)
			}
		}

		for len(queue) != 0 {
			node := queue[0]
			queue = queue[1:]
			bits[node/64] |= uint64(1) << uint(node%64)

			for _, next := range s.relationshipGraph.Connections(node) {
				if next < 0 || int(next) >= n {
					continue
				}
				if seen[next] {
					continue
				}
				seen[next] = true
				queue = append(queue, next)
			}
		}

		s.reachability[src] = bits
	}
}

func (s *DBSchema) canReach(from, to int32) bool {
	if from < 0 || to < 0 || int(from) >= len(s.reachability) {
		return false
	}
	bits := s.reachability[from]
	word := int(to / 64)
	if word >= len(bits) {
		return false
	}
	return bits[word]&(uint64(1)<<uint(to%64)) != 0
}

func (s *DBSchema) buildFKCycles() {
	n := len(s.tables)
	if n == 0 {
		return
	}

	adj := make([][]int32, n)
	seenEdge := make(map[[2]int32]struct{})
	selfSet := make(map[int32]struct{})

	for _, edge := range s.allEdges {
		if edge.L.FKeyTable == "" {
			continue
		}
		from, to := edge.From, edge.To
		if from < 0 || to < 0 || int(from) >= n || int(to) >= n {
			continue
		}
		if from == to {
			selfSet[from] = struct{}{}
		}
		k := [2]int32{from, to}
		if _, ok := seenEdge[k]; ok {
			continue
		}
		seenEdge[k] = struct{}{}
		adj[from] = append(adj[from], to)
	}

	info := fkCycleInfo{}
	for id := range selfSet {
		info.Self = append(info.Self, id)
	}
	sort.Slice(info.Self, func(i, j int) bool { return info.Self[i] < info.Self[j] })

	var (
		index   int32
		stack   []int32
		onStack = make([]bool, n)
		indices = make([]int32, n)
		low     = make([]int32, n)
	)
	for i := range indices {
		indices[i] = -1
	}

	var strongConnect func(v int32)
	strongConnect = func(v int32) {
		indices[v] = index
		low[v] = index
		index++
		stack = append(stack, v)
		onStack[v] = true

		for _, w := range adj[v] {
			if indices[w] == -1 {
				strongConnect(w)
				if low[w] < low[v] {
					low[v] = low[w]
				}
			} else if onStack[w] && indices[w] < low[v] {
				low[v] = indices[w]
			}
		}

		if low[v] != indices[v] {
			return
		}

		var comp []int32
		for {
			w := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[w] = false
			comp = append(comp, w)
			if w == v {
				break
			}
		}
		if len(comp) <= 1 {
			return
		}
		sort.Slice(comp, func(i, j int) bool { return comp[i] < comp[j] })
		info.Components = append(info.Components, comp)
	}

	for v := 0; v < n; v++ {
		if indices[v] == -1 {
			strongConnect(int32(v))
		}
	}

	sort.Slice(info.Components, func(i, j int) bool {
		if len(info.Components[i]) == 0 || len(info.Components[j]) == 0 {
			return len(info.Components[i]) < len(info.Components[j])
		}
		return info.Components[i][0] < info.Components[j][0]
	})
	s.fkCycles = info
}
