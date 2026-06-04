package serv

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/core/v3/sourcecap"
	"github.com/spf13/afero"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap/zaptest"
	_ "modernc.org/sqlite"
)

func TestGraphQLControlPlaneCatalogQuery(t *testing.T) {
	svc := newControlPlaneGraphQLTestService(t, MCPConfig{}, createSQLiteDBFile(t, "app.sqlite3", true))

	res, err := svc.gj.GraphQL(sourceModeUserTestContext(), `query {
		gj_catalog(search: "users", where: { kind: { eq: "table" } }, order_by: { search_rank: desc }, limit: 5) {
			id
			kind
			name
			table_name
			search_rank
		}
	}`, nil, &core.RequestConfig{})
	if err != nil {
		t.Fatalf("control-plane catalog query error: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("control-plane catalog query returned errors: %+v", res.Errors)
	}

	var out struct {
		Items []struct {
			ID        string  `json:"id"`
			Kind      string  `json:"kind"`
			Name      string  `json:"name"`
			TableName string  `json:"table_name"`
			Score     float64 `json:"search_rank"`
		} `json:"gj_catalog"`
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		t.Fatalf("decode catalog query: %v\n%s", err, string(res.Data))
	}
	if len(out.Items) == 0 {
		t.Fatalf("expected catalog items, got %s", string(res.Data))
	}
	if out.Items[0].Kind != "table" || out.Items[0].TableName != "users" || out.Items[0].Name != "users" {
		t.Fatalf("expected users table item first, got %+v", out.Items[0])
	}
	if out.Items[0].Score <= 0 {
		t.Fatalf("expected positive search score, got %+v", out.Items[0])
	}
}

func TestGraphQLControlPlaneCatalogIncludesFragments(t *testing.T) {
	svc := newControlPlaneGraphQLTestService(t, MCPConfig{}, createSQLiteDBFile(t, "app.sqlite3", true))
	if err := svc.fs.Put("/queries/fragments/shop.user_fields.gql", []byte("fragment UserFields on users { id email }")); err != nil {
		t.Fatalf("write fragment: %v", err)
	}
	if err := svc.refreshSystemNanoDB(); err != nil {
		t.Fatalf("refresh system nanodb: %v", err)
	}

	res, err := svc.gj.GraphQL(sourceModeUserTestContext(), `query {
		gj_catalog(where: { kind: { eq: "fragment" } }) {
			id
			kind
			name
			table_name
			details_json
			evidence_json
		}
	}`, nil, &core.RequestConfig{})
	if err != nil {
		t.Fatalf("fragment catalog query error: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("fragment catalog query returned errors: %+v", res.Errors)
	}

	var out struct {
		Items []struct {
			ID           string `json:"id"`
			Kind         string `json:"kind"`
			Name         string `json:"name"`
			TableName    string `json:"table_name"`
			DetailsJSON  string `json:"details_json"`
			EvidenceJSON string `json:"evidence_json"`
		} `json:"gj_catalog"`
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		t.Fatalf("decode fragment catalog query: %v\n%s", err, string(res.Data))
	}
	if len(out.Items) != 1 {
		t.Fatalf("expected one fragment item, got %+v", out.Items)
	}
	item := out.Items[0]
	if item.ID != "fragment:shop.user_fields" || item.Kind != "fragment" || item.Name != "shop.user_fields" || item.TableName != "users" {
		t.Fatalf("unexpected fragment catalog item: %+v", item)
	}
	if !strings.Contains(item.DetailsJSON, "fragment UserFields on users") || !strings.Contains(item.EvidenceJSON, "shop.user_fields") {
		t.Fatalf("expected fragment details/evidence, got %+v", item)
	}
}

func TestGraphQLControlPlaneCatalogIncludesSourceRows(t *testing.T) {
	svc := newControlPlaneGraphQLTestService(t, MCPConfig{}, createSQLiteDBFile(t, "app.sqlite3", true))

	res, err := svc.gj.GraphQL(sourceModeUserTestContext(), `query {
		gj_catalog(where: { kind: { eq: "source" } }, order_by: { source_kind: asc }) {
			id
			kind
			name
			source
			source_kind
			owner_source
			owner_sources_json
			evidence_json
			examples_json
			safety_json
		}
	}`, nil, &core.RequestConfig{})
	if err != nil {
		t.Fatalf("catalog source rows query error: %v", err)
	}
	var out struct {
		Items []struct {
			ID           string `json:"id"`
			Kind         string `json:"kind"`
			Name         string `json:"name"`
			Source       string `json:"source"`
			SourceKind   string `json:"source_kind"`
			OwnerSource  string `json:"owner_source"`
			OwnerSources string `json:"owner_sources_json"`
			EvidenceJSON string `json:"evidence_json"`
			ExamplesJSON string `json:"examples_json"`
			SafetyJSON   string `json:"safety_json"`
		} `json:"gj_catalog"`
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		t.Fatalf("decode catalog source rows: %v\n%s", err, string(res.Data))
	}
	kinds := map[string]bool{}
	for _, item := range out.Items {
		if item.Kind != "source" || item.SourceKind == "" || !strings.Contains(item.EvidenceJSON, `"source_kind":"`+item.SourceKind+`"`) ||
			!strings.Contains(item.EvidenceJSON, "supported_capabilities") ||
			item.OwnerSource != item.Source || !strings.Contains(item.OwnerSources, `"`+item.Source+`"`) {
			t.Fatalf("unexpected source row: %+v", item)
		}
		kinds[item.SourceKind] = true
	}
	for _, kind := range []string{"database", "graphjin", "workflow"} {
		if !kinds[kind] {
			t.Fatalf("missing source kind %q in rows %+v", kind, out.Items)
		}
	}
}

func TestSystemNanoDBRefreshForSourcesPatchesOnlyOwnedCatalogRows(t *testing.T) {
	svc := newControlPlaneGraphQLTestService(t, MCPConfig{}, createSQLiteDBFile(t, "app.sqlite3", true))

	if err := core.UpdateNanoDB(svc.systemNanoDB, func(tx *core.NanoUpdate) error {
		if err := tx.ReplaceRows("main", "gj_catalog", nil, []core.NanoRow{
			{"id": "table:main.public.stale", "kind": "table", "title": "stale", "owner_source": "main", "owner_sources_json": `["main"]`},
			{"id": "table:other.public.keep", "kind": "table", "title": "keep", "owner_source": "other", "owner_sources_json": `["other"]`},
		}); err != nil {
			return err
		}
		return tx.ReplaceRows("main", "gj_security", nil, []core.NanoRow{
			{"id": "finding:stale", "kind": "finding", "summary_json": `{"stale":true}`},
		})
	}); err != nil {
		t.Fatalf("seed catalog rows: %v", err)
	}
	if err := svc.refreshSystemNanoDBForSources([]string{"main"}); err != nil {
		t.Fatalf("source scoped refresh: %v", err)
	}

	rows, ok := svc.systemNanoDB.Snapshot().Rows("main", "gj_catalog")
	if !ok {
		t.Fatal("missing gj_catalog rows")
	}
	ids := map[string]bool{}
	for _, row := range rows {
		ids[fmt.Sprint(row["id"])] = true
	}
	if ids["table:main.public.stale"] {
		t.Fatalf("stale main-owned row survived scoped refresh")
	}
	if !ids["table:other.public.keep"] {
		t.Fatalf("unrelated owner row was removed by scoped refresh")
	}
	if !ids["table:main:main.users"] {
		t.Fatalf("refreshed main users table row missing; ids=%v", ids)
	}
	securityRows, ok := svc.systemNanoDB.Snapshot().Rows("main", "gj_security")
	if !ok || len(securityRows) == 0 {
		t.Fatalf("security rows missing after scoped refresh")
	}
	for _, row := range securityRows {
		if row["id"] == "finding:stale" {
			t.Fatalf("stale security row survived scoped refresh")
		}
	}
}

func TestGraphQLConfigUpdatePatchesCatalogRowsForChangedSource(t *testing.T) {
	livePath := createSQLiteDBFile(t, "live.sqlite3", true)
	replacementPath := createSQLiteDBFile(t, "replacement.sqlite3", true)
	svc := newControlPlaneGraphQLTestService(t, MCPConfig{AllowConfigUpdates: true}, livePath)

	if err := core.UpdateNanoDB(svc.systemNanoDB, func(tx *core.NanoUpdate) error {
		if err := tx.ReplaceRows("main", "gj_catalog", nil, []core.NanoRow{
			{"id": "table:main.public.stale", "kind": "table", "title": "stale", "owner_source": "main", "owner_sources_json": `["main"]`},
			{"id": "table:other.public.keep", "kind": "table", "title": "keep", "owner_source": "other", "owner_sources_json": `["other"]`},
		}); err != nil {
			return err
		}
		return tx.ReplaceTable("main", "gj_config", []core.NanoRow{{"id": "current", "catalog_revision": "old"}})
	}); err != nil {
		t.Fatalf("seed catalog rows: %v", err)
	}

	res, err := svc.gj.GraphQL(sourceModeAdminTestContext(), fmt.Sprintf(`mutation {
		gj_config(id: "current", update: {
			sources: [{
				name: "main",
				kind: "database",
				type: "sqlite",
				path: %q,
				default: true
			}, {
				name: "graphjin",
				kind: "graphjin"
			}, {
				name: "workflows",
				kind: "workflow"
			}]
		}) {
			id
			catalog_revision
		}
	}`, replacementPath), nil, &core.RequestConfig{})
	if err != nil {
		t.Fatalf("config update error: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("config update returned errors: %+v", res.Errors)
	}

	rows, ok := svc.systemNanoDB.Snapshot().Rows("main", "gj_catalog")
	if !ok {
		t.Fatal("missing gj_catalog rows")
	}
	ids := map[string]bool{}
	for _, row := range rows {
		ids[fmt.Sprint(row["id"])] = true
	}
	if ids["table:main.public.stale"] {
		t.Fatalf("stale main-owned row survived config-scoped catalog refresh")
	}
	if !ids["table:other.public.keep"] {
		t.Fatalf("unrelated owner row was removed by config-scoped catalog refresh")
	}
	if !ids["table:main:main.users"] {
		t.Fatalf("refreshed main users table row missing; ids=%v", ids)
	}
	configRows, ok := svc.systemNanoDB.Snapshot().Rows("main", "gj_config")
	if !ok || len(configRows) != 1 || configRows[0]["catalog_revision"] == "old" || fmt.Sprint(configRows[0]["catalog_revision"]) == "" {
		t.Fatalf("expected gj_config catalog revision to update, rows=%#v ok=%v", configRows, ok)
	}
}

func TestCoreConfigSourceScopedCatalogChangeDetection(t *testing.T) {
	oldCore := core.Config{
		Sources: []core.SourceConfig{{Name: "app", Kind: "database", Type: "sqlite"}, {Name: "graphjin", Kind: "graphjin"}},
		Databases: map[string]core.DatabaseConfig{
			"app": {Type: "sqlite", Path: "old.sqlite3"},
		},
	}
	newCore := cloneCoreConfig(oldCore)
	newCore.Databases["app"] = core.DatabaseConfig{Type: "sqlite", Path: "new.sqlite3"}

	changed := changedCatalogSources(oldCore, newCore)
	if len(changed) != 1 {
		t.Fatalf("changed sources = %+v", changed)
	}
	if _, ok := changed["app"]; !ok {
		t.Fatalf("expected app source to change, got %+v", changed)
	}
	if !coreConfigChangeScopedToSources(oldCore, newCore, changed) {
		t.Fatal("database config for matching source should be source scoped")
	}
	if !sourceScopedCatalogPatchAllowed(oldCore, newCore, changed) {
		t.Fatal("database source change should allow source-scoped catalog patch")
	}

	graphjinChanged := cloneCoreConfig(oldCore)
	graphjinChanged.Sources[1].ReadOnly = true
	graphjinSources := changedCatalogSources(oldCore, graphjinChanged)
	if sourceScopedCatalogPatchAllowed(oldCore, graphjinChanged, graphjinSources) {
		t.Fatal("graphjin source changes should use full catalog refresh")
	}

	broad := cloneCoreConfig(newCore)
	broad.DefaultBlock = !broad.DefaultBlock
	if coreConfigChangeScopedToSources(oldCore, broad, changedCatalogSources(oldCore, broad)) {
		t.Fatal("broader core config change should not use source-scoped catalog patch")
	}
}

func TestGraphQLControlPlaneWorkflowLifecycle(t *testing.T) {
	svc := newControlPlaneGraphQLTestService(t, MCPConfig{AllowWorkflowUpdates: true}, createSQLiteDBFile(t, "app.sqlite3", true))
	workflowCtx := sourceModeAdminTestContext()

	create := `mutation {
		gj_workflow(insert: {
			name: "customer_margin"
			description: "Compute customer margin"
			tags: ["finance"]
			variables: [{ name: "customer_id", type: "number", required: true }]
			code: "function main(input) { return { customer_id: input.customer_id, ok: true }; }"
		}) {
			name
			source_hash
			created_at
			updated_at
			workflow_revision
			catalog_item_id
			catalog_revision
		}
	}`
	res, err := svc.gj.GraphQL(workflowCtx, create, nil, &core.RequestConfig{})
	if err != nil {
		t.Fatalf("workflow create error: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("workflow create returned errors: %+v", res.Errors)
	}
	var created struct {
		Workflow struct {
			Name             string `json:"name"`
			SourceHash       string `json:"source_hash"`
			CreatedAt        string `json:"created_at"`
			UpdatedAt        string `json:"updated_at"`
			WorkflowRevision string `json:"workflow_revision"`
			CatalogItemID    string `json:"catalog_item_id"`
			CatalogRevision  string `json:"catalog_revision"`
		} `json:"gj_workflow"`
	}
	if err := json.Unmarshal(res.Data, &created); err != nil {
		t.Fatalf("decode workflow create: %v\n%s", err, string(res.Data))
	}
	if created.Workflow.Name != "customer_margin" || created.Workflow.SourceHash == "" || created.Workflow.WorkflowRevision == "" ||
		created.Workflow.CatalogItemID != "workflow:customer_margin" || created.Workflow.CatalogRevision == "" {
		t.Fatalf("unexpected workflow create response: %+v", created.Workflow)
	}
	if created.Workflow.CreatedAt == "" || created.Workflow.UpdatedAt == "" {
		t.Fatalf("expected workflow lifecycle timestamps: %+v", created.Workflow)
	}
	time.Sleep(2 * time.Millisecond)

	update := `mutation {
		gj_workflow(where: { name: { eq: "customer_margin" } }, update: {
			description: "Updated customer margin"
			tags: ["finance", "updated"]
			variables: [{ name: "customer_id", type: "number", required: true }]
			code: "function main(input) { return { customer_id: input.customer_id, updated: true }; }"
			created_at: "2000-01-01T00:00:00Z"
			updated_at: "2000-01-02T00:00:00Z"
		}) {
			name
			source_hash
			created_at
			updated_at
			workflow_revision
			catalog_revision
		}
	}`
	res, err = svc.gj.GraphQL(workflowCtx, update, nil, &core.RequestConfig{})
	if err != nil {
		t.Fatalf("workflow update error: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("workflow update returned errors: %+v", res.Errors)
	}
	var updated struct {
		Workflow struct {
			Name             string `json:"name"`
			SourceHash       string `json:"source_hash"`
			CreatedAt        string `json:"created_at"`
			UpdatedAt        string `json:"updated_at"`
			WorkflowRevision string `json:"workflow_revision"`
			CatalogRevision  string `json:"catalog_revision"`
		} `json:"gj_workflow"`
	}
	if err := json.Unmarshal(res.Data, &updated); err != nil {
		t.Fatalf("decode workflow update: %v\n%s", err, string(res.Data))
	}
	if updated.Workflow.CreatedAt != created.Workflow.CreatedAt {
		t.Fatalf("expected update to preserve created_at %q, got %q", created.Workflow.CreatedAt, updated.Workflow.CreatedAt)
	}
	if updated.Workflow.UpdatedAt == "" || updated.Workflow.UpdatedAt == created.Workflow.UpdatedAt ||
		updated.Workflow.UpdatedAt == "2000-01-02T00:00:00Z" {
		t.Fatalf("expected server-generated updated_at after update, got create=%q update=%q", created.Workflow.UpdatedAt, updated.Workflow.UpdatedAt)
	}
	if updated.Workflow.WorkflowRevision == "" || updated.Workflow.WorkflowRevision == created.Workflow.WorkflowRevision {
		t.Fatalf("expected update to change workflow_revision, got create=%q update=%q", created.Workflow.WorkflowRevision, updated.Workflow.WorkflowRevision)
	}
	if updated.Workflow.CatalogRevision == "" || updated.Workflow.CatalogRevision == created.Workflow.CatalogRevision {
		t.Fatalf("expected update to change catalog_revision, got create=%q update=%q", created.Workflow.CatalogRevision, updated.Workflow.CatalogRevision)
	}

	res, err = svc.gj.GraphQL(workflowCtx, `query {
		gj_workflow(where: { name: { eq: "customer_margin" } }) {
			name
			source_hash
			workflow_revision
			code
		}
	}`, nil, &core.RequestConfig{})
	if err != nil {
		t.Fatalf("workflow source query error: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("workflow source query returned errors: %+v", res.Errors)
	}
	var sources struct {
		Sources []struct {
			Name             string `json:"name"`
			SourceHash       string `json:"source_hash"`
			WorkflowRevision string `json:"workflow_revision"`
			Code             string `json:"code"`
		} `json:"gj_workflow"`
	}
	if err := json.Unmarshal(res.Data, &sources); err != nil {
		t.Fatalf("decode workflow source query: %v\n%s", err, string(res.Data))
	}
	if len(sources.Sources) != 1 || sources.Sources[0].Name != "customer_margin" ||
		sources.Sources[0].SourceHash != updated.Workflow.SourceHash ||
		sources.Sources[0].WorkflowRevision != updated.Workflow.WorkflowRevision ||
		!strings.Contains(sources.Sources[0].Code, "updated: true") {
		t.Fatalf("unexpected workflow source response: %+v", sources.Sources)
	}

	run := `mutation {
		gj_workflow_execution(insert: {
			workflow_name: "customer_margin"
			variables: { customer_id: 42 }
		}) {
			workflow_name
			status
			result_json
			error
		}
	}`
	res, err = svc.gj.GraphQL(workflowCtx, run, nil, &core.RequestConfig{})
	if err != nil {
		t.Fatalf("workflow run error: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("workflow run returned errors: %+v", res.Errors)
	}
	var ran struct {
		Run struct {
			WorkflowName string `json:"workflow_name"`
			Status       string `json:"status"`
			ResultJSON   string `json:"result_json"`
			Error        string `json:"error"`
		} `json:"gj_workflow_execution"`
	}
	if err := json.Unmarshal(res.Data, &ran); err != nil {
		t.Fatalf("decode workflow run: %v\n%s", err, string(res.Data))
	}
	if ran.Run.WorkflowName != "customer_margin" || ran.Run.Status != "ok" || !strings.Contains(ran.Run.ResultJSON, `"customer_id":42`) || ran.Run.Error != "" {
		t.Fatalf("unexpected workflow run response: %+v", ran.Run)
	}

	res, err = svc.gj.GraphQL(workflowCtx, `query {
		gj_workflow(where: { name: { eq: "customer_margin" } }) {
			name
			workflow_revision
			catalog_revision
		}
	}`, nil, &core.RequestConfig{})
	if err != nil {
		t.Fatalf("workflow revision query after run error: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("workflow revision query after run returned errors: %+v", res.Errors)
	}
	var afterRun struct {
		Workflows []struct {
			Name             string `json:"name"`
			WorkflowRevision string `json:"workflow_revision"`
			CatalogRevision  string `json:"catalog_revision"`
		} `json:"gj_workflow"`
	}
	if err := json.Unmarshal(res.Data, &afterRun); err != nil {
		t.Fatalf("decode workflow revision query after run: %v\n%s", err, string(res.Data))
	}
	if len(afterRun.Workflows) != 1 ||
		afterRun.Workflows[0].WorkflowRevision != updated.Workflow.WorkflowRevision ||
		afterRun.Workflows[0].CatalogRevision != updated.Workflow.CatalogRevision {
		t.Fatalf("workflow run should not invalidate workflow/catalog revisions: before=%+v after=%+v", updated.Workflow, afterRun.Workflows)
	}

	res, err = svc.gj.GraphQL(workflowCtx, `mutation {
		gj_workflow(where: { name: { eq: "customer_margin" } }, delete: true) {
			name
			deleted
			workflow_revision
			catalog_revision
		}
	}`, nil, &core.RequestConfig{})
	if err != nil {
		t.Fatalf("workflow delete error: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("workflow delete returned errors: %+v", res.Errors)
	}
	var deleted struct {
		Workflow struct {
			Name    string `json:"name"`
			Deleted bool   `json:"deleted"`
		} `json:"gj_workflow"`
	}
	if err := json.Unmarshal(res.Data, &deleted); err != nil {
		t.Fatalf("decode workflow delete: %v\n%s", err, string(res.Data))
	}
	if deleted.Workflow.Name != "customer_margin" || !deleted.Workflow.Deleted {
		t.Fatalf("unexpected workflow delete response: %+v", deleted.Workflow)
	}
}

func TestWorkflowTimestampFormatIsLexSortable(t *testing.T) {
	zero := formatWorkflowTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	nano := formatWorkflowTime(time.Date(2026, 1, 1, 0, 0, 0, 1, time.UTC))
	if zero != "2026-01-01T00:00:00.000000000Z" {
		t.Fatalf("expected fixed-width zero nanoseconds, got %q", zero)
	}
	if zero >= nano {
		t.Fatalf("expected lexicographic timestamp order to match chronological order: zero=%q nano=%q", zero, nano)
	}
	if _, err := time.Parse(time.RFC3339Nano, zero); err != nil {
		t.Fatalf("expected timestamp to parse as RFC3339Nano: %v", err)
	}
}

func TestGraphQLControlPlaneWorkflowLifecycleSorting(t *testing.T) {
	svc := newControlPlaneGraphQLTestService(t, MCPConfig{AllowWorkflowUpdates: true}, createSQLiteDBFile(t, "app.sqlite3", true))
	workflowCtx := sourceModeAdminTestContext()

	for _, name := range []string{"alpha", "bravo"} {
		res, err := svc.gj.GraphQL(workflowCtx, `mutation {
			gj_workflow(insert: {
				name: "`+name+`"
				description: "Workflow `+name+`"
				code: "function main(input) { return { ok: true }; }"
			}) { name created_at updated_at }
		}`, nil, &core.RequestConfig{})
		if err != nil {
			t.Fatalf("workflow %s create error: %v", name, err)
		}
		if len(res.Errors) != 0 {
			t.Fatalf("workflow %s create returned errors: %+v", name, res.Errors)
		}
		time.Sleep(2 * time.Millisecond)
	}

	res, err := svc.gj.GraphQL(workflowCtx, `query {
		gj_workflow(order_by: { updated_at: desc }) {
			name
			updated_at
		}
		gj_catalog(where: { kind: { eq: "workflow" } }, order_by: { created_at: asc }) {
			id
			created_at
		}
	}`, nil, &core.RequestConfig{})
	if err != nil {
		t.Fatalf("workflow sort query error: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("workflow sort query returned errors: %+v", res.Errors)
	}
	var out struct {
		Workflows []struct {
			Name      string `json:"name"`
			UpdatedAt string `json:"updated_at"`
		} `json:"gj_workflow"`
		Items []struct {
			ID        string `json:"id"`
			CreatedAt string `json:"created_at"`
		} `json:"gj_catalog"`
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		t.Fatalf("decode workflow sort query: %v\n%s", err, string(res.Data))
	}
	if len(out.Workflows) != 2 || out.Workflows[0].Name != "bravo" || out.Workflows[1].Name != "alpha" {
		t.Fatalf("expected workflows by updated_at desc, got %+v", out.Workflows)
	}
	if len(out.Items) != 2 || out.Items[0].ID != "workflow:alpha" || out.Items[1].ID != "workflow:bravo" {
		t.Fatalf("expected workflow catalog items by created_at asc, got %+v", out.Items)
	}
}

func TestGraphQLControlPlaneAllowsMixedSystemAndAppRoots(t *testing.T) {
	svc := newControlPlaneGraphQLTestService(t, MCPConfig{}, createSQLiteDBFile(t, "app.sqlite3", true))

	res, err := svc.gj.GraphQL(context.Background(), `query {
		users { id }
		gj_catalog(limit: 1) { id }
	}`, nil, &core.RequestConfig{})
	if err != nil {
		t.Fatalf("mixed system/app roots should execute through normal multi-db GraphQL: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("mixed system/app roots returned errors: %+v", res.Errors)
	}
}

func TestGraphQLControlPlaneRejectsCatalogMutations(t *testing.T) {
	svc := newControlPlaneGraphQLTestService(t, MCPConfig{AllowWorkflowUpdates: true}, createSQLiteDBFile(t, "app.sqlite3", true))

	_, err := svc.gj.GraphQL(context.Background(), `mutation {
		gj_catalog(insert: { id: "workflow:nope", kind: "workflow", title: "nope", summary: "nope" }) {
			id
		}
	}`, nil, &core.RequestConfig{})
	if err == nil {
		t.Fatal("expected catalog mutation to be rejected")
	}
}

func TestSystemNanoSnapshotIncludesSecurity(t *testing.T) {
	db, err := core.NewNanoDB(systemNanoSnapshot("graphjin", "", nil))
	if err != nil {
		t.Fatalf("new nanodb: %v", err)
	}
	snap := db.Snapshot()
	if snap == nil {
		t.Fatal("expected nanodb snapshot")
	}
	table, ok := snap.Table("main", "gj_security")
	if !ok {
		t.Fatal("expected gj_security table in system nanodb")
	}
	cols := map[string]bool{}
	for _, col := range table.Columns {
		cols[col.Name] = true
	}
	for _, name := range []string{"id", "kind", "report", "scope", "config_id", "config_file", "config_active", "mode", "audience", "surface", "transport", "database_name", "source", "source_kind", "table_name", "role", "capability", "status", "severity", "summary_json", "evidence_json", "examples_json", "safety_json"} {
		if !cols[name] {
			t.Fatalf("expected gj_security.%s column", name)
		}
	}
}

func TestGraphQLControlPlaneSecurityQuery(t *testing.T) {
	svc := newControlPlaneGraphQLTestService(t, MCPConfig{
		AllowConfigUpdates:   true,
		AllowSchemaUpdates:   true,
		AllowWorkflowUpdates: true,
		AllowRawQueries:      true,
	}, createSQLiteDBFile(t, "app.sqlite3", true))
	svc.conf.Serv.Production = true
	svc.conf.Core.Production = true
	if err := svc.refreshSystemNanoDB(); err != nil {
		t.Fatalf("refresh system nanodb: %v", err)
	}

	res, err := svc.gj.GraphQL(sourceModeAdminTestContext(), `query {
		summary: gj_security(id: "summary") {
			id
			kind
			mode
			summary_json
		}
		findings: gj_security(
			where: {
				kind: { eq: "finding" }
				severity: { in: ["high", "critical"] }
			}
			order_by: { severity_rank: desc }
		) {
			id
			kind
			severity
			severity_rank
			title
			capability
			recommendation
			evidence_json
		}
	}`, nil, &core.RequestConfig{})
	if err != nil {
		t.Fatalf("security query error: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("security query returned errors: %+v", res.Errors)
	}

	var out struct {
		Summary struct {
			ID          string         `json:"id"`
			Kind        string         `json:"kind"`
			Mode        string         `json:"mode"`
			SummaryJSON map[string]any `json:"summary_json"`
		} `json:"summary"`
		Findings []struct {
			ID             string         `json:"id"`
			Kind           string         `json:"kind"`
			Severity       string         `json:"severity"`
			SeverityRank   int            `json:"severity_rank"`
			Title          string         `json:"title"`
			Capability     string         `json:"capability"`
			Recommendation string         `json:"recommendation"`
			EvidenceJSON   map[string]any `json:"evidence_json"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		t.Fatalf("decode security query: %v\n%s", err, string(res.Data))
	}
	if out.Summary.ID != "summary" || out.Summary.Kind != "summary" || out.Summary.Mode != "prod" {
		t.Fatalf("unexpected summary row: %+v", out.Summary)
	}
	if out.Summary.SummaryJSON["mode"] != "prod" {
		t.Fatalf("expected prod summary_json mode, got %+v", out.Summary.SummaryJSON)
	}
	if len(out.Findings) == 0 {
		t.Fatalf("expected prod high/critical findings, got %s", string(res.Data))
	}
	for _, finding := range out.Findings {
		if finding.Kind != "finding" || finding.SeverityRank < 3 || finding.Recommendation == "" || finding.EvidenceJSON["mode"] != "prod" {
			t.Fatalf("unexpected finding row: %+v", finding)
		}
	}
}

func TestGraphQLControlPlaneSecurityReportsSourceAccessPolicy(t *testing.T) {
	svc := newControlPlaneGraphQLTestService(t, MCPConfig{}, createSQLiteDBFile(t, "source-access-security.sqlite3", true))

	res, err := svc.gj.GraphQL(sourceModeAdminTestContext(), `query {
		identity: gj_security(id: "policy:source_access.identity") {
			id
			kind
			status
			details_json
		}
		policy: gj_security(id: "policy:source_access.main.users.missing_namespace") {
			id
			kind
			status
			table_name
			column_name
			details_json
		}
		finding: gj_security(id: "finding:high:source_access.main.users.missing_namespace") {
			id
			kind
			status
			severity
			table_name
		}
	}`, nil, &core.RequestConfig{})
	if err != nil {
		t.Fatalf("security source access query error: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("security source access query returned errors: %+v", res.Errors)
	}

	var out struct {
		Identity *struct {
			ID          string         `json:"id"`
			Kind        string         `json:"kind"`
			Status      string         `json:"status"`
			DetailsJSON map[string]any `json:"details_json"`
		} `json:"identity"`
		Policy *struct {
			ID          string         `json:"id"`
			Kind        string         `json:"kind"`
			Status      string         `json:"status"`
			TableName   string         `json:"table_name"`
			ColumnName  string         `json:"column_name"`
			DetailsJSON map[string]any `json:"details_json"`
		} `json:"policy"`
		Finding *struct {
			ID        string `json:"id"`
			Kind      string `json:"kind"`
			Status    string `json:"status"`
			Severity  string `json:"severity"`
			TableName string `json:"table_name"`
		} `json:"finding"`
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		t.Fatalf("decode security source access query: %v\n%s", err, string(res.Data))
	}
	if out.Identity == nil || out.Identity.Kind != "policy" || out.Identity.DetailsJSON["namespace_claim"] != "account_id" {
		t.Fatalf("expected source access identity policy, got %s", string(res.Data))
	}
	if out.Policy == nil || out.Policy.Kind != "policy" || out.Policy.Status != "finding" ||
		out.Policy.TableName != "users" || out.Policy.ColumnName != "account_id" ||
		out.Policy.DetailsJSON["effective_behavior"] != "blocked" {
		t.Fatalf("expected missing namespace source access policy finding, got %s", string(res.Data))
	}
	if out.Finding == nil || out.Finding.Kind != "finding" || out.Finding.Status != "finding" ||
		out.Finding.Severity != "high" || out.Finding.TableName != "users" {
		t.Fatalf("expected missing namespace finding row, got %s", string(res.Data))
	}
}

func TestGraphQLControlPlaneSecurityConfigScan(t *testing.T) {
	dbPath := createSQLiteDBFile(t, "config-scan.sqlite3", true)
	svc := newControlPlaneGraphQLTestService(t, MCPConfig{}, dbPath)
	devConfig := fmt.Sprintf(`
sources:
  - name: main
    kind: database
    type: sqlite
    path: %q
    default: true
  - name: graphjin
    kind: graphjin
  - name: workflows
    kind: workflow
`, dbPath)
	prodConfig := `
inherits: dev.yml
production: true
mode: prod
mcp:
  allow_raw_queries: true
`
	if err := svc.fs.Put("dev.yml", []byte(devConfig)); err != nil {
		t.Fatalf("write dev config: %v", err)
	}
	if err := svc.fs.Put("prod.yml", []byte(prodConfig)); err != nil {
		t.Fatalf("write prod config: %v", err)
	}
	if err := svc.refreshSystemNanoDB(); err != nil {
		t.Fatalf("refresh system nanodb: %v", err)
	}

	res, err := svc.gj.GraphQL(sourceModeAdminTestContext(), `query {
		summary: gj_security(id: "config:prod:summary") {
			id
			scope
			config_id
			mode
			summary_json
		}
		findings: gj_security(
			where: {
				scope: { eq: "config" }
				config_id: { eq: "prod" }
				kind: { eq: "finding" }
				status: { eq: "finding" }
			}
			order_by: { severity_rank: desc }
		) {
			id
			scope
			config_id
			config_file
			mode
			status
			severity
			override_key
			recommendation
		}
	}`, nil, &core.RequestConfig{})
	if err != nil {
		t.Fatalf("security config scan query error: %v", err)
	}
	var out struct {
		Summary struct {
			ID          string         `json:"id"`
			Scope       string         `json:"scope"`
			ConfigID    string         `json:"config_id"`
			Mode        string         `json:"mode"`
			SummaryJSON map[string]any `json:"summary_json"`
		} `json:"summary"`
		Findings []struct {
			ConfigID    string `json:"config_id"`
			ConfigFile  string `json:"config_file"`
			Mode        string `json:"mode"`
			Status      string `json:"status"`
			Severity    string `json:"severity"`
			OverrideKey string `json:"override_key"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		t.Fatalf("decode config scan query: %v\n%s", err, string(res.Data))
	}
	if out.Summary.ID != "config:prod:summary" || out.Summary.Scope != "config" || out.Summary.ConfigID != "prod" || out.Summary.Mode != "prod" {
		t.Fatalf("unexpected prod config summary: %+v", out.Summary)
	}
	if out.Summary.SummaryJSON["config_inherits"] != "dev.yml" {
		t.Fatalf("expected prod config inheritance evidence, got %+v", out.Summary.SummaryJSON)
	}
	var sawRawQueryFinding bool
	for _, finding := range out.Findings {
		if finding.ConfigID != "prod" || finding.ConfigFile != "prod.yml" || finding.Mode != "prod" || finding.Status != "finding" {
			t.Fatalf("unexpected config finding: %+v", finding)
		}
		if finding.OverrideKey == "mcp.allow_raw_queries" && (finding.Severity == "high" || finding.Severity == "medium") {
			sawRawQueryFinding = true
		}
	}
	if !sawRawQueryFinding {
		t.Fatalf("expected prod mcp.allow_raw_queries finding, got %s", string(res.Data))
	}
}

func TestGraphQLControlPlaneRejectsSecurityMutations(t *testing.T) {
	svc := newControlPlaneGraphQLTestService(t, MCPConfig{}, createSQLiteDBFile(t, "app.sqlite3", true))

	_, err := svc.gj.GraphQL(context.Background(), `mutation {
		gj_security(insert: { id: "finding:nope", kind: "finding", title: "nope" }) {
			id
		}
	}`, nil, &core.RequestConfig{})
	if err == nil {
		t.Fatal("expected security mutation to be rejected")
	}
}

func sourceModeAdminTestContext() context.Context {
	ctx := context.WithValue(context.Background(), core.UserIDKey, "admin-user")
	ctx = context.WithValue(ctx, core.IdentityVarsKey, map[string]interface{}{"account_id": "admin-account", "user_id": "admin-user"})
	ctx = context.WithValue(ctx, core.IdentityRolesKey, []string{"admin"})
	return ctx
}

func sourceModeUserTestContext() context.Context {
	ctx := context.WithValue(context.Background(), core.UserIDKey, "app-user")
	ctx = context.WithValue(ctx, core.IdentityVarsKey, map[string]interface{}{"account_id": "app-account", "user_id": "app-user"})
	ctx = context.WithValue(ctx, core.IdentityRolesKey, []string{"user"})
	return ctx
}

func TestApplySystemRoleQueryDefaultsDatabaseScoped(t *testing.T) {
	conf := &Config{
		Core: core.Config{
			Mode: "agentic",
			Roles: []core.Role{{
				Name: "user",
				Tables: []core.RoleTable{{
					Database: "main",
					Name:     "gj_security",
					Query:    &core.Query{Block: false},
				}},
			}},
		},
	}
	runtimeCore := cloneCoreConfig(conf.Core)
	applySystemRoleQueryDefaults(conf, &runtimeCore, "graphjin")

	var sawMainGrant, sawGraphjinBlock bool
	for _, role := range runtimeCore.Roles {
		if role.Name != "user" {
			continue
		}
		for _, table := range role.Tables {
			if table.Name != "gj_security" {
				continue
			}
			switch table.Database {
			case "main":
				sawMainGrant = table.Query != nil && !table.Query.Block
			case "graphjin":
				sawGraphjinBlock = table.Query != nil && table.Query.Block
			}
		}
	}
	if !sawMainGrant || !sawGraphjinBlock {
		t.Fatalf("expected database-scoped role defaults to keep main grant and add graphjin block: main=%v graphjin=%v roles=%+v",
			sawMainGrant, sawGraphjinBlock, runtimeCore.Roles)
	}
}

func TestSourceModeSystemRootAccessCoversAnonAndCustomRoles(t *testing.T) {
	t.Run("dev anon cannot bypass admin root", func(t *testing.T) {
		svc := newControlPlaneGraphQLTestServiceWithConfig(t, MCPConfig{}, createSQLiteDBFile(t, "source-mode-dev-anon.sqlite3", true), func(conf *Config) {
			conf.Core.Mode = "dev"
			conf.Core.DefaultBlock = false
		})

		res, err := svc.gj.GraphQL(context.Background(), `query {
			security: gj_security(id: "summary") { id }
		}`, nil, &core.RequestConfig{})
		if err != nil {
			if !strings.Contains(strings.ToLower(err.Error()), "blocked") &&
				!strings.Contains(strings.ToLower(err.Error()), "unauthorized") {
				t.Fatalf("unexpected source-mode anon security error: %v", err)
			}
			return
		}
		var out struct {
			Security *struct {
				ID string `json:"id"`
			} `json:"security"`
		}
		if err := json.Unmarshal(res.Data, &out); err != nil {
			t.Fatalf("decode anon source-mode response: %v\n%s", err, string(res.Data))
		}
		if out.Security != nil {
			t.Fatalf("expected source-mode anon to be blocked from gj_security, got %s", string(res.Data))
		}
	})

	t.Run("configured non-admin role gets catalog but not security", func(t *testing.T) {
		svc := newControlPlaneGraphQLTestServiceWithConfig(t, MCPConfig{}, createSQLiteDBFile(t, "source-mode-custom-role.sqlite3", true), func(conf *Config) {
			conf.Core.Mode = "agentic"
			conf.Core.Roles = append(conf.Core.Roles, core.Role{Name: "member"})
		})
		ctx := context.WithValue(context.Background(), core.UserIDKey, "user_1")
		ctx = context.WithValue(ctx, core.IdentityVarsKey, map[string]interface{}{"account_id": "acct_1", "user_id": "user_1"})
		ctx = context.WithValue(ctx, core.IdentityRolesKey, []string{"member"})

		res, err := svc.gj.GraphQL(ctx, `query {
			catalog: gj_catalog(limit: 1) { id }
			security: gj_security(id: "summary") { id }
		}`, nil, &core.RequestConfig{})
		if err != nil {
			t.Fatalf("source-mode custom role query error: %v", err)
		}
		var out struct {
			Catalog []struct {
				ID string `json:"id"`
			} `json:"catalog"`
			Security *struct {
				ID string `json:"id"`
			} `json:"security"`
		}
		if err := json.Unmarshal(res.Data, &out); err != nil {
			t.Fatalf("decode custom role source-mode response: %v\n%s", err, string(res.Data))
		}
		if len(out.Catalog) == 0 {
			t.Fatalf("expected source-mode custom role to read gj_catalog, got %s", string(res.Data))
		}
		if out.Security != nil {
			t.Fatalf("expected source-mode custom role to be blocked from gj_security, got %s", string(res.Data))
		}
	})
}

func TestGraphQLControlPlaneAgenticSystemPermissions(t *testing.T) {
	userCtx := context.WithValue(context.Background(), core.UserIDKey, "company-user")

	t.Run("normal user gets catalog but not detailed audit config or workflow code", func(t *testing.T) {
		svc := newControlPlaneGraphQLTestServiceWithConfig(t, MCPConfig{}, createSQLiteDBFile(t, "agentic-perms.sqlite3", true), func(conf *Config) {
			conf.Core.Mode = "agentic"
		})
		if err := svc.fs.Put(filepath.Join(svc.workflowBasePath(), "daily_report.js"), []byte(`function main(input) { return { ok: true }; }`)); err != nil {
			t.Fatalf("write workflow: %v", err)
		}
		if err := svc.refreshSystemNanoDB(); err != nil {
			t.Fatalf("refresh system nanodb: %v", err)
		}

		res, err := svc.gj.GraphQL(userCtx, `query {
			catalog: gj_catalog(limit: 1) { id }
			security: gj_security(id: "summary") { id }
			config: gj_config(id: "current") { id }
			workflow: gj_workflow(id: "daily_report") { name code }
		}`, nil, &core.RequestConfig{})
		if err != nil {
			t.Fatalf("agentic permissions query error: %v", err)
		}
		var out map[string]json.RawMessage
		if err := json.Unmarshal(res.Data, &out); err != nil {
			t.Fatalf("decode permissions response: %v\n%s", err, string(res.Data))
		}
		if string(out["catalog"]) == "null" || len(out["catalog"]) == 0 {
			t.Fatalf("expected agentic user catalog access, got %s", string(res.Data))
		}
		for _, key := range []string{"security", "config", "workflow"} {
			if string(out[key]) != "null" {
				t.Fatalf("expected %s to be blocked for normal agentic user, got %s", key, string(res.Data))
			}
		}
	})

	t.Run("explicit security grant unblocks only gj_security", func(t *testing.T) {
		svc := newControlPlaneGraphQLTestServiceWithConfig(t, MCPConfig{}, createSQLiteDBFile(t, "agentic-security-grant.sqlite3", true), func(conf *Config) {
			conf.Core.Mode = "agentic"
			for i := range conf.Core.Sources {
				if conf.Core.Sources[i].Name == "graphjin" {
					conf.Core.Sources[i].Access.Roots = map[string]string{"gj_security": core.AccessModeAuthenticated}
				}
			}
		})

		res, err := svc.gj.GraphQL(userCtx, `query {
			security: gj_security(id: "summary") { id scope mode }
			config: gj_config(id: "current") { id }
		}`, nil, &core.RequestConfig{})
		if err != nil {
			t.Fatalf("agentic explicit grant query error: %v", err)
		}
		var out struct {
			Security *struct {
				ID string `json:"id"`
			} `json:"security"`
			Config *struct {
				ID string `json:"id"`
			} `json:"config"`
		}
		if err := json.Unmarshal(res.Data, &out); err != nil {
			t.Fatalf("decode explicit grant response: %v\n%s", err, string(res.Data))
		}
		if out.Security == nil || out.Security.ID != "summary" {
			t.Fatalf("expected gj_security grant to return summary, got %s", string(res.Data))
		}
		if out.Config != nil {
			t.Fatalf("expected gj_config to remain blocked, got %s", string(res.Data))
		}
	})

	t.Run("prod blocks catalog by default", func(t *testing.T) {
		svc := newControlPlaneGraphQLTestServiceWithConfig(t, MCPConfig{}, createSQLiteDBFile(t, "prod-perms.sqlite3", true), func(conf *Config) {
			conf.Core.Mode = "prod"
			conf.Serv.Production = true
			conf.Core.Production = true
		})

		res, err := svc.gj.GraphQL(userCtx, `query {
			catalog: gj_catalog(limit: 1) { id }
		}`, nil, &core.RequestConfig{})
		if err != nil {
			if !strings.Contains(err.Error(), "blocked") && !strings.Contains(err.Error(), "invalid query") {
				t.Fatalf("prod catalog query returned unexpected error: %v", err)
			}
			return
		}
		var out map[string]json.RawMessage
		if err := json.Unmarshal(res.Data, &out); err != nil {
			t.Fatalf("decode prod catalog response: %v\n%s", err, string(res.Data))
		}
		if string(out["catalog"]) != "null" {
			t.Fatalf("expected prod catalog to be blocked by default, got %s", string(res.Data))
		}
	})
}

func TestGraphQLRuntimeRootAvailabilityAndPermissions(t *testing.T) {
	userCtx := context.WithValue(context.Background(), core.UserIDKey, "company-user")
	adminCtx := sourceModeAdminTestContext()
	query := `query {
		gj_runtime(where: { kind: { in: ["status", "event"] } }, order_by: { created_at: desc }, limit: 20) {
			kind
			status
			severity
			summary
			next_action
			details_json
		}
	}`

	t.Run("unavailable outside agentic mode", func(t *testing.T) {
		for _, mode := range []string{"dev", "prod"} {
			mode := mode
			t.Run(mode, func(t *testing.T) {
				svc := newControlPlaneGraphQLTestServiceWithConfig(t, MCPConfig{}, createSQLiteDBFile(t, "runtime-"+mode+".sqlite3", true), func(conf *Config) {
					conf.Core.Mode = mode
				})
				_, err := svc.gj.GraphQL(userCtx, query, nil, &core.RequestConfig{})
				if err == nil {
					t.Fatalf("expected gj_runtime to be unavailable in %s mode", mode)
				}
			})
		}
	})

	t.Run("agentic admin can read bounded runtime rows", func(t *testing.T) {
		svc := newControlPlaneGraphQLTestServiceWithConfig(t, MCPConfig{}, createSQLiteDBFile(t, "runtime-agentic.sqlite3", true), func(conf *Config) {
			conf.Core.Mode = "agentic"
		})
		svc.recordRuntimeEvent(context.Background(), runtimeEvent{
			Phase:      "test",
			Status:     runtimeStatusDegraded,
			Severity:   "warn",
			Summary:    "Test runtime event.",
			NextAction: "Follow the test next action.",
			Details:    map[string]any{"password": "secret", "safe": "visible"},
		})
		res, err := svc.gj.GraphQL(adminCtx, query, nil, &core.RequestConfig{})
		if err != nil {
			t.Fatalf("runtime query error: %v", err)
		}
		var out struct {
			Runtime []struct {
				Kind        string `json:"kind"`
				Status      string `json:"status"`
				Severity    string `json:"severity"`
				Summary     string `json:"summary"`
				NextAction  string `json:"next_action"`
				DetailsJSON string `json:"details_json"`
			} `json:"gj_runtime"`
		}
		if err := json.Unmarshal(res.Data, &out); err != nil {
			t.Fatalf("decode runtime response: %v\n%s", err, string(res.Data))
		}
		if len(out.Runtime) == 0 {
			t.Fatalf("expected runtime rows, got %s", string(res.Data))
		}
		var sawStatus, sawRedacted bool
		for _, row := range out.Runtime {
			if row.Kind == runtimeKindStatus && row.Status != "" {
				sawStatus = true
			}
			if row.Summary == "Test runtime event." && strings.Contains(row.DetailsJSON, `"password":"[REDACTED]"`) && strings.Contains(row.DetailsJSON, `"safe":"visible"`) {
				sawRedacted = true
			}
		}
		if !sawStatus || !sawRedacted {
			t.Fatalf("expected status and redacted event rows, got %s", string(res.Data))
		}
	})

	t.Run("anon, non-admin, and runtime read false are blocked", func(t *testing.T) {
		svc := newControlPlaneGraphQLTestServiceWithConfig(t, MCPConfig{}, createSQLiteDBFile(t, "runtime-agentic-blocks.sqlite3", true), func(conf *Config) {
			conf.Core.Mode = "agentic"
		})
		res, err := svc.gj.GraphQL(context.Background(), query, nil, &core.RequestConfig{})
		if err != nil {
			t.Fatalf("anon runtime query should be blocked by role, not fail compile: %v", err)
		}
		var anonOut map[string]json.RawMessage
		if err := json.Unmarshal(res.Data, &anonOut); err != nil {
			t.Fatalf("decode anon response: %v\n%s", err, string(res.Data))
		}
		if string(anonOut["gj_runtime"]) != "null" {
			t.Fatalf("expected anon gj_runtime to be blocked, got %s", string(res.Data))
		}
		res, err = svc.gj.GraphQL(context.WithValue(context.Background(), core.UserRoleKey, "user"), query, nil, &core.RequestConfig{})
		if err != nil {
			t.Fatalf("explicit user role runtime query should be blocked by role, not fail compile: %v", err)
		}
		var roleOut map[string]json.RawMessage
		if err := json.Unmarshal(res.Data, &roleOut); err != nil {
			t.Fatalf("decode user role response: %v\n%s", err, string(res.Data))
		}
		if string(roleOut["gj_runtime"]) != "null" {
			t.Fatalf("expected explicit user role to block gj_runtime, got %s", string(res.Data))
		}
		res, err = svc.gj.GraphQL(context.WithValue(userCtx, core.UserRoleKey, "anon"), query, nil, &core.RequestConfig{})
		if err != nil {
			t.Fatalf("explicit anon role runtime query should be blocked by role, not fail compile: %v", err)
		}
		var forcedAnonOut map[string]json.RawMessage
		if err := json.Unmarshal(res.Data, &forcedAnonOut); err != nil {
			t.Fatalf("decode forced anon response: %v\n%s", err, string(res.Data))
		}
		if string(forcedAnonOut["gj_runtime"]) != "null" {
			t.Fatalf("expected explicit anon role to block gj_runtime, got %s", string(res.Data))
		}

		disabled := newControlPlaneGraphQLTestServiceWithConfig(t, MCPConfig{}, createSQLiteDBFile(t, "runtime-agentic-disabled.sqlite3", true), func(conf *Config) {
			conf.Core.Mode = "agentic"
			for i := range conf.Core.Sources {
				if conf.Core.Sources[i].Kind == "graphjin" {
					conf.Core.Sources[i].Capabilities = map[string]bool{sourcecap.KeyRuntimeRead: false}
				}
			}
		})
		res, err = disabled.gj.GraphQL(adminCtx, query, nil, &core.RequestConfig{})
		if err != nil {
			t.Fatalf("runtime.read false query should be blocked by role, not fail compile: %v", err)
		}
		var disabledOut map[string]json.RawMessage
		if err := json.Unmarshal(res.Data, &disabledOut); err != nil {
			t.Fatalf("decode disabled response: %v\n%s", err, string(res.Data))
		}
		if string(disabledOut["gj_runtime"]) != "null" {
			t.Fatalf("expected runtime.read false to block gj_runtime, got %s", string(res.Data))
		}
	})
}

func TestGraphQLControlPlaneCatalogSecurityGuidance(t *testing.T) {
	svc := newControlPlaneGraphQLTestService(t, MCPConfig{}, createSQLiteDBFile(t, "app.sqlite3", true))

	res, err := svc.gj.GraphQL(sourceModeUserTestContext(), `query {
		gj_catalog(where: { kind: { eq: "system_capability" }, name: { eq: "gj_security.query" } }, limit: 1) {
			name
			summary
			details_json
			examples_json
			safety_json
			graphql_query
		}
	}`, nil, &core.RequestConfig{})
	if err != nil {
		t.Fatalf("catalog security guidance query error: %v", err)
	}
	var out struct {
		Items []struct {
			Name         string `json:"name"`
			Summary      string `json:"summary"`
			DetailsJSON  string `json:"details_json"`
			ExamplesJSON string `json:"examples_json"`
			SafetyJSON   string `json:"safety_json"`
			GraphQLQuery string `json:"graphql_query"`
		} `json:"gj_catalog"`
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		t.Fatalf("decode catalog security guidance: %v\n%s", err, string(res.Data))
	}
	if len(out.Items) != 1 || out.Items[0].Name != "gj_security.query" {
		t.Fatalf("expected gj_security catalog guidance, got %+v", out.Items)
	}
	item := out.Items[0]
	if !strings.Contains(item.DetailsJSON, "gj_security") ||
		!strings.Contains(item.ExamplesJSON, "high critical findings") ||
		!strings.Contains(item.SafetyJSON, "read_only") ||
		!strings.Contains(item.GraphQLQuery, "gj_security") {
		t.Fatalf("security guidance missing LLM metadata: %+v", item)
	}
}

func TestGraphQLControlPlaneCatalogRuntimeGuidance(t *testing.T) {
	svc := newControlPlaneGraphQLTestServiceWithConfig(t, MCPConfig{}, createSQLiteDBFile(t, "runtime-guidance.sqlite3", true), func(conf *Config) {
		conf.Core.Mode = "agentic"
	})

	res, err := svc.gj.GraphQL(context.WithValue(context.Background(), core.UserIDKey, "company-user"), `query {
		gj_catalog(where: { kind: { eq: "system_capability" }, name: { eq: "gj_runtime.query" } }, limit: 1) {
			name
			summary
			details_json
			examples_json
			safety_json
			graphql_query
		}
	}`, nil, &core.RequestConfig{})
	if err != nil {
		t.Fatalf("catalog runtime guidance query error: %v", err)
	}
	var out struct {
		Items []struct {
			Name         string `json:"name"`
			Summary      string `json:"summary"`
			DetailsJSON  string `json:"details_json"`
			ExamplesJSON string `json:"examples_json"`
			SafetyJSON   string `json:"safety_json"`
			GraphQLQuery string `json:"graphql_query"`
		} `json:"gj_catalog"`
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		t.Fatalf("decode catalog runtime guidance: %v\n%s", err, string(res.Data))
	}
	if len(out.Items) != 1 || out.Items[0].Name != "gj_runtime.query" {
		t.Fatalf("expected gj_runtime catalog guidance, got %+v", out.Items)
	}
	item := out.Items[0]
	for _, want := range []string{"gj_runtime", "runtime.read", "decision", "degraded"} {
		if !strings.Contains(item.DetailsJSON+item.SafetyJSON+item.GraphQLQuery, want) {
			t.Fatalf("runtime guidance missing %q: %+v", want, item)
		}
	}
	if !strings.Contains(item.ExamplesJSON, "latest runtime decision context") {
		t.Fatalf("runtime guidance missing examples: %+v", item)
	}
}

func TestSecurityNanoRowsModes(t *testing.T) {
	conf := &Config{
		Core: core.Config{
			Mode: "agentic",
			Sources: []core.SourceConfig{
				{Name: "graphjin", Kind: "graphjin"},
				{Name: "workflows", Kind: "workflow"},
			},
			Tables: []core.Table{{Name: "gj_workflow", ReadOnly: false}},
		},
		Serv: Serv{
			Production: true,
			MCP: MCPConfig{
				Disable:              true,
				AllowWorkflowUpdates: true,
			},
		},
	}
	conf.Core.Production = true
	rows := securityNanoRows(&graphjinService{conf: conf})

	var sawSummary, sawAgenticWorkflowPolicy, sawWorkflowFinding bool
	for _, row := range rows {
		if row["id"] == "summary" && row["mode"] == "agentic" {
			sawSummary = true
		}
		if row["kind"] == "policy" && row["capability"] == "workflow" && row["action"] == "execute" && row["default_allowed"] == true {
			sawAgenticWorkflowPolicy = true
		}
		if row["kind"] == "finding" && row["capability"] == "workflow" && row["action"] == "write" && row["severity"] == "high" {
			sawWorkflowFinding = true
		}
	}
	if !sawSummary || !sawAgenticWorkflowPolicy || !sawWorkflowFinding {
		t.Fatalf("missing expected agentic security rows: summary=%v workflow_policy=%v workflow_finding=%v rows=%+v",
			sawSummary, sawAgenticWorkflowPolicy, sawWorkflowFinding, rows)
	}
}

func TestSecurityNanoRowsSourceCapabilities(t *testing.T) {
	conf := &Config{
		Core: core.Config{
			Mode: "agentic",
			Sources: []core.SourceConfig{
				{Name: "graphjin", Kind: "graphjin", Capabilities: map[string]bool{"security.read": true}},
				{Name: "workflows", Kind: "workflow", Capabilities: map[string]bool{"workflow.execute": false}},
			},
		},
		Serv: Serv{Production: true},
	}
	conf.Core.Production = true
	rows := securityNanoRows(&graphjinService{conf: conf})

	var sawSecurityRead, sawWorkflowExecute bool
	for _, row := range rows {
		if row["kind"] != "policy" {
			continue
		}
		if row["source_kind"] == "graphjin" && row["capability"] == "security.read" {
			sawSecurityRead = true
			if row["override_key"] != "sources[graphjin].capabilities.security.read" ||
				row["override_explicit"] != true ||
				row["default_effective"] != "block" ||
				row["effective"] != "allow" ||
				row["weakens_default"] != true ||
				row["severity"] != "high" {
				t.Fatalf("unexpected security.read policy: %+v", row)
			}
			evidence, ok := row["evidence_json"].(map[string]any)
			def, _ := sourcecap.Lookup(sourcecap.KindGraphJin, sourcecap.KeySecurityRead)
			if !ok || evidence["enforcement"] != def.Enforcement {
				t.Fatalf("expected runtime enforcement evidence, got %+v", row["evidence_json"])
			}
		}
		if row["source_kind"] == "workflow" && row["capability"] == "workflow.execute" {
			sawWorkflowExecute = true
			if row["override_key"] != "sources[workflows].capabilities.workflow.execute" ||
				row["override_explicit"] != true ||
				row["default_effective"] != "allow" ||
				row["effective"] != "block" {
				t.Fatalf("unexpected workflow.execute policy: %+v", row)
			}
		}
	}
	if !sawSecurityRead || !sawWorkflowExecute {
		t.Fatalf("missing source capability policies: security_read=%v workflow_execute=%v rows=%+v", sawSecurityRead, sawWorkflowExecute, rows)
	}
}

func TestFileSourceCapabilitiesUseCoarseReadOnlyEnforcement(t *testing.T) {
	conf := &Config{
		Core: core.Config{
			Mode: "agentic",
			Sources: []core.SourceConfig{
				{
					Name: "docs",
					Kind: "file",
					Capabilities: map[string]bool{
						"files.write":  true,
						"files.delete": false,
					},
				},
			},
		},
		Serv: Serv{Production: true},
	}
	conf.Core.Production = true
	applySourceCapabilitySourceDefaults(conf)
	if !conf.Core.Sources[0].ReadOnly {
		t.Fatalf("file source should be read-only when either coarse mutating capability is disabled")
	}

	rows := securityNanoRows(&graphjinService{conf: conf})
	var sawWriteEvidence bool
	for _, row := range rows {
		if row["kind"] != "policy" || row["source_kind"] != "file" || row["capability"] != "files.write" {
			continue
		}
		sawWriteEvidence = true
		if row["effective"] != "read_only" || row["read_only"] != true {
			t.Fatalf("expected files.write to be blocked by read-only, got %+v", row)
		}
		evidence, ok := row["evidence_json"].(map[string]any)
		if !ok || evidence["enforcement"] != "runtime_coarse_read_only" {
			t.Fatalf("expected coarse read-only enforcement evidence, got %+v", row["evidence_json"])
		}
	}
	if !sawWriteEvidence {
		t.Fatalf("missing files.write policy evidence rows=%+v", rows)
	}
}

func TestSecurityNanoRowsCoverSourceCapabilityRegistry(t *testing.T) {
	conf := &Config{
		Core: core.Config{
			Mode: "agentic",
			Sources: []core.SourceConfig{
				{Name: "app", Kind: sourcecap.KindDatabase},
				{Name: "repo", Kind: sourcecap.KindCode},
				{Name: "docs", Kind: sourcecap.KindFile},
				{Name: "upstream", Kind: sourcecap.KindAPI},
				{Name: "graphjin", Kind: sourcecap.KindGraphJin},
				{Name: "workflows", Kind: sourcecap.KindWorkflow},
			},
		},
		Serv: Serv{Production: true},
	}
	rows := securityNanoRows(&graphjinService{conf: conf})
	seen := map[string]core.NanoRow{}
	for _, row := range rows {
		if row["kind"] != "policy" || row["scope"] != "runtime" {
			continue
		}
		key := fmt.Sprint(row["source_kind"]) + "." + fmt.Sprint(row["capability"])
		seen[key] = row
	}
	for _, kind := range sourcecap.Kinds() {
		for _, def := range sourcecap.Definitions(kind) {
			row, ok := seen[kind+"."+def.Key]
			if !ok {
				t.Fatalf("missing gj_security policy row for %s.%s", kind, def.Key)
			}
			if row["action"] != def.Action || row["severity"] != def.Severity {
				t.Fatalf("policy row drift for %s.%s: %+v", kind, def.Key, row)
			}
			if !strings.Contains(fmt.Sprint(row["recommendation"]), def.Recommendation) {
				t.Fatalf("policy row recommendation drift for %s.%s: %+v", kind, def.Key, row)
			}
			evidence, ok := row["evidence_json"].(map[string]any)
			if !ok || evidence["enforcement"] != def.Enforcement {
				t.Fatalf("policy row evidence drift for %s.%s: %+v", kind, def.Key, row["evidence_json"])
			}
		}
	}
}

func TestGraphQLControlPlaneWorkflowExecutionReadOnlyMatrix(t *testing.T) {
	t.Run("prod mode blocks execution by table default", func(t *testing.T) {
		svc := newControlPlaneGraphQLTestServiceWithConfig(t, MCPConfig{}, createSQLiteDBFile(t, "prod.sqlite3", true), func(conf *Config) {
			conf.Core.Mode = "prod"
		})

		ctx := context.WithValue(context.Background(), core.UserIDKey, "company-user")
		_, err := svc.gj.GraphQL(ctx, `mutation {
			gj_workflow_execution(insert: { workflow_name: "daily_report" }) { status error }
		}`, nil, &core.RequestConfig{})
		if err == nil || !strings.Contains(err.Error(), "read-only") {
			t.Fatalf("expected prod-mode read-only workflow execution block, got %v", err)
		}
	})

	t.Run("agentic mode allows execution unless read-only is configured", func(t *testing.T) {
		svc := newControlPlaneGraphQLTestServiceWithConfig(t, MCPConfig{}, createSQLiteDBFile(t, "agentic.sqlite3", true), func(conf *Config) {
			conf.Core.Mode = "agentic"
		})

		ctx := context.WithValue(context.Background(), core.UserIDKey, "company-user")
		res, err := svc.gj.GraphQL(ctx, `mutation {
			gj_workflow_execution(insert: { workflow_name: "missing_workflow" }) { status error }
		}`, nil, &core.RequestConfig{})
		if err != nil {
			t.Fatalf("agentic mode should not be blocked by read-only default: %v", err)
		}
		if !strings.Contains(string(res.Data), "missing_workflow") {
			t.Fatalf("expected workflow handler response, got %s", string(res.Data))
		}
	})

	t.Run("workflow source read-only blocks execution in agentic mode", func(t *testing.T) {
		svc := newControlPlaneGraphQLTestServiceWithConfig(t, MCPConfig{}, createSQLiteDBFile(t, "agentic-readonly.sqlite3", true), func(conf *Config) {
			conf.Core.Mode = "agentic"
			conf.Core.Sources[2].ReadOnly = true
		})

		ctx := context.WithValue(context.Background(), core.UserIDKey, "company-user")
		_, err := svc.gj.GraphQL(ctx, `mutation {
			gj_workflow_execution(insert: { workflow_name: "daily_report" }) { status error }
		}`, nil, &core.RequestConfig{})
		if err == nil || !strings.Contains(err.Error(), "read-only") {
			t.Fatalf("expected read-only workflows source to block execution, got %v", err)
		}
	})

	t.Run("agentic mode blocks anonymous execution by role default", func(t *testing.T) {
		svc := newControlPlaneGraphQLTestServiceWithConfig(t, MCPConfig{}, createSQLiteDBFile(t, "agentic-anon.sqlite3", true), func(conf *Config) {
			conf.Core.Mode = "agentic"
		})

		_, err := svc.gj.GraphQL(context.Background(), `mutation {
			gj_workflow_execution(insert: { workflow_name: "missing_workflow" }) { status error }
		}`, nil, &core.RequestConfig{})
		if err == nil || !strings.Contains(err.Error(), "blocked") {
			t.Fatalf("expected anonymous agentic workflow execution block, got %v", err)
		}
	})

	t.Run("workflow execution rejects non-insert mutations", func(t *testing.T) {
		svc := newControlPlaneGraphQLTestServiceWithConfig(t, MCPConfig{}, createSQLiteDBFile(t, "agentic-update.sqlite3", true), func(conf *Config) {
			conf.Core.Mode = "agentic"
		})

		ctx := context.WithValue(context.Background(), core.UserIDKey, "company-user")
		_, err := svc.gj.GraphQL(ctx, `mutation {
			gj_workflow_execution(where: { id: { eq: "run" } }, update: { status: "ok" }) { status error }
		}`, nil, &core.RequestConfig{})
		if err == nil || !strings.Contains(err.Error(), "only supports insert") {
			t.Fatalf("expected non-insert workflow execution mutation block, got %v", err)
		}
	})
}

func TestRedactedConfigValueRedactsPassphrases(t *testing.T) {
	redacted, ok := redactedConfigValue(map[string]any{
		"key_passphrase": "secret-passphrase",
		"nested": map[string]any{
			"api_token": "secret-token",
		},
	}).(map[string]any)
	if !ok {
		t.Fatalf("expected map redaction result, got %T", redacted)
	}
	if redacted["key_passphrase"] != "[REDACTED]" {
		t.Fatalf("expected key_passphrase to be redacted, got %+v", redacted)
	}
	nested := redacted["nested"].(map[string]any)
	if nested["api_token"] != "[REDACTED]" {
		t.Fatalf("expected nested api_token to be redacted, got %+v", nested)
	}
}

func TestGraphQLControlPlaneRemovesLegacyCatalogRoots(t *testing.T) {
	svc := newControlPlaneGraphQLTestService(t, MCPConfig{}, createSQLiteDBFile(t, "app.sqlite3", true))

	for _, query := range []string{
		`query { gj_catalog_cards(limit: 1) { id } }`,
		`query { gj_catalog_card_details(limit: 1) { id } }`,
		`query { gj_nodes(limit: 1) { id } }`,
		`query { gj_edges(limit: 1) { id } }`,
		`query { gj_entrypoints(limit: 1) { id } }`,
		`query { gj_capabilities(limit: 1) { id } }`,
		`query { gj_system_capabilities(limit: 1) { name } }`,
	} {
		_, err := svc.gj.GraphQL(context.Background(), query, nil, &core.RequestConfig{})
		if err == nil {
			t.Fatalf("expected legacy catalog root to be unavailable: %s", query)
		}
	}
}

func TestGraphQLControlPlaneRemovesLegacyConfigRoots(t *testing.T) {
	svc := newControlPlaneGraphQLTestService(t, MCPConfig{AllowConfigUpdates: true}, createSQLiteDBFile(t, "app.sqlite3", true))

	_, err := svc.gj.GraphQL(context.Background(), `query {
		gj_config_settings { id }
	}`, nil, &core.RequestConfig{})
	if err == nil {
		t.Fatal("expected gj_config_settings query root to be unavailable")
	}

	_, err = svc.gj.GraphQL(context.Background(), `mutation {
		gj_config_patches(insert: { mode: "apply", patch: {} }) { id }
	}`, nil, &core.RequestConfig{})
	if err == nil {
		t.Fatal("expected gj_config_patches mutation root to be unavailable")
	}
}

func TestGraphQLControlPlaneConfigValidationAndRepair(t *testing.T) {
	svc := newControlPlaneGraphQLTestService(t, MCPConfig{AllowConfigUpdates: true}, createSQLiteDBFile(t, "app.sqlite3", true))

	res, err := svc.gj.GraphQL(sourceModeAdminTestContext(), `mutation {
		gj_config(id: "current", update: {
			mcp: { allow_raw_queries: true, allow_workflow_execution: true }
		}) {
			id
			mcp
			catalog_revision
		}
	}`, nil, &core.RequestConfig{})
	if err != nil {
		t.Fatalf("config update error: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("config update returned errors: %+v", res.Errors)
	}
	var patched struct {
		Config struct {
			ID              string         `json:"id"`
			MCP             map[string]any `json:"mcp"`
			CatalogRevision string         `json:"catalog_revision"`
		} `json:"gj_config"`
	}
	if err := json.Unmarshal(res.Data, &patched); err != nil {
		t.Fatalf("decode config update: %v\n%s", err, string(res.Data))
	}
	if patched.Config.ID != "current" || patched.Config.CatalogRevision == "" {
		t.Fatalf("unexpected config update response: %+v", patched.Config)
	}
	if got, _ := patched.Config.MCP["allow_raw_queries"].(bool); !got {
		t.Fatalf("expected returned mcp.allow_raw_queries=true, got %+v", patched.Config.MCP)
	}
	if got, _ := patched.Config.MCP["allow_workflow_execution"].(bool); !got {
		t.Fatalf("expected returned mcp.allow_workflow_execution=true, got %+v", patched.Config.MCP)
	}
	if !svc.conf.MCP.AllowRawQueries {
		t.Fatal("expected config update to update mcp.allow_raw_queries")
	}
	if !svc.conf.MCP.AllowWorkflowExecution {
		t.Fatal("expected config update to update mcp.allow_workflow_execution")
	}

	_, err = svc.gj.GraphQL(sourceModeAdminTestContext(), `mutation {
		gj_config(id: "current", update: {
			mcp: { unsupported_flag: true }
		}) {
			id
		}
	}`, nil, &core.RequestConfig{})
	if err == nil || !strings.Contains(err.Error(), "unsupported mcp config key") {
		t.Fatalf("expected unsupported mcp config key error, got %v", err)
	}

	for _, query := range []string{
		`mutation { gj_schema_reloads(insert: {}) { id } }`,
		`mutation { gj_schema_change_sets(insert: { action: "preview" }) { id } }`,
		`mutation { gj_query_validations(insert: { table: "users", where: { id: { eq: 1 } } }) { id } }`,
		`mutation { gj_query_repairs(insert: { query: "query { userz { id } }", error: "table not found: userz" }) { id } }`,
	} {
		_, err = svc.gj.GraphQL(context.Background(), query, nil, &core.RequestConfig{})
		if err == nil {
			t.Fatalf("expected removed control-plane mutation root to be unavailable: %s", query)
		}
	}
}

func TestGraphQLControlPlaneConfigRejectsPlaintextSecretWithoutKeystoreKey(t *testing.T) {
	svc := newControlPlaneGraphQLTestService(t, MCPConfig{AllowConfigUpdates: true}, createSQLiteDBFile(t, "app.sqlite3", true))

	_, err := svc.gj.GraphQL(sourceModeAdminTestContext(), `mutation {
		gj_config(id: "current", update: {
			sources: [{
				name: "main",
				kind: "database",
				type: "sqlite",
				connection_string: "/tmp/model-supplied-secret.sqlite3"
			}]
		}) {
			id
		}
	}`, nil, &core.RequestConfig{})
	if err == nil || !strings.Contains(err.Error(), "secrets.keystore.key") {
		t.Fatalf("expected missing keystore key error, got %v", err)
	}
}

func TestGraphQLControlPlaneAllowsAppGJPrefixWithSystemSource(t *testing.T) {
	dbPath := createSQLiteDBFile(t, "reserved.sqlite3", true)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE gj_custom (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("create reserved table: %v", err)
	}
	db.Close()

	conf := &Config{
		Core: core.Config{
			DBType:           "sqlite",
			DisableAllowList: true,
			Sources: []core.SourceConfig{
				{Name: "main", Kind: "database", Type: "sqlite", Path: dbPath, Default: true},
				{Name: "graphjin", Kind: "graphjin"},
			},
		},
		Serv: Serv{ConfigPath: filepath.Join(t.TempDir(), "config")},
	}
	svc, err := newGraphJinService(conf, nil)
	if err != nil {
		t.Fatalf("app gj_ table should not collide with graphjin nanoDB source: %v", err)
	}
	if svc != nil {
		closeTestService(svc)
	}
}

func newControlPlaneGraphQLTestService(t *testing.T, cfg MCPConfig, dbPath string) *graphjinService {
	return newControlPlaneGraphQLTestServiceWithConfig(t, cfg, dbPath, nil)
}

func newControlPlaneGraphQLTestServiceWithConfig(t *testing.T, cfg MCPConfig, dbPath string, configure func(*Config)) *graphjinService {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	fs := newAferoFS(afero.NewMemMapFs(), "/")
	conf := &Config{
		Core: core.Config{
			DBType:           "sqlite",
			Production:       false,
			DisableAllowList: true,
			Sources: []core.SourceConfig{
				{Name: "main", Kind: "database", Type: "sqlite", Path: dbPath, Default: true},
				{Name: "graphjin", Kind: "graphjin"},
				{Name: "workflows", Kind: "workflow"},
			},
		},
		Serv: Serv{Production: false, MCP: cfg},
	}
	if configure != nil {
		configure(conf)
	}
	applySourceCapabilitySourceDefaults(conf)
	svc := &graphjinService{
		conf:   conf,
		dbs:    map[string]*sql.DB{"main": db},
		fs:     fs,
		log:    zaptest.NewLogger(t).Sugar(),
		tracer: otel.Tracer("graphjin-control-plane-test"),
	}
	if err := normalizeServiceSources(conf); err != nil {
		t.Fatalf("normalize sources: %v", err)
	}
	applySourceCapabilityMCPDefaults(conf)
	runtimeCore := cloneCoreConfig(conf.Core)
	svc.runtimeCore = &runtimeCore
	if err := svc.initRuntimeObservability(); err != nil {
		t.Fatalf("init runtime observability: %v", err)
	}
	if err := svc.initSystemNanoDBBeforeCore(); err != nil {
		t.Fatalf("init system nanodb: %v", err)
	}
	gj, err := core.NewGraphJin(svc.runtimeCore, db, svc.buildCoreOptionsFor(svc.dbs, nil)...)
	if err != nil {
		t.Fatalf("init graphjin: %v", err)
	}
	t.Cleanup(func() { gj.Close() })
	svc.gj = gj
	if err := svc.refreshSystemNanoDB(); err != nil {
		t.Fatalf("refresh system nanodb: %v", err)
	}
	return svc
}
