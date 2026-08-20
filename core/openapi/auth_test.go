package openapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newTestRequest is a tiny helper that builds an outgoing request with
// minimal boilerplate at every call site. Callers that need to inspect
// the request after Apply runs use the returned pointer directly.
func newTestRequest(t *testing.T, url string) *http.Request {
	t.Helper()
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	return req
}

func TestNoopAuth(t *testing.T) {
	p, err := NewAuthProvider(AuthConfig{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	req := newTestRequest(t, "https://api.example.com/users")
	if err := p.Apply(context.Background(), req, nil); err != nil {
		t.Fatal(err)
	}
	if req.Header.Get("Authorization") != "" {
		t.Error("noop auth set Authorization header")
	}
}

func TestBearerAuthStatic(t *testing.T) {
	p, err := NewAuthProvider(AuthConfig{Scheme: "bearer", Token: "abc123"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	req := newTestRequest(t, "https://api.example.com/users")
	if err := p.Apply(context.Background(), req, nil); err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer abc123" {
		t.Errorf("Authorization = %q, want Bearer abc123", got)
	}
}

func TestBearerAuthPassThrough(t *testing.T) {
	p, err := NewAuthProvider(AuthConfig{
		Scheme:           "bearer",
		TokenFromRequest: &TokenFromRequest{Header: "X-User-Token"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	hdrIn := http.Header{}
	hdrIn.Set("X-User-Token", "tenant-tok-99")

	req := newTestRequest(t, "https://api.example.com/users")
	if err := p.Apply(context.Background(), req, hdrIn); err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer tenant-tok-99" {
		t.Errorf("Authorization = %q", got)
	}

	// Missing inbound header should produce a clear error rather than
	// silently sending an unauthenticated request.
	if err := p.Apply(context.Background(), newTestRequest(t, "https://x"), http.Header{}); err == nil {
		t.Error("missing inbound token should error")
	}
}

func TestBasicAuth(t *testing.T) {
	p, err := NewAuthProvider(AuthConfig{Scheme: "basic", Username: "u", Password: "p"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	req := newTestRequest(t, "https://x")
	if err := p.Apply(context.Background(), req, nil); err != nil {
		t.Fatal(err)
	}
	// "u:p" base64-encoded is "dTpw"
	if got := req.Header.Get("Authorization"); got != "Basic dTpw" {
		t.Errorf("Authorization = %q", got)
	}
}

func TestApiKeyAuthHeader(t *testing.T) {
	p, _ := NewAuthProvider(AuthConfig{
		Scheme:   "api_key",
		KeyName:  "X-API-Key",
		KeyValue: "secret",
		KeyIn:    "header",
	}, nil)
	req := newTestRequest(t, "https://x")
	_ = p.Apply(context.Background(), req, nil)
	if got := req.Header.Get("X-API-Key"); got != "secret" {
		t.Errorf("X-API-Key = %q", got)
	}
}

func TestApiKeyAuthQuery(t *testing.T) {
	p, _ := NewAuthProvider(AuthConfig{
		Scheme:   "api_key",
		KeyName:  "api_key",
		KeyValue: "secret",
		KeyIn:    "query",
	}, nil)
	req := newTestRequest(t, "https://x/users")
	_ = p.Apply(context.Background(), req, nil)
	if !strings.Contains(req.URL.RawQuery, "api_key=secret") {
		t.Errorf("RawQuery = %q, missing api_key", req.URL.RawQuery)
	}
}

func TestOAuth2ClientCredentials(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "client_credentials" {
			t.Errorf("grant_type = %q", r.Form.Get("grant_type"))
		}
		if r.Form.Get("client_id") != "cid" || r.Form.Get("client_secret") != "csec" {
			t.Errorf("client creds wrong: id=%q secret=%q", r.Form.Get("client_id"), r.Form.Get("client_secret"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "tok-1",
			"expires_in":   3600,
		})
	}))
	defer srv.Close()

	p, err := NewAuthProvider(AuthConfig{
		Scheme:       "oauth2_client_credentials",
		TokenURL:     srv.URL,
		ClientID:     "cid",
		ClientSecret: "csec",
		Scopes:       []string{"read"},
	}, srv.Client())
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3; i++ {
		req := newTestRequest(t, "https://x")
		if err := p.Apply(context.Background(), req, nil); err != nil {
			t.Fatal(err)
		}
		if got := req.Header.Get("Authorization"); got != "Bearer tok-1" {
			t.Errorf("Authorization = %q", got)
		}
	}
	// Cache should mean only one fetch despite three Apply calls.
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("token endpoint called %d times, want 1 (cache miss)", got)
	}

	// After OnUnauthorized the next Apply should re-fetch.
	if err := p.OnUnauthorized(context.Background()); err != nil {
		t.Fatal(err)
	}
	_ = p.Apply(context.Background(), newTestRequest(t, "https://x"), nil)
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("after invalidate, calls = %d, want 2", got)
	}
}

func TestTokenExchange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if body["apiKeyId"] != "kid" || body["apiKeySecret"] != "ksec" {
			t.Errorf("body = %v", body)
		}
		// Mimic Salesforce MC Personalization shape — non-OAuth2 field names.
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "is-token-xyz",
			"expires_in":   "3500", // string form, on purpose
		})
	}))
	defer srv.Close()

	p, err := NewAuthProvider(AuthConfig{
		Scheme:   "token_exchange",
		TokenURL: srv.URL,
		Request: &TokenExchangeRequest{
			BodyFormat: "json",
			Body: map[string]any{
				"apiKeyId":     "kid",
				"apiKeySecret": "ksec",
			},
		},
		Response: &TokenExchangeResponse{
			TokenField:   "access_token",
			ExpiresField: "expires_in",
		},
	}, srv.Client())
	if err != nil {
		t.Fatal(err)
	}

	req := newTestRequest(t, "https://api.example.com/users")
	if err := p.Apply(context.Background(), req, nil); err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer is-token-xyz" {
		t.Errorf("Authorization = %q", got)
	}
}

