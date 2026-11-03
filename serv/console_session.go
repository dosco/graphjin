package serv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dosco/graphjin/auth/v3"
	"github.com/dosco/graphjin/auth/v3/issuer"
	"github.com/dosco/graphjin/core/v3"
	jwt "github.com/golang-jwt/jwt/v5"
)

// Console sessions let the browser console sign in to a server that only
// trusts JWTs. The session is the JWT itself, held in an HttpOnly,
// SameSite=Strict cookie: page scripts never read it, other sites' requests
// never carry it, and every replica verifies it exactly like a bearer token,
// so no session state is stored anywhere.

const (
	routeAuthSession      = "/api/v1/auth/session"
	routeAuthConsoleLogin = "/api/v1/auth/console"

	consoleSessionCookie = "gj_console_session"
	consoleSessionPath   = "/api/v1/"
	consoleSessionTTL    = 12 * time.Hour
	// Browsers drop cookies over 4KB, so a larger token would sign in and
	// then silently be gone on the next request.
	consoleSessionMaxToken = 3800
)

// DemoSignIn is the identity `graphjin serve --demo` lets the console sign in
// as without credentials.
type DemoSignIn struct {
	UserID    string
	Role      string
	AccountID string
}

// OptionSetDemoSignIn offers a one-click console sign-in as identity. It is for
// local demos only: anyone who can reach the server can take that identity.
func OptionSetDemoSignIn(identity DemoSignIn) Option {
	return func(s *graphjinService) error {
		if strings.TrimSpace(identity.UserID) == "" {
			return errors.New("demo sign-in requires a user id")
		}
		s.demoSignIn = &identity
		return nil
	}
}

// consoleSessionsEnabled reports whether this server needs console sessions:
// it verifies JWTs and ignores the development identity headers.
func consoleSessionsEnabled(conf *Config) bool {
	return conf != nil && strings.EqualFold(conf.Auth.Type, "jwt") && !conf.Auth.Development
}

// withConsoleSession lets a console session cookie stand in for a missing
// bearer token. An explicit Authorization header always wins.
func withConsoleSession(ah auth.HandlerFunc) auth.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) (context.Context, error) {
		if r.Header.Get("Authorization") == "" {
			if ck, err := r.Cookie(consoleSessionCookie); err == nil && ck.Value != "" {
				r = r.Clone(r.Context())
				r.Header.Set("Authorization", "Bearer "+ck.Value)
			}
		}
		return ah(w, r)
	}
}

// usesConsoleSession reports whether r was authenticated by the session
// cookie rather than a bearer token the caller sent itself.
func usesConsoleSession(r *http.Request) bool {
	if r.Header.Get("Authorization") != "" {
		return false
	}
	ck, err := r.Cookie(consoleSessionCookie)
	return err == nil && ck.Value != ""
}

// consoleSignIn tells the console how it can start a session.
type consoleSignIn struct {
	Methods  []string `json:"methods"`
	LoginURL string   `json:"login_url,omitempty"`
}

func (s *graphjinService) consoleSignInMethods() *consoleSignIn {
	if !consoleSessionsEnabled(s.conf) {
		return nil
	}
	out := &consoleSignIn{}
	if s.authLogin != nil {
		out.Methods = append(out.Methods, "oidc")
		out.LoginURL = routeAuthConsoleLogin
	}
	if s.demoSignInAvailable() {
		out.Methods = append(out.Methods, "demo")
	}
	out.Methods = append(out.Methods, "token")
	return out
}

func (s *graphjinService) demoSignInAvailable() bool {
	if s.demoSignIn == nil || s.conf.Auth.JWT.Secret == "" {
		return false
	}
	switch strings.ToLower(s.conf.Auth.JWT.Provider) {
	case "", "generic", "other":
		return true
	}
	return false
}

