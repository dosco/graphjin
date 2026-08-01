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
			Name: "business_code",
			Kind: sourcecap.KindCode,
			Capabilities: map[string]bool{
				sourcecap.KeyCodeRead: true,
			},
		}},
	})
	card, ok := findCatalogCard(snap, "source:business_code")
	if !ok {
		t.Fatalf("source card not found: %+v", snap.Cards)
	}
	if !strings.Contains(card.EvidenceJSON, "supported_capabilities") ||
		!strings.Contains(card.EvidenceJSON, sourcecap.KeyCodeRead) ||
		!strings.Contains(card.EvidenceJSON, sourcecap.EnforcementConfigAudit) {
		t.Fatalf("source card should expose registry-derived capability guidance: %s", card.EvidenceJSON)
	}
	if !strings.Contains(card.ExamplesJSON, sourcecap.KeyCodeRead) {
		t.Fatalf("source examples should come from registry, got %s", card.ExamplesJSON)
	}
	if card.OwnerSource != "business_code" || card.OwnerSourcesJSON != `["business_code"]` {
		t.Fatalf("source card owner fields = %q %s", card.OwnerSource, card.OwnerSourcesJSON)
	}
}

func TestCodeSourceCatalogFallbackCards(t *testing.T) {
	snap := BuildWithOptions(&MetadataSnapshot{}, nil, BuildOptions{
		EnabledTools: []string{"query_catalog", "execute_graphql"},
		Sources: []Source{{
			Name:     "business_code",
			Kind:     sourcecap.KindCode,
			ReadOnly: true,
			Capabilities: map[string]bool{
				sourcecap.KeyCodeSearch:      true,
				sourcecap.KeyCodeRead:        true,
				sourcecap.KeyCodeInferDBRefs: true,
			},
		}},
	})

	table, ok := findCatalogCard(snap, "table:business_code:main.gj_code")
	if !ok {
		t.Fatalf("expected gj_code table card for code source")
	}
	if table.Kind != "table" || table.DatabaseName != "business_code" || table.TableName != "gj_code" || table.SourceKind != sourcecap.KindCode {
		t.Fatalf("unexpected code table card: %+v", table)
	}
	if table.OwnerSource != "business_code" || table.OwnerSourcesJSON != `["business_code"]` {
		t.Fatalf("unexpected code table ownership: %+v", table)
	}

	if _, ok := findCatalogCard(snap, "column:business_code:main.gj_code.code_context"); !ok {
		t.Fatalf("expected code_context column card")
	}
	entry, ok := findCatalogCard(snap, "entrypoint.code:business_code.symbols")
	if !ok {
		t.Fatalf("expected code source entrypoint card")
	}
	if entry.Kind != "entrypoint" || !strings.Contains(entry.QueryJSON, "business_code") {
		t.Fatalf("unexpected code entrypoint card: %+v", entry)
	}

	result, err := snap.Query(Query{Where: map[string]any{"database_name": map[string]any{"eq": "business_code"}}, Limit: 5})
	if err != nil {
		t.Fatalf("query code source catalog rows: %v", err)
	}
	if len(result.Cards) == 0 {
		t.Fatal("expected database_name filter to find code source catalog cards")
	}

	result, err = snap.Query(Query{Search: "business_code code symbols", Limit: 10})
	if err != nil {
		t.Fatalf("search code source catalog rows: %v", err)
	}
	if len(result.Cards) == 0 {
		t.Fatal("expected code-source search to find catalog cards")
	}
}

func TestCodeSourceCatalogFallbackDoesNotDuplicateCachedDDL(t *testing.T) {
	snap := BuildWithOptions(&MetadataSnapshot{
		Tables: []MetadataTable{{
			ID:           "business_code:main.gj_code",
			DatabaseName: "business_code",
			SchemaName:   "main",
			TableName:    "gj_code",
			ColumnCount:  1,
		}},
		Columns: []MetadataColumn{{
			ID:           "business_code:main.gj_code.kind",
			TableID:      "business_code:main.gj_code",
			DatabaseName: "business_code",
			SchemaName:   "main",
			TableName:    "gj_code",
			ColumnName:   "kind",
			Type:         "text",
		}},
	}, nil, BuildOptions{
		Sources: []Source{{Name: "business_code", Kind: sourcecap.KindCode}},
	})

	if countCatalogCards(snap, "table:business_code:main.gj_code") != 1 {
		t.Fatalf("expected cached DDL table card not to be duplicated")
	}
	if countCatalogCards(snap, "column:business_code:main.gj_code.kind") != 1 {
		t.Fatalf("expected cached DDL column card not to be duplicated")
	}
	if countCatalogCards(snap, "entrypoint.code:business_code.symbols") != 1 {
		t.Fatalf("expected one code entrypoint card")
	}
}