func TestTokenEndpointErrorsAreRedacted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"access_token":"response-secret"}`))
	}))
	defer srv.Close()

	p, err := NewAuthProvider(AuthConfig{
		Scheme:       "oauth2_client_credentials",
		TokenURL:     srv.URL + "?client_secret=url-secret",
		ClientID:     "client",
		ClientSecret: "config-secret",
	}, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	err = p.Apply(context.Background(), newTestRequest(t, "https://api.example.com"), nil)
	if err == nil {
		t.Fatal("expected token endpoint failure")
	}
	for _, secret := range []string{"response-secret", "url-secret", "config-secret", "access_token"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("token endpoint error leaked %q: %v", secret, err)
		}
	}
}

func TestCachedTokenExpiry(t *testing.T) {
	c := &cachedToken{}
	var fetchCalls int

	// Use a TTL well past the 30s pre-expiry guard so the cached entry
	// is actually considered fresh on the second lookup.
	tok, err := c.get(context.Background(), func(context.Context) (string, time.Duration, error) {
		fetchCalls++
		return "t1", time.Hour, nil
	})
	if err != nil || tok != "t1" {
		t.Fatalf("first get: tok=%q err=%v", tok, err)
	}

	// Cache hit — no second fetch.
	_, _ = c.get(context.Background(), func(context.Context) (string, time.Duration, error) {
		fetchCalls++
		return "t2", time.Hour, nil
	})
	if fetchCalls != 1 {
		t.Errorf("fetchCalls = %d after cached lookup, want 1", fetchCalls)
	}

	// Force the entry into the pre-expiry guard window. We don't sleep
	// in tests; we manipulate the cache state directly so timing is
	// deterministic. Anything within 30s of expiry must trigger refetch.
	c.expires = time.Now().Add(15 * time.Second)
	_, _ = c.get(context.Background(), func(context.Context) (string, time.Duration, error) {
		fetchCalls++
		return "t3", time.Hour, nil
	})
	if fetchCalls != 2 {
		t.Errorf("fetchCalls = %d after near-expiry, want 2", fetchCalls)
	}

	// Explicit invalidate forces a fetch on the next call.
	c.invalidate()
	_, _ = c.get(context.Background(), func(context.Context) (string, time.Duration, error) {
		fetchCalls++
		return "t4", time.Hour, nil
	})
	if fetchCalls != 3 {
		t.Errorf("fetchCalls = %d after invalidate, want 3", fetchCalls)
	}
}
