package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

// noBaseFS is an FS with no OS directory behind it — a deploy bundle or
// an embedded FS. It deliberately does not implement FSBase, so relative
// config paths must keep falling back to the process working directory.
type noBaseFS struct{}

func (noBaseFS) Get(string) ([]byte, error)    { return nil, os.ErrNotExist }
func (noBaseFS) Put(string, []byte) error      { return nil }
func (noBaseFS) Exists(string) (bool, error)   { return false, nil }
func (noBaseFS) List(string) ([]string, error) { return nil, nil }

func TestConfigRelPath(t *testing.T) {
	cfgDir := filepath.Join(string(filepath.Separator)+"tmp", "proj", "config")
	abs := filepath.Join(string(filepath.Separator)+"srv", "files")

	tests := []struct {
		name string
		fs   FS
		in   string
		want string
	}{
		{"relative resolves against config dir", NewOsFS(cfgDir), "files", filepath.Join(cfgDir, "files")},
		{"dot-relative resolves against config dir", NewOsFS(cfgDir), "./files", filepath.Join(cfgDir, "files")},
		{"parent-relative resolves against config dir", NewOsFS(cfgDir), filepath.Join("..", "files"), filepath.Join(cfgDir, "..", "files")},
		{"absolute is left alone", NewOsFS(cfgDir), abs, abs},
		{"empty stays empty", NewOsFS(cfgDir), "", ""},
		{"fs without a base dir falls back to cwd", noBaseFS{}, "files", "files"},
		// core invents ./config when the caller supplies no FS. That base is
		// core's own default, not a directory the caller named, so a path a
		// caller wrote in a Go-built Config keeps meaning what it says.
		{"core's own default base falls back to cwd", defaultFS{NewOsFS(cfgDir)}, "files", "files"},
		{"empty base dir falls back to cwd", NewOsFS(""), "files", "files"},
		{"nil fs falls back to cwd", nil, "files", "files"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gj := &graphjinEngine{fs: tt.fs}
			if got := gj.configRelPath(tt.in); got != tt.want {
				t.Errorf("configRelPath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestLocalFilesystemRootResolvesAgainstConfigDir is the regression this
// package's `root:` handling exists for: `graphjin serve --path
// /some/where` used to fail core init because a relative root resolved
// against the process working directory instead of the config directory.
func TestLocalFilesystemRootResolvesAgainstConfigDir(t *testing.T) {
	cfgDir := t.TempDir()
	mkfile(t, cfgDir, "files/support-sla-policy.md", []byte("p1"))
	mkfile(t, cfgDir, "files/escalation.md", []byte("p2"))

	gj := fsTestEngine(t, "public", []FilesystemConfig{
		{Name: "sla_policies", Backend: "local", Root: "files"},
	})
	gj.fs = NewOsFS(cfgDir)

	if err := gj.loadFilesystemIntegration(); err != nil {
		t.Fatalf("loadFilesystemIntegration: %v", err)
	}

	rfn := gj.newFilesystemResolverFn()
	r, err := rfn(ResolverProps{"fs_name": "sla_policies"})
	if err != nil {
		t.Fatalf("resolver factory: %v", err)
	}
	resp, err := r.Resolve(context.Background(), ResolverReq{
		Sel: &qcode.Select{ExtraArgs: map[string]string{"prefix": ""}},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	var wrap struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(resp, &wrap); err != nil {
		t.Fatalf("response is not the expected wrapper shape: %v\n%s", err, resp)
	}
	if len(wrap.Items) != 2 {
		t.Fatalf("expected the 2 files under %s/files, got %d: %s", cfgDir, len(wrap.Items), resp)
	}
}

// An absolute root must not be re-anchored on the config directory.
func TestLocalFilesystemAbsoluteRootIgnoresConfigDir(t *testing.T) {
	dataDir := t.TempDir()
	mkfile(t, dataDir, "a.txt", []byte("hi"))

	gj := fsTestEngine(t, "public", []FilesystemConfig{
		{Name: "docs", Backend: "local", Root: dataDir},
	})
	// A config directory that shares no ancestry with the data directory:
	// joining the two would fail to stat.
	gj.fs = NewOsFS(t.TempDir())

	if err := gj.loadFilesystemIntegration(); err != nil {
		t.Fatalf("loadFilesystemIntegration with an absolute root: %v", err)
	}
}

// TestOpenAPISpecsDirResolvesAgainstConfigDir covers the sibling case:
// `sources[].specs_dir` is the other config value handed to the OS
// directly rather than read through gj.fs.
func TestOpenAPISpecsDirResolvesAgainstConfigDir(t *testing.T) {
	cfgDir := t.TempDir()
	mkfile(t, cfgDir, "specs/billing.yaml", []byte(`
openapi: 3.0.0
info: { title: Billing, version: 1.0.0 }
paths:
  /invoices:
    get:
      operationId: listInvoices
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                type: array
                items:
                  type: object
                  properties:
                    id: { type: string }
`))

	gj := &graphjinEngine{
		conf: &Config{
			OpenAPISpecsDir: "specs",
			Sources: []SourceConfig{
				{Name: "billing_api", Kind: "api", SpecsDir: "specs"},
			},
		},
		fs:    NewOsFS(cfgDir),
		log:   silentLogger(t),
		trace: &tracer{},
	}

	if err := gj.loadOpenAPIIntegration(); err != nil {
		t.Fatalf("loadOpenAPIIntegration: %v", err)
	}
	if gj.openapiRuntime == nil {
		t.Fatalf("specs under %s/specs were not loaded; specs_dir resolved against the wrong base", cfgDir)
	}
}
