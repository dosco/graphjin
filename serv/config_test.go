package serv

import (
	"os"
	"strings"
	"testing"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
)

func TestNewConfigParsesOpenAPITimeout(t *testing.T) {
	conf, err := NewConfig(`
mode: dev
sources:
  - name: upstream
    kind: api
    specs:
      billing:
        timeout: 5s
`, "yaml")
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if got := conf.Core.Sources[0].Specs["billing"].Timeout; got != 5*time.Second {
		t.Fatalf("OpenAPI timeout = %s, want 5s", got)
	}
}

func TestNewConfigCatalogEnabledAuto(t *testing.T) {
	conf, err := NewConfig(`
mode: dev
sources:
  - name: graphjin
    kind: database
    type: sqlite
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

func TestNewConfigPreservesDottedFeatureCapabilityKeys(t *testing.T) {
	conf, err := NewConfig(`
mode: agentic
system:
  capabilities:
    catalog.read: false
    runtime.read: true
workflows:
  capabilities:
    execute: false
`, "yaml")
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if got, ok := conf.Core.System.Capabilities["catalog.read"]; !ok || got {
		t.Fatalf("system capability catalog.read = %v, present=%v", got, ok)
	}
	if got, ok := conf.Core.System.Capabilities["runtime.read"]; !ok || !got {
		t.Fatalf("system capability runtime.read = %v, present=%v", got, ok)
	}
	if got, ok := conf.Core.Workflows.Capabilities["execute"]; !ok || got {
		t.Fatalf("workflow capability execute = %v, present=%v", got, ok)
	}
}

func TestNewConfigCatalogEnabledAutoProduction(t *testing.T) {
	conf, err := NewConfig(`
production: true
sources:
  - name: graphjin
    kind: database
    type: sqlite
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

func TestDiscoveryAndSemanticNestedEnvironmentOverrides(t *testing.T) {
	t.Setenv("GJ_DISCOVERY_CACHE_ENABLED", "true")
	t.Setenv("GJ_DISCOVERY_CACHE_PATH", ".graphjin/semantic-smoke")
	t.Setenv("GJ_DISCOVERY_CACHE_REFRESH_INTERVAL", "1h")
	t.Setenv("GJ_CATALOG_SEARCH_SEMANTIC_ENABLED", "true")
	t.Setenv("GJ_CATALOG_SEARCH_SEMANTIC_PROVIDER", "openai")
	t.Setenv("GJ_CATALOG_SEARCH_SEMANTIC_EMBEDDING_MODEL", "coffee-semantic-smoke-v1")
	t.Setenv("GJ_CATALOG_SEARCH_SEMANTIC_API_KEY_ENV", "COFFEE_EMBEDDING_KEY")
	t.Setenv("GJ_CATALOG_SEARCH_SEMANTIC_BASE_URL", "http://127.0.0.1:18081/v1")
	t.Setenv("GJ_CATALOG_SEARCH_SEMANTIC_DIMENSIONS", "tiny")

	vi := newViperWithDefaults()
	var conf Config
	if err := vi.Unmarshal(&conf); err != nil {
		t.Fatal(err)
	}
	if err := normalizeDiscoveryAndSemanticConfig(&conf); err != nil {
		t.Fatal(err)
	}
	if !conf.DiscoveryCache.enabled() || conf.DiscoveryCache.Path != ".graphjin/semantic-smoke" || conf.DiscoveryCache.RefreshInterval.String() != "1h0m0s" {
		t.Fatalf("discovery cache environment overrides were not applied: %+v", conf.DiscoveryCache)
	}
	semantic := conf.CatalogSearch.Semantic
	if !semantic.Enabled || semantic.Provider != "openai" || semantic.EmbeddingModel != "coffee-semantic-smoke-v1" || semantic.APIKeyEnv != "COFFEE_EMBEDDING_KEY" || semantic.BaseURL != "http://127.0.0.1:18081/v1" || semantic.Dimensions != "tiny" {
		t.Fatalf("semantic environment overrides were not applied: %+v", semantic)
	}
}

func TestAgentModelAndBaseURLEnvironmentOverrides(t *testing.T) {
	t.Setenv("GJ_AGENT_MODEL", "coffee-agent-smoke-v1")
	t.Setenv("GJ_AGENT_BASE_URL", "http://127.0.0.1:18081/v1")
	t.Setenv("GJ_AGENT_RESPONSE_FORMAT", "json_object")

	vi := newViperWithDefaults()
	var conf Config
	if err := vi.Unmarshal(&conf); err != nil {
		t.Fatal(err)
	}
	if conf.Agent.Model != "coffee-agent-smoke-v1" || conf.Agent.BaseURL != "http://127.0.0.1:18081/v1" || conf.Agent.ResponseFormat != "json_object" {
		t.Fatalf("agent environment overrides were not applied: %+v", conf.Agent)
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
  response_format: json_object
  rate_limit:
    requests_per_minute: 30
    tokens_per_minute: 75000
  max_steps: 3
  timeout_seconds: 11
  read_only: true
  return_trace: true
`, "yaml")
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if !conf.Agent.Enabled || conf.Agent.Provider != "openai-compatible" || conf.Agent.Model != "local-model" {
		t.Fatalf("agent provider config drift: %+v", conf.Agent)
	}
	if conf.Agent.APIKeyEnv != "GRAPHJIN_AGENT_KEY" || conf.Agent.BaseURL != "http://127.0.0.1:11434/v1" || conf.Agent.ResponseFormat != "json_object" {
		t.Fatalf("agent connection config drift: %+v", conf.Agent)
	}
	if conf.Agent.RateLimit.RequestsPerMinute != 30 || conf.Agent.RateLimit.TokensPerMinute != 75000 {
		t.Fatalf("agent provider rate-limit config drift: %+v", conf.Agent.RateLimit)
	}
	if conf.Agent.MaxSteps != 3 || conf.Agent.TimeoutSeconds != 11 || !conf.Agent.ReadOnly || !conf.Agent.ReturnTrace {
		t.Fatalf("agent runtime config drift: %+v", conf.Agent)
	}

	defaults, err := NewConfig(``, "yaml")
	if err != nil {
		t.Fatalf("NewConfig defaults: %v", err)
	}
	if !defaults.Agent.Enabled || defaults.Agent.Provider != "openai" || defaults.Agent.APIKeyEnv != "OPENAI_API_KEY" {
		t.Fatalf("unexpected agent defaults: %+v", defaults.Agent)
	}
	// Structured output defaults to auto: the Ax deployment profile and its
	// model rules choose the mechanism, and the deprecated response_format
	// alias stays empty unless an operator sets it.
	if defaults.Agent.StructuredOutputMode != "auto" || defaults.Agent.ResponseFormat != "" {
		t.Fatalf("unexpected structured output defaults: %+v", defaults.Agent)
	}
	if defaults.Agent.MaxSteps != 8 || defaults.Agent.TimeoutSeconds != 50 || defaults.Agent.ReadOnly || defaults.Agent.ReturnTrace {
		t.Fatalf("unexpected agent runtime defaults: %+v", defaults.Agent)
	}
	if defaults.Agent.RateLimit != (gjagent.RateLimitConfig{}) {
		t.Fatalf("unexpected default agent rate limits: %+v", defaults.Agent.RateLimit)
	}
}

func TestAgentRateLimitEnvironmentOverrides(t *testing.T) {
	for _, prefix := range []string{"GJ", "SG", "SJ"} {
		t.Run(prefix, func(t *testing.T) {
			for _, clear := range []string{"GJ", "SG", "SJ"} {
				for _, suffix := range []string{"REQUESTS_PER_MINUTE", "TOKENS_PER_MINUTE"} {
					key := clear + "_AGENT_RATE_LIMIT_" + suffix
					old, existed := os.LookupEnv(key)
					if err := os.Unsetenv(key); err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() {
						if existed {
							_ = os.Setenv(key, old)
						} else {
							_ = os.Unsetenv(key)
						}
					})
				}
			}
			t.Setenv(prefix+"_AGENT_RATE_LIMIT_REQUESTS_PER_MINUTE", "17")
			t.Setenv(prefix+"_AGENT_RATE_LIMIT_TOKENS_PER_MINUTE", "42000")
			conf, err := NewConfig("", "yaml")
			if err != nil {
				t.Fatalf("NewConfig: %v", err)
			}
			if conf.Agent.RateLimit.RequestsPerMinute != 17 || conf.Agent.RateLimit.TokensPerMinute != 42000 {
				t.Fatalf("%s environment rate limits not applied: %+v", prefix, conf.Agent.RateLimit)
			}
		})
	}
}

func TestAgentRateLimitRejectsNegativeValues(t *testing.T) {
	if _, err := NewConfig("agent:\n  rate_limit:\n    requests_per_minute: -1\n", "yaml"); err == nil {
		t.Fatal("expected a negative agent request limit to be rejected")
	}
	if _, err := NewConfig("agent:\n  rate_limit:\n    tokens_per_minute: -1\n", "yaml"); err == nil {
		t.Fatal("expected a negative agent token limit to be rejected")
	}
}

func TestAgentConfigRejectsInvalidResponseFormat(t *testing.T) {
	if _, err := NewConfig("agent:\n  response_format: xml\n", "yaml"); err == nil {
		t.Fatal("expected invalid agent.response_format to be rejected")
	}
}

func TestParsedDevAndAgenticRuntimeDefaults(t *testing.T) {
	for _, mode := range []string{"dev", "agentic"} {
		t.Run(mode, func(t *testing.T) {
			conf, err := NewConfig("mode: "+mode+"\n", "yaml")
			if err != nil {
				t.Fatalf("NewConfig: %v", err)
			}
			if !conf.Core.Artifacts.Enabled || conf.Core.Artifacts.Source != "" || !conf.managedArtifactStore {
				t.Fatalf("artifact defaults = %+v managed=%v", conf.Core.Artifacts, conf.managedArtifactStore)
			}
			if !conf.Core.Watches.Enabled || conf.Core.Watches.Runner != "all" {
				t.Fatalf("watch defaults = %+v", conf.Core.Watches)
			}
			if !conf.Core.Tasks.Enabled {
				t.Fatalf("task defaults = %+v", conf.Core.Tasks)
			}
			if !conf.Agent.Enabled || !conf.MCP.IncludeToolsWithAgent {
				t.Fatalf("service defaults: agent=%+v mcp=%+v", conf.Agent, conf.MCP)
			}
			listed := strings.Join(mcpToolList(conf), ",")
			for _, tool := range []string{"graphql_help", "query_catalog", "execute_saved_query", "validate_where_clause", "ask_graphjin_agent"} {
				if !strings.Contains(","+listed+",", ","+tool+",") {
					t.Fatalf("default MCP toolset %q is missing %q", listed, tool)
				}
			}
			settings := conf.EffectiveSettings()
			for key, want := range map[string]any{
				"artifacts.enabled":            true,
				"watches.enabled":              true,
				"watches.runner":               "all",
				"tasks.enabled":                true,
				"agent.enabled":                true,
				"mcp.include_tools_with_agent": true,
			} {
				if got := effectiveSettingValue(settings, key); got != want {
					t.Fatalf("effective %s = %#v, want %#v", key, got, want)
				}
			}
		})
	}
}

func effectiveSettingValue(settings map[string]any, dotted string) any {
	var current any = settings
	for _, part := range strings.Split(dotted, ".") {
		values, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = values[part]
	}
	return current
}

func TestParsedProdAndDirectConfigKeepLiteralDefaults(t *testing.T) {
	prod, err := NewConfig("mode: prod\n", "yaml")
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if prod.Core.Artifacts.Enabled || prod.Core.Watches.Enabled || prod.Core.Tasks.Enabled || prod.Agent.Enabled || prod.MCP.IncludeToolsWithAgent {
		t.Fatalf("prod defaults changed: artifacts=%+v watches=%+v tasks=%+v agent=%+v mcp=%+v", prod.Core.Artifacts, prod.Core.Watches, prod.Core.Tasks, prod.Agent, prod.MCP)
	}

	direct := &Config{Core: Core{Mode: "agentic"}}
	if err := normalizeConfigMode(direct); err != nil {
		t.Fatal(err)
	}
	applyRuntimeModeDefaults(direct)
	if direct.Core.Artifacts.Enabled || direct.Core.Watches.Enabled || direct.Core.Tasks.Enabled || direct.Agent.Enabled || direct.MCP.IncludeToolsWithAgent {
		t.Fatalf("direct Go config received parsed defaults: %+v", direct)
	}
}

func TestParsedRuntimeDefaultsRespectOptOutsAndDependencies(t *testing.T) {
	conf, err := NewConfig(`
mode: agentic
artifacts:
  enabled: false
watches:
  runner: all
agent:
  enabled: false
mcp:
  include_tools_with_agent: false
`, "yaml")
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if conf.Core.Artifacts.Enabled || conf.Core.Watches.Enabled || conf.Core.Tasks.Enabled || conf.Agent.Enabled || conf.MCP.IncludeToolsWithAgent {
		t.Fatalf("explicit opt-outs not preserved: artifacts=%+v watches=%+v tasks=%+v agent=%+v mcp=%+v", conf.Core.Artifacts, conf.Core.Watches, conf.Core.Tasks, conf.Agent, conf.MCP)
	}
	if conf.Core.Watches.Runner != "all" {
		t.Fatalf("explicit runner = %q, want all", conf.Core.Watches.Runner)
	}

	invalid, err := NewConfig(`
mode: agentic
artifacts:
  enabled: false
watches:
  enabled: true
`, "yaml")
	if err != nil {
		t.Fatalf("NewConfig invalid dependency parse: %v", err)
	}
	if err := validateServiceIsSourcesUsedConfig(invalid); err == nil || !strings.Contains(err.Error(), "watches require artifacts.enabled") {
		t.Fatalf("dependency validation error = %v", err)
	}
}

func TestParsedRuntimeDefaultsRespectTaskOptOut(t *testing.T) {
	conf, err := NewConfig(`
mode: agentic
tasks:
  enabled: false
`, "yaml")
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if !conf.Core.Artifacts.Enabled || !conf.Core.Watches.Enabled || conf.Core.Tasks.Enabled {
		t.Fatalf("task opt-out changed related defaults: artifacts=%+v watches=%+v tasks=%+v", conf.Core.Artifacts, conf.Core.Watches, conf.Core.Tasks)
	}
	if got := effectiveSettingValue(conf.EffectiveSettings(), "tasks.enabled"); got != false {
		t.Fatalf("effective tasks.enabled = %#v, want false", got)
	}
}

func TestManagedArtifactStoreStillValidatesTaskLimits(t *testing.T) {
	conf, err := NewConfig(`
mode: agentic
tasks:
  max_entries_per_task: -1
`, "yaml")
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if !conf.managedArtifactStore || !conf.Core.Tasks.Enabled {
		t.Fatalf("expected managed-store task defaults, got managed=%v tasks=%+v", conf.managedArtifactStore, conf.Core.Tasks)
	}
	if err := validateServiceIsSourcesUsedConfig(conf); err == nil || !strings.Contains(err.Error(), "tasks.max_entries_per_task") {
		t.Fatalf("managed-store task validation error = %v", err)
	}
}

func TestParsedRuntimeDefaultsRespectEnvironmentOverrides(t *testing.T) {
	t.Setenv("GJ_ARTIFACTS_ENABLED", "false")
	t.Setenv("GJ_AGENT_ENABLED", "false")
	t.Setenv("GJ_MCP_INCLUDE_TOOLS_WITH_AGENT", "false")
	conf, err := NewConfig("mode: agentic\n", "yaml")
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if conf.Core.Artifacts.Enabled || conf.Core.Watches.Enabled || conf.Core.Tasks.Enabled || conf.Agent.Enabled || conf.MCP.IncludeToolsWithAgent {
		t.Fatalf("environment opt-outs not preserved: artifacts=%+v watches=%+v tasks=%+v agent=%+v mcp=%+v", conf.Core.Artifacts, conf.Core.Watches, conf.Core.Tasks, conf.Agent, conf.MCP)
	}
}

func TestRemovedMCPSettingsAreRejected(t *testing.T) {
	for _, config := range []string{
		"agent:\n  sampling: off\n",
		"mcp:\n  http_stateful: true\n",
	} {
		if _, err := NewConfig(config, "yaml"); err == nil || !strings.Contains(err.Error(), "was removed") {
			t.Fatalf("removed config %q error = %v", config, err)
		}
	}
	t.Setenv("GJ_AGENT_SAMPLING", "auto")
	if _, err := NewConfig("", "yaml"); err == nil || !strings.Contains(err.Error(), "agent.sampling was removed") {
		t.Fatalf("removed environment alias error = %v", err)
	}
}

func TestExplicitArtifactEnableKeepsLegacySourceSelection(t *testing.T) {
	conf, err := NewConfig(`
mode: agentic
sources:
  - name: app
    kind: database
    type: sqlite
    path: app.sqlite3
artifacts:
  enabled: true
`, "yaml")
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if conf.managedArtifactStore || conf.Core.Artifacts.Source != "" {
		t.Fatalf("explicit legacy artifact config moved stores: source=%q managed=%v", conf.Core.Artifacts.Source, conf.managedArtifactStore)
	}
	if err := normalizeServiceSources(conf); err != nil {
		t.Fatalf("normalizeServiceSources: %v", err)
	}
	if conf.Core.Artifacts.Source != "app" {
		t.Fatalf("legacy artifact source = %q, want app", conf.Core.Artifacts.Source)
	}
}

func TestFormerManagedArtifactSourceNameIsAllowed(t *testing.T) {
	conf, err := NewConfig(`
mode: agentic
sources:
  - name: __graphjin_artifacts
    kind: database
    type: sqlite
    path: user.sqlite3
`, "yaml")
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if err := validateServiceIsSourcesUsedConfig(conf); err != nil {
		t.Fatalf("former internal alias should be a valid application source name: %v", err)
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
    kind: database
    type: sqlite
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

func TestSourcesProductionDisablesMCPByDefault(t *testing.T) {
	conf, err := NewConfig(`
production: true
sources:
  - name: graphjin
    kind: database
    type: sqlite
`, "yaml")
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	// New security model: prod hard-gates the agentic surface in source mode, so
	// the MCP server never mounts there even without an explicit mcp.disable.
	if !conf.mcpDisabled() {
		t.Fatal("sources production config must hard-gate MCP (agentic surface off in prod)")
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
