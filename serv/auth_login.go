package serv

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"html/template"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dosco/graphjin/auth/v3/issuer"
	"github.com/dosco/graphjin/auth/v3/oidc"
)

const (
	routeAuthDevice      = "/api/v1/auth/device"
	routeAuthDeviceToken = "/api/v1/auth/device/token"
	routeAuthLogin       = "/api/v1/auth/login"
	routeAuthCallback    = "/api/v1/auth/callback"
	routeAuthWhoami      = "/api/v1/auth/whoami"

	deviceCodeTTL      = 10 * time.Minute
	deviceCodeInterval = 5 * time.Second
	deviceCodeGCEvery  = 1 * time.Minute
)

// deviceSessionStatus tracks where a device-code session is in the dance.
type deviceSessionStatus int

const (
	deviceStatusPending   deviceSessionStatus = iota // user has not entered user_code yet
	deviceStatusVerified                             // user_code entered; awaiting OIDC sign-in
	deviceStatusCompleted                            // id_token verified; local JWT minted
	deviceStatusRedeemed                             // CLI has picked up the token
	deviceStatusDenied                               // user rejected / allow-list blocked
)

// deviceSession is persisted as JSON by the shared auth store. The bearer
// secrets (device code, user code, OIDC state) are row keys there and are
// stored hashed, so they are excluded from the payload.
type deviceSession struct {
	DeviceCode  string `json:"-"`
	UserCode    string `json:"-"`
	CreatedAt   time.Time
	ExpiresAt   time.Time
	LastPollAt  time.Time
	Status      deviceSessionStatus
	Token       string `json:"-"` // pre-minted local JWT; minted at redemption when empty
	TokenExp    time.Time
	Identity    *oidc.Identity
	OIDCState   string `json:"-"` // opaque state tying OIDC callback back to this session
	DeniedError string
}

// authLoginService is built once at startup when AuthLogin.Enabled is true.
type authLoginService struct {
	cfg      AuthLogin
	mcpOAuth MCPOAuthConfig
	provider *oidc.Provider
	issuer   *issuer.Issuer

	// store holds login state shared across replicas. When nil the maps
	// below are used instead (single-process fallback).
	store authStateStore

	mu       sync.Mutex
	byDevice map[string]*deviceSession // keyed by device_code
	byUser   map[string]string         // user_code -> device_code
	byState  map[string]string         // oidc state -> device_code

	oauthClients       map[string]*mcpOAuthClient
	oauthStates        map[string]*mcpOAuthAuthRequest
	oauthCodes         map[string]*mcpOAuthCode
	oauthRefreshTokens map[string]*mcpOAuthRefreshToken
}

// newAuthLoginService builds a login service that keeps its state in process
// memory.
func newAuthLoginService(ctx context.Context, conf *Config) (*authLoginService, error) {
	return newAuthLoginServiceWithStore(ctx, conf, nil)
}

