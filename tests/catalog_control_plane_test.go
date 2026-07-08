package tests_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/serv/v3"
)

func TestCatalogGraphQLDiscoveryIntegration(t *testing.T) {
	gjs := newCatalogControlPlaneService(t, serv.MCPConfig{})
	gj := gjs.GetGraphJin()

	data := runControlPlaneGraphQL(t, gj, `query {
		table_items: gj_catalog(search: "users", where: { kind: { eq: "table" } }, order_by: { score: desc }, limit: 5) {
			id
			kind
			name
			title
				table_name
				score
				detail_ref
				query_json
			}
		entrypoint_items: gj_catalog(where: { kind: { eq: "entrypoint" }, name: { eq: "discover_workflows" } }, limit: 1) {
			kind
			name
			query_json
		}
		system_capability_items: gj_catalog(where: { kind: { eq: "system_capability" }, name: { eq: "validate_where_clause" } }, limit: 1) {
			kind
			name
			enabled
			safety_json
		}
	}`)

	var out struct {
		Items []struct {
			ID        string  `json:"id"`
			Kind      string  `json:"kind"`
			Name      string  `json:"name"`
			Title     string  `json:"title"`
			TableName string  `json:"table_name"`
			Score     float64 `json:"score"`
			DetailRef string  `json:"detail_ref"`
		} `json:"table_items"`
		EntryPoints []struct {
			Kind      string `json:"kind"`
			Name      string `json:"name"`
			QueryJSON string `json:"query_json"`
		} `json:"entrypoint_items"`
		Capabilities []struct {
			Kind       string `json:"kind"`
			Name       string `json:"name"`
			Enabled    bool   `json:"enabled"`
			SafetyJSON string `json:"safety_json"`
		} `json:"system_capability_items"`
	}
	decodeControlPlaneJSON(t, data, &out)

	if len(out.Items) == 0 {
		t.Fatalf("expected catalog table items, got %s", string(data))
	}
	if out.Items[0].Kind != "table" || out.Items[0].TableName != "users" || out.Items[0].Name != "users" {
		t.Fatalf("expected users table item first, got %+v", out.Items[0])
	}
	if out.Items[0].Score <= 0 || out.Items[0].DetailRef == "" {
		t.Fatalf("expected scored item with detail ref, got %+v", out.Items[0])
	}
	if !strings.Contains(string(data), `"query_json":null`) {
		t.Fatalf("expected wide optional fields to be returned as null when absent, got %s", string(data))
	}
	if len(out.EntryPoints) != 1 || out.EntryPoints[0].Kind != "entrypoint" || out.EntryPoints[0].Name != "discover_workflows" || !strings.Contains(out.EntryPoints[0].QueryJSON, `"workflow"`) {
		t.Fatalf("expected discover_workflows entrypoint, got %+v", out.EntryPoints)
	}
	if len(out.Capabilities) != 1 || out.Capabilities[0].Kind != "system_capability" || !out.Capabilities[0].Enabled || !strings.Contains(out.Capabilities[0].SafetyJSON, "true") {
		t.Fatalf("expected where validation system capability, got %+v", out.Capabilities)
	}
}

