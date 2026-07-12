package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withDemoCwd points the test at an empty working directory and restores
// the cpath global afterwards.
func withDemoCwd(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	prev := cpath
	t.Cleanup(func() { cpath = prev })
}

func TestResolveDemoPathExplicitPathUnchanged(t *testing.T) {
	withDemoCwd(t)
	cpath = "examples/webshop"

	var out bytes.Buffer
	got, err := resolveDemoPath(true, &out)
	if err != nil {
		t.Fatalf("resolveDemoPath: %v", err)
	}
	if got != "examples/webshop" {
		t.Errorf("expected explicit path to pass through, got %q", got)
	}
	if _, err := os.Stat(demoDefaultPath); !os.IsNotExist(err) {
		t.Errorf("explicit --path must not create %s", demoDefaultPath)
	}
	if out.Len() != 0 {
		t.Errorf("expected no status output, got %q", out.String())
	}
}

func TestResolveDemoPathExtractsBuiltinDemo(t *testing.T) {
	withDemoCwd(t)

	var out bytes.Buffer
	got, err := resolveDemoPath(false, &out)
	if err != nil {
		t.Fatalf("resolveDemoPath: %v", err)
	}
	if got != demoDefaultPath {
		t.Errorf("expected %q, got %q", demoDefaultPath, got)
	}
	if !strings.Contains(out.String(), "created") {
		t.Errorf("expected created status, got %q", out.String())
	}

	// The extracted project must be a complete, bootable demo.
	for _, name := range []string{
		"dev.yml",
		"prod.yml",
		"agentic.yml",
		"schema-ddl/app.ddl",
		"seed/app.js",
		"queries/churn_risk_context.graphql",
		"queries/mrr_summary_context.graphql",
		"queries/ticket_sla_context.graphql",
		"workflows/sla_breach_check.js",
		"workflows/dunning_retry_check.js",
		".env.example",
		"README.md",
	} {
		path := filepath.Join(demoDefaultPath, filepath.FromSlash(name))
		st, err := os.Stat(path)
		if err != nil {
			t.Errorf("missing extracted file %s: %v", name, err)
			continue
		}
		if st.Size() == 0 {
			t.Errorf("extracted file %s is empty", name)
		}
	}

	// The repo smoke suite depends on the examples/ tree and is left out.
	if _, err := os.Stat(filepath.Join(demoDefaultPath, "scripts")); !os.IsNotExist(err) {
		t.Errorf("scripts/ must not be extracted")
	}

	// The demo config must keep sources on SQLite so no containers start.
	data, err := os.ReadFile(filepath.Join(demoDefaultPath, "dev.yml"))
	if err != nil {
		t.Fatalf("read dev.yml: %v", err)
	}
	if !strings.Contains(string(data), "type: sqlite") {
		t.Errorf("extracted dev.yml lost its sqlite source")
	}
}

func TestResolveDemoPathReusesExistingDir(t *testing.T) {
	withDemoCwd(t)

	if _, err := resolveDemoPath(false, nil); err != nil {
		t.Fatalf("first resolveDemoPath: %v", err)
	}

	// User edits survive later boots: the extracted copy is never overwritten.
	marker := filepath.Join(demoDefaultPath, "dev.yml")
	if err := os.WriteFile(marker, []byte("app_name: Edited\n"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	var out bytes.Buffer
	got, err := resolveDemoPath(false, &out)
	if err != nil {
		t.Fatalf("second resolveDemoPath: %v", err)
	}
	if got != demoDefaultPath {
		t.Errorf("expected %q, got %q", demoDefaultPath, got)
	}
	if !strings.Contains(out.String(), "reused") {
		t.Errorf("expected reused status, got %q", out.String())
	}
	data, err := os.ReadFile(marker)
	if err != nil || string(data) != "app_name: Edited\n" {
		t.Errorf("reuse overwrote user edits: %q, %v", data, err)
	}
}

func TestResolveDemoPathExtractsIntoEmptyDir(t *testing.T) {
	withDemoCwd(t)
	if err := os.Mkdir(demoDefaultPath, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := resolveDemoPath(false, nil); err != nil {
		t.Fatalf("resolveDemoPath: %v", err)
	}
	if _, err := os.Stat(filepath.Join(demoDefaultPath, "dev.yml")); err != nil {
		t.Errorf("expected extraction into empty dir: %v", err)
	}
}

func TestResolveDemoPathRejectsForeignDir(t *testing.T) {
	withDemoCwd(t)
	if err := os.Mkdir(demoDefaultPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(demoDefaultPath, "notes.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := resolveDemoPath(false, nil)
	if err == nil {
		t.Fatal("expected error for a non-demo directory")
	}
	if !strings.Contains(err.Error(), "--path") {
		t.Errorf("error should point at --path, got %q", err)
	}
	data, err2 := os.ReadFile(filepath.Join(demoDefaultPath, "notes.txt"))
	if err2 != nil || string(data) != "mine" {
		t.Errorf("foreign dir contents must be untouched: %q, %v", data, err2)
	}
}
