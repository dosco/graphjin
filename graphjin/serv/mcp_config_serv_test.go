package serv

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/spf13/viper"
)

// Workstream C: server-side settings (serv.Config) are first-class in the same
// runtime config machinery as the core sections — visible on gj_config
// (redacted) and patchable through update_current_config with hot/restart
// classification.

func TestServConfigMap_RedactsSecretsAndExposesSettings(t *testing.T) {
	conf := &Config{}
	conf.Serv.LogLevel = "info"
	conf.Serv.Agent.Model = "gpt-test"
	conf.Serv.Auth.JWT.Secret = "jwt-signing-secret"

	m := servConfigMap(conf)
	if m["log_level"] != "info" {
		t.Fatalf("expected log_level exposed, got %v", m["log_level"])
	}
	if m["agent"] == nil {
		t.Fatal("expected agent settings exposed on the serv config map")
	}

	blob, _ := json.Marshal(m)
	if bytesContains(blob, "jwt-signing-secret") {
		t.Fatalf("serv config map leaked the JWT secret: %s", blob)
	}
}

func TestValidateServConfigPatch_ClassifiesReloadAndRejectsUnknown(t *testing.T) {
	// agent-only patch is hot
	if _, reload, err := validateServConfigPatch(map[string]any{
		"agent": map[string]any{"model": "gpt-x", "response_format": "json_object", "max_steps": float64(12)},
	}); err != nil || reload != servReloadHot {
		t.Fatalf("agent patch: reload=%q err=%v, want hot/nil", reload, err)
	}

	// rate_limiter makes the whole patch restart-class
	if _, reload, err := validateServConfigPatch(map[string]any{
		"agent":        map[string]any{"model": "gpt-x"},
		"rate_limiter": map[string]any{"rate": float64(50)},
	}); err != nil || reload != servReloadRestart {
		t.Fatalf("mixed patch: reload=%q err=%v, want restart/nil", reload, err)
	}

	// unknown top-level serv key is rejected
	if _, _, err := validateServConfigPatch(map[string]any{"host_port": "0.0.0.0:9999"}); err == nil {
		t.Fatal("expected host_port to be rejected (not in v1 writable allowlist)")
	}
	// non-writable agent field is rejected
	if _, _, err := validateServConfigPatch(map[string]any{"agent": map[string]any{"api_key_env": "X"}}); err == nil {
		t.Fatal("expected agent.api_key_env to be rejected")
	}
	// bad enum value is rejected
	if _, _, err := validateServConfigPatch(map[string]any{"log_level": "loud"}); err == nil {
		t.Fatal("expected invalid log_level to be rejected")
	}
	if _, _, err := validateServConfigPatch(map[string]any{"agent": map[string]any{"response_format": "xml"}}); err == nil {
		t.Fatal("expected invalid agent.response_format to be rejected")
	}
	// The canonical key is writable and validated the same way.
	if _, reload, err := validateServConfigPatch(map[string]any{
		"agent": map[string]any{"structured_output_mode": "json_object"},
	}); err != nil || reload != servReloadHot {
		t.Fatalf("structured_output_mode patch: reload=%q err=%v, want hot/nil", reload, err)
	}
	if _, _, err := validateServConfigPatch(map[string]any{"agent": map[string]any{"structured_output_mode": "strict"}}); err == nil {
		t.Fatal("expected an invalid agent.structured_output_mode to be rejected")
	}
}

