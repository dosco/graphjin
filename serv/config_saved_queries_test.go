package serv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3"
)

func TestParseSavedQueryUpdates(t *testing.T) {
	writes, removals, changes, errs := parseSavedQueryUpdates(map[string]any{
		"update_saved_queries": []any{
			map[string]any{"name": "forum_latest", "query": "  query forum_latest { latest { id } }  "},
			map[string]any{"name": "../escape", "query": "query { a }"},
			map[string]any{"name": "empty", "query": "  "},
			"not an object",
		},
		"remove_saved_queries": []any{"old_query", "bad/name"},
	})
	if len(writes) != 1 || writes[0].name != "forum_latest" || writes[0].query != "query forum_latest { latest { id } }" {
		t.Fatalf("writes = %+v", writes)
	}
	if len(removals) != 1 || removals[0] != "old_query" {
		t.Fatalf("removals = %+v", removals)
	}
	if len(changes) != 2 {
		t.Fatalf("changes = %v", changes)
	}
	if len(errs) != 4 {
		t.Fatalf("errs = %v, want the bad name, the empty query, the non-object and the bad removal", errs)
	}
}

func TestApplySavedQueryUpdatesWritesAndRemovesFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "queries"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "queries", "old_query.gql"), []byte("query old_query { a }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ms := &mcpServer{service: &graphjinService{fs: core.NewOsFS(dir)}}

	err := ms.applySavedQueryUpdates(
		[]savedQueryUpdate{{name: "forum_latest", query: "query forum_latest { latest { id } }"}},
		[]string{"old_query", "never_existed"},
	)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "queries", "forum_latest.gql"))
	if err != nil || strings.TrimSpace(string(got)) != "query forum_latest { latest { id } }" {
		t.Fatalf("forum_latest.gql = %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "queries", "old_query.gql")); !os.IsNotExist(err) {
		t.Fatalf("old_query.gql should be removed, stat err = %v", err)
	}
}
