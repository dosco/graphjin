package serv

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/dosco/graphjin/auth/v3"
	"github.com/dosco/graphjin/core/v3"
	jwt "github.com/golang-jwt/jwt/v5"
)

const consoleTestSecret = "console-session-test-secret"

func newConsoleSessionService(t *testing.T) (*HttpService, auth.HandlerFunc) {
	t.Helper()
	conf := &Config{Core: core.Config{Mode: "agentic"}}
	conf.Auth.Type = "jwt"
	conf.Auth.JWT.Secret = consoleTestSecret
	ah, err := auth.NewAuthHandlerFunc(conf.Auth)
	if err != nil {
		t.Fatal(err)
	}
	return newAgentHTTPTestService(conf), ah
}

func consoleTestToken(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(consoleTestSecret))
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func consoleSessionRequest(method, body string) *http.Request {
	req := httptest.NewRequest(method, "http://console.test"+routeAuthSession, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Origin", "http://console.test")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	return req
}

func sessionCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == consoleSessionCookie {
			return ck
		}
	}
	return nil
}

// sessionUser runs a request carrying ck through the API's auth handler and
// returns the user it authenticates as.
func sessionUser(t *testing.T, ah auth.HandlerFunc, ck *http.Cookie, header string) any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "http://console.test/api/v1/graphql", nil)
	if ck != nil {
		req.AddCookie(ck)
	}
	if header != "" {
		req.Header.Set("Authorization", header)
	}
	ctx, err := withConsoleSession(ah)(httptest.NewRecorder(), req)
	if err != nil || ctx == nil {
		return nil
	}
	return ctx.Value(core.UserIDKey)
}

func TestConsoleSessionFromPastedToken(t *testing.T) {
	hs, ah := newConsoleSessionService(t)
	token := consoleTestToken(t, jwt.MapClaims{"sub": "ana", "exp": time.Now().Add(time.Hour).Unix()})

	body, _ := json.Marshal(map[string]string{"token": token})
	rec := httptest.NewRecorder()
	hs.AuthSession(ah).ServeHTTP(rec, consoleSessionRequest(http.MethodPost, string(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	ck := sessionCookie(t, rec)
	if ck == nil {
		t.Fatal("no session cookie set")
	}
	if !ck.HttpOnly || ck.SameSite != http.SameSiteStrictMode || ck.Path != consoleSessionPath {
		t.Fatalf("cookie = %+v, want HttpOnly, SameSite=Strict, Path=%s", ck, consoleSessionPath)
	}
	// The session ends with the token, not later.
	if ck.MaxAge > int(time.Hour.Seconds()) || ck.MaxAge < int((59*time.Minute).Seconds()) {
		t.Fatalf("cookie MaxAge = %d, want about an hour", ck.MaxAge)
	}

	if got := sessionUser(t, ah, ck, ""); got != "ana" {
		t.Fatalf("cookie authenticates as %v, want ana", got)
	}
	// A bearer the caller sends itself wins over the cookie.
	other := consoleTestToken(t, jwt.MapClaims{"sub": "ben"})
	if got := sessionUser(t, ah, ck, "Bearer "+other); got != "ben" {
		t.Fatalf("explicit bearer authenticates as %v, want ben", got)
	}

	rec = httptest.NewRecorder()
	hs.AuthSession(ah).ServeHTTP(rec, consoleSessionRequest(http.MethodDelete, ""))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("sign out status = %d", rec.Code)
	}
	if cleared := sessionCookie(t, rec); cleared == nil || cleared.MaxAge >= 0 {
		t.Fatalf("sign out cookie = %+v, want an expired cookie", cleared)
	}
}

func TestConsoleSessionRefusals(t *testing.T) {
	hs, ah := newConsoleSessionService(t)
	good, _ := json.Marshal(map[string]string{"token": consoleTestToken(t, jwt.MapClaims{"sub": "ana"})})
	forged, _ := json.Marshal(map[string]string{"token": func() string {
		tok, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "ana"}).SignedString([]byte("wrong-secret"))
		return tok
	}()})

	for name, tc := range map[string]struct {
		req  func() *http.Request
		want int
	}{
		"forged token": {func() *http.Request { return consoleSessionRequest(http.MethodPost, string(forged)) }, http.StatusUnauthorized},
		"no subject": {func() *http.Request {
			body, _ := json.Marshal(map[string]string{"token": consoleTestToken(t, jwt.MapClaims{"email": "a@b"})})
			return consoleSessionRequest(http.MethodPost, string(body))
		}, http.StatusUnauthorized},
		"demo not enabled": {func() *http.Request { return consoleSessionRequest(http.MethodPost, `{"method":"demo"}`) }, http.StatusNotFound},
		"form body": {func() *http.Request {
			req := consoleSessionRequest(http.MethodPost, string(good))
			req.Header.Set("Content-Type", "text/plain")
			return req
		}, http.StatusUnsupportedMediaType},
		"other origin": {func() *http.Request {
			req := consoleSessionRequest(http.MethodPost, string(good))
			req.Header.Set("Origin", "https://evil.test")
			return req
		}, http.StatusForbidden},
		"cross-site fetch": {func() *http.Request {
			req := consoleSessionRequest(http.MethodDelete, "")
			req.Header.Set("Sec-Fetch-Site", "cross-site")
			return req
		}, http.StatusForbidden},
		"oversized token": {func() *http.Request {
			body, _ := json.Marshal(map[string]string{"token": strings.Repeat("a", consoleSessionMaxToken+1)})
			return consoleSessionRequest(http.MethodPost, string(body))
		}, http.StatusBadRequest},
	} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			hs.AuthSession(ah).ServeHTTP(rec, tc.req())
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
			if sessionCookie(t, rec) != nil {
				t.Fatal("a refused request must not set a session")
			}
		})
	}
}