// newAuthLoginServiceWithStore validates config, builds the OIDC provider and
// JWT issuer, and starts the background GC. A nil store selects the
// in-memory fallback.
func newAuthLoginServiceWithStore(ctx context.Context, conf *Config, store authStateStore) (*authLoginService, error) {
	a := conf.AuthLogin
	if !a.Enabled {
		return nil, nil
	}
	if a.OIDC.IssuerURL == "" || a.OIDC.ClientID == "" {
		return nil, errors.New("auth_login: oidc.issuer_url and oidc.client_id are required when enabled")
	}
	switch strings.ToLower(conf.Auth.JWT.Provider) {
	case "", "generic", "other":
		// ok — we sign and verify with the same generic provider
	default:
		return nil, fmt.Errorf("auth_login: incompatible with auth.jwt.provider=%q (must be blank or 'generic' — external IdPs mint their own tokens)", conf.Auth.JWT.Provider)
	}
	if conf.Auth.JWT.Secret == "" && conf.Auth.JWT.PubKey == "" {
		return nil, errors.New("auth_login: auth.jwt.secret (or public_key + private key) required for signing local tokens")
	}

	ttl := a.TokenTTL
	if ttl <= 0 {
		ttl = 720 * time.Hour
	}

	// Resolve audience. `audience_graphjin: true` is a convenience shortcut
	// for the canonical "graphjin-cli" string and is mutually exclusive with
	// an explicit `audience:` value.
	audience := a.Audience
	if a.AudienceGraphjin {
		if audience != "" && audience != "graphjin-cli" {
			return nil, fmt.Errorf("auth_login: audience_graphjin=true conflicts with audience=%q", audience)
		}
		audience = "graphjin-cli"
	}
	if audience == "" {
		audience = firstNonEmpty(conf.Auth.JWT.Audience, "graphjin-cli")
	}

	iss, err := issuer.New(issuer.Config{
		Secret:   conf.Auth.JWT.Secret,
		Issuer:   firstNonEmpty(a.Issuer, conf.Auth.JWT.Issuer),
		Audience: audience,
		TTL:      ttl,
	})
	if err != nil {
		return nil, fmt.Errorf("auth_login: init issuer: %w", err)
	}

	prov, err := oidc.NewProvider(ctx, oidc.Config{
		IssuerURL:      a.OIDC.IssuerURL,
		ClientID:       a.OIDC.ClientID,
		ClientSecret:   a.OIDC.ClientSecret,
		Scopes:         a.OIDC.Scopes,
		AllowedEmails:  a.OIDC.AllowedEmails,
		AllowedDomains: a.OIDC.AllowedDomains,
	})
	if err != nil {
		return nil, fmt.Errorf("auth_login: oidc discovery: %w", err)
	}

	as := &authLoginService{
		cfg:                a,
		mcpOAuth:           conf.MCP.OAuth,
		provider:           prov,
		issuer:             iss,
		store:              store,
		byDevice:           map[string]*deviceSession{},
		byUser:             map[string]string{},
		byState:            map[string]string{},
		oauthClients:       map[string]*mcpOAuthClient{},
		oauthStates:        map[string]*mcpOAuthAuthRequest{},
		oauthCodes:         map[string]*mcpOAuthCode{},
		oauthRefreshTokens: map[string]*mcpOAuthRefreshToken{},
	}
	go as.gc(ctx)
	return as, nil
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// gc sweeps expired sessions. With a shared store every replica sweeps;
// the deletes are idempotent.
func (a *authLoginService) gc(ctx context.Context) {
	t := time.NewTicker(deviceCodeGCEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			_ = a.state().sweep(ctx, now)
		}
	}
}

// ---- handlers ----

func (a *authLoginService) routes(mux Mux) {
	mux.Handle(routeAuthDevice, http.HandlerFunc(a.handleDevice))
	mux.Handle(routeAuthDeviceToken, http.HandlerFunc(a.handleDeviceToken))
	mux.Handle(routeAuthLogin, http.HandlerFunc(a.handleLogin))
	mux.Handle(routeAuthCallback, http.HandlerFunc(a.handleCallback))
	mux.Handle(routeAuthConsoleLogin, http.HandlerFunc(a.handleConsoleLogin))
	if a.mcpOAuthEnabledBuiltin() {
		mux.Handle(routeMCPOAuthAuthorize, http.HandlerFunc(a.handleMCPOAuthAuthorize))
		mux.Handle(routeMCPOAuthToken, http.HandlerFunc(a.handleMCPOAuthToken))
		if a.mcpOAuth.DynamicClientRegistration {
			mux.Handle(routeMCPOAuthRegister, http.HandlerFunc(a.handleMCPOAuthRegister))
		}
	}
}

