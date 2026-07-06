package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportArtifactsWritesGitFriendlyLayout(t *testing.T) {
	srv := newTestMCPServer(t, func(req *jsonRPCRequest, _ *http.Request) (any, *jsonRPCError, int) {
		if req.Params["name"] != "execute_graphql" {
			t.Fatalf("tool = %v, want execute_graphql", req.Params["name"])
		}
		args, _ := req.Params["arguments"].(map[string]any)
		query, _ := args["query"].(string)
		if !strings.Contains(query, "gj_artifacts") || !strings.Contains(query, "order_by") {
			t.Fatalf("unexpected export query: %s", query)
		}
		body := `{
			"data": {
				"gj_artifacts": [
					{
						"name": "user_lookup",
						"kind": "saved_query",
						"content": "query user_lookup { users { id name } }\n",
						"metadata_json": {"operation": "query"}
					},
					{
						"name": "user_fields",
						"kind": "fragment",
						"content": "fragment user_fields on users { id name }\n"
					},
					{
						"name": "sync_inventory",
						"kind": "workflow",
						"content": "export default async function run() { return { ok: true } }\n",
						"metadata_json": {"description": "Sync inventory"}
					},
					{
						"name": "daily_digest",
						"kind": "watch",
						"content_json": {"schedule": "daily"},
						"metadata_json": {"enabled": true}
					}
				]
			}
		}`
		return mcpToolResult{
			StructuredContent: json.RawMessage(body),
			Content:           []mcpContent{{Type: "text", Text: body}},
		}, nil, http.StatusOK
	})
	defer srv.Close()
	resetMCPClientFlags(srv.URL)

	dir := t.TempDir()
	summary, err := exportArtifacts(context.Background(), newEmptyCobraCmd(), artifactTransferOptions{Dir: dir})
	if err != nil {
		t.Fatalf("exportArtifacts: %v", err)
	}
	if summary.Exported != 4 {
		t.Fatalf("exported = %d, want 4", summary.Exported)
	}

	assertFileContains(t, filepath.Join(dir, "queries", "user_lookup.gql"), "query user_lookup")
	assertFileContains(t, filepath.Join(dir, "queries", "user_lookup.json"), `"operation": "query"`)
	assertFileContains(t, filepath.Join(dir, "fragments", "user_fields.gql"), "fragment user_fields")
	assertFileContains(t, filepath.Join(dir, "scripts", "workflows", "sync_inventory.js"), workflowMetaPrefix+`{"description":"Sync inventory"}`)
	assertFileContains(t, filepath.Join(dir, "watches", "daily_digest.watch.json"), `"schedule": "daily"`)
}

func TestImportArtifactsUpsertsViaExecuteGraphQL(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "queries", "user_lookup.gql"), "query user_lookup { users { id name } }\n")
	writeTestFile(t, filepath.Join(dir, "queries", "user_lookup.json"), `{"operation":"query"}`)
	writeTestFile(t, filepath.Join(dir, "scripts", "workflows", "sync_inventory.js"), workflowMetaPrefix+`{"description":"Sync inventory"}`+"\nexport default async function run() {}\n")
	writeTestFile(t, filepath.Join(dir, "watches", "daily_digest.watch.json"), `{"content_json":{"schedule":"daily"},"metadata_json":{"enabled":true}}`)

	var upserts []map[string]any
	srv := newTestMCPServer(t, func(req *jsonRPCRequest, _ *http.Request) (any, *jsonRPCError, int) {
		if req.Params["name"] != "execute_graphql" {
			t.Fatalf("tool = %v, want execute_graphql", req.Params["name"])
		}
		args, _ := req.Params["arguments"].(map[string]any)
		query, _ := args["query"].(string)
		if !strings.Contains(query, "gj_artifacts(upsert: $data)") {
			t.Fatalf("unexpected import mutation: %s", query)
		}
		vars, _ := args["variables"].(map[string]any)
		data, _ := vars["data"].(map[string]any)
		upserts = append(upserts, data)
		body := `{"data":{"gj_artifacts":{"name":"ok","kind":"saved_query"}}}`
		return mcpToolResult{
			StructuredContent: json.RawMessage(body),
			Content:           []mcpContent{{Type: "text", Text: body}},
		}, nil, http.StatusOK
	})
	defer srv.Close()
	resetMCPClientFlags(srv.URL)

	summary, err := importArtifacts(context.Background(), newEmptyCobraCmd(), artifactTransferOptions{Dir: dir})
	if err != nil {
		t.Fatalf("importArtifacts: %v", err)
	}
	if summary.Imported != 3 || len(upserts) != 3 {
		t.Fatalf("imported=%d upserts=%d, want 3/3", summary.Imported, len(upserts))
	}

	query := artifactUpsertByKindName(t, upserts, "saved_query", "user_lookup")
	if !strings.Contains(query["content"].(string), "query user_lookup") {
		t.Fatalf("query content not preserved: %+v", query)
	}
	queryMeta, _ := query["metadata_json"].(map[string]any)
	if queryMeta["operation"] != "query" {
		t.Fatalf("query metadata = %+v", queryMeta)
	}

	workflow := artifactUpsertByKindName(t, upserts, "workflow", "sync_inventory")
	workflowMeta, _ := workflow["metadata_json"].(map[string]any)
	if workflowMeta["description"] != "Sync inventory" {
		t.Fatalf("workflow metadata = %+v", workflowMeta)
	}

	watch := artifactUpsertByKindName(t, upserts, "watch", "daily_digest")
	watchContent, _ := watch["content_json"].(map[string]any)
	if watchContent["schedule"] != "daily" {
		t.Fatalf("watch content_json = %+v", watchContent)
	}
}

func TestImportArtifactsDryRunSkipsServer(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "queries", "user_lookup.gql"), "query user_lookup { users { id } }\n")
	resetMCPClientFlags("")

	summary, err := importArtifacts(context.Background(), newEmptyCobraCmd(), artifactTransferOptions{Dir: dir, DryRun: true})
	if err != nil {
		t.Fatalf("importArtifacts dry-run: %v", err)
	}
	if summary.Imported != 1 || len(summary.Files) != 1 {
		t.Fatalf("summary = %+v, want one planned import", summary)
	}
}

func assertFileContains(t *testing.T, path, want string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(b), want) {
		t.Fatalf("%s does not contain %q:\n%s", path, want, string(b))
	}
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func artifactUpsertByKindName(t *testing.T, rows []map[string]any, kind, name string) map[string]any {
	t.Helper()
	for _, row := range rows {
		if row["kind"] == kind && row["name"] == name {
			return row
		}
	}
	t.Fatalf("missing upsert kind=%s name=%s in %+v", kind, name, rows)
	return nil
}
