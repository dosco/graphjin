package serv

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/spf13/afero"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap/zaptest"
)

func TestQueryCatalogReturnsWorkflowCards(t *testing.T) {
	ms := workflowCatalogTestServer(t, MCPConfig{}, map[string]string{
		"order_pnl.js": `// @graphjin-workflow {"description":"Compute P&L from orders","tags":["orders","finance","pnl"],"variables":[{"name":"customer_id","type":"number","required":true}]}
function main(input) { return {secretSource:"do-not-index-source"}; }
`,
	})

	res, err := ms.handleQueryCatalog(sourceModeUserTestContext(), newToolRequest(map[string]any{
		"where": map[string]any{"kind": map[string]any{"eq": "workflow"}},
		"limit": 10,
	}))
	if err != nil {
		t.Fatalf("query workflow catalog: %v", err)
	}
	text := assertToolSuccess(t, res)
	if strings.Contains(text, "do-not-index-source") || strings.Contains(text, "function main") {
		t.Fatalf("workflow source should not be exposed in query_catalog output: %s", text)
	}

	var out CatalogQueryResult
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("decode query_catalog response: %v", err)
	}
	if out.Count != 1 {
		t.Fatalf("expected 1 workflow card, got %d: %+v", out.Count, out.Cards)
	}
	if out.Cards[0].ID != "workflow:order_pnl" {
		t.Fatalf("expected order_pnl workflow card, got %+v", out.Cards[0])
	}
	if out.Revision == "" {
		t.Fatalf("expected catalog revision metadata")
	}
}

func TestQueryCatalogByIDReturnsWorkflowDetails(t *testing.T) {
	ms := workflowCatalogTestServer(t, MCPConfig{}, map[string]string{
		"order_pnl.js": `// @graphjin-workflow {"description":"Compute P&L from orders","tags":["orders","finance","pnl"],"variables":[{"name":"customer_id","type":"number","description":"Customer to analyze","required":true}]}
function main(input) { return {}; }
`,
	})

	res, err := ms.handleQueryCatalog(sourceModeUserTestContext(), newToolRequest(map[string]any{
		"id": "workflow:order_pnl",
	}))
	if err != nil {
		t.Fatalf("query workflow card: %v", err)
	}
	text := assertToolSuccess(t, res)

	var out CatalogQueryResult
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("decode query_catalog response: %v", err)
	}
	if out.Count != 1 || out.Cards[0].Kind != "workflow" {
		t.Fatalf("expected one workflow card, got %+v", out.Cards)
	}
	card := out.Cards[0]
	if strings.Contains(card.SuggestedNext, "execute_workflow") || strings.Contains(card.ExamplesJSON, "gj_workflow_execution") {
		t.Fatalf("workflow catalog card should not expose execution actions: %+v", card)
	}
	if !strings.Contains(card.DetailsJSON, "customer_id") {
		t.Fatalf("expected workflow variable metadata in details_json: %+v", card)
	}
	if card.EdgesJSON != "" && card.EdgesJSON != "[]" && card.EdgesJSON != "null" {
		t.Fatalf("did not expect workflow action edges: %+v", card.EdgesJSON)
	}
}

