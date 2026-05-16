package catalog

import (
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/sourcecap"
)

func TestBuildWithOptionsWorkflowCards(t *testing.T) {
	snap := BuildWithOptions(&MetadataSnapshot{}, nil, BuildOptions{
		EnabledTools: []string{"query_catalog", "get_catalog_card", "execute_workflow"},
		Workflows: []Workflow{{
			Name:        "customer_margin",
			Description: "Compute gross margin for one customer",
			Tags:        []string{"finance", "customers", "margin"},
			Variables: []WorkflowVariable{{
				Name:        "customer_id",
				Type:        "number",
				Description: "Customer id to analyze",
				Required:    true,
			}},
			Path:           "workflows/customer_margin.js",
			SourceHash:     "abc123",
			Runtime:        "goja",
			TimeoutSeconds: 12,
			CreatedAt:      "2026-01-01T00:00:00Z",
			UpdatedAt:      "2026-01-02T00:00:00Z",
		}},
	})

	card, ok := findCatalogCard(snap, "workflow:customer_margin")
	if !ok {
		t.Fatalf("workflow card not found: %+v", snap.Cards)
	}
	if card.Kind != "workflow" {
		t.Fatalf("expected workflow kind, got %q", card.Kind)
	}
	if card.Summary != "Compute gross margin for one customer" {
		t.Fatalf("unexpected summary: %q", card.Summary)
	}
	if strings.Contains(card.EvidenceJSON, "function main") {
		t.Fatalf("workflow source code should not be in evidence: %s", card.EvidenceJSON)
	}
	if card.CreatedAt != "2026-01-01T00:00:00Z" || card.UpdatedAt != "2026-01-02T00:00:00Z" {
		t.Fatalf("expected workflow lifecycle timestamps on card, got created=%q updated=%q", card.CreatedAt, card.UpdatedAt)
	}

	details := detailsForCard(snap, card.ID)
	if len(details) == 0 {
		t.Fatal("expected workflow card details")
	}
	if !strings.Contains(details[0].DataJSON, `"customer_id"`) {
		t.Fatalf("expected workflow variable metadata in details: %s", details[0].DataJSON)
	}
	if !strings.Contains(details[0].DataJSON, `"created_at":"2026-01-01T00:00:00Z"`) ||
		!strings.Contains(details[0].DataJSON, `"updated_at":"2026-01-02T00:00:00Z"`) {
		t.Fatalf("expected workflow timestamps in details: %s", details[0].DataJSON)
	}
	if strings.Contains(details[0].DataJSON, "function main") || strings.Contains(details[0].Content, "function main") {
		t.Fatalf("workflow source code should not be in details: %+v", details[0])
	}

	if !hasNode(snap, "node:workflow:customer_margin", "workflow") {
		t.Fatalf("expected workflow node")
	}
	if hasEdge(snap, "node:workflow:customer_margin", "capability.execute_workflow", "uses_capability") {
		t.Fatalf("workflow card should not link to execution capability")
	}
	if !hasCapability(snap, "capability.gj_workflow_execution.insert") {
		t.Fatalf("expected GraphQL workflow execution capability to remain in capability list")
	}
	if _, ok := findCatalogCard(snap, "capability.gj_workflow_execution.insert"); ok {
		t.Fatalf("did not expect GraphQL workflow execution capability as catalog card")
	}
	if strings.Contains(card.ExamplesJSON, "gj_workflow_execution") || strings.Contains(card.SuggestedNext, "execute_workflow") {
		t.Fatalf("workflow card should not include execution actions: %+v", card)
	}
	if !hasEntrypoint(snap, "discover_workflows") {
		t.Fatalf("expected discover_workflows entrypoint")
	}
}

