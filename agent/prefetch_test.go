package agent

import (
	"context"
	"testing"
)

// A run the server already knows the subject of starts with that table's
// catalog detail prepared; consuming it is discovery, and it marks the table
// inspected for a write, so a routine does not spend its steps rediscovering
// what its watch knows.
func TestPrefetchedCatalogIsConsumedAsDiscovery(t *testing.T) {
	rt := &fakeRuntime{}
	a := &Agent{runtime: rt}
	prepared := a.prefetchCatalog(context.Background(), Request{PrefetchCatalogIDs: []string{"table:app:main.support_tickets"}})
	if prepared == nil || len(prepared.reads) != 1 {
		t.Fatalf("prepared = %+v", prepared)
	}
	if ctx := mapValue(prepared.context()); len(anySlice(ctx["detail_ids"])) != 1 && len(prepared.stats.DetailIDs) != 1 {
		t.Fatalf("prepared context = %v", ctx)
	}
	protocol := newProtocolRuntime(rt, "Close duplicate ticket 1.", "", 8, nil, nil, CatalogSearchFeatures{})
	if _, err := prepared.consume(protocol); err != nil {
		t.Fatal(err)
	}
	if !protocol.state.modelDiscoveryAction || !protocol.state.tablesDetailed["support_tickets"] {
		t.Fatalf("discovery=%v detailed=%v", protocol.state.modelDiscoveryAction, protocol.state.tablesDetailed)
	}
	// A detail the catalog does not return prepares nothing.
	empty := &fakeRuntime{catalogOverride: func(map[string]any) any { return map[string]any{"cards": []any{}} }}
	if got := (&Agent{runtime: empty}).prefetchCatalog(context.Background(), Request{PrefetchCatalogIDs: []string{"table:app:main.gone"}}); got != nil {
		t.Fatalf("an unknown table prepared %+v", got)
	}
}