func TestGraphQLHelpTopicsUseCatalogGraphQL(t *testing.T) {
	ms := workflowCatalogTestServer(t, MCPConfig{}, nil)

	for _, topic := range graphQLHelpTopics() {
		t.Run(topic, func(t *testing.T) {
			res, err := ms.handleGraphQLHelp(sourceModeUserTestContext(), newToolRequest(map[string]any{"for": topic}))
			if err != nil {
				t.Fatalf("graphql_help: %v", err)
			}
			text := assertToolSuccess(t, res)
			var out GraphQLHelpResult
			if err := json.Unmarshal([]byte(text), &out); err != nil {
				t.Fatalf("decode graphql_help: %v\n%s", err, text)
			}
			if out.For != topic || out.GraphQLQuery == "" || !strings.Contains(out.GraphQLQuery, "gj_catalog") {
				t.Fatalf("expected graphql query guidance for %s, got %+v", topic, out)
			}
			if out.RecommendedFirstQuery == "" {
				t.Fatalf("expected recommended first query for %s", topic)
			}
			if len(out.Bootstrap) == 0 {
				t.Fatalf("expected bootstrap guidance for %s", topic)
			}
			if out.GraphQLVariables == nil {
				t.Fatalf("expected stable graphql_variables map")
			}
			if len(out.CatalogRows) == 0 {
				t.Fatalf("expected catalog rows for %s", topic)
			}
			if out.Next == nil || out.Next.RecommendedTool == "" {
				t.Fatalf("expected next guidance for %s: %+v", topic, out.Next)
			}
			if out.CapabilityProfile == nil {
				t.Fatalf("expected caller capability_profile for %s", topic)
			}
			if !stringSliceContains(out.CapabilityProfile.AvailableTools, "query_catalog") {
				t.Fatalf("expected query_catalog in capability_profile for %s: %+v", topic, out.CapabilityProfile)
			}
			if strings.Contains(text, "app-user") || strings.Contains(text, "app-account") {
				t.Fatalf("graphql_help leaked plaintext identity values for %s: %s", topic, text)
			}
			wantID := "help:" + topic
			foundHelp := false
			for _, row := range out.CatalogRows {
				if row.ID == wantID {
					foundHelp = true
					break
				}
			}
			if !foundHelp {
				t.Fatalf("expected %s in graphql_help rows: %+v", wantID, out.CatalogRows)
			}
			if topic == "discovery" {
				if len(out.TopicRoutes) == 0 || len(out.ReplacesTools) == 0 {
					t.Fatalf("expected discovery help to include topic routes and replacement map: %+v", out)
				}
				foundMCPTools := false
				foundOldTool := false
				for _, route := range out.TopicRoutes {
					if route.For == "mcp_tools" && strings.Contains(route.DetailQuery, "help:mcp_tools") {
						foundMCPTools = true
						break
					}
				}
				for _, repl := range out.ReplacesTools {
					if repl.Tool == "get_query_syntax" && strings.Contains(repl.Replacement, "graphql_help") {
						foundOldTool = true
						break
					}
				}
				if !foundMCPTools || !foundOldTool {
					t.Fatalf("discovery help missing mcp_tools route or old tool replacement: routes=%+v replacements=%+v", out.TopicRoutes, out.ReplacesTools)
				}
			}
		})
	}
}

func TestQueryCatalogByIDReturnsHelpDetails(t *testing.T) {
	ms := workflowCatalogTestServer(t, MCPConfig{}, nil)

	res, err := ms.handleQueryCatalog(sourceModeUserTestContext(), newToolRequest(map[string]any{"id": "help:query"}))
	if err != nil {
		t.Fatalf("query help card: %v", err)
	}
	text := assertToolSuccess(t, res)
	var out CatalogQueryResult
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("decode query_catalog response: %v", err)
	}
	if out.Count != 1 || out.Cards[0].ID != "help:query" {
		t.Fatalf("expected help:query detail row, got %+v", out.Cards)
	}
	card := out.Cards[0]
	for name, value := range map[string]string{
		"details_json":  card.DetailsJSON,
		"evidence_json": card.EvidenceJSON,
		"examples_json": card.ExamplesJSON,
		"safety_json":   card.SafetyJSON,
	} {
		if strings.TrimSpace(value) == "" {
			t.Fatalf("expected %s on help detail row: %+v", name, card)
		}
	}
}

