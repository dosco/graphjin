package tests_test

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3"
)

// TestMixedRootDBPlusFilesystemContent pins, end to end against a real engine,
// the composition the DeepORG cross-source tasks require: one GraphQL execution
// that reads a policy file from a filesystem source AND computes a database-side
// aggregate. Core has supported mixed remote+database roots deliberately
// (injectRemoteMarkers in core/remote_join.go), but nothing exercised the
// file-content half — every benchmark model composed this shape correctly and
// nothing in CI proved the engine kept honoring it.
func TestMixedRootDBPlusFilesystemContent(t *testing.T) {
	root := t.TempDir()
	policy := "Urgent tickets: resolve within 4 hours.\nHigh tickets: resolve within 24 hours.\n"
	if err := os.MkdirAll(filepath.Join(root, "support"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "support", "sla-policy.md"), []byte(policy), 0o644); err != nil {
		t.Fatal(err)
	}

	conf := newConfig(&core.Config{
		DBType:           dbType,
		DisableAllowList: true,
		DefaultLimit:     10,
		Sources: []core.SourceConfig{
			{Name: core.DefaultDBName, Kind: "database", Type: dbType, Default: true, Access: core.SourceAccessConfig{
				Read: core.AccessModeAuthenticated,
			}},
			{Name: "policies", Kind: "file", Backend: "local", Root: root},
		},
	})
	// Engines whose shared fixture declares source-less tables cannot coexist
	// with a non-database source (same skip as the OpenAPI join test).
	for _, table := range conf.Tables {
		if strings.TrimSpace(table.Source) == "" {
			t.Skipf("%s: shared fixture declares table %q without a source", dbType, table.Name)
		}
	}

	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}
	defer gj.Close() //nolint:errcheck

	// One document, one execution: a database aggregate root and a file-content
	// root — the exact shape of the benchmark oracle
	// query { support_tickets(...) { count_id } sla_policies(key: ...) { data } }.
	res, err := gj.GraphQL(sourceModeIntegrationUserContext(),
		`query {
			users(where: { id: { eq: 1 } }) { id email count_id }
			policies(key: "support/sla-policy.md") { key content_type text data }
		}`, nil, nil)
	if err != nil {
		t.Fatalf("mixed-root execution: %v", err)
	}

	var out struct {
		Users    []map[string]any `json:"users"`
		Policies []map[string]any `json:"policies"`
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Users) == 0 {
		t.Fatalf("database root returned no rows: %s", res.Data)
	}
	if len(out.Policies) != 1 {
		t.Fatalf("file root returned %d rows, want 1: %s", len(out.Policies), res.Data)
	}
	got := out.Policies[0]
	if got["text"] != policy {
		t.Fatalf("decoded text = %q, want the policy body", got["text"])
	}
	raw, err := base64.StdEncoding.DecodeString(got["data"].(string))
	if err != nil || string(raw) != policy {
		t.Fatalf("base64 data disagrees with the file: %v", err)
	}
	// The dimension the benchmark checks for must be readable straight from the
	// decoded column — no in-head base64 work required.
	if !strings.Contains(got["text"].(string), "4 hours") {
		t.Fatalf("policy content lost: %q", got["text"])
	}
}
