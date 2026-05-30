package sdata

import (
	"container/heap"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/util"
)

type pathThroughKind uint8

const (
	pathThroughNone pathThroughKind = iota
	pathThroughTable
	pathThroughColumn
)

type pathOptions struct {
	kind        pathThroughKind
	through     string
	throughNode int32
}

type pathCacheKey struct {
	fromNode, toNode   int32
	throughNode        int32
	fromEdges, toEdges uint64
	throughKind        pathThroughKind
	through            string
}

func (s *DBSchema) resolvePath(from, to []edgeInfo, opts pathOptions) (res graphResult, err error) {
	if opts.kind == pathThroughTable && opts.through == "" {
		opts.kind = pathThroughNone
	}

	for _, f := range from {
		for _, t := range to {
			res, err = s.resolvePathPair(f, t, opts)
			if err == ErrPathNotFound {
				continue
			}
			return
		}
	}
	return res, ErrPathNotFound
}

func (s *DBSchema) resolvePathPair(from, to edgeInfo, opts pathOptions) (res graphResult, err error) {
	res.from = from
	res.to = to

	if opts.kind == pathThroughTable {
		if edges, ok := s.directPathEdges(from, to, opts); ok {
			res.edges = edges
			return res, nil
		}

		v, ok := s.tindex[(s.DBSchema() + ":" + opts.through)]
		if !ok {
			return res, ErrThoughNodeNotFound
		}
		opts.throughNode = v.nodeID
	} else if opts.kind == pathThroughNone {
		lines := s.relationshipGraph.GetEdges(from.nodeID, to.nodeID)
		if cands := s.detectDirectAmbiguityFromLines(lines, from); len(cands) > 1 {
			return res, &AmbiguousPathError{
				From:       s.tables[from.nodeID].Name,
				To:         s.tables[to.nodeID].Name,
				Candidates: cands,
			}
		}
		if from.nodeID == to.nodeID {
			if edges, ok := s.directRecursivePathEdges(from, opts); ok {
				res.edges = edges
				return res, nil
			}
		}
	} else if opts.kind == pathThroughColumn {
		opts.through = strings.ToLower(opts.through)
		if from.nodeID == to.nodeID {
			if edges, ok := s.directRecursivePathEdges(from, opts); ok {
				res.edges = edges
				return res, nil
			}
		}
	}

	if !s.canReach(from.nodeID, to.nodeID) {
		return res, ErrPathNotFound
	}

	key := makePathCacheKey(from, to, opts)
	if edges, ok := s.lookupPathCache(key); ok {
		res.edges = edges
		return res, nil
	}

	edges, err := s.shortestPathEdges(from, to, opts)
	if err != nil {
		return res, err
	}
	res.edges = edges
	s.storePathCache(key, edges)
	return res, nil
}

func (s *DBSchema) directPathEdges(from, to edgeInfo, opts pathOptions) ([]int32, bool) {
	lines := s.relationshipGraph.GetEdges(from.nodeID, to.nodeID)
	var best *util.Edge
	for i := range lines {
		line := &lines[i]
		if !edgeIDIn(from.edgeIDs, line.ID) {
			continue
		}
		if opts.kind == pathThroughColumn && !s.lineMatchesColumn(*line, opts.through) {
			continue
		}
		if best == nil || line.Weight < best.Weight {
			best = line
		}
	}
	if best == nil {
		return nil, false
	}
	return []int32{best.ID}, true
}

func (s *DBSchema) directRecursivePathEdges(from edgeInfo, opts pathOptions) ([]int32, bool) {
	lines := s.relationshipGraph.GetEdges(from.nodeID, from.nodeID)
	var best *util.Edge
	for i := range lines {
		line := &lines[i]
		if !edgeIDIn(from.edgeIDs, line.ID) {
			continue
		}
		if opts.kind == pathThroughColumn && !s.lineMatchesColumn(*line, opts.through) {
			continue
		}
		edge, ok := s.allEdges[line.ID]
		if !ok || edge.Type != RelRecursive {
			continue
		}
		if best == nil || line.Weight < best.Weight {
			best = line
		}
	}
	if best == nil {
		return nil, false
	}
	return []int32{best.ID}, true
}

type pathStateKey struct {
	node        int32
	prevEdgeID  int32
	throughSeen bool
}

type pathItem struct {
	node        int32
	prevEdgeID  int32
	edgeID      int32
	cost        int32
	order       int
	depth       int
	throughSeen bool
	parent      *pathItem
	index       int
}

type pathHeap []*pathItem

func (h pathHeap) Len() int { return len(h) }

func (h pathHeap) Less(i, j int) bool {
	if h[i].cost == h[j].cost {
		if h[i].depth == h[j].depth {
			return h[i].order < h[j].order
		}
		return h[i].depth < h[j].depth
	}
	return h[i].cost < h[j].cost
}

func (h pathHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *pathHeap) Push(x any) {
	item := x.(*pathItem)
	item.index = len(*h)
	*h = append(*h, item)
}

func (h *pathHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*h = old[:n-1]
	return item
}

