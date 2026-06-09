package serv

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/dosco/graphjin/auth/v3"
	"github.com/dosco/graphjin/auth/v3/issuer"
)

func TestMCPOAuthProtectedResourceMetadata(t *testing.T) {
	s := &graphjinService{conf: &Config{Serv: Serv{MCP: MCPConfig{OAuth: MCPOAuthConfig{
		Enabled:              true,
		Mode:                 "external",
		Scopes:               []string{"mcp", "catalog.read"},
		AuthorizationServers: []string{"https://auth.example.com"},
	}}}}}
	req := httptest.NewRequest(http.MethodGet, "https://graphjin.example.com/.well-known/oauth-protected-resource/api/v1/mcp", nil)
	rec := httptest.NewRecorder()

	s.handleMCPOAuthProtectedResourceMetadata(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode metadata: %s", err)
	}
	if body["resource"] != "https://graphjin.example.com/api/v1/mcp" {
		t.Fatalf("resource = %v", body["resource"])
	}
	authServers, _ := body["authorization_servers"].([]interface{})
	if len(authServers) != 1 || authServers[0] != "https://auth.example.com" {
		t.Fatalf("authorization_servers = %#v", body["authorization_servers"])
	}
}

func TestMCPOAuthAuthorizationServerMetadataIncludesDCRCIMD(t *testing.T) {
	s := &graphjinService{conf: &Config{Serv: Serv{MCP: MCPConfig{OAuth: MCPOAuthConfig{
		Enabled:                   true,
		Mode:                      "builtin",
		Scopes:                    []string{"mcp"},
		DynamicClientRegistration: true,
		ClientIDMetadataDocuments: true,
	}}}}}
	req := httptest.NewRequest(http.MethodGet, "https://graphjin.example.com/.well-known/oauth-authorization-server", nil)
	rec := httptest.NewRecorder()

	s.handleMCPOAuthAuthorizationServerMetadata(rec, req)

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode metadata: %s", err)
	}
	if body["registration_endpoint"] != "https://graphjin.example.com/api/v1/oauth/register" {
		t.Fatalf("registration_endpoint = %v", body["registration_endpoint"])
	}
	if body["client_id_metadata_document_supported"] != true {
		t.Fatalf("client_id_metadata_document_supported = %v", body["client_id_metadata_document_supported"])
	}
}

