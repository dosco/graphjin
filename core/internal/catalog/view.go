package catalog

// Without returns a copy of s that leaves out the cards drop selects, along
// with their details, graph nodes, edges touching those nodes, and
// capabilities. The copy gets its own search index, so it is meant to be
// built once per snapshot and reused, not built per query.
func (s *Snapshot) Without(drop func(Card) bool) *Snapshot {
	if s == nil {
		return nil
	}
	dropped := map[string]bool{}
	out := &Snapshot{
		GeneratedAt:     s.GeneratedAt,
		Revision:        s.Revision,
		SourceRevisions: s.SourceRevisions,
		EntryPoints:     s.EntryPoints,
		Cards:           make([]Card, 0, len(s.Cards)),
	}
	for _, card := range s.Cards {
		if drop(card) {
			dropped[card.ID] = true
			continue
		}
		out.Cards = append(out.Cards, card)
	}
	if len(dropped) == 0 {
		return s
	}
	for _, detail := range s.Details {
		if !dropped[detail.CardID] {
			out.Details = append(out.Details, detail)
		}
	}
	droppedNodes := map[string]bool{}
	for _, node := range s.Nodes {
		if node.CardID != "" && dropped[node.CardID] {
			droppedNodes[node.ID] = true
			continue
		}
		out.Nodes = append(out.Nodes, node)
	}
	for _, edge := range s.Edges {
		if !droppedNodes[edge.FromID] && !droppedNodes[edge.ToID] {
			out.Edges = append(out.Edges, edge)
		}
	}
	for _, capability := range s.Capabilities {
		if !dropped[capability.ID] {
			out.Capabilities = append(out.Capabilities, capability)
		}
	}
	out.search = newSearchIndex(out)
	return out
}