func (s *DBSchema) shortestPathEdges(from, to edgeInfo, opts pathOptions) ([]int32, error) {
	const noEdge int32 = -2

	startThroughSeen := opts.kind == pathThroughTable && from.nodeID == opts.throughNode
	pq := pathHeap{}
	heap.Push(&pq, &pathItem{
		node:        from.nodeID,
		prevEdgeID:  noEdge,
		throughSeen: startThroughSeen,
	})

	best := make(map[pathStateKey]int32)
	limit := len(s.tables)*maxInt(1, len(s.allEdges))*4 + 1
	var expanded, order int

	for pq.Len() != 0 {
		if expanded > limit {
			return nil, ErrPathSearchLimit
		}
		expanded++

		item := heap.Pop(&pq).(*pathItem)
		key := pathStateKey{
			node:        item.node,
			prevEdgeID:  item.prevEdgeID,
			throughSeen: item.throughSeen,
		}
		if cost, ok := best[key]; ok && cost <= item.cost {
			continue
		}
		best[key] = item.cost

		if item.node == to.nodeID && item.depth != 0 {
			if opts.kind != pathThroughTable || item.throughSeen {
				return pathItemEdges(item), nil
			}
		}

		for _, next := range s.relationshipGraph.Connections(item.node) {
			lines := s.relationshipGraph.GetEdges(item.node, next)
			requireToAnchor := false
			if next == to.nodeID && item.depth != 0 {
				for _, line := range lines {
					if s.lineAllowed(line, from, to, item.prevEdgeID, item.depth, opts, false) &&
						edgeIDIn(to.edgeIDs, line.ID) {
						requireToAnchor = true
						break
					}
				}
			}

			for _, line := range lines {
				if !s.lineAllowed(line, from, to, item.prevEdgeID, item.depth, opts, requireToAnchor) {
					continue
				}

				throughSeen := item.throughSeen ||
					(opts.kind == pathThroughTable && next == opts.throughNode)
				order++
				heap.Push(&pq, &pathItem{
					node:        next,
					prevEdgeID:  line.ID,
					edgeID:      line.ID,
					cost:        item.cost + line.Weight,
					order:       order,
					depth:       item.depth + 1,
					throughSeen: throughSeen,
					parent:      item,
				})
			}
		}
	}

	return nil, ErrPathNotFound
}

func pathItemEdges(item *pathItem) []int32 {
	edges := make([]int32, item.depth)
	for i := item.depth - 1; i >= 0; i-- {
		edges[i] = item.edgeID
		item = item.parent
	}
	return edges
}

func (s *DBSchema) lineAllowed(
	line util.Edge,
	from, to edgeInfo,
	prevEdgeID int32,
	edgeDepth int,
	opts pathOptions,
	requireToAnchor bool,
) bool {
	if line.OppID == prevEdgeID {
		return false
	}
	if edgeDepth == 0 && !edgeIDIn(from.edgeIDs, line.ID) {
		return false
	}
	if requireToAnchor && !edgeIDIn(to.edgeIDs, line.ID) {
		return false
	}
	if opts.kind == pathThroughColumn && !s.lineMatchesColumn(line, opts.through) {
		return false
	}
	return true
}

func (s *DBSchema) lineMatchesColumn(line util.Edge, col string) bool {
	if col == "" {
		return true
	}
	edge, ok := s.allEdges[line.ID]
	if !ok {
		return false
	}
	if strings.EqualFold(edge.L.Name, col) || strings.EqualFold(edge.R.Name, col) {
		return true
	}
	for _, p := range edge.ExtraPairs {
		if strings.EqualFold(p.L.Name, col) || strings.EqualFold(p.R.Name, col) {
			return true
		}
	}
	return false
}

func edgeIDIn(ids []int32, id int32) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

func makePathCacheKey(from, to edgeInfo, opts pathOptions) pathCacheKey {
	return pathCacheKey{
		fromNode:    from.nodeID,
		toNode:      to.nodeID,
		throughNode: opts.throughNode,
		fromEdges:   hashEdgeIDs(from.edgeIDs),
		toEdges:     hashEdgeIDs(to.edgeIDs),
		throughKind: opts.kind,
		through:     opts.through,
	}
}

func hashEdgeIDs(ids []int32) uint64 {
	var h uint64 = 1469598103934665603
	for _, id := range ids {
		h ^= uint64(uint32(id)) + 0x9e3779b97f4a7c15
		h *= 1099511628211
	}
	h ^= uint64(len(ids))
	return h
}

func (s *DBSchema) lookupPathCache(key pathCacheKey) ([]int32, bool) {
	s.pathCacheMu.RLock()
	edges, ok := s.pathCache[key]
	s.pathCacheMu.RUnlock()
	if !ok {
		return nil, false
	}
	out := append([]int32{}, edges...)
	return out, true
}

func (s *DBSchema) storePathCache(key pathCacheKey, edges []int32) {
	s.pathCacheMu.Lock()
	if s.pathCache == nil {
		s.pathCache = make(map[pathCacheKey][]int32)
	}
	s.pathCache[key] = append([]int32{}, edges...)
	s.pathCacheMu.Unlock()
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
