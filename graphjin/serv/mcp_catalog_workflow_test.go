package serv

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/spf13/afero"
)

func TestQueryCatalogReturnsWorkflowCards(t *testing.T) {
	ms := workflowCatalogTestServer(t, MCPConfig{}, map[string]string{
		"order_pnl.js": `// @graphjin-workflow {"description":"Compute P&L from orders","tags":["orders","finance","pnl"],"variables":[{"name":"customer_id","type":"number","required":true}]}
function main(input) { return {secretSource:"do-not-index-source"}; }
`,
	})

	res, err := ms.handleQueryCatalog(context.Background(), newToolRequest(map[string]any{
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
	if out.Revision == "" || out.SourceRevisions["workflows"] == "" {
		t.Fatalf("expected catalog revision metadata, got revision=%q sources=%+v", out.Revision, out.SourceRevisions)
	}
}

func TestGetCatalogCardWorkflowDetails(t *testing.T) {
	ms := workflowCatalogTestServer(t, MCPConfig{}, map[string]string{
		"order_pnl.js": `// @graphjin-workflow {"description":"Compute P&L from orders","tags":["orders","finance","pnl"],"variables":[{"name":"customer_id","type":"number","description":"Customer to analyze","required":true}]}
function main(input) { return {}; }
`,
	})

	res, err := ms.handleGetCatalogCard(context.Background(), newToolRequest(map[string]any{
		"id": "workflow:order_pnl",
	}))
	if err != nil {
		t.Fatalf("get workflow card: %v", err)
	}
	text := assertToolSuccess(t, res)

	var out CatalogCardResult
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("decode get_catalog_card response: %v", err)
	}
	if out.Card.Kind != "workflow" {
		t.Fatalf("expected workflow card, got %+v", out.Card)
	}
	if strings.Contains(out.Card.SuggestedNext, "execute_workflow") || strings.Contains(out.Card.ExamplesJSON, "gj_workflow_execution") {
		t.Fatalf("workflow catalog card should not expose execution actions: %+v", out.Card)
	}
	if len(out.Details) == 0 || !strings.Contains(out.Details[0].DataJSON, "customer_id") {
		t.Fatalf("expected workflow variable metadata in details: %+v", out.Details)
	}
	if len(out.Edges) != 0 {
		t.Fatalf("did not expect workflow action edges: %+v", out.Edges)
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

func TestCatalogCacheRevisionChangesWhenEnabledToolsChange(t *testing.T) {
	ms := workflowCatalogTestServer(t, MCPConfig{}, nil)
	s := ms.service

	first, err := s.catalogSnapshot()
	if err != nil {
		t.Fatalf("first catalog snapshot: %v", err)
	}

	s.conf.MCP.AllowRawQueries = true
	updated, err := s.catalogSnapshot()
	if err != nil {
		t.Fatalf("updated catalog snapshot: %v", err)
	}
	if updated.Revision == first.Revision {
		t.Fatalf("expected enabled tool manifest change to alter catalog revision")
	}
	if updated.SourceRevisions["tools"] == first.SourceRevisions["tools"] {
		t.Fatalf("expected tools source revision to change")
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
		fs: newAferoFS(mem, "/"),
		conf: &Config{
			Core: core.Config{
				Sources: []core.SourceConfig{
					{Name: "graphjin", Kind: "graphjin"},
					{Name: "workflows", Kind: "workflows"},
				},
			},
			Serv: Serv{MCP: cfg},
		},
	}
	return &mcpServer{service: s, ctx: context.Background()}
}

func coreCatalogWorkflowQuery() core.CatalogQuery {
	return core.CatalogQuery{
		Where: map[string]any{"kind": map[string]any{"eq": "workflow"}},
		Limit: 10,
	}
}
