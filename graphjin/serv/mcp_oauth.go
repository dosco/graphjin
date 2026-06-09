package serv

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dosco/graphjin/auth/v3"
	"github.com/dosco/graphjin/auth/v3/oidc"
)

const (
	routeMCPOAuthAuthorize = "/api/v1/oauth/authorize"
	routeMCPOAuthToken     = "/api/v1/oauth/token"
	routeMCPOAuthRegister  = "/api/v1/oauth/register"

	oauthProtectedResourceWellKnown   = "/.well-known/oauth-protected-resource"
	oauthAuthorizationServerWellKnown = "/.well-known/oauth-authorization-server"
	oidcConfigurationWellKnown        = "/.well-known/openid-configuration"

	mcpOAuthAuthCodeTTL = 5 * time.Minute
	mcpOAuthRequestTTL  = 10 * time.Minute
)

var mcpOAuthHTTPClient = &http.Client{Timeout: 5 * time.Second}

type mcpOAuthClient struct {
	ClientID                string   `json:"client_id"`
	ClientName              string   `json:"client_name,omitempty"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
	IssuedAt                time.Time
}

type mcpOAuthAuthRequest struct {
	ClientID            string
	RedirectURI         string
	ClientState         string
	Scope               string
	Resource            string
	CodeChallenge       string
	CodeChallengeMethod string
	CreatedAt           time.Time
	ExpiresAt           time.Time
}

type mcpOAuthCode struct {
	ClientID    string
	RedirectURI string
	Scope       string
	Resource    string
	Challenge   string
	Identity    *oidc.Identity
	ExpiresAt   time.Time
}

type mcpOAuthRefreshToken struct {
	ClientID  string
	Scope     string
	Resource  string
	Identity  *oidc.Identity
	ExpiresAt time.Time
}

func (c *Config) mcpOAuthEnabled() bool {
	return c != nil && c.MCP.OAuth.Enabled
}

func (c *Config) mcpOAuthMode() string {
	mode := strings.ToLower(strings.TrimSpace(c.MCP.OAuth.Mode))
	if mode == "" {
		return "external"
	}
	return mode
}

func validateMCPOAuthConfig(c *Config) error {
	if c == nil || !c.mcpOAuthEnabled() {
		return nil
	}
	switch c.mcpOAuthMode() {
	case "builtin":
		if !c.AuthLogin.Enabled {
			return errors.New("mcp.oauth: mode=builtin requires auth_login.enabled=true")
		}
	case "external":
		// ok
	default:
		return fmt.Errorf("mcp.oauth: unsupported mode %q (valid: builtin, external)", c.MCP.OAuth.Mode)
	}
	if !strings.EqualFold(strings.TrimSpace(c.Auth.Type), "jwt") {
		return errors.New("mcp.oauth: auth.type must be jwt so MCP bearer tokens can be audience-checked")
	}
	return nil
}

func (c *Config) mcpOAuthScopes() []string {
	if c == nil || len(c.MCP.OAuth.Scopes) == 0 {
		return []string{"mcp"}
	}
	out := make([]string, 0, len(c.MCP.OAuth.Scopes))
	for _, s := range c.MCP.OAuth.Scopes {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return []string{"mcp"}
	}
	return out
}

func (c *Config) mcpOAuthResource(r *http.Request) string {
	if c != nil && c.MCP.OAuth.Resource != "" {
		return strings.TrimRight(c.MCP.OAuth.Resource, "/")
	}
	return publicBaseURL(r) + routeMCP
}

func (c *Config) mcpOAuthIssuer(r *http.Request) string {
	if c != nil && c.MCP.OAuth.Issuer != "" {
		return strings.TrimRight(c.MCP.OAuth.Issuer, "/")
	}
	return publicBaseURL(r)
}

func (c *Config) mcpOAuthAuthorizationServers(r *http.Request) []string {
	if c == nil {
		return nil
	}
	if len(c.MCP.OAuth.AuthorizationServers) != 0 {
		out := make([]string, 0, len(c.MCP.OAuth.AuthorizationServers))
		for _, s := range c.MCP.OAuth.AuthorizationServers {
			if s = strings.TrimRight(strings.TrimSpace(s), "/"); s != "" {
				out = append(out, s)
			}
		}
		if len(out) != 0 {
			return out
		}
	}
	return []string{c.mcpOAuthIssuer(r)}
}

func (c *Config) mcpOAuthProtectedResourceMetadataURL(r *http.Request) string {
	return publicBaseURL(r) + oauthProtectedResourceWellKnown + routeMCP
}

func (s *graphjinService) registerMCPOAuthMetadataRoutes(mux Mux) {
	if s == nil || s.conf == nil || !s.conf.mcpOAuthEnabled() {
		return
	}
	mux.Handle(oauthProtectedResourceWellKnown, http.HandlerFunc(s.handleMCPOAuthProtectedResourceMetadata))
	mux.Handle(oauthProtectedResourceWellKnown+"/", http.HandlerFunc(s.handleMCPOAuthProtectedResourceMetadata))
	if s.conf.mcpOAuthMode() == "builtin" {
		mux.Handle(oauthAuthorizationServerWellKnown, http.HandlerFunc(s.handleMCPOAuthAuthorizationServerMetadata))
		mux.Handle(oidcConfigurationWellKnown, http.HandlerFunc(s.handleMCPOAuthAuthorizationServerMetadata))
	}
}

func (s *graphjinService) handleMCPOAuthProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	writeJSONStatus(w, http.StatusOK, map[string]any{
		"resource":                 s.conf.mcpOAuthResource(r),
		"authorization_servers":    s.conf.mcpOAuthAuthorizationServers(r),
		"scopes_supported":         s.conf.mcpOAuthScopes(),
		"bearer_methods_supported": []string{"header"},
		"resource_name":            "GraphJin MCP",
	})
}

func (s *graphjinService) handleMCPOAuthAuthorizationServerMetadata(w http.ResponseWriter, r *http.Request) {
	issuer := s.conf.mcpOAuthIssuer(r)
	body := map[string]any{
		"issuer":                                issuer,
		"authorization_endpoint":                issuer + routeMCPOAuthAuthorize,
		"token_endpoint":                        issuer + routeMCPOAuthToken,
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"scopes_supported":                      s.conf.mcpOAuthScopes(),
		"token_endpoint_auth_methods_supported": []string{"none"},
		"client_id_metadata_document_supported": s.conf.MCP.OAuth.ClientIDMetadataDocuments,
	}
	if s.conf.MCP.OAuth.DynamicClientRegistration {
		body["registration_endpoint"] = issuer + routeMCPOAuthRegister
	}
	writeJSONStatus(w, http.StatusOK, body)
}

func (s *graphjinService) newMCPAuthHandler() (auth.HandlerFunc, error) {
	if s == nil || s.conf == nil {
		return nil, errors.New("missing GraphJin service config")
	}
	ac := s.conf.Auth
	oauthOn := s.conf.mcpOAuthEnabled()
	if oauthOn && strings.EqualFold(ac.Type, "jwt") {
		ac.JWT.Audience = ""
	}
	base, err := auth.NewAuthHandlerFunc(ac)
	if err != nil {
		return nil, err
	}
	if !oauthOn {
		return base, nil
	}
	return func(w http.ResponseWriter, r *http.Request) (context.Context, error) {
		ctx, err := base(w, r)
		if err != nil {
			setMCPOAuthChallenge(w, r, s.conf)
			return ctx, auth.Err401
		}
		if !auth.IsAuth(ctx) {
			setMCPOAuthChallenge(w, r, s.conf)
			return ctx, auth.Err401
		}
		if !mcpOAuthAudienceMatches(auth.UserClaims(ctx), s.conf.mcpOAuthResource(r)) {
			setMCPOAuthChallenge(w, r, s.conf)
			return ctx, auth.Err401
		}
		return ctx, nil
	}, nil
}

func setMCPOAuthChallenge(w http.ResponseWriter, r *http.Request, conf *Config) {
	if conf == nil || !conf.mcpOAuthEnabled() {
		return
	}
	w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer resource_metadata="%s"`, conf.mcpOAuthProtectedResourceMetadataURL(r)))
}

