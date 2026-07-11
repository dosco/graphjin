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
		"agent": map[string]any{"model": "gpt-x", "max_steps": float64(12)},
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
}

func TestHandleUpdateCurrentConfig_ServAgentPatchHotAppliesAndPersists(t *testing.T) {
	dbPath := createSQLiteDBFile(t, "serv.sqlite3", true)
	v := viper.New()
	ms := newTransactionalConfigMCPServerWithOptions(t, dbPath, false, v)
	ms.service.conf.MCP.AllowConfigUpdates = true

	res, err := ms.handleUpdateCurrentConfig(context.Background(), newToolRequest(map[string]any{
		"serv": map[string]any{
			"agent": map[string]any{"model": "gpt-hot", "max_steps": float64(11)},
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
	if out.ReloadMode != servReloadHot {
		t.Fatalf("expected reload_mode=hot, got %q", out.ReloadMode)
	}
	// Applied live to conf.Serv.
	if got := ms.service.conf.Serv.Agent.Model; got != "gpt-hot" {
		t.Fatalf("agent.model not applied live, got %q", got)
	}
	if got := ms.service.conf.Serv.Agent.MaxSteps; got != 11 {
		t.Fatalf("agent.max_steps not applied live, got %d", got)
	}
	// Persisted into viper so a save writes it. viper stores the struct under
	// "agent" (dotted traversal into a struct value is unsupported), mirroring
	// how the existing code persists "mcp".
	staged, ok := v.Get("agent").(AgentConfig)
	if !ok || staged.Model != "gpt-hot" {
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
	if out.ReloadMode != servReloadRestart {
		t.Fatalf("expected reload_mode=restart, got %q", out.ReloadMode)
	}
	if ms.service.conf.Serv.RateLimiter.Rate != 42 || ms.service.conf.Serv.RateLimiter.Bucket != 7 {
		t.Fatalf("rate_limiter not applied: %+v", ms.service.conf.Serv.RateLimiter)
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
