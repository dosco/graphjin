package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/fstable"
)

// fakeFileContent answers searches from a fixed ranking and extracts text
// by upper-casing the file body, so the test sees which path served it.
type fakeFileContent struct{}

func (fakeFileContent) Text(_ context.Context, source string, e fstable.Entry, data []byte) (string, bool, error) {
	if !strings.HasSuffix(e.Key, ".pdf") {
		return "", false, nil
	}
	return "[page 1]\nextracted from " + source + "/" + e.Key, true, nil
}

func (fakeFileContent) Search(_ context.Context, source, query string, limit int) ([]FileMatch, error) {
	matches := []FileMatch{
		{Key: "b/report.pdf", Score: 2, Passages: []FilePassage{{Location: "page 3", Snippet: "the " + query}}},
		{Key: "gone.pdf", Score: 1.5},
		{Key: "a/prices.xlsx", Score: 1, Passages: []FilePassage{{Location: "sheet Q4", Snippet: query}}},
	}
	if len(matches) > limit {
		matches = matches[:limit]
	}
	return matches, nil
}

func (fakeFileContent) Tables(_ context.Context, source string, e fstable.Entry) ([]FileTable, error) {
	if !strings.HasSuffix(e.Key, ".xlsx") {
		return nil, nil
	}
	return []FileTable{{Name: "prices__q4", Sheet: "Q4", Rows: 2, Columns: []FileTableColumn{{Name: "sku", Type: "VARCHAR"}}}}, nil
}

func fileContentEngine(t *testing.T, opts ...Option) *GraphJin {
	t.Helper()
	root := t.TempDir()
	mkfile(t, root, "a/prices.xlsx", []byte{0x50, 0x4b, 0x03, 0x04, 0xff})
	mkfile(t, root, "b/report.pdf", []byte{0x25, 0x50, 0x44, 0x46, 0xff})
	mkfile(t, root, "b/notes.txt", []byte("plain notes"))
	cfgRoot := t.TempDir()
	schema := []byte("# dbinfo:postgres,120005,public\n\ntype users {\n  id: Bigint! @id @unique\n}\n")
	if err := os.WriteFile(filepath.Join(cfgRoot, "db.ddl"), schema, 0o644); err != nil {
		t.Fatal(err)
	}
	gj, err := NewGraphJinWithFS(&Config{
		MockDB: true, EnableSchema: true, DisableAllowList: true, DBType: "postgres",
		Sources: []SourceConfig{
			{Name: "app", Kind: "database", Type: "postgres", Default: true},
			{Name: "docs", Kind: "file", Backend: "local", Root: root},
		},
	}, nil, NewOsFS(cfgRoot), opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { gj.Close() })
	return gj
}

func TestFileContentSearchRanksFilesWithPassagesAndTables(t *testing.T) {
	gj := fileContentEngine(t, OptionSetFileContent(fakeFileContent{}))
	res, err := gj.GraphQL(context.Background(),
		`query { docs(search: "rogers laminate", limit: 5) { key passages tables } }`, nil, nil)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	var out struct {
		Docs []struct {
			Key      string        `json:"key"`
			Passages []FilePassage `json:"passages"`
			Tables   []FileTable   `json:"tables"`
		} `json:"docs"`
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		t.Fatalf("%v\n%s", err, res.Data)
	}
	// Relevance order, a match whose file is gone is skipped.
	if len(out.Docs) != 2 || out.Docs[0].Key != "b/report.pdf" || out.Docs[1].Key != "a/prices.xlsx" {
		t.Fatalf("docs = %s", res.Data)
	}
	if out.Docs[0].Passages[0].Location != "page 3" || out.Docs[0].Passages[0].Snippet != "the rogers laminate" {
		t.Fatalf("passages = %+v", out.Docs[0].Passages)
	}
	if len(out.Docs[0].Tables) != 0 || len(out.Docs[1].Tables) != 1 || out.Docs[1].Tables[0].Name != "prices__q4" {
		t.Fatalf("tables = %s", res.Data)
	}

	// where still filters search results.
	res, err = gj.GraphQL(context.Background(),
		`query { docs(search: "rogers", where: { key: { like: "a/%" } }) { key } }`, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(res.Data), "a/prices.xlsx") || strings.Contains(string(res.Data), "report") {
		t.Fatalf("filtered search = %s", res.Data)
	}
}

func TestFileContentTextExtractsBinaryFiles(t *testing.T) {
	gj := fileContentEngine(t, OptionSetFileContent(fakeFileContent{}))
	res, err := gj.GraphQL(context.Background(),
		`query { docs(prefix: "b/") { key text } }`, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Docs []map[string]any `json:"docs"`
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		t.Fatal(err)
	}
	got := map[string]any{}
	for _, d := range out.Docs {
		got[d["key"].(string)] = d["text"]
	}
	if got["b/notes.txt"] != "plain notes" || got["b/report.pdf"] != "[page 1]\nextracted from docs/b/report.pdf" {
		t.Fatalf("text = %s", res.Data)
	}
}

func TestFileContentColumnsNeedTheReader(t *testing.T) {
	gj := fileContentEngine(t)
	if _, err := gj.GraphQL(context.Background(), `query { docs(search: "x") { key } }`, nil, nil); err == nil {
		t.Fatal("search without a content reader should fail")
	}
	if _, err := gj.GraphQL(context.Background(), `query { docs { key passages } }`, nil, nil); err == nil {
		t.Fatal("passages without a content reader should be an unknown column")
	}
}