func mcpOAuthAudienceMatches(claims map[string]interface{}, resource string) bool {
	if resource == "" || claims == nil {
		return false
	}
	switch aud := claims["aud"].(type) {
	case string:
		return aud == resource
	case []string:
		for _, a := range aud {
			if a == resource {
				return true
			}
		}
	case []interface{}:
		for _, v := range aud {
			if a, ok := v.(string); ok && a == resource {
				return true
			}
		}
	}
	return false
}

func (a *authLoginService) mcpOAuthEnabledBuiltin() bool {
	return a != nil && a.mcpOAuth.Enabled && strings.EqualFold(strings.TrimSpace(a.mcpOAuth.Mode), "builtin")
}

func (a *authLoginService) mcpOAuthResource(r *http.Request) string {
	if a != nil && a.mcpOAuth.Resource != "" {
		return strings.TrimRight(a.mcpOAuth.Resource, "/")
	}
	return publicBaseURL(r) + routeMCP
}

func (a *authLoginService) handleMCPOAuthRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req mcpOAuthClient
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "invalid registration JSON")
		return
	}
	if len(req.RedirectURIs) == 0 {
		writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", "redirect_uris is required")
		return
	}
	for _, ru := range req.RedirectURIs {
		if err := validateOAuthRedirectURI(ru); err != nil {
			writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", err.Error())
			return
		}
	}
	clientID, err := randomURLToken(24)
	if err != nil {
		http.Error(w, "failed to generate client id", http.StatusInternalServerError)
		return
	}
	clientID = "gj-dcr-" + clientID
	now := time.Now()
	req.ClientID = clientID
	req.IssuedAt = now
	if req.TokenEndpointAuthMethod == "" {
		req.TokenEndpointAuthMethod = "none"
	}
	a.mu.Lock()
	a.oauthClients[clientID] = &req
	a.mu.Unlock()

	writeJSONStatus(w, http.StatusCreated, map[string]any{
		"client_id":                  clientID,
		"client_id_issued_at":        now.Unix(),
		"client_name":                req.ClientName,
		"redirect_uris":              req.RedirectURIs,
		"grant_types":                firstNonEmptyStrings(req.GrantTypes, []string{"authorization_code", "refresh_token"}),
		"response_types":             firstNonEmptyStrings(req.ResponseTypes, []string{"code"}),
		"token_endpoint_auth_method": req.TokenEndpointAuthMethod,
	})
}

