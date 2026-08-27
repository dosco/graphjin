package serv

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dosco/graphjin/core/v3"
)

// A `kind: file` source with a relative `root:` names a directory beside
// the config file. Before this resolved against config_path, `graphjin
// serve --path /some/where` from any other working directory failed core
// init ("stat root <cwd>/files") and the service came up with no query
// engine — a warning, easy to miss, with the semantic catalog index and
// every query silently gone.
func TestFileSourceRelativeRootBootsFromAnyWorkingDirectory(t *testing.T) {
	configRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(configRoot, "files"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(configRoot, "files", "support-sla-policy.md"), "resolve urgent tickets within 4 hours\n")

	source := t.TempDir()
	writeTestFile(t, filepath.Join(source, "main.go"), "package main\n\nfunc Handler() {}\n")

	conf := &Config{
		Core: Core{
			Mode: "dev",
			Sources: []core.SourceConfig{
				{Name: "code", Kind: "code", Path: source},
				{Name: "sla_policies", Kind: "file", Backend: "local", Root: "files", ReadOnly: true},
			},
		},
		Serv: Serv{
			ConfigPath: configRoot,
			MCP:        MCPConfig{Disable: true},
		},
	}

	// The working directory is this package, not configRoot — the same
	// mismatch `--path` creates.
	s, err := newGraphJinService(conf, nil)
	if err != nil {
		t.Fatalf("service start: %v", err)
	}
	defer closeTestService(s)

	if s.gj == nil {
		t.Fatal("service started without a query engine; relative file root did not resolve against config_path")
	}
	if _, ok := s.gj.FilesystemBackend("sla_policies"); !ok {
		t.Fatal("sla_policies filesystem backend was not registered")
	}
}