func TestCatalogGraphQLWorkflowMutationIntegration(t *testing.T) {
	gjs := newCatalogControlPlaneService(t, serv.MCPConfig{AllowWorkflowUpdates: true})
	gj := gjs.GetGraphJin()

	createData := runControlPlaneGraphQL(t, gj, `mutation {
		gj_workflow(insert: {
			name: "catalog_integration_workflow"
			description: "Integration workflow for catalog discovery"
			tags: ["integration", "catalog"]
			variables: [{ name: "customer_id", type: "number", required: true }]
			code: "function main(input) { return { customer_id: input.customer_id, ok: true }; }"
		}) {
			name
			description
			source_hash
			catalog_item_id
			catalog_revision
		}
	}`)

	var created struct {
		Workflow struct {
			Name            string `json:"name"`
			Description     string `json:"description"`
			SourceHash      string `json:"source_hash"`
			CatalogItemID   string `json:"catalog_item_id"`
			CatalogRevision string `json:"catalog_revision"`
		} `json:"gj_workflow"`
	}
	decodeControlPlaneJSON(t, createData, &created)
	if created.Workflow.Name != "catalog_integration_workflow" ||
		created.Workflow.Description == "" ||
		created.Workflow.SourceHash == "" ||
		created.Workflow.CatalogItemID != "workflow:catalog_integration_workflow" ||
		created.Workflow.CatalogRevision == "" {
		t.Fatalf("unexpected workflow create response: %+v", created.Workflow)
	}

	catalogData := runControlPlaneGraphQL(t, gj, `query {
		gj_catalog(search: "catalog integration workflow", where: { kind: { eq: "workflow" } }, order_by: { score: desc }, limit: 5) {
			id
			kind
			title
			summary
			score
		}
		gj_workflow(where: { name: { eq: "catalog_integration_workflow" } }, limit: 1) {
			name
			code
			source_hash
		}
	}`)

	var discovered struct {
		Items []struct {
			ID      string  `json:"id"`
			Kind    string  `json:"kind"`
			Title   string  `json:"title"`
			Summary string  `json:"summary"`
			Score   float64 `json:"score"`
		} `json:"gj_catalog"`
		Sources []struct {
			Name       string `json:"name"`
			Code       string `json:"code"`
			SourceHash string `json:"source_hash"`
		} `json:"gj_workflow"`
	}
	decodeControlPlaneJSON(t, catalogData, &discovered)
	if len(discovered.Items) == 0 || discovered.Items[0].ID != "workflow:catalog_integration_workflow" || discovered.Items[0].Kind != "workflow" {
		t.Fatalf("expected workflow catalog item, got %+v", discovered.Items)
	}
	if strings.Contains(discovered.Items[0].Summary, "function main") {
		t.Fatalf("workflow catalog summary leaked source code: %+v", discovered.Items[0])
	}
	if len(discovered.Sources) != 1 ||
		discovered.Sources[0].Name != "catalog_integration_workflow" ||
		!strings.Contains(discovered.Sources[0].Code, "function main") ||
		discovered.Sources[0].SourceHash != created.Workflow.SourceHash {
		t.Fatalf("expected workflow source through gated source root, got %+v", discovered.Sources)
	}

	runData := runControlPlaneGraphQL(t, gj, `mutation {
		gj_workflow_execution(insert: {
			workflow_name: "catalog_integration_workflow"
			variables: { customer_id: 42 }
		}) {
			workflow_name
			status
			result_json
			error
		}
	}`)
	var ran struct {
		Run struct {
			WorkflowName string `json:"workflow_name"`
			Status       string `json:"status"`
			ResultJSON   string `json:"result_json"`
			Error        string `json:"error"`
		} `json:"gj_workflow_execution"`
	}
	decodeControlPlaneJSON(t, runData, &ran)
	if ran.Run.WorkflowName != "catalog_integration_workflow" ||
		ran.Run.Status != "ok" ||
		!strings.Contains(ran.Run.ResultJSON, `"customer_id":42`) ||
		ran.Run.Error != "" {
		t.Fatalf("unexpected workflow run response: %+v", ran.Run)
	}

	updateData := runControlPlaneGraphQL(t, gj, `mutation {
		gj_workflow(where: { name: { eq: "catalog_integration_workflow" } }, update: {
			description: "Updated integration workflow"
			tags: ["integration", "updated"]
			variables: [{ name: "customer_id", type: "number", required: true }]
			code: "function main(input) { return { customer_id: input.customer_id, updated: true }; }"
		}) {
			name
			description
			source_hash
			catalog_revision
		}
	}`)
	var updated struct {
		Workflow struct {
			Name            string `json:"name"`
			Description     string `json:"description"`
			SourceHash      string `json:"source_hash"`
			CatalogRevision string `json:"catalog_revision"`
		} `json:"gj_workflow"`
	}
	decodeControlPlaneJSON(t, updateData, &updated)
	if updated.Workflow.Name != "catalog_integration_workflow" ||
		updated.Workflow.Description != "Updated integration workflow" ||
		updated.Workflow.SourceHash == "" ||
		updated.Workflow.SourceHash == created.Workflow.SourceHash ||
		updated.Workflow.CatalogRevision == "" {
		t.Fatalf("unexpected workflow update response: %+v", updated.Workflow)
	}

	deleteData := runControlPlaneGraphQL(t, gj, `mutation {
		gj_workflow(where: { name: { eq: "catalog_integration_workflow" } }, delete: true) {
			name
			deleted
			catalog_revision
		}
	}`)
	var deleted struct {
		Workflow struct {
			Name            string `json:"name"`
			Deleted         bool   `json:"deleted"`
			CatalogRevision string `json:"catalog_revision"`
		} `json:"gj_workflow"`
	}
	decodeControlPlaneJSON(t, deleteData, &deleted)
	if deleted.Workflow.Name != "catalog_integration_workflow" || !deleted.Workflow.Deleted || deleted.Workflow.CatalogRevision == "" {
		t.Fatalf("unexpected workflow delete response: %+v", deleted.Workflow)
	}
}

