package agent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
)

// recordingProvider is an OpenAI-compatible endpoint that records the bearer
// token of every request it receives.
type recordingProvider struct {
	mu     sync.Mutex
	tokens []string
	paths  []string
}

func (p *recordingProvider) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.tokens = append(p.tokens, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		p.paths = append(p.paths, r.URL.Path)
		p.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"r1","object":"chat.completion","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (p *recordingProvider) lastToken(t *testing.T) string {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.tokens) == 0 {
		t.Fatal("the provider received no request")
	}
	return p.tokens[len(p.tokens)-1]
}

func chatOnce(t *testing.T, cfg Config) {
	t.Helper()
	client, err := DefaultClientFactory(cfg)
	if err != nil {
		t.Fatalf("build client: %v", err)
	}
	_, _ = client.Chat(context.Background(), map[string]ax.Value{
		"chat_prompt": []ax.Value{map[string]ax.Value{"role": "user", "content": "hi"}},
	}, nil)
}

func writeKey(t *testing.T, path, key string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(key), 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}
}

// Acceptance (#655): change the key file, then send a request — the request
// carries the new key, and nothing is rebuilt but the per-request client.
func TestDefaultClientFactoryReadsKeyFileEachBuild(t *testing.T) {
	provider := &recordingProvider{}
	srv := provider.server(t)
	keyFile := filepath.Join(t.TempDir(), "provider.key")
	cfg := Config{Provider: "openai", Model: "gpt-4.1-mini", BaseURL: srv.URL, APIKeyFile: keyFile}

	writeKey(t, keyFile, "sk-first\n")
	chatOnce(t, cfg)
	if got := provider.lastToken(t); got != "sk-first" {
		t.Fatalf("first request carried %q, want sk-first (trailing newline trimmed)", got)
	}

	writeKey(t, keyFile, "sk-rotated")
	chatOnce(t, cfg)
	if got := provider.lastToken(t); got != "sk-rotated" {
		t.Fatalf("request after rotation carried %q, want sk-rotated", got)
	}
}

func TestKeyFileFailsClosed(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]func() string{
		"missing": func() string { return filepath.Join(dir, "absent.key") },
		"empty": func() string {
			p := filepath.Join(dir, "empty.key")
			writeKey(t, p, "  \n")
			return p
		},
		"not a single key": func() string {
			p := filepath.Join(dir, "env.key")
			writeKey(t, p, "OPENAI_API_KEY=sk-a\nDB_PASSWORD=hunter2\n")
			return p
		},
		"directory": func() string { return dir },
	}
	for name, path := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := Config{Provider: "openai", APIKeyFile: path()}
			if _, err := DefaultClientFactory(cfg); !errors.Is(err, ErrMissingAPIKey) {
				t.Fatalf("err = %v, want ErrMissingAPIKey", err)
			}
			if cfg.APIKeyConfigured() {
				t.Fatal("APIKeyConfigured reported true for an unusable key file")
			}
		})
	}
	// The error names the file, never what is in it.
	p := filepath.Join(dir, "leak.key")
	writeKey(t, p, "sk-a sk-b")
	_, err := Config{APIKeyFile: p}.ResolveAPIKey()
	if err == nil || strings.Contains(err.Error(), "sk-a") {
		t.Fatalf("err = %v, want a refusal that does not quote the file", err)
	}
}

func TestKeySourceConflictAndDefaults(t *testing.T) {
	if err := (Config{APIKeyEnv: "OPENAI_API_KEY", APIKeyFile: "/k"}).ValidateKeySource(); !errors.Is(err, ErrAPIKeySourceConflict) {
		t.Fatalf("both sources: err = %v, want ErrAPIKeySourceConflict", err)
	}
	if _, err := DefaultClientFactory(Config{APIKeyEnv: "OPENAI_API_KEY", APIKeyFile: "/k"}); !errors.Is(err, ErrAPIKeySourceConflict) {
		t.Fatalf("factory with both sources: err = %v, want ErrAPIKeySourceConflict", err)
	}
	// A key file replaces the defaulted variable rather than conflicting with it.
	if got := (Config{APIKeyFile: "/k"}).withDefaults().APIKeyEnv; got != "" {
		t.Fatalf("withDefaults set api_key_env %q next to a key file", got)
	}
	if got := (Config{}).withDefaults().APIKeyEnv; got != defaultAPIKeyEnv {
		t.Fatalf("withDefaults api_key_env = %q, want %s", got, defaultAPIKeyEnv)
	}
	if got := (Config{APIKeyFile: "/run/keys/agent"}).KeySource(); got != "file /run/keys/agent" {
		t.Fatalf("KeySource = %q", got)
	}
}

// A lead on another provider must never inherit the worker's key file: that
// would send one provider's key to another.
func TestLeadOnOtherProviderDropsWorkerKeyFile(t *testing.T) {
	cfg := Config{Provider: "openai", APIKeyFile: "/keys/openai", Lead: LeadConfig{Provider: "anthropic", APIKeyEnv: "ANTHROPIC_API_KEY"}}
	lead := cfg.LeadModelConfig()
	if lead.APIKeyFile != "" || lead.APIKeyEnv != "ANTHROPIC_API_KEY" {
		t.Fatalf("lead key source = file %q env %q, want only ANTHROPIC_API_KEY", lead.APIKeyFile, lead.APIKeyEnv)
	}
	same := Config{Provider: "openai", APIKeyFile: "/keys/openai", Lead: LeadConfig{Model: "gpt-x"}}.LeadModelConfig()
	if same.APIKeyFile != "/keys/openai" {
		t.Fatalf("lead on the worker's provider should share its key file, got %q", same.APIKeyFile)
	}
}