func (a *authLoginService) handleMCPOAuthAuthorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	if q.Get("response_type") != "code" {
		writeOAuthError(w, http.StatusBadRequest, "unsupported_response_type", "response_type must be code")
		return
	}
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")
	if clientID == "" || redirectURI == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "client_id and redirect_uri are required")
		return
	}
	client, err := a.resolveMCPOAuthClient(r.Context(), clientID)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client", err.Error())
		return
	}
	if !clientAllowsRedirect(client, redirectURI) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", "redirect_uri is not registered for this client")
		return
	}
	if err := validateOAuthRedirectURI(redirectURI); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", err.Error())
		return
	}
	challenge := q.Get("code_challenge")
	if challenge == "" || q.Get("code_challenge_method") != "S256" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "PKCE S256 code_challenge is required")
		return
	}
	scope, err := normalizeRequestedScopes(q.Get("scope"), a.mcpOAuthScopes())
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_scope", err.Error())
		return
	}
	resource := strings.TrimRight(q.Get("resource"), "/")
	if resource == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "resource is required")
		return
	}
	if resource != a.mcpOAuthResource(r) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_target", "resource does not match this GraphJin MCP endpoint")
		return
	}

	oidcState, err := randomURLToken(24)
	if err != nil {
		http.Error(w, "failed to generate state", http.StatusInternalServerError)
		return
	}
	now := time.Now()
	a.mu.Lock()
	a.oauthStates[oidcState] = &mcpOAuthAuthRequest{
		ClientID:            clientID,
		RedirectURI:         redirectURI,
		ClientState:         q.Get("state"),
		Scope:               scope,
		Resource:            resource,
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
		CreatedAt:           now,
		ExpiresAt:           now.Add(mcpOAuthRequestTTL),
	}
	a.mu.Unlock()

	redirectTo := a.provider.AuthCodeURL(oidcState, publicBaseURL(r)+routeAuthCallback)
	http.Redirect(w, r, redirectTo, http.StatusFound)
}

