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

// TestFilesystemUnknownColumnErrors pins the failure mode observed in
// benchmark episodes: a model hallucinated relational columns onto a file
// source — `sla_policies { id severity response_time_hours ... }` — and the
// engine executed the query successfully, returning rows that silently
// omitted every hallucinated field. The agent then answered from nulls
// without ever reading the file. Filesystem tables have a fixed, known
// column set (core/fstable_bridge.go fixedFilesystemColumns), so unknown
// selections must fail at compile time like they do on database tables,
// giving the model an error it can self-correct from.
func TestFilesystemUnknownColumnErrors(t *testing.T) {
	root := t.TempDir()
	policy := "Urgent tickets: resolve within 4 hours.\n"
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

	// The exact benchmark shape: every selected field is a hallucinated
	// relational column; none exist on a file source.
	_, err = gj.GraphQL(sourceModeIntegrationUserContext(),
		`query {
			policies { id severity response_time_hours resolution_time_hours description }
		}`, nil, nil)
	if err == nil {
		t.Fatal("selecting nonexistent columns on a filesystem table succeeded; want a compile error")
	}
	for _, want := range []string{"id", "policies"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q should name %q so the model can self-correct", err, want)
		}
	}

	// A single unknown column mixed into valid ones must also fail — this is
	// the second episode shape (`{ id severity max_resolution_hours ... }`).
	_, err = gj.GraphQL(sourceModeIntegrationUserContext(),
		`query {
			policies(key: "support/sla-policy.md") { key size severity }
		}`, nil, nil)
	if err == nil {
		t.Fatal("mixed valid+unknown column selection succeeded; want a compile error")
	}
	if !strings.Contains(err.Error(), "severity") {
		t.Fatalf("error %q should name the unknown column", err)
	}

	// Guardrail: the full fixed column surface still works. (Aliases are
	// deliberately absent here — remote-table responses drop aliased fields,
	// a separate pre-existing gap in the jsn filter, not a validation issue.)
	res, err := gj.GraphQL(sourceModeIntegrationUserContext(),
		`query {
			policies(key: "support/sla-policy.md") {
				key size content_type etag modified_at url text
			}
		}`, nil, nil)
	if err != nil {
		t.Fatalf("valid fixed-column selection broke: %v", err)
	}
	var out struct {
		Policies []map[string]any `json:"policies"`
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Policies) != 1 || out.Policies[0]["key"] != "support/sla-policy.md" {
		t.Fatalf("valid selection returned %s", res.Data)
	}
	if out.Policies[0]["text"] != policy {
		t.Fatalf("text column = %q, want the policy body", out.Policies[0]["text"])
	}
}