func TestNewMCPAuthHandlerAllowsResourceAudience(t *testing.T) {
	resource := "https://graphjin.example.com/api/v1/mcp"
	s := &graphjinService{conf: &Config{Serv: Serv{
		Auth: Auth{
			Type: "jwt",
			JWT: JWTConfig{
				Secret:   "secret",
				Issuer:   "https://graphjin.example.com",
				Audience: "graphjin-cli",
			},
		},
		MCP: MCPConfig{OAuth: MCPOAuthConfig{Enabled: true, Resource: resource}},
	}}}
	h, err := s.newMCPAuthHandler()
	if err != nil {
		t.Fatalf("newMCPAuthHandler: %s", err)
	}
	iss, err := issuer.New(issuer.Config{Secret: "secret", Issuer: "https://graphjin.example.com", Audience: "graphjin-cli"})
	if err != nil {
		t.Fatalf("issuer: %s", err)
	}
	tok, err := iss.Mint(map[string]interface{}{"sub": "user-1", "aud": resource})
	if err != nil {
		t.Fatalf("mint: %s", err)
	}
	req := httptest.NewRequest(http.MethodPost, resource, nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	ctx, err := h(httptest.NewRecorder(), req)
	if err != nil {
		t.Fatalf("auth failed: %s", err)
	}
	if !auth.IsAuth(ctx) {
		t.Fatal("expected authenticated context")
	}
}

func TestNewMCPAuthHandlerRejectsWrongAudienceWithChallenge(t *testing.T) {
	resource := "https://graphjin.example.com/api/v1/mcp"
	s := &graphjinService{conf: &Config{Serv: Serv{
		Auth: Auth{
			Type: "jwt",
			JWT: JWTConfig{
				Secret:   "secret",
				Issuer:   "https://graphjin.example.com",
				Audience: "graphjin-cli",
			},
		},
		MCP: MCPConfig{OAuth: MCPOAuthConfig{Enabled: true, Resource: resource}},
	}}}
	h, err := s.newMCPAuthHandler()
	if err != nil {
		t.Fatalf("newMCPAuthHandler: %s", err)
	}
	iss, err := issuer.New(issuer.Config{Secret: "secret", Issuer: "https://graphjin.example.com"})
	if err != nil {
		t.Fatalf("issuer: %s", err)
	}
	tok, err := iss.Mint(map[string]interface{}{"sub": "user-1", "aud": "graphjin-cli"})
	if err != nil {
		t.Fatalf("mint: %s", err)
	}
	req := httptest.NewRequest(http.MethodPost, resource, nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	_, err = h(rec, req)
	if err != auth.Err401 {
		t.Fatalf("err = %v, want auth.Err401", err)
	}
	challenge := rec.Header().Get("WWW-Authenticate")
	if !strings.Contains(challenge, `resource_metadata="https://graphjin.example.com/.well-known/oauth-protected-resource/api/v1/mcp"`) {
		t.Fatalf("challenge = %q", challenge)
	}
}

func TestMCPOAuthRedirectAndCIMDValidation(t *testing.T) {
	if err := validateOAuthRedirectURI("http://127.0.0.1:3111/callback"); err != nil {
		t.Fatalf("loopback http redirect should be allowed: %s", err)
	}
	if err := validateOAuthRedirectURI("http://10.0.0.2/callback"); err == nil {
		t.Fatal("private-network http redirect should be rejected")
	}
	if err := validateCIMDURL("https://127.0.0.1/client.json"); err == nil {
		t.Fatal("private-network CIMD metadata URL should be rejected")
	}
	if err := validateCIMDURL("http://client.example.com/client.json"); err == nil {
		t.Fatal("non-https CIMD metadata URL should be rejected")
	}
	if err := validateResolvedPublicHost(context.Background(), "localhost", "client_id metadata document URL"); err == nil {
		t.Fatal("resolved localhost CIMD metadata host should be rejected")
	}
}

func TestMCPOAuthAuthorizeRequiresMatchingResource(t *testing.T) {
	a := &authLoginService{
		mcpOAuth: MCPOAuthConfig{Enabled: true, Mode: "builtin", Scopes: []string{"mcp"}},
		oauthClients: map[string]*mcpOAuthClient{"client-1": {
			ClientID:     "client-1",
			RedirectURIs: []string{"http://127.0.0.1/callback"},
		}},
		oauthStates: map[string]*mcpOAuthAuthRequest{},
	}

	req := httptest.NewRequest(http.MethodGet, "https://graphjin.example.com/api/v1/oauth/authorize?response_type=code&client_id=client-1&redirect_uri=http://127.0.0.1/callback&code_challenge=abc&code_challenge_method=S256", nil)
	rec := httptest.NewRecorder()
	a.handleMCPOAuthAuthorize(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "resource is required") {
		t.Fatalf("missing resource status/body = %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "https://graphjin.example.com/api/v1/oauth/authorize?response_type=code&client_id=client-1&redirect_uri=http://127.0.0.1/callback&code_challenge=abc&code_challenge_method=S256&resource=https://other.example.com/api/v1/mcp", nil)
	rec = httptest.NewRecorder()
	a.handleMCPOAuthAuthorize(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_target") {
		t.Fatalf("wrong resource status/body = %d %s", rec.Code, rec.Body.String())
	}
}

func TestMCPOAuthTokenRequiresMatchingResource(t *testing.T) {
	resource := "https://graphjin.example.com/api/v1/mcp"
	a := &authLoginService{
		oauthCodes: map[string]*mcpOAuthCode{
			"code-1": {
				ClientID:    "client-1",
				RedirectURI: "http://127.0.0.1/callback",
				Scope:       "mcp",
				Resource:    resource,
				Challenge:   "challenge",
				ExpiresAt:   time.Now().Add(time.Minute),
			},
		},
		oauthRefreshTokens: map[string]*mcpOAuthRefreshToken{
			"refresh-1": {
				ClientID:  "client-1",
				Scope:     "mcp",
				Resource:  resource,
				ExpiresAt: time.Now().Add(time.Minute),
			},
		},
	}

	form := url.Values{
		"code":          {"code-1"},
		"client_id":     {"client-1"},
		"redirect_uri":  {"http://127.0.0.1/callback"},
		"code_verifier": {"verifier"},
		"resource":      {"https://other.example.com/api/v1/mcp"},
	}
	req := httptest.NewRequest(http.MethodPost, routeMCPOAuthToken, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	a.handleMCPOAuthAuthorizationCodeToken(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_target") {
		t.Fatalf("wrong auth-code resource status/body = %d %s", rec.Code, rec.Body.String())
	}

	form = url.Values{
		"client_id":     {"client-1"},
		"refresh_token": {"refresh-1"},
		"resource":      {"https://other.example.com/api/v1/mcp"},
	}
	req = httptest.NewRequest(http.MethodPost, routeMCPOAuthToken, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	a.handleMCPOAuthRefreshToken(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_target") {
		t.Fatalf("wrong refresh resource status/body = %d %s", rec.Code, rec.Body.String())
	}
}

func TestValidateMCPOAuthConfig(t *testing.T) {
	err := validateMCPOAuthConfig(&Config{Serv: Serv{MCP: MCPConfig{OAuth: MCPOAuthConfig{
		Enabled: true,
		Mode:    "builtin",
	}}}})
	if err == nil || !strings.Contains(err.Error(), "auth_login.enabled=true") {
		t.Fatalf("expected builtin auth_login requirement, got %v", err)
	}

	err = validateMCPOAuthConfig(&Config{Serv: Serv{Auth: Auth{Type: "jwt"}, AuthLogin: AuthLogin{Enabled: true}, MCP: MCPConfig{OAuth: MCPOAuthConfig{
		Enabled: true,
		Mode:    "builtin",
	}}}})
	if err != nil {
		t.Fatalf("builtin with auth_login should validate: %s", err)
	}

	err = validateMCPOAuthConfig(&Config{Serv: Serv{MCP: MCPConfig{OAuth: MCPOAuthConfig{
		Enabled: true,
		Mode:    "external",
	}}}})
	if err == nil || !strings.Contains(err.Error(), "auth.type must be jwt") {
		t.Fatalf("expected jwt auth requirement, got %v", err)
	}

	err = validateMCPOAuthConfig(&Config{Serv: Serv{MCP: MCPConfig{OAuth: MCPOAuthConfig{
		Enabled: true,
		Mode:    "weird",
	}}}})
	if err == nil || !strings.Contains(err.Error(), "unsupported mode") {
		t.Fatalf("expected unsupported mode error, got %v", err)
	}
}