func TestSourceCardsUseCapabilityRegistry(t *testing.T) {
	snap := BuildWithOptions(&MetadataSnapshot{}, nil, BuildOptions{
		Sources: []Source{{
			Name: "graphjin",
			Kind: sourcecap.KindGraphJin,
			Capabilities: map[string]bool{
				sourcecap.KeySecurityRead: true,
			},
		}},
	})
	card, ok := findCatalogCard(snap, "source:graphjin")
	if !ok {
		t.Fatalf("source card not found: %+v", snap.Cards)
	}
	if !strings.Contains(card.EvidenceJSON, "supported_capabilities") ||
		!strings.Contains(card.EvidenceJSON, sourcecap.KeySecurityRead) ||
		!strings.Contains(card.EvidenceJSON, sourcecap.EnforcementRuntime) {
		t.Fatalf("source card should expose registry-derived capability guidance: %s", card.EvidenceJSON)
	}
	if !strings.Contains(card.ExamplesJSON, sourcecap.KeySecurityRead) {
		t.Fatalf("source examples should come from registry, got %s", card.ExamplesJSON)
	}
}

func TestWorkflowCardsSortByLifecycleFields(t *testing.T) {
	snap := BuildWithOptions(&MetadataSnapshot{}, nil, BuildOptions{
		Workflows: []Workflow{
			{Name: "old", SourceHash: "old", CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-02T00:00:00Z"},
			{Name: "new", SourceHash: "new", CreatedAt: "2026-01-03T00:00:00Z", UpdatedAt: "2026-01-04T00:00:00Z"},
		},
	})

	result, err := snap.Query(Query{
		Where:   map[string]any{"kind": map[string]any{"eq": "workflow"}},
		OrderBy: map[string]string{"updated_at": "desc"},
	})
	if err != nil {
		t.Fatalf("query workflow catalog: %v", err)
	}
	if len(result.Cards) != 2 || result.Cards[0].ID != "workflow:new" || result.Cards[1].ID != "workflow:old" {
		t.Fatalf("expected updated_at desc order, got %+v", result.Cards)
	}

	result, err = snap.Query(Query{
		Where:   map[string]any{"kind": map[string]any{"eq": "workflow"}},
		OrderBy: map[string]string{"created_on": "asc"},
	})
	if err != nil {
		t.Fatalf("query workflow catalog by alias: %v", err)
	}
	if len(result.Cards) != 2 || result.Cards[0].ID != "workflow:old" || result.Cards[1].ID != "workflow:new" {
		t.Fatalf("expected created_on asc order, got %+v", result.Cards)
	}
}

func TestWorkflowCardsSortByLifecycleFieldsParsesRFC3339Nano(t *testing.T) {
	snap := BuildWithOptions(&MetadataSnapshot{}, nil, BuildOptions{
		Workflows: []Workflow{
			{Name: "zero", SourceHash: "zero", CreatedAt: "2026-01-01T00:00:00Z"},
			{Name: "nano", SourceHash: "nano", CreatedAt: "2026-01-01T00:00:00.000000001Z"},
		},
	})

	result, err := snap.Query(Query{
		Where:   map[string]any{"kind": map[string]any{"eq": "workflow"}},
		OrderBy: map[string]string{"created_at": "asc"},
	})
	if err != nil {
		t.Fatalf("query workflow catalog by timestamp: %v", err)
	}
	if len(result.Cards) != 2 || result.Cards[0].ID != "workflow:zero" || result.Cards[1].ID != "workflow:nano" {
		t.Fatalf("expected parsed timestamp order, got %+v", result.Cards)
	}
}

func TestWorkflowCardsAreSearchableByMetadata(t *testing.T) {
	snap := BuildWithOptions(&MetadataSnapshot{}, nil, BuildOptions{
		EnabledTools: []string{"query_catalog", "get_catalog_card", "execute_workflow"},
		Workflows: []Workflow{{
			Name:        "customer_margin",
			Description: "Compute gross margin for one customer",
			Tags:        []string{"finance", "customers", "margin"},
			Variables: []WorkflowVariable{{
				Name:        "customer_id",
				Type:        "number",
				Description: "Customer id to analyze",
				Required:    true,
			}},
			Path:       "workflows/customer_margin.js",
			SourceHash: "abc123",
		}},
	})

	result, err := snap.Query(Query{
		Search:  "finance customer_id gross margin",
		Where:   map[string]any{"kind": map[string]any{"eq": "workflow"}},
		OrderBy: map[string]string{"score": "desc"},
		Explain: true,
		Limit:   5,
	})
	if err != nil {
		t.Fatalf("query workflow catalog: %v", err)
	}
	if len(result.Cards) == 0 || result.Cards[0].ID != "workflow:customer_margin" {
		t.Fatalf("expected workflow search result first, got %+v", result.Cards)
	}
	match := result.Matches["workflow:customer_margin"]
	if match.Score <= 0 {
		t.Fatalf("expected positive match score, got %+v", match)
	}
	if strings.Contains(match.Why, "function main") || strings.Contains(strings.Join(match.MatchedTerms, " "), "function main") {
		t.Fatalf("match explanation leaked source-like text: %+v", match)
	}
}

func TestBuildWithOptionsFragmentCards(t *testing.T) {
	snap := BuildWithOptions(&MetadataSnapshot{
		Tables: []MetadataTable{{
			ID:           "default.public.users",
			DatabaseName: "default",
			SchemaName:   "public",
			TableName:    "users",
		}},
	}, nil, BuildOptions{
		EnabledTools: []string{"query_catalog", "get_catalog_card", "get_fragment"},
		Fragments: []Fragment{{
			Name:       "user_fields",
			Namespace:  "shop",
			Definition: "fragment UserFields on users { id email }",
			On:         "users",
		}},
	})

	card, ok := findCatalogCard(snap, "fragment:shop.user_fields")
	if !ok {
		t.Fatalf("fragment card not found: %+v", snap.Cards)
	}
	if card.Kind != "fragment" || card.Title != "shop.user_fields" || card.TableName != "users" {
		t.Fatalf("unexpected fragment card: %+v", card)
	}
	for _, want := range []string{`"namespace":"shop"`, `#import \"./fragments/shop.user_fields\"`, `"on":"users"`, `"source_hash"`} {
		if !strings.Contains(card.EvidenceJSON, want) {
			t.Fatalf("expected evidence to contain %q, got %s", want, card.EvidenceJSON)
		}
	}

	details := detailsForCard(snap, card.ID)
	if len(details) != 1 || details[0].Section != "fragment_definition" || !strings.Contains(details[0].DataJSON, "fragment UserFields on users") {
		t.Fatalf("expected fragment definition detail, got %+v", details)
	}
	if !hasEntrypoint(snap, "discover_fragments") {
		t.Fatalf("expected discover_fragments entrypoint")
	}
	if !hasEdge(snap, "node:fragment:shop.user_fields", "node:table:default.public.users", "applies_to") {
		t.Fatalf("expected fragment-to-table edge")
	}
}

func TestFragmentRevisionChangesWithDefinition(t *testing.T) {
	base := &MetadataSnapshot{}
	opts := BuildOptions{
		Fragments: []Fragment{{
			Name:       "user_fields",
			Definition: "fragment UserFields on users { id }",
			On:         "users",
		}},
	}
	rev1 := RevisionFromSourceRevisions(SourceRevisions(base, nil, opts))
	opts.Fragments[0].Definition = "fragment UserFields on users { id email }"
	rev2 := RevisionFromSourceRevisions(SourceRevisions(base, nil, opts))
	if rev1 == rev2 {
		t.Fatalf("expected fragment definition change to change revision")
	}
}

func TestActionCapabilitiesAreNotCatalogCards(t *testing.T) {
	snap := BuildWithOptions(&MetadataSnapshot{}, nil, BuildOptions{
		EnabledTools: []string{
			"query_catalog",
			"get_catalog_card",
			"execute_workflow",
			"save_workflow",
			"update_current_config",
			"reload_schema",
			"preview_schema_changes",
			"apply_schema_changes",
			"validate_where_clause",
			"fix_query_error",
			"execute_graphql",
		},
	})

	for _, id := range []string{
		"capability.gj_workflow_execution.insert",
		"capability.gj_workflow.write",
		"capability.gj_config.update",
		"capability.reload_schema",
		"capability.preview_schema_changes",
		"capability.apply_schema_changes",
		"capability.execute_workflow",
		"capability.save_workflow",
		"capability.execute_graphql",
	} {
		if _, ok := findCatalogCard(snap, id); ok {
			t.Fatalf("action capability %s should not be emitted as a catalog card", id)
		}
		if !hasCapability(snap, id) {
			t.Fatalf("action capability %s should remain available outside card projection", id)
		}
	}
	for _, id := range []string{
		"capability.gj_query_validations.insert",
		"capability.gj_query_repairs.insert",
		"capability.gj_schema_reloads.insert",
		"capability.gj_schema_change_sets.insert",
	} {
		if hasCapability(snap, id) {
			t.Fatalf("removed GraphQL capability %s should not be advertised", id)
		}
	}
	if _, ok := findCatalogCard(snap, "capability.query_catalog"); !ok {
		t.Fatalf("expected read-only catalog capability card")
	}

	configCap := findCapability(snap, "capability.gj_config.update")
	if configCap == nil {
		t.Fatal("expected gj_config update capability")
	}
	for _, want := range []string{`gj_config(id: \"current\", update: { ... })`, "singleton_id", "mcp_fields", "normal GraphQL errors"} {
		if !strings.Contains(configCap.InputSchemaJSON, want) {
			t.Fatalf("expected gj_config input schema to mention %q, got %s", want, configCap.InputSchemaJSON)
		}
	}
}

func TestKnownEmptyToolManifestDoesNotInventToolCapabilities(t *testing.T) {
	snap := BuildWithOptions(&MetadataSnapshot{}, nil, BuildOptions{
		EnabledToolsKnown: true,
		Workflows: []Workflow{{
			Name:       "daily_report",
			SourceHash: "abc123",
		}},
	})
	for _, card := range snap.Cards {
		if card.ID == "capability.execute_workflow" || card.ID == "capability.query_catalog" {
			t.Fatalf("did not expect disabled MCP tool capability card: %+v", card)
		}
	}
	if _, ok := findCatalogCard(snap, "capability.catalog_samples_profiles"); !ok {
		t.Fatalf("expected non-tool catalog sample/profile capability")
	}
	workflow, ok := findCatalogCard(snap, "workflow:daily_report")
	if !ok {
		t.Fatalf("expected workflow card")
	}
	if strings.Contains(workflow.SuggestedNext, "execute_workflow") || strings.Contains(workflow.ExamplesJSON, "execute_workflow") {
		t.Fatalf("workflow card should not recommend disabled execute_workflow: %+v", workflow)
	}
	if hasEdge(snap, "node:workflow:daily_report", "capability.execute_workflow", "uses_capability") {
		t.Fatalf("workflow card should not link to disabled execute_workflow capability")
	}
}

func TestBuildIncludesHelpRows(t *testing.T) {
	snap := BuildWithOptions(&MetadataSnapshot{}, nil, BuildOptions{EnabledTools: []string{"query_catalog", "validate_where_clause", "execute_saved_query"}})

	for _, id := range []string{"help:discovery", "help:query", "help:mutations", "help:saved_queries", "help:workflow_runtime", "help:security", "help:errors"} {
		card, ok := findCatalogCard(snap, id)
		if !ok {
			t.Fatalf("expected help card %s", id)
		}
		if card.Kind != "help" || card.QueryJSON == "" || card.SafetyJSON == "" || card.GraphQLQuery == "" {
			t.Fatalf("help card should include query/safety/graphql guidance: %+v", card)
		}
		if len(detailsForCard(snap, id)) == 0 {
			t.Fatalf("expected help details for %s", id)
		}
	}

	result, err := snap.Query(Query{Where: map[string]any{"kind": map[string]any{"eq": "help"}}, Limit: 50})
	if err != nil {
		t.Fatalf("query help rows: %v", err)
	}
	if len(result.Cards) < len(helpTopics) {
		t.Fatalf("expected at least %d help rows, got %d", len(helpTopics), len(result.Cards))
	}
}

func TestBuildWithOptionsSavedQueryCards(t *testing.T) {
	snap := BuildWithOptions(&MetadataSnapshot{}, nil, BuildOptions{
		EnabledTools: []string{"query_catalog", "execute_saved_query"},
		SavedQueries: []SavedQuery{{
			Name:      "users_by_id",
			Namespace: "shop",
			Operation: "query",
			Query:     `query { users(id: $id) { id email } }`,
			Variables: map[string]any{"id": "number"},
		}},
	})

	card, ok := findCatalogCard(snap, "saved_query:shop.users_by_id")
	if !ok {
		t.Fatalf("expected saved query card")
	}
	if card.Kind != "saved_query" || card.InputSchemaJSON == "" || card.SafetyJSON == "" || !strings.Contains(card.SuggestedNext, "execute_saved_query") {
		t.Fatalf("saved query card missing execution guidance: %+v", card)
	}
	if !strings.Contains(card.GraphQLQuery, "users") {
		t.Fatalf("expected saved query text in GraphQLQuery: %+v", card)
	}
}

func TestWorkflowRevisionChangesWithWorkflowSourceHash(t *testing.T) {
	base := &MetadataSnapshot{}
	opts := BuildOptions{
		EnabledTools: []string{"query_catalog", "execute_workflow"},
		Workflows: []Workflow{{
			Name:       "customer_margin",
			SourceHash: "abc123",
		}},
	}
	rev1 := RevisionFromSourceRevisions(SourceRevisions(base, nil, opts))
	opts.Workflows[0].SourceHash = "def456"
	rev2 := RevisionFromSourceRevisions(SourceRevisions(base, nil, opts))
	if rev1 == rev2 {
		t.Fatalf("expected workflow source hash change to change revision")
	}
}

func TestRevisionChangesWithSchemaMetadata(t *testing.T) {
	opts := BuildOptions{EnabledTools: []string{"query_catalog"}}
	rev1 := RevisionFromSourceRevisions(SourceRevisions(&MetadataSnapshot{
		Tables: []MetadataTable{{ID: "default.public.orders", DatabaseName: "default", SchemaName: "public", TableName: "orders"}},
	}, nil, opts))
	rev2 := RevisionFromSourceRevisions(SourceRevisions(&MetadataSnapshot{
		Tables: []MetadataTable{{ID: "default.public.customers", DatabaseName: "default", SchemaName: "public", TableName: "customers"}},
	}, nil, opts))
	if rev1 == rev2 {
		t.Fatalf("expected schema metadata change to change revision")
	}
}

func TestRevisionChangesWithConfigMapValue(t *testing.T) {
	type testConfig struct {
		Roles map[string]map[string]bool `json:"roles"`
	}
	opts := BuildOptions{EnabledTools: []string{"query_catalog"}}
	rev1 := RevisionFromSourceRevisions(SourceRevisions(&MetadataSnapshot{}, testConfig{
		Roles: map[string]map[string]bool{"user": {"read": true}},
	}, opts))
	rev2 := RevisionFromSourceRevisions(SourceRevisions(&MetadataSnapshot{}, testConfig{
		Roles: map[string]map[string]bool{"user": {"read": false}},
	}, opts))
	if rev1 == rev2 {
		t.Fatalf("expected config map value change to change revision")
	}
}

func findCatalogCard(snap *Snapshot, id string) (Card, bool) {
	for _, card := range snap.Cards {
		if card.ID == id {
			return card, true
		}
	}
	return Card{}, false
}

func hasCapability(snap *Snapshot, id string) bool {
	for _, cap := range snap.Capabilities {
		if cap.ID == id {
			return true
		}
	}
	return false
}

func findCapability(snap *Snapshot, id string) *Capability {
	for i := range snap.Capabilities {
		if snap.Capabilities[i].ID == id {
			return &snap.Capabilities[i]
		}
	}
	return nil
}

func detailsForCard(snap *Snapshot, id string) []CardDetail {
	var out []CardDetail
	for _, detail := range snap.Details {
		if detail.CardID == id {
			out = append(out, detail)
		}
	}
	return out
}

func hasNode(snap *Snapshot, id, kind string) bool {
	for _, node := range snap.Nodes {
		if node.ID == id && node.Kind == kind {
			return true
		}
	}
	return false
}

func hasEdge(snap *Snapshot, from, to, kind string) bool {
	for _, edge := range snap.Edges {
		if edge.FromID == from && edge.ToID == to && edge.Kind == kind {
			return true
		}
	}
	return false
}

func hasEntrypoint(snap *Snapshot, name string) bool {
	for _, ep := range snap.EntryPoints {
		if ep.Name == name {
			return true
		}
	}
	return false
}