func (a *authLoginService) handleMCPOAuthCallback(w http.ResponseWriter, r *http.Request, state, providerCode string) bool {
	a.mu.Lock()
	req, ok := a.oauthStates[state]
	if !ok {
		a.mu.Unlock()
		return false
	}
	if time.Now().After(req.ExpiresAt) {
		delete(a.oauthStates, state)
		a.mu.Unlock()
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "authorization request expired")
		return true
	}
	a.mu.Unlock()

	redirectURI := publicBaseURL(r) + routeAuthCallback
	ident, err := a.provider.Exchange(r.Context(), providerCode, redirectURI)
	if err != nil {
		writeOAuthError(w, http.StatusForbidden, "access_denied", err.Error())
		return true
	}

	code, err := randomURLToken(32)
	if err != nil {
		http.Error(w, "failed to generate authorization code", http.StatusInternalServerError)
		return true
	}
	a.mu.Lock()
	delete(a.oauthStates, state)
	a.oauthCodes[code] = &mcpOAuthCode{
		ClientID:    req.ClientID,
		RedirectURI: req.RedirectURI,
		Scope:       req.Scope,
		Resource:    req.Resource,
		Challenge:   req.CodeChallenge,
		Identity:    ident,
		ExpiresAt:   time.Now().Add(mcpOAuthAuthCodeTTL),
	}
	a.mu.Unlock()

	u, _ := url.Parse(req.RedirectURI)
	v := u.Query()
	v.Set("code", code)
	if req.ClientState != "" {
		v.Set("state", req.ClientState)
	}
	u.RawQuery = v.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
	return true
}

func (a *authLoginService) handleMCPOAuthToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "invalid token request")
		return
	}
	switch r.FormValue("grant_type") {
	case "authorization_code":
		a.handleMCPOAuthAuthorizationCodeToken(w, r)
	case "refresh_token":
		a.handleMCPOAuthRefreshToken(w, r)
	default:
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "grant_type must be authorization_code or refresh_token")
	}
}

func (a *authLoginService) handleMCPOAuthAuthorizationCodeToken(w http.ResponseWriter, r *http.Request) {
	code := r.FormValue("code")
	clientID := r.FormValue("client_id")
	redirectURI := r.FormValue("redirect_uri")
	verifier := r.FormValue("code_verifier")
	resource := strings.TrimRight(r.FormValue("resource"), "/")
	if code == "" || clientID == "" || redirectURI == "" || verifier == "" || resource == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "code, client_id, redirect_uri, code_verifier, and resource are required")
		return
	}
	a.mu.Lock()
	authCode := a.oauthCodes[code]
	if authCode != nil {
		delete(a.oauthCodes, code)
	}
	a.mu.Unlock()
	if authCode == nil || time.Now().After(authCode.ExpiresAt) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "authorization code is invalid or expired")
		return
	}
	if authCode.ClientID != clientID || authCode.RedirectURI != redirectURI {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "authorization code was issued to a different client or redirect_uri")
		return
	}
	if authCode.Resource != resource {
		writeOAuthError(w, http.StatusBadRequest, "invalid_target", "resource does not match authorization request")
		return
	}
	if !pkceS256Matches(verifier, authCode.Challenge) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "code_verifier does not match code_challenge")
		return
	}
	a.writeMCPOAuthTokenResponse(w, authCode.ClientID, authCode.Scope, authCode.Resource, authCode.Identity)
}