func TestSchemaCardsIncludeSourceOwnership(t *testing.T) {
	snap := BuildWithOptions(&MetadataSnapshot{
		Databases: []MetadataDatabase{{Name: "app", Type: "postgres"}, {Name: "billing", Type: "postgres"}},
		Tables: []MetadataTable{
			{ID: "app.public.users", DatabaseName: "app", SchemaName: "public", TableName: "users", ColumnCount: 1},
			{ID: "billing.public.invoices", DatabaseName: "billing", SchemaName: "public", TableName: "invoices", ColumnCount: 1},
		},
		Columns: []MetadataColumn{
			{ID: "app.public.users.id", TableID: "app.public.users", DatabaseName: "app", SchemaName: "public", TableName: "users", ColumnName: "id", Type: "int"},
			{ID: "billing.public.invoices.user_id", TableID: "billing.public.invoices", DatabaseName: "billing", SchemaName: "public", TableName: "invoices", ColumnName: "user_id", Type: "int"},
		},
		Relationships: []MetadataRelationship{{
			ID:               "billing.public.invoices.user_id->app.public.users.id",
			FromDatabaseName: "billing",
			FromSchemaName:   "public",
			FromTableName:    "invoices",
			FromColumnName:   "user_id",
			FromColumnID:     "billing.public.invoices.user_id",
			ToDatabaseName:   "app",
			ToSchemaName:     "public",
			ToTableName:      "users",
			ToColumnName:     "id",
			ToColumnID:       "app.public.users.id",
		}},
	}, nil, BuildOptions{})

	table, ok := findCatalogCard(snap, "table:app.public.users")
	if !ok || table.OwnerSource != "app" || table.OwnerSourcesJSON != `["app"]` {
		t.Fatalf("table ownership = %+v, ok=%v", table, ok)
	}
	column, ok := findCatalogCard(snap, "column:billing.public.invoices.user_id")
	if !ok || column.OwnerSource != "billing" || column.OwnerSourcesJSON != `["billing"]` {
		t.Fatalf("column ownership = %+v, ok=%v", column, ok)
	}
	relationship, ok := findCatalogCard(snap, "relationship:billing.public.invoices.user_id->app.public.users.id")
	if !ok || relationship.OwnerSource != "app" || relationship.OwnerSourcesJSON != `["app","billing"]` {
		t.Fatalf("relationship ownership = %+v, ok=%v", relationship, ok)
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
	for _, want := range []string{`gj_config(id: \"current\", update: { ... })`, "singleton_id", "update_sources", "remove_sources", "source_patch_semantics", "mcp_fields", "normal GraphQL errors"} {
		if !strings.Contains(configCap.InputSchemaJSON, want) {
			t.Fatalf("expected gj_config input schema to mention %q, got %s", want, configCap.InputSchemaJSON)
		}
	}
	for _, want := range []string{`"serv"`, `"scope"`, `"reload_mode"`, `"reload_strategy"`} {
		if !strings.Contains(configCap.OutputSchemaJSON, want) {
			t.Fatalf("expected gj_config output schema to advertise %q, got %s", want, configCap.OutputSchemaJSON)
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

	for _, id := range []string{"help:discovery", "help:mcp_tools", "help:query", "help:mutations", "help:saved_queries", "help:workflow_runtime", "help:security", "help:errors", "help:artifacts", "help:watches", "help:refusals"} {
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

	for _, oldTool := range []string{"get_query_syntax", "get_catalog_card", "get_js_runtime_api", "get_config_docs", "fix_query_error", "list_saved_queries", "get_fragment"} {
		result, err := snap.Query(Query{Search: oldTool, Where: map[string]any{"kind": map[string]any{"eq": "help"}}, Limit: 10})
		if err != nil {
			t.Fatalf("search help rows for %s: %v", oldTool, err)
		}
		found := false
		for _, card := range result.Cards {
			details := detailsForCard(snap, card.ID)
			for _, detail := range details {
				if strings.Contains(detail.Content, oldTool) || strings.Contains(detail.DataJSON, oldTool) {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			t.Fatalf("expected help row search for legacy tool %s, got %+v", oldTool, result.Cards)
		}
	}
}

func TestBuildIncludesConfigRecipes(t *testing.T) {
	snap := BuildWithOptions(&MetadataSnapshot{}, nil, BuildOptions{EnabledTools: []string{"query_catalog"}})

	for _, id := range []string{
		"recipe.config.identity_claims",
		"recipe.config.add_role",
		"recipe.config.source_access_defaults",
		"recipe.config.table_classifications",
		"recipe.config.graphjin_roots",
		"recipe.config.enable_artifacts",
		"recipe.config.enable_watches",
		"recipe.config.migrate_legacy_roles_tables",
		"recipe.config.rate_limiting",
		"recipe.config.agent_tuning",
		"recipe.config.enable_jwt_auth",
		"recipe.config.enable_redis_caching",
		"recipe.config.enable_uploads",
		"recipe.config.production_hardening",
	} {
		card, ok := findCatalogCard(snap, id)
		if !ok {
			t.Fatalf("expected config recipe %s", id)
		}
		if card.Kind != "config_recipe" || card.SafetyJSON == "" || card.ExamplesJSON == "" || card.QueryJSON == "" {
			t.Fatalf("config recipe missing machine guidance: %+v", card)
		}
		text := card.Summary + card.EvidenceJSON + card.ExamplesJSON + card.SafetyJSON
		for _, want := range []string{"preflight", "verify", "stop_conditions", "forbidden_patterns", "no dry-run"} {
			if !strings.Contains(strings.ToLower(text), strings.ToLower(want)) {
				t.Fatalf("recipe %s missing %q in guidance: %s", id, want, text)
			}
		}
		if !strings.Contains(text, "gj_security") || !strings.Contains(text, "gj_runtime") || !strings.Contains(text, "gj_config") {
			t.Fatalf("recipe %s should mention security/runtime/config preflight: %s", id, text)
		}
		for _, forbidden := range []string{"connection_string", "private_key_pem", "password:"} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("recipe %s leaked forbidden config term %q: %s", id, forbidden, text)
			}
		}
		if len(detailsForCard(snap, id)) == 0 {
			t.Fatalf("expected config recipe details for %s", id)
		}
		if card.GraphQLMutation != "" {
			for _, field := range []string{"scope", "reload_mode", "reload_strategy"} {
				if !strings.Contains(card.GraphQLMutation, field) {
					t.Fatalf("config recipe %s mutation should select %s: %s", id, field, card.GraphQLMutation)
				}
			}
		}
	}
	if !hasEntrypoint(snap, "discover_config_security") {
		t.Fatal("expected discover_config_security entrypoint")
	}
	for _, id := range []string{"recipe.config.source_access_defaults", "recipe.config.table_classifications"} {
		card, ok := findCatalogCard(snap, id)
		if !ok {
			t.Fatalf("expected recipe %s", id)
		}
		text := card.SafetyJSON + card.ExamplesJSON + card.GraphQLMutation
		for _, want := range []string{"source_patches", "preview", "apply", "preview_id"} {
			if !strings.Contains(text, want) {
				t.Fatalf("recipe %s should expose preview/apply source_patches with %q: %s", id, want, text)
			}
		}
		if strings.Contains(text, "Direct source patch-by-name is not supported") || strings.Contains(text, "not supported yet") {
			t.Fatalf("recipe %s should not mark source_patches as unsupported: %s", id, text)
		}
	}
	rootRecipe, ok := findCatalogCard(snap, "recipe.config.graphjin_roots")
	if !ok {
		t.Fatal("expected system root recipe")
	}
	rootText := rootRecipe.SafetyJSON + rootRecipe.ExamplesJSON + rootRecipe.GraphQLMutation
	for _, want := range []string{"system", "root_access", "preview", "apply", "preview_id"} {
		if !strings.Contains(rootText, want) {
			t.Fatalf("system root recipe should expose feature preview/apply with %q: %s", want, rootText)
		}
	}
	if strings.Contains(rootText, "source_patches") || strings.Contains(rootText, "roots_set") {
		t.Fatalf("system root recipe still models built-in roots as a source patch: %s", rootText)
	}
	for _, id := range []string{"recipe.config.enable_artifacts", "recipe.config.enable_watches"} {
		card, ok := findCatalogCard(snap, id)
		if !ok {
			t.Fatalf("expected config recipe %s", id)
		}
		text := card.Summary + card.ExamplesJSON + card.SafetyJSON
		for _, want := range []string{"dev", "agentic"} {
			if !strings.Contains(text, want) {
				t.Fatalf("recipe %s missing %s zero-config guidance: %s", id, want, text)
			}
		}
	}
	agentRecipe, ok := findCatalogCard(snap, "recipe.config.agent_tuning")
	if !ok {
		t.Fatal("expected agent tuning recipe")
	}
	if !strings.Contains(agentRecipe.ExamplesJSON, "missing server credentials fail closed") {
		t.Fatalf("agent tuning recipe lacks server-credential guidance: %s", agentRecipe.ExamplesJSON)
	}
}

func TestConfigRecipeSearchRanking(t *testing.T) {
	snap := BuildWithOptions(&MetadataSnapshot{}, nil, BuildOptions{EnabledTools: []string{"query_catalog"}})

	tests := []struct {
		search string
		want   string
	}{
		{search: "add role from jwt", want: "recipe.config.add_role"},
		{search: "make audit_logs admin only", want: "recipe.config.table_classifications"},
		{search: "user scoped artifacts", want: "recipe.config.enable_artifacts"},
		{search: "enable watches", want: "recipe.config.enable_watches"},
		{search: "roles tables filters presets", want: "recipe.config.migrate_legacy_roles_tables"},
	}
	for _, tt := range tests {
		result, err := snap.Query(Query{Search: tt.search, Limit: 5, Explain: true})
		if err != nil {
			t.Fatalf("query catalog for %q: %v", tt.search, err)
		}
		if len(result.Cards) == 0 {
			t.Fatalf("expected results for %q", tt.search)
		}
		if result.Cards[0].ID != tt.want {
			t.Fatalf("search %q ranked %s first, want %s; results=%+v", tt.search, result.Cards[0].ID, tt.want, result.Cards)
		}
		if result.Matches[tt.want].Score <= 0 || !strings.Contains(result.Matches[tt.want].Why, "config recipe") {
			t.Fatalf("expected config recipe intent boost for %q, got %+v", tt.search, result.Matches[tt.want])
		}
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
	sources1 := SourceRevisions(&MetadataSnapshot{
		Tables: []MetadataTable{{ID: "default.public.orders", DatabaseName: "default", SchemaName: "public", TableName: "orders"}},
	}, nil, opts)
	sources2 := SourceRevisions(&MetadataSnapshot{
		Tables: []MetadataTable{{ID: "default.public.customers", DatabaseName: "default", SchemaName: "public", TableName: "customers"}},
	}, nil, opts)
	rev1 := RevisionFromSourceRevisions(sources1)
	rev2 := RevisionFromSourceRevisions(sources2)
	if rev1 == rev2 {
		t.Fatalf("expected schema metadata change to change revision")
	}
	if sources1["schema:default"] == "" || sources2["schema:default"] == "" || sources1["schema:default"] == sources2["schema:default"] {
		t.Fatalf("expected default source schema revision to change: %q -> %q", sources1["schema:default"], sources2["schema:default"])
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

func TestConfigRecipesApplyVsUnsupported(t *testing.T) {
	snap := BuildWithOptions(&MetadataSnapshot{}, nil, BuildOptions{EnabledTools: []string{"query_catalog"}})

	// serv-writable settings carry a real gj_config apply payload (with a serv patch)
	for _, id := range []string{"recipe.config.rate_limiting", "recipe.config.agent_tuning"} {
		card, ok := findCatalogCard(snap, id)
		if !ok {
			t.Fatalf("missing recipe %s", id)
		}
		if card.GraphQLMutation == "" || !strings.Contains(card.GraphQLMutation, "serv:") {
			t.Fatalf("recipe %s should carry a serv-patch apply payload, got %q", id, card.GraphQLMutation)
		}
	}

	// secret-bearing / cross-cutting settings are unsupported via gj_config and
	// route to config-file/CLI guidance instead of an apply payload
	for _, id := range []string{"recipe.config.enable_jwt_auth", "recipe.config.enable_redis_caching", "recipe.config.enable_uploads", "recipe.config.production_hardening"} {
		card, ok := findCatalogCard(snap, id)
		if !ok {
			t.Fatalf("missing recipe %s", id)
		}
		if card.GraphQLMutation != "" {
			t.Fatalf("recipe %s should not carry an apply mutation (secret/cross-cutting), got %q", id, card.GraphQLMutation)
		}
		if !strings.Contains(card.SafetyJSON, "unsupported_apply") {
			t.Fatalf("recipe %s should carry unsupported_apply guidance", id)
		}
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

func countCatalogCards(snap *Snapshot, id string) int {
	count := 0
	for _, card := range snap.Cards {
		if card.ID == id {
			count++
		}
	}
	return count
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