func TestCatalogGraphQLControlMutationHelpersIntegration(t *testing.T) {
	gjs := newCatalogControlPlaneService(t, serv.MCPConfig{AllowConfigUpdates: true})
	gj := gjs.GetGraphJin()

	revision := controlPlaneIntegrationConfigRevision(t, gj)
	previewData := runControlPlaneGraphQL(t, gj, fmt.Sprintf(`mutation {
		gj_config(id: "current", update: {
			mode: "preview",
			expected_catalog_revision: %q,
			mcp: { allow_raw_queries: true }
		}) {
			valid
			preview_id
			expires_at
			errors_json
		}
	}`, revision))
	var preview struct {
		Config struct {
			Valid     bool   `json:"valid"`
			PreviewID string `json:"preview_id"`
			ExpiresAt string `json:"expires_at"`
		} `json:"gj_config"`
	}
	decodeControlPlaneJSON(t, previewData, &preview)
	if !preview.Config.Valid || preview.Config.PreviewID == "" || preview.Config.ExpiresAt == "" {
		t.Fatalf("unexpected config preview response: %+v", preview.Config)
	}

	configData := runControlPlaneGraphQL(t, gj, fmt.Sprintf(`mutation {
		gj_config(id: "current", update: {
			mode: "apply",
			preview_id: %q,
			expected_catalog_revision: %q,
			mcp: { allow_raw_queries: true }
		}) {
			applied
			mcp
			catalog_revision
		}
	}`, preview.Config.PreviewID, revision))
	var patched struct {
		Config struct {
			Applied         bool           `json:"applied"`
			MCP             map[string]any `json:"mcp"`
			CatalogRevision string         `json:"catalog_revision"`
		} `json:"gj_config"`
	}
	decodeControlPlaneJSON(t, configData, &patched)
	if !patched.Config.Applied || patched.Config.CatalogRevision == "" {
		t.Fatalf("unexpected config update response: %+v", patched.Config)
	}
	if got, _ := patched.Config.MCP["allow_raw_queries"].(bool); !got {
		t.Fatalf("expected config update to return mcp.allow_raw_queries=true, got %+v", patched.Config.MCP)
	}

	for _, query := range []string{
		`mutation { gj_schema_reloads(insert: {}) { id } }`,
		`mutation { gj_schema_change_sets(insert: { action: "preview" }) { id } }`,
		`mutation { gj_query_validations(insert: { table: "users", where: { id: { eq: 1 } } }) { id } }`,
		`mutation { gj_query_repairs(insert: { query: "query { userz { id } }", error: "table not found: userz" }) { id } }`,
	} {
		if _, err := gj.GraphQL(context.Background(), query, nil, &core.RequestConfig{}); err == nil {
			t.Fatalf("expected removed control-plane mutation root to be unavailable: %s", query)
		}
	}
}

func controlPlaneIntegrationConfigRevision(t *testing.T, gj *core.GraphJin) string {
	t.Helper()
	data := runControlPlaneGraphQL(t, gj, `query {
		gj_config(id: "current") { catalog_revision }
	}`)
	var out struct {
		Config struct {
			CatalogRevision string `json:"catalog_revision"`
		} `json:"gj_config"`
	}
	decodeControlPlaneJSON(t, data, &out)
	if out.Config.CatalogRevision == "" {
		t.Fatal("expected config catalog_revision")
	}
	return out.Config.CatalogRevision
}

func newCatalogControlPlaneService(t *testing.T, mcp serv.MCPConfig) *serv.HttpService {
	t.Helper()
	skipCassandra(t, "catalog/control-plane discovery needs SQL-path features (counts, DDL) CQL lacks")
	skipClickHouse(t, "catalog/control-plane discovery needs SQL-path features (counts, DDL) the DSL driver lacks")
	if db == nil {
		t.Skip("catalog control-plane integration tests require -db")
	}

	dir := t.TempDir()
	coreConf := newConfig(&core.Config{
		Mode:             "dev",
		DBType:           dbType,
		DisableAllowList: true,
		Roles:            []core.Role{{Name: "admin"}},
		Sources: []core.SourceConfig{
			{Name: core.DefaultDBName, Kind: "database", Type: dbType, Default: true, Access: core.SourceAccessConfig{
				Read:  core.AccessModeAuthenticated,
				Write: core.AccessModeAuthenticated,
			}},
			{Name: "graphjin", Kind: "graphjin"},
			{Name: "workflows", Kind: "workflow"},
		},
	})
	svcConf := &serv.Config{
		Core: *coreConf,
		Serv: serv.Serv{
			ConfigPath: filepath.Join(dir, "config"),
			MCP:        mcp,
		},
	}

	gjs, err := serv.NewGraphJinService(
		svcConf,
		serv.OptionSetDB(db),
		serv.OptionSetFS(core.NewOsFS(dir)),
	)
	if err != nil {
		t.Fatalf("new graphjin service: %v", err)
	}
	return gjs
}

func runControlPlaneGraphQL(t *testing.T, gj *core.GraphJin, query string) []byte {
	t.Helper()

	res, err := gj.GraphQL(sourceModeIntegrationAdminContext(), query, nil, nil)
	if err != nil {
		t.Fatalf("graphql execution error: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("graphql returned errors: %+v\n%s", res.Errors, string(res.Data))
	}
	return res.Data
}

func decodeControlPlaneJSON(t *testing.T, data []byte, out any) {
	t.Helper()
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("decode graphql response: %v\n%s", err, string(data))
	}
}