func (a *authLoginService) handleMCPOAuthRefreshToken(w http.ResponseWriter, r *http.Request) {
	clientID := r.FormValue("client_id")
	refresh := r.FormValue("refresh_token")
	resource := strings.TrimRight(r.FormValue("resource"), "/")
	if clientID == "" || refresh == "" || resource == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "client_id, refresh_token, and resource are required")
		return
	}
	a.mu.Lock()
	rt := a.oauthRefreshTokens[refresh]
	if rt != nil && time.Now().After(rt.ExpiresAt) {
		delete(a.oauthRefreshTokens, refresh)
		rt = nil
	}
	a.mu.Unlock()
	if rt == nil || rt.ClientID != clientID {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "refresh_token is invalid or expired")
		return
	}
	if rt.Resource != resource {
		writeOAuthError(w, http.StatusBadRequest, "invalid_target", "resource does not match refresh token")
		return
	}
	a.writeMCPOAuthTokenResponse(w, rt.ClientID, rt.Scope, rt.Resource, rt.Identity)
}

func (a *authLoginService) writeMCPOAuthTokenResponse(w http.ResponseWriter, clientID, scope, resource string, ident *oidc.Identity) {
	if ident == nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "missing identity")
		return
	}
	ttl := a.mcpOAuth.AccessTokenTTL
	if ttl <= 0 {
		ttl = time.Hour
	}
	claims := map[string]any{
		"sub":   ident.NamespacedSubject(),
		"email": ident.Email,
		"name":  ident.Name,
		"aud":   resource,
		"scope": scope,
		"exp":   time.Now().Add(ttl).Unix(),
	}
	tok, err := a.issuer.Mint(claims)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to mint token")
		return
	}
	refreshTTL := a.mcpOAuth.RefreshTokenTTL
	if refreshTTL <= 0 {
		refreshTTL = 720 * time.Hour
	}
	refresh, err := randomURLToken(32)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to mint refresh token")
		return
	}
	a.mu.Lock()
	a.oauthRefreshTokens[refresh] = &mcpOAuthRefreshToken{
		ClientID:  clientID,
		Scope:     scope,
		Resource:  resource,
		Identity:  ident,
		ExpiresAt: time.Now().Add(refreshTTL),
	}
	a.mu.Unlock()
	writeJSONStatus(w, http.StatusOK, map[string]any{
		"access_token":  tok,
		"token_type":    "Bearer",
		"expires_in":    int(ttl.Seconds()),
		"refresh_token": refresh,
		"scope":         scope,
	})
}

func (a *authLoginService) resolveMCPOAuthClient(ctx context.Context, clientID string) (*mcpOAuthClient, error) {
	a.mu.Lock()
	client := a.oauthClients[clientID]
	a.mu.Unlock()
	if client != nil {
		return client, nil
	}
	if !a.mcpOAuth.ClientIDMetadataDocuments {
		return nil, errors.New("unknown client_id and CIMD is disabled")
	}
	client, err := fetchClientIDMetadataDocument(ctx, clientID)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.oauthClients[clientID] = client
	a.mu.Unlock()
	return client, nil
}