// handleDevice serves both the CLI-facing POST (start a device session) and
// the human-facing GET (form to enter user_code) / POST (submit form).
func (a *authLoginService) handleDevice(w http.ResponseWriter, r *http.Request) {
	ct := r.Header.Get("Content-Type")
	switch {
	case r.Method == http.MethodPost && strings.Contains(ct, "application/json"):
		a.handleDeviceStart(w, r)
	case r.Method == http.MethodPost:
		a.handleDeviceSubmit(w, r)
	case r.Method == http.MethodGet:
		a.handleDevicePage(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleDeviceStart: POST /api/v1/auth/device (JSON) — create session.
func (a *authLoginService) handleDeviceStart(w http.ResponseWriter, r *http.Request) {
	dc, err := randomURLToken(32)
	if err != nil {
		http.Error(w, "failed to generate device code", http.StatusInternalServerError)
		return
	}
	uc := randomUserCode()
	now := time.Now()
	session := &deviceSession{
		DeviceCode: dc,
		UserCode:   uc,
		CreatedAt:  now,
		ExpiresAt:  now.Add(deviceCodeTTL),
		Status:     deviceStatusPending,
	}
	if err := a.state().createDevice(r.Context(), session); err != nil {
		http.Error(w, "failed to store device session", http.StatusInternalServerError)
		return
	}

	base := publicBaseURL(r)
	writeJSONStatus(w, http.StatusOK, map[string]any{
		"device_code":               dc,
		"user_code":                 uc,
		"verification_uri":          base + routeAuthDevice,
		"verification_uri_complete": base + routeAuthDevice + "?user_code=" + uc,
		"expires_in":                int(deviceCodeTTL.Seconds()),
		"interval":                  int(deviceCodeInterval.Seconds()),
	})
}

// handleDevicePage: GET /api/v1/auth/device — HTML form.
func (a *authLoginService) handleDevicePage(w http.ResponseWriter, r *http.Request) {
	prefilled := r.URL.Query().Get("user_code")
	// If ?user_code= is provided and valid, skip the form entirely and kick
	// off OIDC directly.
	if prefilled != "" {
		if dc, ok := a.lookupUserCode(r.Context(), prefilled); ok {
			a.redirectToOIDC(w, r, dc)
			return
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = deviceFormTmpl.Execute(w, map[string]string{"Prefilled": prefilled, "Error": ""})
}

// handleDeviceSubmit: POST /api/v1/auth/device (form) — user typed a user_code.
func (a *authLoginService) handleDeviceSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	uc := strings.ToUpper(strings.TrimSpace(r.FormValue("user_code")))
	dc, ok := a.lookupUserCode(r.Context(), uc)
	if !ok {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_ = deviceFormTmpl.Execute(w, map[string]string{"Prefilled": uc, "Error": "Unknown or expired code. Please run `graphjin cli setup` again."})
		return
	}
	a.redirectToOIDC(w, r, dc)
}

func (a *authLoginService) redirectToOIDC(w http.ResponseWriter, r *http.Request, dc string) {
	state, err := randomURLToken(24)
	if err != nil {
		http.Error(w, "failed to generate state", http.StatusInternalServerError)
		return
	}
	if err := a.state().bindDeviceState(r.Context(), dc, state, time.Now()); err != nil {
		if errors.Is(err, errAuthSessionExpired) {
			http.Error(w, "session expired", http.StatusBadRequest)
			return
		}
		http.Error(w, "failed to update device session", http.StatusInternalServerError)
		return
	}

	redirectURI := publicBaseURL(r) + routeAuthCallback
	http.Redirect(w, r, a.provider.AuthCodeURL(state, redirectURI), http.StatusFound)
}

// handleLogin: GET /api/v1/auth/login?user_code=XXXX — alternative entry point
// (skip the form when the CLI gives the user a `verification_uri_complete`).
func (a *authLoginService) handleLogin(w http.ResponseWriter, r *http.Request) {
	uc := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("user_code")))
	if uc == "" {
		http.Redirect(w, r, routeAuthDevice, http.StatusFound)
		return
	}
	dc, ok := a.lookupUserCode(r.Context(), uc)
	if !ok {
		http.Error(w, "unknown or expired code", http.StatusBadRequest)
		return
	}
	a.redirectToOIDC(w, r, dc)
}

// handleCallback: GET /api/v1/auth/callback?state=...&code=... — exchange,
// mint local JWT, mark session completed.
func (a *authLoginService) handleCallback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	if state == "" || code == "" {
		http.Error(w, "missing state or code", http.StatusBadRequest)
		return
	}
	if a.handleMCPOAuthCallback(w, r, state, code) {
		return
	}
	dc, err := a.state().deviceByState(r.Context(), state, time.Now())
	switch {
	case errors.Is(err, errAuthSessionUnknown):
		http.Error(w, "unknown state", http.StatusBadRequest)
		return
	case errors.Is(err, errAuthSessionExpired):
		http.Error(w, "session expired", http.StatusBadRequest)
		return
	case err != nil:
		http.Error(w, "failed to load device session", http.StatusInternalServerError)
		return
	}

	redirectURI := publicBaseURL(r) + routeAuthCallback
	ident, err := a.provider.Exchange(r.Context(), code, redirectURI)
	if err != nil {
		_ = a.state().denyDevice(r.Context(), dc, state, err.Error())
		w.WriteHeader(http.StatusForbidden)
		_ = doneTmpl.Execute(w, map[string]string{"Title": "Sign-in failed", "Body": html.EscapeString(err.Error())})
		return
	}

	// The local JWT is minted when the CLI redeems the session, so no live
	// token ever sits in the shared store.
	if err := a.state().completeDevice(r.Context(), dc, state, ident); err != nil {
		if errors.Is(err, errAuthSessionUnknown) {
			http.Error(w, "session was restarted; finish sign-in in the newest tab", http.StatusBadRequest)
			return
		}
		http.Error(w, "failed to complete device session", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = doneTmpl.Execute(w, map[string]string{
		"Title": "Sign-in complete",
		"Body":  "You can close this tab and return to your terminal.",
	})
}

// handleConsoleLogin: GET /api/v1/auth/console?next=/path — browser sign-in
// for the console. The OIDC state is a single-use request in the shared store,
// so the callback can land on any replica.
func (a *authLoginService) handleConsoleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	state, err := randomURLToken(24)
	if err != nil {
		http.Error(w, "failed to generate state", http.StatusInternalServerError)
		return
	}
	now := time.Now()
	if err := a.state().putOAuthRequest(r.Context(), state, &mcpOAuthAuthRequest{
		ConsoleNext: consoleNextPath(r.URL.Query().Get("next")),
		CreatedAt:   now,
		ExpiresAt:   now.Add(mcpOAuthRequestTTL),
	}); err != nil {
		http.Error(w, "failed to store sign-in request", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, a.provider.AuthCodeURL(state, publicBaseURL(r)+routeAuthCallback), http.StatusFound)
}

// finishConsoleLogin mints the same claims a CLI sign-in gets, holds the token
// in the console session cookie, and returns the browser to the console.
func (a *authLoginService) finishConsoleLogin(w http.ResponseWriter, r *http.Request, ident *oidc.Identity, next string) {
	exp := time.Now().Add(consoleSessionTTL)
	if ttl := a.tokenTTL(); ttl < consoleSessionTTL {
		exp = time.Now().Add(ttl)
	}
	tok, err := a.issuer.Mint(map[string]any{
		"sub":   ident.NamespacedSubject(),
		"email": ident.Email,
		"name":  ident.Name,
		"exp":   exp.Unix(),
	})
	if err != nil {
		http.Error(w, "failed to mint session", http.StatusInternalServerError)
		return
	}
	setConsoleSessionCookie(w, r, tok, exp)
	http.Redirect(w, r, consoleNextPath(next), http.StatusFound)
}

// handleDeviceToken: POST /api/v1/auth/device/token — CLI polls for the token.
func (a *authLoginService) handleDeviceToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		DeviceCode string `json:"device_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.DeviceCode == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "invalid_request"})
		return
	}

	res, err := a.state().pollDevice(r.Context(), req.DeviceCode, time.Now())
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "server_error"})
		return
	}
	s := res.Session
	switch res.Outcome {
	case devicePollUnknown:
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "unknown_device_code"})
	case devicePollExpired:
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "expired_token"})
	case devicePollSlowDown:
		writeJSONStatus(w, http.StatusTooManyRequests, map[string]any{"error": "slow_down"})
	case devicePollPending:
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "authorization_pending"})
	case devicePollCompleted:
		tok, exp := s.Token, s.TokenExp
		if tok == "" {
			if s.Identity == nil {
				writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "server_error"})
				return
			}
			tok, err = a.issuer.Mint(map[string]any{
				"sub":   s.Identity.NamespacedSubject(),
				"email": s.Identity.Email,
				"name":  s.Identity.Name,
			})
			if err != nil {
				writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "server_error", "error_description": "failed to mint token"})
				return
			}
			exp = time.Now().Add(a.tokenTTL())
		}
		email := ""
		iss := ""
		if s.Identity != nil {
			email = s.Identity.Email
			iss = s.Identity.Issuer
		}
		writeJSONStatus(w, http.StatusOK, map[string]any{
			"token":      tok,
			"expires_at": exp.Unix(),
			"email":      email,
			"issuer":     iss,
		})
	case devicePollRedeemed:
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "access_denied", "error_description": "token already redeemed"})
	case devicePollDenied:
		writeJSONStatus(w, http.StatusForbidden, map[string]any{"error": "access_denied", "error_description": s.DeniedError})
	default:
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "server_error"})
	}
}

func (a *authLoginService) lookupUserCode(ctx context.Context, uc string) (string, bool) {
	dc, ok, err := a.state().lookupUserCode(ctx, uc, time.Now())
	if err != nil {
		return "", false
	}
	return dc, ok
}

func (a *authLoginService) tokenTTL() time.Duration {
	if a.cfg.TokenTTL > 0 {
		return a.cfg.TokenTTL
	}
	return 720 * time.Hour
}

// ---- utilities ----

func writeJSONStatus(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func randomURLToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// randomUserCode returns a short, human-typable code like "ABCD-1234".
func randomUserCode() string {
	const alphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789" // no 0/O/1/I confusions
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	out := make([]byte, 9)
	for i, c := range b {
		idx := int(c) % len(alphabet)
		if i == 4 {
			out[i] = '-'
			out[i+1] = alphabet[idx]
			continue
		}
		if i < 4 {
			out[i] = alphabet[idx]
		} else {
			out[i+1] = alphabet[idx]
		}
	}
	return string(out)
}

// publicBaseURL reconstructs the externally-visible origin (scheme://host) of
// the incoming request, honoring X-Forwarded-Proto/Host when present.
func publicBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		scheme = p
	}
	host := r.Host
	if h := r.Header.Get("X-Forwarded-Host"); h != "" {
		host = h
	}
	return scheme + "://" + host
}

// randomHex is used by tests to seed the rng deterministically.
var _ = hex.EncodeToString

// ---- templates ----

var deviceFormTmpl = template.Must(template.New("device").Parse(`<!doctype html>
<html>
<head>
<meta charset="utf-8">
<title>GraphJin sign-in</title>
<style>
body { font: 14px -apple-system, sans-serif; max-width: 420px; margin: 10vh auto; padding: 24px; }
input[type=text] { font-size: 24px; letter-spacing: 2px; padding: 8px; width: 100%; text-align: center; text-transform: uppercase; }
button { margin-top: 16px; padding: 10px 16px; font-size: 16px; }
.err { color: #b00; margin-bottom: 12px; }
</style>
</head>
<body>
<h1>GraphJin CLI sign-in</h1>
{{if .Error}}<p class="err">{{.Error}}</p>{{end}}
<p>Enter the code shown in your terminal:</p>
<form method="post">
<input name="user_code" type="text" autocomplete="off" autofocus value="{{.Prefilled}}" placeholder="ABCD-EFGH" />
<button type="submit">Continue</button>
</form>
</body>
</html>`))

var doneTmpl = template.Must(template.New("done").Parse(`<!doctype html>
<html>
<head><meta charset="utf-8"><title>{{.Title}}</title>
<style>body{font:14px -apple-system,sans-serif;max-width:480px;margin:10vh auto;padding:24px;}</style>
</head>
<body><h1>{{.Title}}</h1><p>{{.Body}}</p></body>
</html>`))
