package serv

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/spf13/viper"
)

// keyRecordingProvider is an OpenAI-compatible endpoint that records the
// bearer token each request carries.
type keyRecordingProvider struct {
	mu     sync.Mutex
	tokens []string
}

func newKeyRecordingProvider(t *testing.T) (*keyRecordingProvider, *httptest.Server) {
	t.Helper()
	p := &keyRecordingProvider{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.tokens = append(p.tokens, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		p.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"r1","object":"chat.completion","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	t.Cleanup(srv.Close)
	return p, srv
}

func (p *keyRecordingProvider) received() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.tokens...)
}

func writeAgentKey(t *testing.T, path, key string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(key+"\n"), 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}
}

// Acceptance (#655): change the provider, base URL and key source through
// gj_config, then send an agent request. The request builds its client from
// the new values and reaches the new provider with the new key, and the
// schema does not reload.
func TestGJConfigSwitchesAgentModelSourceWithoutReload(t *testing.T) {
	oldProvider, oldSrv := newKeyRecordingProvider(t)
	newProvider, newSrv := newKeyRecordingProvider(t)
	keyDir := t.TempDir()
	oldKey, newKey := filepath.Join(keyDir, "old.key"), filepath.Join(keyDir, "new.key")
	writeAgentKey(t, oldKey, "sk-old")
	writeAgentKey(t, newKey, "sk-new")

	dbPath := createSQLiteDBFile(t, "agent-key.sqlite3", true)
	ms := newTransactionalConfigMCPServerWithOptions(t, dbPath, false, viper.New())
	ms.service.conf.MCP.AllowConfigUpdates = true
	ms.service.conf.Agent = AgentConfig{Enabled: true, Provider: "anthropic", Model: "gpt-4.1-mini", BaseURL: oldSrv.URL, APIKeyFile: oldKey}
	ms.service.conf.agentKeyFileDir = keyDir

	// Every client the agent builds is recorded, then probed with one chat
	// call so the test sees where that client sends requests and with what key.
	var built []gjagent.Config
	ms.service.agentClientFactory = func(cfg gjagent.Config) (ax.AIClient, error) {
		built = append(built, cfg)
		client, err := gjagent.DefaultClientFactory(cfg)
		if err == nil {
			_, _ = client.Chat(context.Background(), map[string]ax.Value{
				"chat_prompt": []ax.Value{map[string]ax.Value{"role": "user", "content": "probe"}},
			}, nil)
		}
		return client, err
	}
	gjBefore := ms.service.gj

	res, err := ms.handleUpdateCurrentConfig(context.Background(), newToolRequest(map[string]any{
		"serv": map[string]any{"agent": map[string]any{
			"provider": "openai", "base_url": newSrv.URL, "api_key_file": "new.key",
		}},
	}))
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	var out ConfigUpdateResult
	if err := json.Unmarshal([]byte(assertToolSuccess(t, res)), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !out.Success || out.ReloadMode != servReloadHot || out.ReloadStrategy != "" {
		t.Fatalf("expected a hot serv-only change with no core reload, got %+v", out)
	}
	if ms.service.gj != gjBefore {
		t.Fatal("the agent model-source change reloaded the GraphJin schema")
	}
	agent := ms.service.conf.Agent
	if agent.Provider != "openai" || agent.BaseURL != newSrv.URL || agent.APIKeyFile != newKey || agent.APIKeyEnv != "" {
		t.Fatalf("live agent config = provider %q base %q file %q env %q", agent.Provider, agent.BaseURL, agent.APIKeyFile, agent.APIKeyEnv)
	}

	runner, err := ms.service.agentEntryRunner(agentConfigFromService(ms.service.conf))
	if err != nil {
		t.Fatalf("agent runner: %v", err)
	}
	_, _ = runner.Run(context.Background(), gjagent.Request{Instruction: "how many accounts are there?"})
	if len(built) == 0 {
		t.Fatal("the agent request built no model client")
	}
	if got := built[0]; got.Provider != "openai" || got.BaseURL != newSrv.URL {
		t.Fatalf("request client built for provider %q at %q, want openai at the new base URL", got.Provider, got.BaseURL)
	}
	if got := newProvider.received(); len(got) == 0 || got[0] != "sk-new" {
		t.Fatalf("new provider received keys %v, want sk-new", got)
	}
	if got := oldProvider.received(); len(got) != 0 {
		t.Fatalf("old provider still received requests after the switch: %v", got)
	}

	// Rotating the file needs no gj_config call: the next request reads it.
	writeAgentKey(t, newKey, "sk-rotated")
	built = nil
	_, _ = runner.Run(context.Background(), gjagent.Request{Instruction: "how many accounts are there?"})
	if got := newProvider.received(); got[len(got)-1] != "sk-rotated" {
		t.Fatalf("request after rotating the key file carried %q, want sk-rotated", got[len(got)-1])
	}
}

// A bad provider or a missing key fails the next request closed.
func TestGJConfigBadModelSourceFailsRequestClosed(t *testing.T) {
	keyDir := t.TempDir()
	keyFile := filepath.Join(keyDir, "agent.key")
	writeAgentKey(t, keyFile, "sk-live")
	dbPath := createSQLiteDBFile(t, "agent-bad.sqlite3", true)
	ms := newTransactionalConfigMCPServerWithOptions(t, dbPath, false, viper.New())
	ms.service.conf.MCP.AllowConfigUpdates = true
	ms.service.conf.Agent = AgentConfig{Enabled: true, Provider: "openai", APIKeyFile: keyFile}
	ms.service.conf.agentKeyFileDir = keyDir

	apply := func(agent map[string]any) {
		t.Helper()
		res, err := ms.handleUpdateCurrentConfig(context.Background(), newToolRequest(map[string]any{"serv": map[string]any{"agent": agent}}))
		if err != nil {
			t.Fatalf("update: %v", err)
		}
		var out ConfigUpdateResult
		if err := json.Unmarshal([]byte(assertToolSuccess(t, res)), &out); err != nil || !out.Success {
			t.Fatalf("update %v failed: %+v err=%v", agent, out, err)
		}
	}
	run := func() error {
		t.Helper()
		runner, err := ms.service.agentEntryRunner(agentConfigFromService(ms.service.conf))
		if err != nil {
			return err
		}
		_, err = runner.Run(context.Background(), gjagent.Request{Instruction: "how many accounts are there?"})
		return err
	}

	apply(map[string]any{"provider": "no-such-profile"})
	if err := run(); err == nil || !strings.Contains(err.Error(), "no-such-profile") {
		t.Fatalf("bad provider: err = %v, want the request refused naming the provider", err)
	}

	apply(map[string]any{"provider": "openai", "api_key_file": "absent.key"})
	if err := run(); !errors.Is(err, gjagent.ErrMissingAPIKey) {
		t.Fatalf("missing key file: err = %v, want ErrMissingAPIKey", err)
	}
}

// A runtime write may switch key sources but never widen which secrets the
// agent can send out.
func TestValidateAgentModelSourcePatch(t *testing.T) {
	keyDir := t.TempDir()
	conf := &Config{}
	conf.Agent = AgentConfig{Provider: "openai", APIKeyFile: filepath.Join(keyDir, "agent.key")}
	conf.agentKeyFileDir = keyDir

	ok := []map[string]any{
		{"provider": "anthropic", "api_key_env": "ANTHROPIC_API_KEY"},
		{"base_url": "https://llm.internal.example/v1"},
		{"base_url": ""},
		{"api_key_file": "rotated.key"},
		{"api_key_file": filepath.Join(keyDir, "other.key")},
	}
	for _, patch := range ok {
		if _, _, err := validateServConfigPatchFor(conf, map[string]any{"agent": patch}); err != nil {
			t.Errorf("patch %v refused: %v", patch, err)
		}
	}
	refused := map[string]map[string]any{
		"both key sources":         {"api_key_env": "OPENAI_API_KEY", "api_key_file": "agent.key"},
		"key file outside the dir": {"api_key_file": "/etc/passwd"},
		"key file escaping":        {"api_key_file": "../agent.key"},
		"non-key variable":         {"api_key_env": "DATABASE_PASSWORD"},
		"empty provider":           {"provider": ""},
		"non-http base url":        {"base_url": "file:///etc/passwd"},
		"credentials in base url":  {"base_url": "https://user:pass@llm.example/v1"},
		"non-string provider":      {"provider": float64(1)},
	}
	for name, patch := range refused {
		if _, _, err := validateServConfigPatchFor(conf, map[string]any{"agent": patch}); err == nil {
			t.Errorf("%s: patch %v accepted", name, patch)
		}
	}
	// Without a key file in the config file there is no vouched-for directory.
	if _, _, err := validateServConfigPatchFor(&Config{}, map[string]any{"agent": map[string]any{"api_key_file": "/keys/agent.key"}}); err == nil {
		t.Error("a runtime key file was accepted with no configured key directory")
	}
	// A provider switch must still satisfy the lead's own-key rule.
	withLead := &Config{}
	withLead.Agent = AgentConfig{Provider: "openai", APIKeyEnv: "OPENAI_API_KEY", Lead: gjagent.LeadConfig{Provider: "openai"}}
	if _, _, err := validateServConfigPatchFor(withLead, map[string]any{"agent": map[string]any{"provider": "anthropic", "api_key_env": "ANTHROPIC_API_KEY"}}); err == nil {
		t.Error("a provider switch that strands the lead without its own key was accepted")
	}

	// Writing one source clears the other.
	a := conf.Agent
	applyAgentConfigPatch(&a, map[string]any{"api_key_env": "ANTHROPIC_API_KEY"}, conf)
	if a.APIKeyEnv != "ANTHROPIC_API_KEY" || a.APIKeyFile != "" {
		t.Fatalf("switch to env left file %q env %q", a.APIKeyFile, a.APIKeyEnv)
	}
	applyAgentConfigPatch(&a, map[string]any{"api_key_file": "agent.key"}, conf)
	if a.APIKeyFile != filepath.Join(keyDir, "agent.key") || a.APIKeyEnv != "" {
		t.Fatalf("switch to file left file %q env %q", a.APIKeyFile, a.APIKeyEnv)
	}
}

// Acceptance (#655): /api/v1/agent/status reports api_key_configured from the
// file when api_key_file is set, read at request time.
func TestAgentStatusReportsKeyFromFile(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "agent.key")
	conf := &Config{Serv: Serv{Agent: AgentConfig{Enabled: true, Provider: "openai", APIKeyFile: keyFile}}}
	hs := newAgentHTTPTestService(conf)
	status := func() agentStatusResponse {
		t.Helper()
		rec := httptest.NewRecorder()
		hs.AgentStatus(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, routeAgentStatus, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
		var out agentStatusResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode status: %v", err)
		}
		if strings.Contains(rec.Body.String(), "sk-from-file") {
			t.Fatalf("status leaked the key: %s", rec.Body.String())
		}
		return out
	}

	// The variable the env source would default to is set, and must not count.
	t.Setenv("OPENAI_API_KEY", "sk-from-env")
	missing := status()
	if missing.APIKeyConfigured || missing.Status != "missing_key" || missing.APIKeyFile != keyFile || missing.APIKeyEnv != "" {
		t.Fatalf("status with no key file = %+v", missing)
	}
	if !strings.Contains(missing.Message, keyFile) {
		t.Fatalf("missing-key message should name the key file, got %q", missing.Message)
	}

	writeAgentKey(t, keyFile, "sk-from-file")
	ready := status()
	if !ready.APIKeyConfigured || !ready.Ready || ready.Status != "ready" {
		t.Fatalf("status after writing the key file = %+v", ready)
	}

	if err := os.Remove(keyFile); err != nil {
		t.Fatal(err)
	}
	if gone := status(); gone.APIKeyConfigured {
		t.Fatalf("status still reports a key after the file was removed: %+v", gone)
	}
}

