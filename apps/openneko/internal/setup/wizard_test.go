package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type recorded struct {
	method string
	path   string
	body   map[string]any
}

// mockSetupServer answers every endpoint the onboarding flow touches and
// records each request so tests can assert the call sequence and bodies.
func mockSetupServer(changed bool, recs *[]recorded) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		*recs = append(*recs, recorded{r.Method, r.URL.Path, body})
		enc := json.NewEncoder(w)
		switch r.Method + " " + r.URL.Path {
		case "GET /api/admin/change-password":
			_ = enc.Encode(map[string]bool{"changed": changed})
		case "GET /api/settings/data-source":
			_ = enc.Encode(map[string]any{"source": "unset"})
		case "GET /api/settings/agent":
			_ = enc.Encode(map[string]any{
				"agent":    map[string]any{"source": "default", "globalCap": 5},
				"defaults": map[string]any{"globalCap": 5},
			})
		case "GET /api/settings/provider":
			_ = enc.Encode(map[string]any{
				"primary":  map[string]any{"scope": "primary", "source": "default", "provider": "anthropic", "model": "", "enabled": false, "config": map[string]any{}},
				"research": map[string]any{"scope": "research", "source": "default", "provider": "disabled", "model": "", "enabled": false, "config": map[string]any{}},
				"options": map[string]any{
					"primary":  []map[string]string{{"value": "anthropic", "label": "Anthropic", "description": "claude"}},
					"research": []map[string]string{{"value": "disabled", "label": "Disabled", "description": "off"}, {"value": "perplexity", "label": "Perplexity", "description": "research"}},
				},
				"defaults": map[string]any{
					"primary":  map[string]string{"anthropic": "claude-opus-4-7"},
					"research": map[string]string{"perplexity": "sonar"},
				},
				"fields": map[string]any{
					"primary":  map[string]any{"anthropic": []map[string]any{{"key": "apiKey", "label": "API key", "kind": "secret", "required": true}}},
					"research": map[string]any{"perplexity": []map[string]any{{"key": "apiKey", "label": "API key", "kind": "secret"}}},
				},
			})
		default:
			_ = enc.Encode(map[string]any{"ok": true})
		}
	}))
}

func findWhere(recs []recorded, method, path string, pred func(map[string]any) bool) (recorded, bool) {
	for _, rec := range recs {
		if rec.method == method && rec.path == path && (pred == nil || pred(rec.body)) {
			return rec, true
		}
	}
	return recorded{}, false
}

