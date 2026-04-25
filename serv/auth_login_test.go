package serv

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dosco/graphjin/auth/v3/issuer"
	"github.com/dosco/graphjin/auth/v3/oidc"
)

// newStubAuthLogin returns an authLoginService wired with an in-memory issuer
// and a nil OIDC provider (sufficient for device-code state-machine tests
// that don't exercise the OIDC callback).
func newStubAuthLogin(t *testing.T) *authLoginService {
	t.Helper()
	iss, err := issuer.New(issuer.Config{
		Secret:   "test-secret-plenty-long",
		Issuer:   "https://graphjin.test",
		Audience: "graphjin-cli",
		TTL:      time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &authLoginService{
		cfg:      AuthLogin{Enabled: true, TokenTTL: time.Hour, Audience: "graphjin-cli"},
		issuer:   iss,
		provider: &oidc.Provider{}, // never invoked in these tests
		byDevice: map[string]*deviceSession{},
		byUser:   map[string]string{},
		byState:  map[string]string{},
	}
}

func TestDeviceStart(t *testing.T) {
	a := newStubAuthLogin(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/device", bytes.NewReader([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	req.Host = "graphjin.example:8080"
	w := httptest.NewRecorder()
	a.handleDevice(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"device_code", "user_code", "verification_uri", "expires_in", "interval"} {
		if _, ok := resp[k]; !ok {
			t.Errorf("missing key %q in response", k)
		}
	}
	if v := resp["verification_uri"].(string); !strings.HasSuffix(v, "/api/v1/auth/device") {
		t.Errorf("verification_uri = %s", v)
	}
}

func TestDeviceTokenAuthorizationPending(t *testing.T) {
	a := newStubAuthLogin(t)
	// Seed a pending session.
	a.byDevice["dc1"] = &deviceSession{
		DeviceCode: "dc1", UserCode: "A-B",
		CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute),
		Status: deviceStatusPending,
	}
	a.byUser["A-B"] = "dc1"

	body, _ := json.Marshal(map[string]string{"device_code": "dc1"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/device/token", bytes.NewReader(body))
	w := httptest.NewRecorder()
	a.handleDeviceToken(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", w.Code)
	}
	var r map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &r)
	if r["error"] != "authorization_pending" {
		t.Errorf("error = %s", r["error"])
	}
}

func TestDeviceTokenCompletedThenRedeemed(t *testing.T) {
	a := newStubAuthLogin(t)
	a.byDevice["dc1"] = &deviceSession{
		DeviceCode: "dc1", Status: deviceStatusCompleted,
		Token: "signed-jwt-here", TokenExp: time.Now().Add(time.Hour),
		ExpiresAt: time.Now().Add(10 * time.Minute),
	}

	body, _ := json.Marshal(map[string]string{"device_code": "dc1"})

	// First poll: should return 200 + token.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/device/token", bytes.NewReader(body))
	w := httptest.NewRecorder()
	a.handleDeviceToken(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first poll: status = %d, body = %s", w.Code, w.Body.String())
	}
	var r1 map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &r1)
	if r1["token"] != "signed-jwt-here" {
		t.Errorf("token missing in first poll: %v", r1)
	}

	// Advance to avoid the slow_down guard, then second poll should be access_denied.
	a.byDevice["dc1"].LastPollAt = time.Now().Add(-time.Hour)

	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/device/token", bytes.NewReader(body))
	w2 := httptest.NewRecorder()
	a.handleDeviceToken(w2, req2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("second poll: status = %d, body = %s", w2.Code, w2.Body.String())
	}
	var r2 map[string]string
	_ = json.Unmarshal(w2.Body.Bytes(), &r2)
	if r2["error"] != "access_denied" {
		t.Errorf("second poll error = %s", r2["error"])
	}
}

func TestDeviceTokenExpired(t *testing.T) {
	a := newStubAuthLogin(t)
	a.byDevice["dc1"] = &deviceSession{
		DeviceCode: "dc1", UserCode: "X-Y",
		ExpiresAt: time.Now().Add(-time.Minute),
		Status:    deviceStatusPending,
	}
	a.byUser["X-Y"] = "dc1"

	body, _ := json.Marshal(map[string]string{"device_code": "dc1"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/device/token", bytes.NewReader(body))
	w := httptest.NewRecorder()
	a.handleDeviceToken(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", w.Code)
	}
	var r map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &r)
	if r["error"] != "expired_token" {
		t.Errorf("error = %s", r["error"])
	}
	if _, ok := a.byDevice["dc1"]; ok {
		t.Error("expired session should be evicted")
	}
}

func TestDeviceTokenSlowDown(t *testing.T) {
	a := newStubAuthLogin(t)
	a.byDevice["dc1"] = &deviceSession{
		DeviceCode: "dc1",
		ExpiresAt:  time.Now().Add(time.Minute),
		Status:     deviceStatusPending,
		LastPollAt: time.Now(), // just polled
	}
	body, _ := json.Marshal(map[string]string{"device_code": "dc1"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/device/token", bytes.NewReader(body))
	w := httptest.NewRecorder()
	a.handleDeviceToken(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d", w.Code)
	}
	var r map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &r)
	if r["error"] != "slow_down" {
		t.Errorf("error = %s", r["error"])
	}
}

func TestDeviceTokenUnknown(t *testing.T) {
	a := newStubAuthLogin(t)
	body, _ := json.Marshal(map[string]string{"device_code": "ghost"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/device/token", bytes.NewReader(body))
	w := httptest.NewRecorder()
	a.handleDeviceToken(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", w.Code)
	}
	var r map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &r)
	if r["error"] != "unknown_device_code" {
		t.Errorf("error = %s", r["error"])
	}
}

func TestRandomUserCodeFormat(t *testing.T) {
	for i := 0; i < 20; i++ {
		uc := randomUserCode()
		if len(uc) != 9 || uc[4] != '-' {
			t.Fatalf("unexpected format: %q", uc)
		}
	}
}

func TestPublicBaseURL(t *testing.T) {
	cases := []struct {
		name string
		r    *http.Request
		want string
	}{
		{"plain", httptest.NewRequest(http.MethodGet, "http://localhost:8080/foo", nil), "http://localhost:8080"},
		{"forwarded", func() *http.Request {
			r := httptest.NewRequest(http.MethodGet, "http://internal/foo", nil)
			r.Header.Set("X-Forwarded-Proto", "https")
			r.Header.Set("X-Forwarded-Host", "graphjin.example")
			return r
		}(), "https://graphjin.example"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := publicBaseURL(tc.r); got != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}

// TestAuthLoginServiceRejectsMisconfig ensures newAuthLoginService refuses to
// start in configurations where built-in login would silently be unusable
// (e.g. auth.jwt.provider=auth0 — external IdP mints its own tokens).
func TestAuthLoginServiceRejectsMisconfig(t *testing.T) {
	conf := &Config{}
	conf.AuthLogin = AuthLogin{Enabled: true, OIDC: AuthLoginOIDC{IssuerURL: "https://accounts.google.com", ClientID: "cid"}}
	conf.Auth.JWT.Provider = "auth0"
	conf.Auth.JWT.Secret = "secret"
	_, err := newAuthLoginService(context.Background(), conf)
	if err == nil || !strings.Contains(err.Error(), "incompatible") {
		t.Errorf("expected incompatible-provider error, got %v", err)
	}
}

// TestAudienceGraphjinConflict ensures the convenience shortcut
// audience_graphjin: true rejects a conflicting explicit audience value, and
// happily accepts a redundant matching one.
func TestAudienceGraphjinConflict(t *testing.T) {
	conf := &Config{}
	conf.Auth.JWT.Secret = "test-secret-plenty-long"
	conf.AuthLogin = AuthLogin{
		Enabled:          true,
		AudienceGraphjin: true,
		Audience:         "something-else",
		OIDC:             AuthLoginOIDC{IssuerURL: "https://accounts.google.com", ClientID: "cid"},
	}
	_, err := newAuthLoginService(context.Background(), conf)
	if err == nil || !strings.Contains(err.Error(), "audience_graphjin") {
		t.Errorf("expected audience conflict error, got %v", err)
	}

	// Same value is fine — no surprise.
	conf2 := &Config{}
	conf2.Auth.JWT.Secret = "test-secret-plenty-long"
	conf2.AuthLogin = AuthLogin{
		Enabled:          true,
		AudienceGraphjin: true,
		Audience:         "graphjin-cli",
		OIDC:             AuthLoginOIDC{IssuerURL: "https://invalid-discovery.example", ClientID: "cid"},
	}
	// The OIDC discovery will fail (URL unreachable) — we only care that
	// the validation passed *before* discovery is attempted.
	_, err = newAuthLoginService(context.Background(), conf2)
	if err != nil && strings.Contains(err.Error(), "audience_graphjin") {
		t.Errorf("matching audience should not trigger conflict error: %v", err)
	}
}

func TestAuthLoginServiceRequiresSigningKey(t *testing.T) {
	conf := &Config{}
	conf.AuthLogin = AuthLogin{Enabled: true, OIDC: AuthLoginOIDC{IssuerURL: "https://accounts.google.com", ClientID: "cid"}}
	_, err := newAuthLoginService(context.Background(), conf)
	if err == nil || !strings.Contains(err.Error(), "secret") {
		t.Errorf("expected signing-key error, got %v", err)
	}
}
