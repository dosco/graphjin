package serv

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/afero"
)

func TestHandleSaveWorkflow(t *testing.T) {
	mem := afero.NewMemMapFs()
	s := &graphjinService{
		fs:   newAferoFS(mem, "/"),
		conf: &Config{},
	}
	s.conf.MCP.AllowWorkflowUpdates = true
	ms := s.newMCPServerWithContext(context.Background())

	// Save a workflow
	res, err := ms.handleSaveWorkflow(context.Background(), newToolRequest(map[string]any{
		"name":        "order_pnl",
		"description": "Fetch all orders and compute profit & loss",
		"code":        "function main(input) {\n  var tables = gj.tools.listTables();\n  return { tables: tables };\n}\n",
		"tags":        []any{"orders", "finance", "pnl"},
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := assertToolSuccess(t, res)

	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if saved, _ := out["saved"].(bool); !saved {
		t.Fatalf("expected saved=true, got %v", out["saved"])
	}
	if name, _ := out["name"].(string); name != "order_pnl" {
		t.Fatalf("expected name=order_pnl, got %v", out["name"])
	}

	// Verify file was written
	src, err := afero.ReadFile(mem, "/workflows/order_pnl.js")
	if err != nil {
		t.Fatalf("workflow file not written: %v", err)
	}
	if !strings.Contains(string(src), "@graphjin-workflow") {
		t.Fatalf("missing metadata header in saved file")
	}
	if !strings.Contains(string(src), "function main(input)") {
		t.Fatalf("missing code in saved file")
	}
}

func TestHandleListWorkflows(t *testing.T) {
	mem := afero.NewMemMapFs()
	if err := mem.MkdirAll("/workflows", 0o755); err != nil {
		t.Fatal(err)
	}

	// Write a workflow with metadata
	meta := `// @graphjin-workflow {"description":"Compute P&L from orders","tags":["orders","pnl"]}
function main(input) { return {}; }
`
	if err := afero.WriteFile(mem, "/workflows/order_pnl.js", []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write a workflow without metadata
	if err := afero.WriteFile(mem, "/workflows/legacy.js", []byte("function main() { return 1; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := &graphjinService{
		fs:   newAferoFS(mem, "/"),
		conf: &Config{},
	}
	ms := s.newMCPServerWithContext(context.Background())

	res, err := ms.handleListWorkflows(context.Background(), newToolRequest(map[string]any{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := assertToolSuccess(t, res)

	var out struct {
		Workflows []WorkflowInfo `json:"workflows"`
		Count     int            `json:"count"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if out.Count != 2 {
		t.Fatalf("expected 2 workflows, got %d", out.Count)
	}

	// Find order_pnl and verify metadata
	var found bool
	for _, w := range out.Workflows {
		if w.Name == "order_pnl" {
			found = true
			if w.Description != "Compute P&L from orders" {
				t.Fatalf("wrong description: %s", w.Description)
			}
			if len(w.Tags) != 2 || w.Tags[0] != "orders" {
				t.Fatalf("wrong tags: %v", w.Tags)
			}
		}
	}
	if !found {
		t.Fatal("order_pnl workflow not found in list")
	}
}

func TestHandleListWorkflows_EmptyDir(t *testing.T) {
	mem := afero.NewMemMapFs()
	s := &graphjinService{
		fs:   newAferoFS(mem, "/"),
		conf: &Config{},
	}
	ms := s.newMCPServerWithContext(context.Background())

	res, err := ms.handleListWorkflows(context.Background(), newToolRequest(map[string]any{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := assertToolSuccess(t, res)

	var out struct {
		Workflows []any `json:"workflows"`
		Count     int   `json:"count"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Count != 0 {
		t.Fatalf("expected 0 workflows, got %d", out.Count)
	}
}

func TestHandleSaveWorkflow_ValidationErrors(t *testing.T) {
	mem := afero.NewMemMapFs()
	s := &graphjinService{
		fs:   newAferoFS(mem, "/"),
		conf: &Config{},
	}
	s.conf.MCP.AllowWorkflowUpdates = true
	ms := s.newMCPServerWithContext(context.Background())

	tests := []struct {
		name string
		args map[string]any
	}{
		{"missing name", map[string]any{"description": "d", "code": "function main() {}"}},
		{"missing description", map[string]any{"name": "x", "code": "function main() {}"}},
		{"missing code", map[string]any{"name": "x", "description": "d"}},
		{"invalid name", map[string]any{"name": "../bad", "description": "d", "code": "function main() {}"}},
		{"no main function", map[string]any{"name": "x", "description": "d", "code": "var x = 1;"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := ms.handleSaveWorkflow(context.Background(), newToolRequest(tc.args))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !res.IsError {
				t.Fatal("expected error result")
			}
		})
	}
}

func TestParseWorkflowMeta(t *testing.T) {
	src := `// @graphjin-workflow {"description":"Test workflow","tags":["a","b"]}
function main() { return 1; }
`
	meta, ok := parseWorkflowMeta(src)
	if !ok {
		t.Fatal("expected metadata to be found")
	}
	if meta.Description != "Test workflow" {
		t.Fatalf("wrong description: %s", meta.Description)
	}
	if len(meta.Tags) != 2 {
		t.Fatalf("wrong tags count: %d", len(meta.Tags))
	}

	// No metadata
	_, ok = parseWorkflowMeta("function main() {}")
	if ok {
		t.Fatal("expected no metadata")
	}
}

