// Package oidc provides a minimal OpenID Connect client used by GraphJin's
// built-in login flow. It performs OIDC discovery, exchanges an authorization
// code for tokens via golang.org/x/oauth2, and verifies the ID token using the
// JWKS from the discovery document.
//
// It intentionally does not depend on github.com/coreos/go-oidc to keep the
// module's transitive dependency set small — JWKS verification reuses the
// lestrrat-go/jwx library already pulled in by auth/provider.
package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/lestrrat-go/jwx/jwk"
	"golang.org/x/oauth2"
)

// Config describes a single OIDC identity provider. It works for any
// OIDC-compliant issuer (Google, Okta, Keycloak, Azure AD, Auth0-as-IdP, ...).
type Config struct {
	// IssuerURL is the OIDC issuer — discovery happens at
	// <IssuerURL>/.well-known/openid-configuration.
	IssuerURL string

	// ClientID / ClientSecret are the registered OAuth client credentials.
	ClientID     string
	ClientSecret string

	// RedirectURI registered with the IdP. If empty, the caller is expected to
	// override it per-request (useful when the server listens on multiple
	// hostnames).
	RedirectURI string

	// Scopes requested. Defaults to ["openid", "email", "profile"] when empty.
	Scopes []string

	// AllowedEmails / AllowedDomains are optional allow-lists applied after a
	// successful OIDC sign-in. If both are empty, any verified identity is
	// accepted.
	AllowedEmails  []string
	AllowedDomains []string
}

// discovery is the subset of the OIDC discovery document we use.
type discovery struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
	UserInfoEndpoint      string `json:"userinfo_endpoint"`
}

// Provider is a configured OIDC client.
type Provider struct {
	cfg   Config
	disc  discovery
	oauth *oauth2.Config
	keys  *jwk.AutoRefresh
}

// Identity is the minimum user info extracted from a verified ID token.
type Identity struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	Issuer        string
	Raw           jwt.MapClaims
}

// NamespacedSubject returns a stable identifier combining issuer and subject,
// safe to use as the `sub` claim of a locally-minted JWT when multiple IdPs
// share the same subject namespace.
func (i Identity) NamespacedSubject() string {
	return i.Issuer + "#" + i.Subject
}

// NewProvider fetches the OIDC discovery document and returns a ready-to-use
// Provider.
func NewProvider(ctx context.Context, cfg Config) (*Provider, error) {
	if cfg.IssuerURL == "" {
		return nil, errors.New("oidc: IssuerURL is required")
	}
	if cfg.ClientID == "" {
		return nil, errors.New("oidc: ClientID is required")
	}

	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "email", "profile"}
	}

	disc, err := fetchDiscovery(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, err
	}

	oc := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURI,
		Scopes:       scopes,
		Endpoint: oauth2.Endpoint{
			AuthURL:  disc.AuthorizationEndpoint,
			TokenURL: disc.TokenEndpoint,
		},
	}

	ar := jwk.NewAutoRefresh(context.Background())
	ar.Configure(disc.JWKSURI, jwk.WithMinRefreshInterval(15*time.Minute))

	return &Provider{cfg: cfg, disc: disc, oauth: oc, keys: ar}, nil
}