func TestClassifyConfigUpdateImpact(t *testing.T) {
	tests := []struct {
		name           string
		coreChanged    bool
		plan           configRuntimeReloadPlan
		mcpChanged     bool
		servChanged    bool
		servReload     string
		wantScope      string
		wantMode       string
		wantStrategy   string
		wantFallback   bool
		wantSourceName string
	}{
		{name: "no change"},
		{name: "mcp only", mcpChanged: true, wantScope: ConfigScopeServ, wantMode: servReloadHot},
		{name: "serv hot", servChanged: true, servReload: servReloadHot, wantScope: ConfigScopeServ, wantMode: servReloadHot},
		{name: "serv restart", servChanged: true, servReload: servReloadRestart, wantScope: ConfigScopeServ, wantMode: servReloadRestart},
		{name: "core full", coreChanged: true, plan: configRuntimeReloadPlan{mode: "full", fallback: true}, wantScope: ConfigScopeCore, wantMode: servReloadHot, wantStrategy: "full", wantFallback: true},
		{name: "core source scoped", coreChanged: true, plan: configRuntimeReloadPlan{mode: "source_scoped", changedSources: []string{"main"}}, wantScope: ConfigScopeCore, wantMode: servReloadHot, wantStrategy: "source_scoped", wantSourceName: "main"},
		{name: "mixed hot", coreChanged: true, plan: configRuntimeReloadPlan{mode: "source_scoped"}, servChanged: true, servReload: servReloadHot, wantScope: ConfigScopeMixed, wantMode: servReloadHot, wantStrategy: "source_scoped"},
		{name: "mixed restart", coreChanged: true, plan: configRuntimeReloadPlan{mode: "full"}, servChanged: true, servReload: servReloadRestart, wantScope: ConfigScopeMixed, wantMode: servReloadRestart, wantStrategy: "full"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyConfigUpdateImpact(tt.coreChanged, tt.plan, tt.mcpChanged, tt.servChanged, tt.servReload)
			if got.scope != tt.wantScope || got.reloadMode != tt.wantMode || got.reloadStrategy != tt.wantStrategy || got.reloadFallback != tt.wantFallback {
				t.Fatalf("impact = scope %q mode %q strategy %q fallback %v, want %q %q %q %v", got.scope, got.reloadMode, got.reloadStrategy, got.reloadFallback, tt.wantScope, tt.wantMode, tt.wantStrategy, tt.wantFallback)
			}
			if tt.wantSourceName != "" && (len(got.changedSources) != 1 || got.changedSources[0] != tt.wantSourceName) {
				t.Fatalf("changed sources = %v, want [%s]", got.changedSources, tt.wantSourceName)
			}
		})
	}
}