func TestHeadlessConfigure(t *testing.T) {
	var recs []recorded
	srv := mockSetupServer(false, &recs)
	defer srv.Close()

	out := &bytes.Buffer{}
	cfg := Config{
		Mode: "demo", BaseURL: srv.URL, Headless: true,
		AdminPassword: "supersecret", Provider: "anthropic", ProviderKey: "sk-test", NoResearch: true,
	}
	outcome, err := Run(context.Background(), NewClient(srv.URL), out, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !outcome.Configured || outcome.Skipped {
		t.Fatalf("want Configured, got %+v", outcome)
	}
	// The rotated password is surfaced so the CLI can reconnect the gateway.
	if outcome.PasswordSet != "supersecret" {
		t.Errorf("PasswordSet = %q, want supersecret", outcome.PasswordSet)
	}

	if _, ok := findWhere(recs, "POST", "/api/admin/change-password", func(b map[string]any) bool {
		return b["password"] == "supersecret"
	}); !ok {
		t.Error("missing password change with the supplied password")
	}
	// Data source saved with demo's internal GraphJin root, canonical suffix.
	if _, ok := findWhere(recs, "PUT", "/api/settings/data-source", func(b map[string]any) bool {
		return b["graphqlUrl"] == "http://graphjin:8080/api/v1/graphql"
	}); !ok {
		t.Error("data source not saved with derived demo endpoint")
	}
	// Provider key tested before save, keyed by the provider's secret field.
	if _, ok := findWhere(recs, "POST", "/api/settings/provider/test", func(b map[string]any) bool {
		s, _ := b["secrets"].(map[string]any)
		return s != nil && s["apiKey"] == "sk-test"
	}); !ok {
		t.Error("provider key not tested with apiKey secret")
	}
	if _, ok := findWhere(recs, "PUT", "/api/settings/agent", func(b map[string]any) bool {
		return b["globalCap"] == float64(5) && b["backend"] == nil
	}); !ok {
		t.Error("agent concurrency not saved")
	}
	if _, ok := findWhere(recs, "PUT", "/api/settings/provider", func(b map[string]any) bool {
		return b["scope"] == "research" && b["provider"] == "disabled"
	}); !ok {
		t.Error("research not explicitly disabled")
	}
	if _, ok := findWhere(recs, "POST", "/settings/finish", nil); !ok {
		t.Error("setup not finished")
	}
}

func TestHeadlessAllowsAnyProvider(t *testing.T) {
	var recs []recorded
	srv := mockSetupServer(true, &recs) // password already changed
	defer srv.Close()

	cfg := Config{
		Mode: "prod", BaseURL: srv.URL, Headless: true,
		Provider: "openai", ProviderKey: "sk", DataURL: "http://x:8080", NoResearch: true,
	}
	outcome, err := Run(context.Background(), NewClient(srv.URL), &bytes.Buffer{}, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Password was already changed → no rotation this run, so nothing to
	// reconnect for.
	if outcome.PasswordSet != "" {
		t.Errorf("PasswordSet = %q, want empty (password already set)", outcome.PasswordSet)
	}
	// Setup preserves the selected provider.
	if _, ok := findWhere(recs, "PUT", "/api/settings/provider", func(b map[string]any) bool {
		return b["scope"] == "primary" && b["provider"] == "openai"
	}); !ok {
		t.Error("setup should preserve the selected primary provider")
	}
}

func TestHeadlessMissingFlags(t *testing.T) {
	var recs []recorded
	srv := mockSetupServer(true, &recs) // password already set, so not the blocker
	defer srv.Close()

	cfg := Config{Mode: "demo", BaseURL: srv.URL, Headless: true, Provider: "anthropic"} // no provider-key
	_, err := Run(context.Background(), NewClient(srv.URL), &bytes.Buffer{}, cfg)
	if err == nil {
		t.Fatal("expected error for missing --provider-key")
	}
}

func TestHeadlessMissingPassword(t *testing.T) {
	var recs []recorded
	srv := mockSetupServer(false, &recs) // password NOT set and none provided
	defer srv.Close()

	cfg := Config{Mode: "demo", BaseURL: srv.URL, Headless: true, Provider: "anthropic", ProviderKey: "k"}
	_, err := Run(context.Background(), NewClient(srv.URL), &bytes.Buffer{}, cfg)
	if err == nil {
		t.Fatal("expected error for missing --admin-password on a bootstrap-default DB")
	}
}

func TestPureHelpers(t *testing.T) {
	if err := validatePassword("short"); err == nil {
		t.Error("short password should fail")
	}
	if err := validatePassword("secret"); err == nil {
		t.Error("forbidden password should fail")
	}
	if err := validatePassword("longenough"); err != nil {
		t.Errorf("valid password rejected: %v", err)
	}

	if k := secretFieldKey([]Field{{Key: "X", Kind: "text"}, {Key: "tok", Kind: "secret"}}); k != "tok" {
		t.Errorf("secretFieldKey = %q, want tok", k)
	}
	if k := secretFieldKey(nil); k != "apiKey" {
		t.Errorf("secretFieldKey default = %q, want apiKey", k)
	}

	demo := defaultDataURL(Config{Mode: "demo"}, &DataSource{Source: "unset"})
	if demo != "http://graphjin:8080" {
		t.Errorf("demo default = %q", demo)
	}
	dev := defaultDataURL(Config{Mode: "dev"}, &DataSource{Source: "unset"})
	if dev != "http://localhost:8080" {
		t.Errorf("dev default = %q", dev)
	}
	prod := defaultDataURL(Config{Mode: "prod"}, &DataSource{Source: "unset"})
	if prod != "http://graphjin:8080" {
		t.Errorf("prod default = %q", prod)
	}
	saved := defaultDataURL(Config{Mode: "demo"}, &DataSource{Source: "org", GraphqlURL: "http://h:9/api/v1/graphql"})
	if saved != "http://h:9" {
		t.Errorf("saved-source default = %q, want root", saved)
	}

	if m := modelDefault(map[string]string{"anthropic": "claude-x"}, "anthropic", ProviderConfig{}); m != "claude-x" {
		t.Errorf("modelDefault = %q", m)
	}
	if r := disabledResearch(); r.Provider != "disabled" || r.Enabled {
		t.Errorf("disabledResearch = %+v", r)
	}
	if got := filterOut([]ProviderOption{{Value: "disabled"}, {Value: "perplexity"}}, "disabled"); len(got) != 1 || got[0].Value != "perplexity" {
		t.Errorf("filterOut = %+v", got)
	}
}

// A rotation that succeeded before a later step failed must still surface
// through the Outcome — the CLI persists it to the host config either way.
// Dropping it left the remote role rotated while the host kept the stale
// bootstrap password: a lockout on the next host-side connection.
func TestHeadlessErrorAfterPasswordRotationCarriesPasswordSet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		enc := json.NewEncoder(w)
		switch r.Method + " " + r.URL.Path {
		case "GET /api/admin/change-password":
			_ = enc.Encode(map[string]bool{"changed": false})
		case "POST /api/admin/change-password":
			_ = enc.Encode(map[string]bool{"ok": true})
		case "GET /api/settings/data-source":
			_ = enc.Encode(map[string]any{"source": "unset"})
		case "POST /api/settings/data-source/test":
			// The step after the rotation fails.
			w.WriteHeader(http.StatusBadGateway)
			_ = enc.Encode(map[string]string{"error": "graphjin still starting"})
		default:
			_ = enc.Encode(map[string]any{"ok": true})
		}
	}))
	defer srv.Close()

	out := &bytes.Buffer{}
	cfg := Config{
		Mode: "demo", BaseURL: srv.URL, Headless: true,
		AdminPassword: "supersecret", Provider: "anthropic", ProviderKey: "sk-test", NoResearch: true,
	}
	outcome, err := Run(context.Background(), NewClient(srv.URL), out, cfg)
	if err == nil {
		t.Fatal("expected the data-source test failure to surface as an error")
	}
	if outcome.PasswordSet != "supersecret" {
		t.Errorf("PasswordSet = %q, want supersecret carried through the error return", outcome.PasswordSet)
	}
}