func fetchDiscovery(ctx context.Context, issuerURL string) (discovery, error) {
	url := strings.TrimRight(issuerURL, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return discovery{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return discovery{}, fmt.Errorf("oidc discovery: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return discovery{}, fmt.Errorf("oidc discovery: %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	var d discovery
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return discovery{}, fmt.Errorf("oidc discovery decode: %w", err)
	}
	if d.AuthorizationEndpoint == "" || d.TokenEndpoint == "" || d.JWKSURI == "" {
		return discovery{}, errors.New("oidc discovery: missing endpoints")
	}
	return d, nil
}

// AuthCodeURL returns the IdP URL the browser should be redirected to. `state`
// must be opaque and verified on the callback. `redirectURI` overrides the
// configured one if non-empty (useful when the callback host is derived from
// the request).
func (p *Provider) AuthCodeURL(state, redirectURI string) string {
	oc := *p.oauth
	if redirectURI != "" {
		oc.RedirectURL = redirectURI
	}
	return oc.AuthCodeURL(state, oauth2.AccessTypeOffline)
}

// Exchange swaps an authorization code for an ID token and verifies it,
// returning the resulting Identity.
func (p *Provider) Exchange(ctx context.Context, code, redirectURI string) (*Identity, error) {
	oc := *p.oauth
	if redirectURI != "" {
		oc.RedirectURL = redirectURI
	}
	tok, err := oc.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("oidc: code exchange: %w", err)
	}
	rawID, _ := tok.Extra("id_token").(string)
	if rawID == "" {
		return nil, errors.New("oidc: id_token missing from token response")
	}
	ident, err := p.verifyIDToken(ctx, rawID)
	if err != nil {
		return nil, err
	}
	if err := p.enforceAllowlist(ident); err != nil {
		return nil, err
	}
	return ident, nil
}

func (p *Provider) verifyIDToken(ctx context.Context, raw string) (*Identity, error) {
	keyFunc := func(token *jwt.Token) (interface{}, error) {
		kid, _ := token.Header["kid"].(string)
		if kid == "" {
			return nil, errors.New("id_token missing kid header")
		}
		set, err := p.keys.Fetch(ctx, p.disc.JWKSURI)
		if err != nil {
			return nil, fmt.Errorf("fetch jwks: %w", err)
		}
		key, ok := set.LookupKeyID(kid)
		if !ok {
			// Force a refresh — the IdP may have rotated its keys.
			set, err = p.keys.Refresh(ctx, p.disc.JWKSURI)
			if err != nil {
				return nil, fmt.Errorf("refresh jwks: %w", err)
			}
			key, ok = set.LookupKeyID(kid)
			if !ok {
				return nil, fmt.Errorf("no jwks key for kid %q", kid)
			}
		}
		var pk interface{}
		if err := key.Raw(&pk); err != nil {
			return nil, fmt.Errorf("load jwks key: %w", err)
		}
		return pk, nil
	}

	claims := jwt.MapClaims{}
	tok, err := jwt.ParseWithClaims(raw, claims, keyFunc)
	if err != nil {
		return nil, fmt.Errorf("verify id_token: %w", err)
	}
	if !tok.Valid {
		return nil, errors.New("id_token invalid")
	}

	iss, _ := claims["iss"].(string)
	if iss != p.disc.Issuer {
		return nil, fmt.Errorf("id_token issuer mismatch: %s", iss)
	}
	if !verifyAudienceMatches(claims, p.cfg.ClientID) {
		return nil, errors.New("id_token audience mismatch")
	}
	if exp, ok := claims["exp"].(float64); ok && time.Unix(int64(exp), 0).Before(time.Now()) {
		return nil, errors.New("id_token expired")
	}

	sub, _ := claims["sub"].(string)
	if sub == "" {
		return nil, errors.New("id_token missing sub")
	}
	email, _ := claims["email"].(string)
	emailV, _ := claims["email_verified"].(bool)
	name, _ := claims["name"].(string)

	return &Identity{
		Subject:       sub,
		Email:         email,
		EmailVerified: emailV,
		Name:          name,
		Issuer:        iss,
		Raw:           claims,
	}, nil
}

func verifyAudienceMatches(claims jwt.MapClaims, want string) bool {
	switch v := claims["aud"].(type) {
	case string:
		return v == want
	case []interface{}:
		for _, a := range v {
			if s, ok := a.(string); ok && s == want {
				return true
			}
		}
	}
	return false
}

func (p *Provider) enforceAllowlist(i *Identity) error {
	if len(p.cfg.AllowedEmails) == 0 && len(p.cfg.AllowedDomains) == 0 {
		return nil
	}
	for _, e := range p.cfg.AllowedEmails {
		if strings.EqualFold(e, i.Email) {
			return nil
		}
	}
	if idx := strings.LastIndex(i.Email, "@"); idx >= 0 {
		domain := strings.ToLower(i.Email[idx+1:])
		for _, d := range p.cfg.AllowedDomains {
			if strings.EqualFold(d, domain) {
				return nil
			}
		}
	}
	return fmt.Errorf("oidc: %q is not on the allowed list", i.Email)
}
