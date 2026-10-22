package serv

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3"
)

func fileContentTestService(t *testing.T, enabled bool) *graphjinService {
	t.Helper()
	configRoot := t.TempDir()
	files := filepath.Join(configRoot, "files")
	for _, dir := range []string{"policies", "pricing"} {
		if err := os.MkdirAll(filepath.Join(files, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, filepath.Join(files, "policies", "travel.md"), "# Travel\nBook economy.\n# Per diem\nTier 1 cities pay 70 per day.\n")
	writeTestFile(t, filepath.Join(files, "pricing", "laminates.csv"), "SKU,Material,Price\nHI-1,Rogers 4350B,12.5\nHI-2,FR-4,7\nHI-3,Rogers 3003,19\n")

	duck := os.Getenv("GRAPHJIN_TEST_DUCKDB")
	conf := &Config{
		Core: Core{
			Mode: "dev",
			Sources: []core.SourceConfig{
				{Name: "docs", Kind: "file", Backend: "local", Root: "files", ReadOnly: true},
			},
		},
		Serv: Serv{
			ConfigPath:  configRoot,
			MCP:         MCPConfig{Disable: true},
			FileContent: FileContentConfig{Enabled: enabled, DuckDB: duck},
		},
	}
	s, err := newGraphJinService(conf, nil)
	if err != nil {
		t.Fatalf("service start: %v", err)
	}
	t.Cleanup(func() { closeTestService(s) })
	if s.gj == nil {
		t.Fatal("service started without a query engine")
	}
	return s
}

func TestFileContentSearchAndTablesThroughGraphQL(t *testing.T) {
	s := fileContentTestService(t, true)
	ctx := context.Background()
	res, err := s.gj.GraphQL(ctx, `query { docs(search: "which laminates are Rogers", limit: 3) { key passages tables } }`, nil, nil)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	var out struct {
		Docs []struct {
			Key      string             `json:"key"`
			Passages []core.FilePassage `json:"passages"`
			Tables   []core.FileTable   `json:"tables"`
		} `json:"docs"`
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		t.Fatalf("%v\n%s", err, res.Data)
	}
	if len(out.Docs) == 0 || out.Docs[0].Key != "pricing/laminates.csv" {
		t.Fatalf("search = %s", res.Data)
	}
	if len(out.Docs[0].Passages) == 0 || !strings.Contains(out.Docs[0].Passages[0].Snippet, "Rogers") {
		t.Fatalf("passages = %+v", out.Docs[0].Passages)
	}
	if len(out.Docs[0].Tables) != 2 || out.Docs[0].Tables[1].Name != "laminates" {
		t.Fatalf("tables = %+v", out.Docs[0].Tables)
	}

	duck := os.Getenv("GRAPHJIN_TEST_DUCKDB")
	if duck == "" {
		duck = "duckdb"
	}
	if _, err := exec.LookPath(duck); err != nil {
		t.Skip("DuckDB CLI not installed; set GRAPHJIN_TEST_DUCKDB")
	}
	rows, err := s.queryFileTables(ctx, "docs", `select count(*) as n, max(price) as top from laminates where material like 'Rogers%'`)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows.Rows) != 1 || rows.Rows[0][0].(json.Number).String() != "2" || rows.Rows[0][1].(json.Number).String() != "19.0" {
		t.Fatalf("sql = %+v", rows)
	}
	if !(&serviceAgentRuntime{service: s}).FileTablesEnabled() {
		t.Fatal("agent runtime should offer file tables")
	}
	if _, err := s.queryFileTables(ctx, "nope", "select 1"); err == nil {
		t.Fatal("unknown source should fail")
	}
}

func TestFileContentOffKeepsFileTablesPlain(t *testing.T) {
	s := fileContentTestService(t, false)
	if _, err := s.gj.GraphQL(context.Background(), `query { docs(search: "rogers") { key } }`, nil, nil); err == nil {
		t.Fatal("search should be unavailable when file content is off")
	}
	if (&serviceAgentRuntime{service: s}).FileTablesEnabled() {
		t.Fatal("agent runtime should not offer file tables")
	}
}