func TestCatalogCacheReusesRevisionAndUpdatesAfterSaveWorkflow(t *testing.T) {
	ms := workflowCatalogTestServer(t, MCPConfig{AllowWorkflowUpdates: true}, nil)
	s := ms.service

	first, err := s.catalogSnapshot()
	if err != nil {
		t.Fatalf("first catalog snapshot: %v", err)
	}
	second, err := s.catalogSnapshot()
	if err != nil {
		t.Fatalf("second catalog snapshot: %v", err)
	}
	if first != second {
		t.Fatalf("expected repeated catalog reads to reuse cached snapshot")
	}
	if first.Revision != second.Revision {
		t.Fatalf("expected cached revision to be stable: %q != %q", first.Revision, second.Revision)
	}

	res, err := ms.handleSaveWorkflow(context.Background(), newToolRequest(map[string]any{
		"name":        "customer_margin",
		"description": "Compute margin by customer",
		"code":        "function main(input) { return {ok: true}; }\n",
		"tags":        []any{"finance", "customers"},
		"variables": []any{
			map[string]any{"name": "customer_id", "type": "number", "required": true},
		},
	}))
	if err != nil {
		t.Fatalf("save workflow: %v", err)
	}
	assertToolSuccess(t, res)

	updated, err := s.catalogSnapshot()
	if err != nil {
		t.Fatalf("updated catalog snapshot: %v", err)
	}
	if updated == second {
		t.Fatalf("expected save_workflow to invalidate cached snapshot")
	}
	if updated.Revision == second.Revision {
		t.Fatalf("expected save_workflow to change catalog revision")
	}

	result, err := updated.QueryResult(coreCatalogWorkflowQuery())
	if err != nil {
		t.Fatalf("query updated workflow catalog: %v", err)
	}
	if len(result.Cards) != 1 || result.Cards[0].ID != "workflow:customer_margin" {
		t.Fatalf("expected saved workflow to be discoverable, got %+v", result.Cards)
	}
}

func TestCatalogCacheRevisionStableWhenRemovedSourceWorkflowFlagChanges(t *testing.T) {
	ms := workflowCatalogTestServer(t, MCPConfig{}, nil)
	s := ms.service

	first, err := s.catalogSnapshot()
	if err != nil {
		t.Fatalf("first catalog snapshot: %v", err)
	}

	s.conf.MCP.AllowWorkflowExecution = true
	updated, err := s.catalogSnapshot()
	if err != nil {
		t.Fatalf("updated catalog snapshot: %v", err)
	}
	if updated.Revision != first.Revision {
		t.Fatalf("expected removed sources-used workflow flag to leave catalog revision unchanged")
	}
	if updated.SourceRevisions["tools"] != first.SourceRevisions["tools"] {
		t.Fatalf("expected tools source revision to remain unchanged")
	}
}

func TestCatalogCacheRevisionChangesWhenConfigChanges(t *testing.T) {
	ms := workflowCatalogTestServer(t, MCPConfig{}, nil)
	s := ms.service

	first, err := s.catalogSnapshot()
	if err != nil {
		t.Fatalf("first catalog snapshot: %v", err)
	}
	s.conf.Core.DefaultBlock = !s.conf.Core.DefaultBlock

	updated, err := s.catalogSnapshot()
	if err != nil {
		t.Fatalf("updated catalog snapshot: %v", err)
	}
	if updated.Revision == first.Revision {
		t.Fatalf("expected config change to change catalog revision")
	}
	if updated.SourceRevisions["config"] == first.SourceRevisions["config"] {
		t.Fatalf("expected config source revision to change")
	}
}

func TestCatalogCapabilitiesReflectDisabledMCP(t *testing.T) {
	ms := workflowCatalogTestServer(t, MCPConfig{Disable: true}, nil)
	snap, err := ms.service.catalogSnapshot()
	if err != nil {
		t.Fatalf("catalog snapshot: %v", err)
	}
	for _, cap := range snap.Capabilities {
		if cap.Name == "execute_workflow" || cap.Name == "query_catalog" {
			t.Fatalf("did not expect disabled MCP tool capability: %+v", cap)
		}
	}
	if _, ok := snap.Card("capability.catalog_samples_profiles"); !ok {
		t.Fatalf("expected non-tool catalog sample/profile capability")
	}
}