func fetchClientIDMetadataDocument(ctx context.Context, clientID string) (*mcpOAuthClient, error) {
	if err := validateCIMDURL(clientID); err != nil {
		return nil, err
	}
	u, err := url.Parse(clientID)
	if err != nil {
		return nil, err
	}
	if err := validateResolvedPublicHost(ctx, u.Hostname(), "client_id metadata document URL"); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, clientID, nil)
	if err != nil {
		return nil, err
	}
	resp, err := mcpOAuthHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch client metadata: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("client metadata returned HTTP %d", resp.StatusCode)
	}
	var meta mcpOAuthClient
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&meta); err != nil {
		return nil, fmt.Errorf("decode client metadata: %w", err)
	}
	if meta.ClientID != clientID {
		return nil, errors.New("client metadata client_id does not match document URL")
	}
	if len(meta.RedirectURIs) == 0 {
		return nil, errors.New("client metadata redirect_uris is required")
	}
	for _, ru := range meta.RedirectURIs {
		if err := validateOAuthRedirectURI(ru); err != nil {
			return nil, err
		}
	}
	meta.IssuedAt = time.Now()
	return &meta, nil
}

func validateCIMDURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Scheme != "https" {
		return errors.New("client_id metadata document URL must use https")
	}
	if u.Host == "" || u.Path == "" || u.Path == "/" {
		return errors.New("client_id metadata document URL must include host and path")
	}
	if u.User != nil {
		return errors.New("client_id metadata document URL must not include userinfo")
	}
	return validatePublicHost(u.Hostname(), "client_id metadata document URL")
}

func validateOAuthRedirectURI(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Host == "" {
		return errors.New("redirect URI must include a host")
	}
	if u.User != nil {
		return errors.New("redirect URI must not include userinfo")
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if isLoopbackHost(u.Hostname()) {
			return nil
		}
		return errors.New("http redirect URIs are allowed only for loopback localhost development")
	default:
		return errors.New("redirect URI must use https, or http for loopback localhost development")
	}
}

func validatePublicHost(host, label string) error {
	ip := net.ParseIP(host)
	if ip == nil {
		if strings.EqualFold(host, "localhost") {
			return fmt.Errorf("%s must not use localhost", label)
		}
		return nil
	}
	return validatePublicIP(ip, label)
}

func validateResolvedPublicHost(ctx context.Context, host, label string) error {
	if err := validatePublicHost(host, label); err != nil {
		return err
	}
	if net.ParseIP(host) != nil {
		return nil
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(lookupCtx, host)
	if err != nil {
		return fmt.Errorf("%s DNS lookup failed: %w", label, err)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("%s DNS lookup returned no addresses", label)
	}
	for _, addr := range addrs {
		if err := validatePublicIP(addr.IP, label); err != nil {
			return err
		}
	}
	return nil
}

func validatePublicIP(ip net.IP, label string) error {
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("%s must not point at a private or loopback address", label)
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func clientAllowsRedirect(client *mcpOAuthClient, redirectURI string) bool {
	if client == nil {
		return false
	}
	for _, ru := range client.RedirectURIs {
		if ru == redirectURI {
			return true
		}
	}
	return false
}

func normalizeRequestedScopes(raw string, supported []string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return strings.Join(supported, " "), nil
	}
	support := map[string]bool{}
	for _, s := range supported {
		support[s] = true
	}
	parts := strings.Fields(raw)
	for _, p := range parts {
		if !support[p] {
			return "", fmt.Errorf("unsupported scope %q", p)
		}
	}
	return strings.Join(parts, " "), nil
}

func (a *authLoginService) mcpOAuthScopes() []string {
	if len(a.mcpOAuth.Scopes) == 0 {
		return []string{"mcp"}
	}
	return a.mcpOAuth.Scopes
}

func pkceS256Matches(verifier, challenge string) bool {
	sum := sha256.Sum256([]byte(verifier))
	got := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(got), []byte(challenge)) == 1
}

func writeOAuthError(w http.ResponseWriter, status int, code, desc string) {
	writeJSONStatus(w, status, map[string]any{
		"error":             code,
		"error_description": desc,
	})
}

func firstNonEmptyStrings(v, fallback []string) []string {
	if len(v) != 0 {
		return v
	}
	return fallback
}
