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

func TestGetConfigNameAgentic(t *testing.T) {
	t.Setenv("GO_ENV", "agentic")
	if got := GetConfigName(); got != "agentic" {
		t.Fatalf("GetConfigName() = %q, want agentic", got)
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

func TestModeAgenticKeepsMCPEnabledByDefault(t *testing.T) {
	conf, err := NewConfig(`
mode: agentic
`, "yaml")
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if conf.Core.Mode != "agentic" || conf.Serv.Production || conf.Core.Production {
		t.Fatalf("agentic mode normalization drift: mode=%q serv_prod=%v core_prod=%v",
			conf.Core.Mode, conf.Serv.Production, conf.Core.Production)
	}
	if conf.mcpDisabled() {
		t.Fatal("agentic config should keep MCP enabled by default")
	}
}

func TestAgentConfigDefaultsAndOverrides(t *testing.T) {
	conf, err := NewConfig(`
agent:
  enabled: true
  provider: openai-compatible
  model: local-model
  api_key_env: GRAPHJIN_AGENT_KEY
  base_url: http://127.0.0.1:11434/v1
  max_steps: 3
  timeout_seconds: 11
  allow_raw_graphql: true
  return_trace: true
`, "yaml")
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if !conf.Agent.Enabled || conf.Agent.Provider != "openai-compatible" || conf.Agent.Model != "local-model" {
		t.Fatalf("agent provider config drift: %+v", conf.Agent)
	}
	if conf.Agent.APIKeyEnv != "GRAPHJIN_AGENT_KEY" || conf.Agent.BaseURL != "http://127.0.0.1:11434/v1" {
		t.Fatalf("agent connection config drift: %+v", conf.Agent)
	}
	if conf.Agent.MaxSteps != 3 || conf.Agent.TimeoutSeconds != 11 || !conf.Agent.AllowRawGraphQL || !conf.Agent.ReturnTrace {
		t.Fatalf("agent runtime config drift: %+v", conf.Agent)
	}

	defaults, err := NewConfig(``, "yaml")
	if err != nil {
		t.Fatalf("NewConfig defaults: %v", err)
	}
	if defaults.Agent.Enabled || defaults.Agent.Provider != "openai" || defaults.Agent.APIKeyEnv != "OPENAI_API_KEY" {
		t.Fatalf("unexpected agent defaults: %+v", defaults.Agent)
	}
	if defaults.Agent.MaxSteps != 8 || defaults.Agent.TimeoutSeconds != 50 || defaults.Agent.AllowRawGraphQL || defaults.Agent.ReturnTrace {
		t.Fatalf("unexpected agent runtime defaults: %+v", defaults.Agent)
	}
}

func TestModeProdDisablesLegacyMCPByDefault(t *testing.T) {
	conf, err := NewConfig(`
mode: prod
`, "yaml")
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if conf.Core.Mode != "prod" || conf.Serv.Production || conf.Core.Production {
		t.Fatalf("prod mode normalization drift: mode=%q serv_prod=%v core_prod=%v",
			conf.Core.Mode, conf.Serv.Production, conf.Core.Production)
	}
	if !conf.mcpDisabled() {
		t.Fatal("legacy prod mode config should disable MCP by default")
	}
}

func TestModeDevControlsCatalogAutoEvenWithLegacyProduction(t *testing.T) {
	conf, err := NewConfig(`
production: true
mode: dev
sources:
  - name: graphjin
    kind: graphjin
`, "yaml")
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if conf.Core.Mode != "dev" || !conf.Serv.Production || !conf.Core.Production {
		t.Fatalf("dev mode normalization drift: mode=%q serv_prod=%v core_prod=%v",
			conf.Core.Mode, conf.Serv.Production, conf.Core.Production)
	}
	if !conf.Core.CatalogEnabled() {
		t.Fatal("catalog.enabled: auto should follow dev mode even if legacy production is true")
	}
}

func TestWebUIDefaultsFollowMode(t *testing.T) {
	clearWebUIEnv(t)

	tests := []struct {
		name   string
		config string
		want   bool
	}{
		{name: "implicit dev", config: `app_name: test`, want: true},
		{name: "dev", config: `mode: dev`, want: true},
		{name: "agentic", config: `mode: agentic`, want: true},
		{name: "prod", config: `mode: prod`, want: false},
		{name: "legacy production", config: `production: true`, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conf, err := NewConfig(tt.config, "yaml")
			if err != nil {
				t.Fatalf("NewConfig: %v", err)
			}
			if conf.Serv.WebUI != tt.want {
				t.Fatalf("web_ui = %v, want %v", conf.Serv.WebUI, tt.want)
			}
		})
	}
}

func TestWebUIExplicitOverride(t *testing.T) {
	clearWebUIEnv(t)

	tests := []struct {
		name   string
		config string
		want   bool
	}{
		{name: "dev disabled", config: `
mode: dev
web_ui: false
`, want: false},
		{name: "agentic disabled", config: `
mode: agentic
web_ui: false
`, want: false},
		{name: "prod enabled", config: `
mode: prod
web_ui: true
`, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conf, err := NewConfig(tt.config, "yaml")
			if err != nil {
				t.Fatalf("NewConfig: %v", err)
			}
			if conf.Serv.WebUI != tt.want {
				t.Fatalf("web_ui = %v, want %v", conf.Serv.WebUI, tt.want)
			}
			if !conf.webUIExplicit {
				t.Fatal("web_ui explicit marker should be set")
			}
		})
	}
}

func clearWebUIEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"GJ_WEB_UI", "SG_WEB_UI", "SJ_WEB_UI"} {
		value, ok := os.LookupEnv(key)
		if !ok {
			continue
		}
		k, v := key, value
		t.Cleanup(func() {
			os.Setenv(k, v) //nolint:errcheck
		})
		os.Unsetenv(key) //nolint:errcheck
	}
}

func TestInvalidModeIsRejected(t *testing.T) {
	_, err := NewConfig(`
mode: secure-ish
`, "yaml")
	if err == nil || !strings.Contains(err.Error(), "unsupported mode") {
		t.Fatalf("expected unsupported mode error, got %v", err)
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
