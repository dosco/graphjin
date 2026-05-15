package serv

import (
	"strings"
	"testing"
)

func TestNewConfigCatalogEnabledAuto(t *testing.T) {
	conf, err := NewConfig(`
sources:
  - name: graphjin
    kind: graphjin
`, "yaml")
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if !conf.Core.CatalogEnabled() {
		t.Fatal("catalog.enabled: auto should enable catalog outside production")
	}
	if !conf.Core.CatalogAutoCodeRelationsEnabled() {
		t.Fatal("catalog.auto_code_relations: auto should follow catalog.enabled")
	}
}

func TestNewConfigCatalogEnabledAutoProduction(t *testing.T) {
	conf, err := NewConfig(`
production: true
sources:
  - name: graphjin
    kind: graphjin
    catalog: false
`, "yaml")
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if conf.Core.CatalogEnabled() {
		t.Fatal("catalog.enabled: auto should disable catalog in production")
	}
	if conf.Core.CatalogAutoCodeRelationsEnabled() {
		t.Fatal("catalog.auto_code_relations: auto should follow catalog.enabled in production")
	}
}

func TestSourceModeRejectsLegacyDatabaseSection(t *testing.T) {
	conf, err := NewConfig(`
sources:
  - name: app
    kind: sql
    type: sqlite
    path: /tmp/app.sqlite

database:
  type: sqlite
  path: /tmp/legacy.sqlite
`, "yaml")
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	_, err = NewGraphJinService(conf)
	if err == nil || !strings.Contains(err.Error(), "database is legacy database-only config") {
		t.Fatalf("expected legacy database rejection, got %v", err)
	}
}
