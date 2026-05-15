package serv

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/spf13/afero"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap/zaptest"
	_ "modernc.org/sqlite"
)

func TestGraphQLControlPlaneCatalogQuery(t *testing.T) {
	svc := newControlPlaneGraphQLTestService(t, MCPConfig{}, createSQLiteDBFile(t, "app.sqlite3", true))

	res, err := svc.gj.GraphQL(context.Background(), `query {
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

func TestGraphQLControlPlaneWorkflowLifecycle(t *testing.T) {
	svc := newControlPlaneGraphQLTestService(t, MCPConfig{AllowWorkflowUpdates: true}, createSQLiteDBFile(t, "app.sqlite3", true))

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
	res, err := svc.gj.GraphQL(context.Background(), create, nil, &core.RequestConfig{})
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
	res, err = svc.gj.GraphQL(context.Background(), update, nil, &core.RequestConfig{})
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

	res, err = svc.gj.GraphQL(context.Background(), `query {
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
	res, err = svc.gj.GraphQL(context.Background(), run, nil, &core.RequestConfig{})
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

	res, err = svc.gj.GraphQL(context.Background(), `query {
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

	res, err = svc.gj.GraphQL(context.Background(), `mutation {
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

	for _, name := range []string{"alpha", "bravo"} {
		res, err := svc.gj.GraphQL(context.Background(), `mutation {
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

	res, err := svc.gj.GraphQL(context.Background(), `query {
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
	for _, name := range []string{"id", "kind", "report", "mode", "capability", "severity", "summary_json", "evidence_json", "examples_json", "safety_json"} {
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

	res, err := svc.gj.GraphQL(context.Background(), `query {
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

func TestGraphQLControlPlaneCatalogSecurityGuidance(t *testing.T) {
	svc := newControlPlaneGraphQLTestService(t, MCPConfig{}, createSQLiteDBFile(t, "app.sqlite3", true))

	res, err := svc.gj.GraphQL(context.Background(), `query {
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

func TestSecurityNanoRowsModes(t *testing.T) {
	conf := &Config{
		Core: core.Config{
			SecurityMode: "agentic",
			Sources: []core.SourceConfig{
				{Name: "graphjin", Kind: "graphjin"},
				{Name: "workflows", Kind: "workflows"},
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

func TestGraphQLControlPlaneWorkflowExecutionReadOnlyMatrix(t *testing.T) {
	t.Run("prod mode blocks execution by table default", func(t *testing.T) {
		svc := newControlPlaneGraphQLTestServiceWithConfig(t, MCPConfig{}, createSQLiteDBFile(t, "prod.sqlite3", true), func(conf *Config) {
			conf.Core.SecurityMode = "prod"
		})

		_, err := svc.gj.GraphQL(context.Background(), `mutation {
			gj_workflow_execution(insert: { workflow_name: "daily_report" }) { status error }
		}`, nil, &core.RequestConfig{})
		if err == nil || !strings.Contains(err.Error(), "read-only") {
			t.Fatalf("expected prod-mode read-only workflow execution block, got %v", err)
		}
	})

	t.Run("agentic mode allows execution unless read-only is configured", func(t *testing.T) {
		svc := newControlPlaneGraphQLTestServiceWithConfig(t, MCPConfig{}, createSQLiteDBFile(t, "agentic.sqlite3", true), func(conf *Config) {
			conf.Core.SecurityMode = "agentic"
		})

		res, err := svc.gj.GraphQL(context.Background(), `mutation {
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
			conf.Core.SecurityMode = "agentic"
			conf.Core.Sources[2].ReadOnly = true
		})

		_, err := svc.gj.GraphQL(context.Background(), `mutation {
			gj_workflow_execution(insert: { workflow_name: "daily_report" }) { status error }
		}`, nil, &core.RequestConfig{})
		if err == nil || !strings.Contains(err.Error(), "read-only") {
			t.Fatalf("expected read-only workflows source to block execution, got %v", err)
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

	res, err := svc.gj.GraphQL(context.Background(), `mutation {
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

	_, err = svc.gj.GraphQL(context.Background(), `mutation {
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
				{Name: "main", Kind: "sql", Type: "sqlite", Path: dbPath, Default: true},
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
				{Name: "main", Kind: "sql", Type: "sqlite", Path: dbPath, Default: true},
				{Name: "graphjin", Kind: "graphjin"},
				{Name: "workflows", Kind: "workflows"},
			},
		},
		Serv: Serv{Production: false, MCP: cfg},
	}
	if configure != nil {
		configure(conf)
	}
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
	runtimeCore := cloneCoreConfig(conf.Core)
	svc.runtimeCore = &runtimeCore
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
