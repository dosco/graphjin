package conf

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewConfigCatalogAuto(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "dev.yml"), []byte(`
sources:
  - name: graphjin
    kind: database
    type: sqlite
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	conf, err := NewConfig(dir, "dev.yml")
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if !conf.CatalogEnabled() {
		t.Fatal("dev mode should enable the built-in catalog by default")
	}
	if !conf.CatalogAutoCodeRelationsEnabled() {
		t.Fatal("dev mode should enable catalog code relations by default")
	}
}