func TestCatalogCacheQueryResultsRemainDeterministic(t *testing.T) {
	ms := workflowCatalogTestServer(t, MCPConfig{}, map[string]string{
		"zulu.js": `// @graphjin-workflow {"description":"Zulu workflow","tags":["z"]}
function main(input) { return {}; }
`,
		"alpha.js": `// @graphjin-workflow {"description":"Alpha workflow","tags":["a"]}
function main(input) { return {}; }
`,
	})
	s := ms.service

	first, err := s.catalogSnapshot()
	if err != nil {
		t.Fatalf("first catalog snapshot: %v", err)
	}
	second, err := s.catalogSnapshot()
	if err != nil {
		t.Fatalf("second catalog snapshot: %v", err)
	}

	q := coreCatalogWorkflowQuery()
	left, err := first.QueryResult(q)
	if err != nil {
		t.Fatalf("query first snapshot: %v", err)
	}
	right, err := second.QueryResult(q)
	if err != nil {
		t.Fatalf("query second snapshot: %v", err)
	}
	if len(left.Cards) != len(right.Cards) {
		t.Fatalf("result counts differ: %d vs %d", len(left.Cards), len(right.Cards))
	}
	for i := range left.Cards {
		if left.Cards[i].ID != right.Cards[i].ID {
			t.Fatalf("result order differs at %d: %s vs %s", i, left.Cards[i].ID, right.Cards[i].ID)
		}
	}
	if len(left.Cards) != 2 || left.Cards[0].ID != "workflow:alpha" || left.Cards[1].ID != "workflow:zulu" {
		t.Fatalf("expected deterministic workflow id order, got %+v", left.Cards)
	}
}

func TestWorkflowCatalogSanitizesManualMetadata(t *testing.T) {
	ms := workflowCatalogTestServer(t, MCPConfig{}, map[string]string{
		"manual.js": `// @graphjin-workflow {"description":"  Manual workflow  ","tags":[" ops ","ops",""],"variables":[{"name":"bad-name","type":"string"},{"name":"customer_id","type":" number ","description":" Customer id ","required":true},{"name":"customer_id","type":"string"}]}
function main(input) { return {}; }
`,
	})

	snap, err := ms.service.catalogSnapshot()
	if err != nil {
		t.Fatalf("catalog snapshot: %v", err)
	}
	details := snap.CardDetails("workflow:manual")
	if len(details) == 0 {
		t.Fatalf("expected workflow details")
	}
	if strings.Contains(details[0].DataJSON, "bad-name") {
		t.Fatalf("invalid variable name should not be cataloged: %s", details[0].DataJSON)
	}
	if strings.Count(details[0].DataJSON, "customer_id") != 1 {
		t.Fatalf("duplicate variable should be collapsed: %s", details[0].DataJSON)
	}
	if !strings.Contains(details[0].DataJSON, `"tags":["ops"]`) {
		t.Fatalf("tags should be trimmed and deduplicated: %s", details[0].DataJSON)
	}
}

func workflowCatalogTestServer(t *testing.T, cfg MCPConfig, files map[string]string) *mcpServer {
	t.Helper()

	mem := afero.NewMemMapFs()
	if len(files) != 0 {
		if err := mem.MkdirAll("/workflows", 0o755); err != nil {
			t.Fatal(err)
		}
		for name, src := range files {
			if err := afero.WriteFile(mem, "/workflows/"+name, []byte(src), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	s := &graphjinService{
		fs:     newAferoFS(mem, "/"),
		log:    zaptest.NewLogger(t).Sugar(),
		tracer: otel.Tracer("graphjin-workflow-catalog-test"),
		conf: &Config{
			Core: core.Config{
				Sources: []core.SourceConfig{
					{Name: "graphjin", Kind: "graphjin"},
					{Name: "workflows", Kind: "workflow"},
				},
			},
			Serv: Serv{MCP: cfg},
		},
	}
	if err := normalizeServiceSources(s.conf); err != nil {
		t.Fatalf("normalize workflow catalog test sources: %v", err)
	}
	if err := s.normalStart(); err != nil {
		t.Fatalf("start workflow catalog test service: %v", err)
	}
	t.Cleanup(func() { closeTestService(s) })
	return &mcpServer{service: s, ctx: context.Background()}
}

func coreCatalogWorkflowQuery() core.CatalogQuery {
	return core.CatalogQuery{
		Where: map[string]any{"kind": map[string]any{"eq": "workflow"}},
		Limit: 10,
	}
}
