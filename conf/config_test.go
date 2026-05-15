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
    kind: graphjin
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	conf, err := NewConfig(dir, "dev.yml")
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if !conf.CatalogEnabled() {
		t.Fatal("graphjin source should enable catalog by default")
	}
	if !conf.CatalogAutoCodeRelationsEnabled() {
		t.Fatal("graphjin source should enable catalog code relations by default")
	}
}