// AuthSession serves POST (start a session from a token or the demo
// identity) and DELETE (sign out) for the console.
func (s1 *HttpService) AuthSession(ah auth.HandlerFunc) http.Handler {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := s1.Load().(*graphjinService)
		w.Header().Set("Cache-Control", "no-store")
		if !consoleSameOrigin(r) {
			writeJSONStatus(w, http.StatusForbidden, map[string]any{"error": "cross-origin session requests are refused"})
			return
		}
		switch r.Method {
		case http.MethodPost:
			s.startConsoleSession(w, r, ah)
		case http.MethodDelete:
			clearConsoleSessionCookie(w, r)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.Header().Set("Allow", "POST, DELETE")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	return apiV1Handler(s1, nil, h, nil)
}

func (s *graphjinService) startConsoleSession(w http.ResponseWriter, r *http.Request, ah auth.HandlerFunc) {
	// A JSON body cannot be sent cross-origin without a preflight, which is
	// what keeps another site from signing a visitor in as someone else.
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		writeJSONStatus(w, http.StatusUnsupportedMediaType, map[string]any{"error": "send a JSON body"})
		return
	}
	var req struct {
		Token  string `json:"token"`
		Method string `json:"method"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON body"})
		return
	}

	var token string
	var exp time.Time
	switch {
	case req.Method == "demo":
		if !s.demoSignInAvailable() {
			writeJSONStatus(w, http.StatusNotFound, map[string]any{"error": "demo sign-in is not enabled on this server"})
			return
		}
		var err error
		if token, exp, err = s.mintDemoSession(); err != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
	case strings.TrimSpace(req.Token) != "":
		token = strings.TrimPrefix(strings.TrimSpace(req.Token), "Bearer ")
		if len(token) > consoleSessionMaxToken {
			writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": fmt.Sprintf("token is %d bytes; a browser session holds at most %d", len(token), consoleSessionMaxToken)})
			return
		}
	default:
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": `send {"token": "<jwt>"} or {"method": "demo"}`})
		return
	}

	// Verify with the same handler every API request uses, so a session can
	// only ever hold a token the API would have accepted as a bearer.
	probe := r.Clone(r.Context())
	probe.Header.Del("Cookie")
	probe.Header.Set("Authorization", "Bearer "+token)
	ctx, err := ah(w, probe)
	if err != nil || ctx == nil {
		msg := "token was not accepted"
		if err != nil {
			msg += ": " + err.Error()
		}
		writeJSONStatus(w, http.StatusUnauthorized, map[string]any{"error": msg})
		return
	}
	ctx = s.applyIdentityContext(ctx)
	userID := ctx.Value(core.UserIDKey)
	if userID == nil {
		writeJSONStatus(w, http.StatusUnauthorized, map[string]any{"error": "token has no user id claim"})
		return
	}
	if exp.IsZero() {
		exp = consoleSessionExpiry(auth.UserClaims(ctx), time.Now())
	}
	setConsoleSessionCookie(w, r, token, exp)
	writeJSONStatus(w, http.StatusOK, map[string]any{"user_id": fmt.Sprint(userID), "expires_at": exp.Unix()})
}

// consoleSessionExpiry ends the session when the token does, and never later
// than consoleSessionTTL from now.
func consoleSessionExpiry(claims map[string]any, now time.Time) time.Time {
	limit := now.Add(consoleSessionTTL)
	var exp time.Time
	switch v := claims["exp"].(type) {
	case float64:
		exp = time.Unix(int64(v), 0)
	case json.Number:
		if n, err := v.Int64(); err == nil {
			exp = time.Unix(n, 0)
		}
	}
	if exp.IsZero() || exp.After(limit) {
		return limit
	}
	return exp
}

// mintDemoSession signs a session for the demo identity with the server's own
// JWT secret, using the claim names this server reads identity from.
func (s *graphjinService) mintDemoSession() (string, time.Time, error) {
	d := s.demoSignIn
	iss, err := issuer.New(issuer.Config{
		Secret:   s.conf.Auth.JWT.Secret,
		Issuer:   s.conf.Auth.JWT.Issuer,
		Audience: s.conf.Auth.JWT.Audience,
	})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("demo sign-in: %w", err)
	}
	id := s.conf.Core.EffectiveIdentityConfig()
	exp := time.Now().Add(consoleSessionTTL)
	claims := jwt.MapClaims{"sub": d.UserID, "exp": exp.Unix()}
	if id.UserIDClaim != "" {
		claims[id.UserIDClaim] = d.UserID
	}
	if role := strings.TrimSpace(d.Role); role != "" {
		roleClaim := "roles"
		if len(id.RoleClaims) != 0 {
			roleClaim = id.RoleClaims[0]
		}
		claims[roleClaim] = []string{role}
	}
	if d.AccountID != "" && id.NamespaceClaim != "" {
		claims[id.NamespaceClaim] = d.AccountID
	}
	tok, err := iss.Mint(claims)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("demo sign-in: %w", err)
	}
	return tok, exp, nil
}

func setConsoleSessionCookie(w http.ResponseWriter, r *http.Request, token string, exp time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     consoleSessionCookie,
		Value:    token,
		Path:     consoleSessionPath,
		Expires:  exp,
		MaxAge:   int(time.Until(exp).Seconds()),
		HttpOnly: true,
		Secure:   strings.HasPrefix(publicBaseURL(r), "https://"),
		SameSite: http.SameSiteStrictMode,
	})
}

func clearConsoleSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     consoleSessionCookie,
		Path:     consoleSessionPath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   strings.HasPrefix(publicBaseURL(r), "https://"),
		SameSite: http.SameSiteStrictMode,
	})
}

// consoleSameOrigin refuses requests a browser marks as coming from another
// site. Clients that are not browsers send neither header and are allowed:
// they hold the token already.
func consoleSameOrigin(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != publicBaseURL(r) {
		return false
	}
	return true
}

// consoleNextPath keeps a post-login redirect on this server.
func consoleNextPath(next string) string {
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.Contains(next, `\`) {
		return "/"
	}
	u, err := url.Parse(next)
	if err != nil || u.Scheme != "" || u.Host != "" {
		return "/"
	}
	return u.RequestURI()
}