func TestConsoleDemoSessionCarriesConfiguredClaims(t *testing.T) {
	hs, ah := newConsoleSessionService(t)
	s := hs.Load().(*graphjinService)
	s.conf.Core.Identity.RoleClaims = []string{"groups_role"}
	s.conf.Core.Identity.NamespaceClaim = "tenant"
	s.demoSignIn = &DemoSignIn{UserID: "demo-operator", Role: "admin", AccountID: "acct_demo"}

	rec := httptest.NewRecorder()
	hs.AuthSession(ah).ServeHTTP(rec, consoleSessionRequest(http.MethodPost, `{"method":"demo"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	ck := sessionCookie(t, rec)
	if ck == nil {
		t.Fatal("no session cookie set")
	}
	claims := jwt.MapClaims{}
	if _, err := jwt.ParseWithClaims(ck.Value, claims, func(*jwt.Token) (any, error) { return []byte(consoleTestSecret), nil }); err != nil {
		t.Fatal(err)
	}
	if claims["sub"] != "demo-operator" || claims["tenant"] != "acct_demo" {
		t.Fatalf("claims = %v, want sub and the configured namespace claim", claims)
	}
	if roles, _ := claims["groups_role"].([]any); len(roles) != 1 || roles[0] != "admin" {
		t.Fatalf("claims = %v, want the role under the configured role claim", claims)
	}
	if got := sessionUser(t, ah, ck, ""); got != "demo-operator" {
		t.Fatalf("demo session authenticates as %v", got)
	}

	// The demo identity is never offered by a server whose tokens it could
	// not sign.
	s.conf.Auth.JWT.Provider = "auth0"
	if s.demoSignInAvailable() {
		t.Fatal("demo sign-in must not be offered for an external JWT provider")
	}
}

func TestConsoleBootstrapOffersSignInThenSession(t *testing.T) {
	hs, ah := newConsoleSessionService(t)
	s := hs.Load().(*graphjinService)
	s.demoSignIn = &DemoSignIn{UserID: "demo-operator"}
	bootstrap := func(ck *http.Cookie) consoleBootstrapIdentity {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "http://console.test"+routeConsoleBootstrap, nil)
		if ck != nil {
			req.AddCookie(ck)
		}
		rec := httptest.NewRecorder()
		hs.ConsoleBootstrap(withConsoleSession(ah)).ServeHTTP(rec, req)
		return decodeConsoleBootstrap(t, rec).Identity
	}

	anon := bootstrap(nil)
	if anon.Authenticated || anon.SignIn == nil || strings.Join(anon.SignIn.Methods, ",") != "demo,token" {
		t.Fatalf("anonymous identity = %+v, want demo and token sign-in", anon)
	}
	if anon.Session {
		t.Fatal("no session yet")
	}

	ck := &http.Cookie{Name: consoleSessionCookie, Value: consoleTestToken(t, jwt.MapClaims{"sub": "ana", "name": "Ana Ruiz"})}
	signed := bootstrap(ck)
	if !signed.Authenticated || !signed.Session || signed.SignIn != nil || signed.DisplayName != "Ana Ruiz" {
		t.Fatalf("signed-in identity = %+v, want a named session and no sign-in offer", signed)
	}

	// Header-trusting development servers never need a session.
	s.conf.Auth.Development = true
	if s.consoleSignInMethods() != nil {
		t.Fatal("development auth must not offer console sign-in")
	}
}

func TestConsoleNextPathStaysOnServer(t *testing.T) {
	for in, want := range map[string]string{
		"":                      "/",
		"/user/agent?thread=1":  "/user/agent?thread=1",
		"https://evil.test/x":   "/",
		"//evil.test/x":         "/",
		`/\evil.test`:           "/",
		"user/agent":            "/",
		"/admin/runtime#events": "/admin/runtime",
	} {
		if got := consoleNextPath(in); got != want {
			t.Errorf("consoleNextPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// fakeOIDC is just enough of an identity provider for the authorization code
// flow: discovery, a token endpoint that returns a signed id_token, and JWKS.
func fakeOIDC(t *testing.T, subject, email string) *httptest.Server {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 srv.URL,
			"authorization_endpoint": srv.URL + "/authorize",
			"token_endpoint":         srv.URL + "/token",
			"jwks_uri":               srv.URL + "/jwks",
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
			"iss": srv.URL, "aud": "console-client", "sub": subject, "email": email,
			"name": "Ana Ruiz", "exp": time.Now().Add(time.Minute).Unix(),
		})
		tok.Header["kid"] = "k1"
		idToken, err := tok.SignedString(key)
		if err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer", "id_token": idToken})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": "k1", "alg": "RS256", "use": "sig",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestConsoleSingleSignOnSetsSessionAndReturns(t *testing.T) {
	idp := fakeOIDC(t, "u-123", "ana@example.com")
	conf := &Config{}
	conf.Auth.Type = "jwt"
	conf.Auth.JWT.Secret = consoleTestSecret
	conf.AuthLogin = AuthLogin{Enabled: true}
	conf.AuthLogin.OIDC.IssuerURL = idp.URL
	conf.AuthLogin.OIDC.ClientID = "console-client"
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	login, err := newAuthLoginService(ctx, conf)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	login.routes(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://console.test"+routeAuthConsoleLogin+"?next="+url.QueryEscape("/user/agent?thread=th_1"), nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("login status = %d: %s", rec.Code, rec.Body.String())
	}
	authorize, _ := url.Parse(rec.Header().Get("Location"))
	state := authorize.Query().Get("state")
	if !strings.HasPrefix(authorize.String(), idp.URL+"/authorize") || state == "" {
		t.Fatalf("login redirect = %s, want the IdP authorize URL with state", authorize)
	}
	if got := authorize.Query().Get("redirect_uri"); got != "http://console.test"+routeAuthCallback {
		t.Fatalf("redirect_uri = %q", got)
	}

	callback := "http://console.test" + routeAuthCallback + "?state=" + url.QueryEscape(state) + "&code=c1"
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, callback, nil))
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/user/agent?thread=th_1" {
		t.Fatalf("callback = %d to %q: %s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	ck := sessionCookie(t, rec)
	if ck == nil {
		t.Fatal("callback set no session cookie")
	}
	ah, err := auth.NewAuthHandlerFunc(conf.Auth)
	if err != nil {
		t.Fatal(err)
	}
	if got := sessionUser(t, ah, ck, ""); got != idp.URL+"#u-123" {
		t.Fatalf("session authenticates as %v, want the namespaced IdP subject", got)
	}

	// The state is single-use: a replayed callback starts nothing.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, callback, nil))
	if rec.Code == http.StatusFound || sessionCookie(t, rec) != nil {
		t.Fatalf("replayed callback = %d with a session, want a refusal", rec.Code)
	}
}

func TestConsoleSingleSignOnRefusalSetsNoSession(t *testing.T) {
	idp := fakeOIDC(t, "u-9", "mallory@other.test")
	conf := &Config{}
	conf.Auth.Type = "jwt"
	conf.Auth.JWT.Secret = consoleTestSecret
	conf.AuthLogin = AuthLogin{Enabled: true}
	conf.AuthLogin.OIDC.IssuerURL = idp.URL
	conf.AuthLogin.OIDC.ClientID = "console-client"
	conf.AuthLogin.OIDC.AllowedDomains = []string{"example.com"}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	login, err := newAuthLoginService(ctx, conf)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	login.routes(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://console.test"+routeAuthConsoleLogin, nil))
	authorize, _ := url.Parse(rec.Header().Get("Location"))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://console.test"+routeAuthCallback+"?code=c1&state="+url.QueryEscape(authorize.Query().Get("state")), nil))
	if rec.Code != http.StatusForbidden || sessionCookie(t, rec) != nil {
		t.Fatalf("refused sign-in = %d with cookie %v, want 403 and no session", rec.Code, sessionCookie(t, rec))
	}
	if !strings.Contains(rec.Body.String(), "Sign-in failed") {
		t.Fatalf("refusal body = %q, want the sign-in failed page", rec.Body.String())
	}
}
