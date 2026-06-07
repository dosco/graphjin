package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSourceSeedFilesSortedBySource(t *testing.T) {
	oldCpath := cpath
	defer func() { cpath = oldCpath }()
	cpath = t.TempDir()

	seedDir := filepath.Join(cpath, "seed")
	if err := os.MkdirAll(seedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"warehouse.sql", "ops.js", "alpha.js", "README.md"} {
		if err := os.WriteFile(filepath.Join(seedDir, name), []byte("// seed"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	files, err := sourceSeedFiles(".js")
	if err != nil {
		t.Fatalf("sourceSeedFiles: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("len(files) = %d, want 2", len(files))
	}
	if files[0].Source != "alpha" || files[1].Source != "ops" {
		t.Fatalf("sources = %q, %q; want alpha, ops", files[0].Source, files[1].Source)
	}
}
