package serv

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/spf13/afero"
)

func TestNormalizeWorkflowName(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "plain", in: "daily_report", want: "daily_report"},
		{name: "prefixed slash", in: "/daily_report", want: "daily_report"},
		{name: "js suffix", in: "daily_report.js", want: "daily_report"},
		{name: "path traversal", in: "../secret", wantErr: true},
		{name: "slash path", in: "team/daily", wantErr: true},
		{name: "empty", in: "", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeWorkflowName(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRunNamedWorkflow_LoadsFromWorkflowsDir(t *testing.T) {
	mem := afero.NewMemMapFs()
	if err := mem.MkdirAll("/workflows", 0o755); err != nil {
		t.Fatalf("mkdir workflows: %v", err)
	}
	if err := afero.WriteFile(mem, "/workflows/hello.js", []byte(`function main(input) { return { ok: true, value: input.value }; }`), 0o644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	s := &graphjinService{
		fs:   newAferoFS(mem, "/"),
		conf: &Config{},
	}

	out, err := s.runNamedWorkflow(context.Background(), "hello", map[string]any{"value": 7}, nil)
	if err != nil {
		t.Fatalf("runNamedWorkflow error: %v", err)
	}

	res, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", out)
	}

	if okVal, _ := res["ok"].(bool); !okVal {
		t.Fatalf("expected ok=true, got %#v", res["ok"])
	}
	if val, _ := res["value"].(int64); val != 7 {
		t.Fatalf("expected value=7, got %#v", res["value"])
	}
}

func TestRunNamedWorkflow_CanCallGJTools(t *testing.T) {
	mem := afero.NewMemMapFs()
	if err := mem.MkdirAll("/workflows", 0o755); err != nil {
		t.Fatalf("mkdir workflows: %v", err)
	}
	if err := afero.WriteFile(mem, "/workflows/syntax.js", []byte(`
function main() {
  return gj.tools.getQuerySyntax({});
}
`), 0o644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	s := &graphjinService{
		fs:   newAferoFS(mem, "/"),
		conf: &Config{},
	}

	out, err := s.runNamedWorkflow(context.Background(), "syntax", map[string]any{}, nil)
	if err != nil {
		t.Fatalf("runNamedWorkflow error: %v", err)
	}

	res, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", out)
	}

	if _, ok := res["filter_operators"]; !ok {
		t.Fatalf("expected get_query_syntax output, got keys: %#v", res)
	}
}

func TestRunNamedWorkflow_CanExecuteGraphQLWhenAllowed(t *testing.T) {
	ms := newSQLiteReadyMCPServer(t, nil, nil)
	ms.service.conf.MCP.AllowRawQueries = true

	if err := ms.service.fs.Put("/workflows/query.js", []byte(`
function main(input) {
  return gj.tools.executeGraphql({
    query: "query { users(where: { id: { eq: $id } }) { id name } }",
    variables: { id: input.id }
  });
}
`)); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	out, err := ms.service.runNamedWorkflow(context.Background(), "query", map[string]any{"id": 1}, nil)
	if err != nil {
		t.Fatalf("runNamedWorkflow error: %v", err)
	}

	res, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", out)
	}

	data, ok := res["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected executeGraphql data payload, got %#v", res)
	}
	users, ok := data["users"].([]any)
	if !ok || len(users) != 1 {
		t.Fatalf("expected one user, got %#v", data["users"])
	}
}