func TestHandleUpdateCurrentConfig_ServAgentPatchHotAppliesAndPersists(t *testing.T) {
	dbPath := createSQLiteDBFile(t, "serv.sqlite3", true)
	v := viper.New()
	ms := newTransactionalConfigMCPServerWithOptions(t, dbPath, false, v)
	ms.service.conf.MCP.AllowConfigUpdates = true

	res, err := ms.handleUpdateCurrentConfig(context.Background(), newToolRequest(map[string]any{
		"serv": map[string]any{
			"agent": map[string]any{"model": "gpt-hot", "response_format": "json_object", "max_steps": float64(11)},
		},
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out ConfigUpdateResult
	if err := json.Unmarshal([]byte(assertToolSuccess(t, res)), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !out.Success {
		t.Fatalf("expected success, got %+v", out)
	}
	if out.Scope != ConfigScopeServ || out.ReloadMode != servReloadHot || out.ReloadStrategy != "" {
		t.Fatalf("expected serv/hot with no core strategy, got scope=%q mode=%q strategy=%q", out.Scope, out.ReloadMode, out.ReloadStrategy)
	}
	// Applied live to conf.Serv.
	if got := ms.service.conf.Serv.Agent.Model; got != "gpt-hot" {
		t.Fatalf("agent.model not applied live, got %q", got)
	}
	if got := ms.service.conf.Serv.Agent.MaxSteps; got != 11 {
		t.Fatalf("agent.max_steps not applied live, got %d", got)
	}
	if got := ms.service.conf.Serv.Agent.ResponseFormat; got != "json_object" {
		t.Fatalf("agent.response_format not applied live, got %q", got)
	}
	// The deprecated alias still resolves to a canonical mode, so the runtime
	// reads one value regardless of which key the operator patched.
	if got := ms.service.conf.Serv.Agent.StructuredOutputMode; got != "json_object" {
		t.Fatalf("legacy response_format did not resolve to a mode, got %q", got)
	}
	// Persisted into viper so a save writes it. viper stores the struct under
	// "agent" (dotted traversal into a struct value is unsupported), mirroring
	// how the existing code persists "mcp".
	staged, ok := v.Get("agent").(AgentConfig)
	if !ok || staged.Model != "gpt-hot" || staged.ResponseFormat != "json_object" {
		t.Fatalf("agent not staged into viper for persistence, got %#v", v.Get("agent"))
	}
}

func TestHandleUpdateCurrentConfig_ServRateLimiterPatchReportsRestart(t *testing.T) {
	dbPath := createSQLiteDBFile(t, "serv2.sqlite3", true)
	v := viper.New()
	ms := newTransactionalConfigMCPServerWithOptions(t, dbPath, false, v)
	ms.service.conf.MCP.AllowConfigUpdates = true

	res, err := ms.handleUpdateCurrentConfig(context.Background(), newToolRequest(map[string]any{
		"serv": map[string]any{
			"rate_limiter": map[string]any{"rate": float64(42), "bucket": float64(7)},
		},
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out ConfigUpdateResult
	if err := json.Unmarshal([]byte(assertToolSuccess(t, res)), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Scope != ConfigScopeServ || out.ReloadMode != servReloadRestart || out.ReloadStrategy != "" {
		t.Fatalf("expected serv/restart with no core strategy, got scope=%q mode=%q strategy=%q", out.Scope, out.ReloadMode, out.ReloadStrategy)
	}
	if ms.service.conf.Serv.RateLimiter.Rate != 42 || ms.service.conf.Serv.RateLimiter.Bucket != 7 {
		t.Fatalf("rate_limiter not applied: %+v", ms.service.conf.Serv.RateLimiter)
	}
}

func TestHandleUpdateCurrentConfig_ServPreviewReportsImpactWithoutMutating(t *testing.T) {
	tests := []struct {
		name       string
		serv       map[string]any
		wantMode   string
		assertLive func(*testing.T, *mcpServer)
	}{
		{
			name:     "hot",
			serv:     map[string]any{"agent": map[string]any{"model": "preview-only"}},
			wantMode: servReloadHot,
			assertLive: func(t *testing.T, ms *mcpServer) {
				if ms.service.conf.Serv.Agent.Model == "preview-only" {
					t.Fatal("serv preview mutated the live agent model")
				}
			},
		},
		{
			name:     "restart",
			serv:     map[string]any{"rate_limiter": map[string]any{"rate": float64(73)}},
			wantMode: servReloadRestart,
			assertLive: func(t *testing.T, ms *mcpServer) {
				if ms.service.conf.Serv.RateLimiter.Rate == 73 {
					t.Fatal("serv preview mutated the live rate limiter")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms := newSourceModeConfigMCPServer(t, map[string]string{"main": createSQLiteDBFile(t, tt.name+".sqlite3", true)})
			revision := ms.currentConfigCatalogRevision(context.Background())
			out := applyConfigUpdate(t, ms, map[string]any{
				"mode":                      "preview",
				"expected_catalog_revision": revision,
				"serv":                      tt.serv,
			})
			if !out.Success || !out.Valid || out.PreviewID == "" {
				t.Fatalf("expected valid serv preview, got %+v", out)
			}
			if out.Scope != ConfigScopeServ || out.ReloadMode != tt.wantMode || out.ReloadStrategy != "" {
				t.Fatalf("preview impact = scope %q mode %q strategy %q, want serv/%s/empty", out.Scope, out.ReloadMode, out.ReloadStrategy, tt.wantMode)
			}
			validated := applyConfigUpdate(t, ms, map[string]any{
				"mode":                      "validate",
				"expected_catalog_revision": revision,
				"serv":                      tt.serv,
			})
			if !validated.Success || !validated.Valid || validated.Applied {
				t.Fatalf("expected valid serv dry-run, got %+v", validated)
			}
			if validated.Scope != out.Scope || validated.ReloadMode != out.ReloadMode || validated.ReloadStrategy != out.ReloadStrategy {
				t.Fatalf("preview/validate impact mismatch: preview=%+v validate=%+v", out, validated)
			}
			tt.assertLive(t, ms)

			applied := applyConfigUpdate(t, ms, map[string]any{
				"mode":                      "apply",
				"preview_id":                out.PreviewID,
				"expected_catalog_revision": revision,
				"serv":                      tt.serv,
			})
			if !applied.Success || !applied.Applied {
				t.Fatalf("expected successful serv apply, got %+v", applied)
			}
			if applied.Scope != out.Scope || applied.ReloadMode != out.ReloadMode || applied.ReloadStrategy != out.ReloadStrategy {
				t.Fatalf("preview/apply impact mismatch: preview=%+v apply=%+v", out, applied)
			}
		})
	}
}

func TestConfigKeyScope(t *testing.T) {
	cases := []struct {
		key, scope, reload string
	}{
		{"rate_limiter.rate", ConfigScopeServ, servReloadRestart},
		{"agent.model", ConfigScopeServ, servReloadHot},
		{"auth", ConfigScopeServ, ""},
		{"host_port", ConfigScopeServ, ""},
		{"tables", ConfigScopeCore, ""},
		{"sources", ConfigScopeCore, ""},
	}
	for _, c := range cases {
		scope, reload := ConfigKeyScope(c.key)
		if scope != c.scope || reload != c.reload {
			t.Fatalf("ConfigKeyScope(%q) = (%q,%q), want (%q,%q)", c.key, scope, reload, c.scope, c.reload)
		}
	}
}

func bytesContains(b []byte, sub string) bool {
	return len(sub) > 0 && len(b) >= len(sub) && indexOf(string(b), sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
