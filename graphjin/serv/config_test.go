package serv

import (
	"os"
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

func TestLegacyProductionDisablesMCPByDefault(t *testing.T) {
	conf, err := NewConfig(`
production: true
`, "yaml")
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if !conf.mcpDisabled() {
		t.Fatal("legacy production config should disable MCP by default")
	}
}

func TestLegacyProductionCanEnableMCPExplicitly(t *testing.T) {
	conf, err := NewConfig(`
production: true
mcp:
  disable: false
`, "yaml")
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if conf.mcpDisabled() {
		t.Fatal("explicit mcp.disable=false should enable MCP in legacy production config")
	}
}

func TestLegacyAgenticKeepsMCPEnabledByDefault(t *testing.T) {
	conf, err := NewConfig(`
production: true
security_mode: agentic
`, "yaml")
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if conf.mcpDisabled() {
		t.Fatal("legacy agentic config should keep MCP enabled by default")
	}
}

func TestSourcesProductionKeepsMCPEnabledByDefault(t *testing.T) {
	conf, err := NewConfig(`
production: true
sources:
  - name: graphjin
    kind: graphjin
`, "yaml")
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if conf.mcpDisabled() {
		t.Fatal("sources production config should keep MCP enabled by default")
	}
}

func TestIsSourcesUsedRejectsLegacyDatabaseSection(t *testing.T) {
	clearLegacyDatabaseEnv(t)
	conf, err := NewConfig(`
sources:
  - name: app
    kind: database
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

// Regression: viper applies database.* defaults (host=localhost,
// port=5432, type=postgres, ...) on every load. The pre-fix validator
// inspected the unmarshaled struct and treated those defaults as a
// user-supplied legacy database, so any sources-only config was
// wrongly rejected. The validator must only fail when the user
// actually wrote a `database:` block (or set GJ_DATABASE_* env).
func TestIsSourcesUsedAcceptsSourcesWithoutLegacyDatabase(t *testing.T) {
	clearLegacyDatabaseEnv(t)
	conf, err := NewConfig(`
sources:
  - name: app
    kind: database
    type: sqlite
    path: /tmp/app.sqlite
    default: true
`, "yaml")
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if _, err := NewGraphJinService(conf); err != nil {
		if strings.Contains(err.Error(), "database is legacy database-only config") {
			t.Fatalf("validator wrongly rejected sources-only config (defaults must not count): %v", err)
		}
	}
}

func TestIsSourcesUsedRejectsLegacyDatabaseEnv(t *testing.T) {
	clearLegacyDatabaseEnv(t)
	t.Setenv("GJ_DATABASE_HOST", "legacy-host")
	conf, err := NewConfig(`
sources:
  - name: app
    kind: database
    type: sqlite
    path: /tmp/app.sqlite
`, "yaml")
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	_, err = NewGraphJinService(conf)
	if err == nil || !strings.Contains(err.Error(), "database is legacy database-only config") {
		t.Fatalf("expected legacy database rejection from GJ_DATABASE_* env, got %v", err)
	}
}

// clearLegacyDatabaseEnv removes any GJ_DATABASE_* / SJ_DATABASE_* /
// SG_DATABASE_* values from the process environment for the duration
// of the test, so a developer's shell can't make these tests flaky.
func clearLegacyDatabaseEnv(t *testing.T) {
	t.Helper()
	for _, e := range os.Environ() {
		kv := strings.SplitN(e, "=", 2)
		k := kv[0]
		if strings.HasPrefix(k, "GJ_DATABASE_") ||
			strings.HasPrefix(k, "SJ_DATABASE_") ||
			strings.HasPrefix(k, "SG_DATABASE_") {
			t.Setenv(k, kv[1])
			os.Unsetenv(k) //nolint:errcheck
		}
	}
}