func TestHandleExecuteWorkflow_PassesVariables(t *testing.T) {
	mem := afero.NewMemMapFs()
	if err := mem.MkdirAll("/workflows", 0o755); err != nil {
		t.Fatalf("mkdir workflows: %v", err)
	}
	if err := afero.WriteFile(mem, "/workflows/echo.js", []byte(`function main(input) { return { seen: input.value }; }`), 0o644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	s := &graphjinService{
		fs:   newAferoFS(mem, "/"),
		conf: &Config{},
	}
	ms := &mcpServer{service: s}

	res, err := ms.handleExecuteWorkflow(context.Background(), newToolRequest(map[string]any{
		"name": "echo",
		"variables": map[string]any{
			"value": 42,
		},
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := assertToolSuccess(t, res)

	var out struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got, _ := out.Data["seen"].(float64); got != 42 {
		t.Fatalf("expected seen=42, got %v", out.Data["seen"])
	}
}

func TestHandleExecuteWorkflow_RequiresDeclaredVariables(t *testing.T) {
	mem := afero.NewMemMapFs()
	if err := mem.MkdirAll("/workflows", 0o755); err != nil {
		t.Fatalf("mkdir workflows: %v", err)
	}
	src := `// @graphjin-workflow {"description":"Echo named input","variables":[{"name":"value","type":"number","required":true}]}
function main(input) { return { seen: input.value }; }
`
	if err := afero.WriteFile(mem, "/workflows/echo.js", []byte(src), 0o644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	s := &graphjinService{
		fs:   newAferoFS(mem, "/"),
		conf: &Config{},
	}
	ms := &mcpServer{service: s}

	res, err := ms.handleExecuteWorkflow(context.Background(), newToolRequest(map[string]any{
		"name":      "echo",
		"variables": map[string]any{},
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !res.IsError {
		t.Fatal("expected missing required workflow variable to fail")
	}
	assertToolError(t, res, "workflow requires variables")
	assertToolError(t, res, "value")

	res, err = ms.handleExecuteWorkflow(context.Background(), newToolRequest(map[string]any{
		"name": "echo",
		"variables": map[string]any{
			"value": 7,
		},
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := assertToolSuccess(t, res)
	var out struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got, _ := out.Data["seen"].(float64); got != 7 {
		t.Fatalf("expected seen=7 after providing required workflow variable, got %v", out.Data["seen"])
	}
}

func TestRunNamedWorkflow_RespectsContextCancellation(t *testing.T) {
	mem := afero.NewMemMapFs()
	if err := mem.MkdirAll("/workflows", 0o755); err != nil {
		t.Fatalf("mkdir workflows: %v", err)
	}
	if err := afero.WriteFile(mem, "/workflows/spin.js", []byte(`function main() { while (true) {} }`), 0o644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	s := &graphjinService{
		fs:   newAferoFS(mem, "/"),
		conf: &Config{},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	_, err := s.runNamedWorkflow(ctx, "spin", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected timeout/cancel error")
	}

	if !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("expected deadline exceeded error, got: %v", err)
	}
}

func TestRunNamedWorkflow_BlocksExecuteWorkflowTool(t *testing.T) {
	mem := afero.NewMemMapFs()
	if err := mem.MkdirAll("/workflows", 0o755); err != nil {
		t.Fatalf("mkdir workflows: %v", err)
	}
	if err := afero.WriteFile(mem, "/workflows/nested.js", []byte(`
function main() {
  return gj.tools.call("execute_workflow", { name: "nested" });
}
`), 0o644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	s := &graphjinService{
		fs:   newAferoFS(mem, "/"),
		conf: &Config{},
	}

	_, err := s.runNamedWorkflow(context.Background(), "nested", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error from blocked execute_workflow tool")
	}

	if !strings.Contains(err.Error(), "cannot be called from workflow runtime") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunNamedWorkflow_BlocksWorkflowMutationTools(t *testing.T) {
	mem := afero.NewMemMapFs()
	if err := mem.MkdirAll("/workflows", 0o755); err != nil {
		t.Fatalf("mkdir workflows: %v", err)
	}
	if err := afero.WriteFile(mem, "/workflows/nope.js", []byte(`
function main() {
  return gj.tools.call("save_workflow", {
    name: "oops",
    description: "nope",
    code: "function main(input) { return input; }"
  });
}
`), 0o644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	s := &graphjinService{
		fs:   newAferoFS(mem, "/"),
		conf: &Config{},
	}

	_, err := s.runNamedWorkflow(context.Background(), "nope", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected blocked workflow mutation tool error")
	}
	if !strings.Contains(err.Error(), "save_workflow") || !strings.Contains(err.Error(), "cannot be called from workflow runtime") {
		t.Fatalf("unexpected error: %v", err)
	}
}