func TestConfigAgentKeyFileSource(t *testing.T) {
	t.Setenv("GJ_AGENT_API_KEY_ENV", "")
	t.Setenv("GJ_AGENT_API_KEY_FILE", "")

	c, err := NewConfig("agent:\n  enabled: true\n  api_key_file: /run/secrets/agent.key\n", "yaml")
	if err != nil {
		t.Fatalf("key file alone: %v", err)
	}
	if c.Agent.APIKeyFile != "/run/secrets/agent.key" || c.Agent.APIKeyEnv != "" || c.agentKeyFileDir != "/run/secrets" {
		t.Fatalf("key file config = file %q env %q dir %q", c.Agent.APIKeyFile, c.Agent.APIKeyEnv, c.agentKeyFileDir)
	}

	if _, err := NewConfig("agent:\n  api_key_env: OPENAI_API_KEY\n  api_key_file: /run/secrets/agent.key\n", "yaml"); !errors.Is(err, gjagent.ErrAPIKeySourceConflict) {
		t.Fatalf("both sources: err = %v, want ErrAPIKeySourceConflict", err)
	}

	c, err = NewConfig("agent:\n  enabled: true\n", "yaml")
	if err != nil {
		t.Fatal(err)
	}
	if c.Agent.APIKeyEnv != "OPENAI_API_KEY" || c.Agent.APIKeyFile != "" {
		t.Fatalf("default key source = env %q file %q", c.Agent.APIKeyEnv, c.Agent.APIKeyFile)
	}

	// A relative key file resolves against the config directory.
	rel := &Config{}
	rel.ConfigPath = "/etc/graphjin"
	rel.Agent = AgentConfig{APIKeyFile: "keys/agent.key"}
	if err := normalizeAgentKeySource(rel); err != nil {
		t.Fatal(err)
	}
	if rel.Agent.APIKeyFile != "/etc/graphjin/keys/agent.key" || rel.agentKeyFileDir != "/etc/graphjin/keys" {
		t.Fatalf("relative key file resolved to %q (dir %q)", rel.Agent.APIKeyFile, rel.agentKeyFileDir)
	}
}
