package openapi

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// AuthProvider attaches authentication to outgoing requests to an upstream
// API. Implementations are constructed once per spec at boot time and
// reused across every operation against that spec.
//
// Apply mutates req in place. The hdrIn parameter carries headers from
// the *incoming* GraphJin request, enabling per-tenant pass-through
// patterns where the end-user's credentials are forwarded upstream
// rather than a single service credential being shared across tenants.
//
// OnUnauthorized is invoked by the resolver after a 401 response so
// providers that cache tokens can invalidate them and the resolver can
// retry once. Stateless providers (bearer with literal token, basic
// auth) implement this as a no-op.
type AuthProvider interface {
	Apply(ctx context.Context, req *http.Request, hdrIn http.Header) error
	OnUnauthorized(ctx context.Context) error
}

// NewAuthProvider builds the right AuthProvider for cfg.Scheme. An empty
// or unrecognised scheme returns the no-op provider — operations against
// the spec will simply send unauthenticated requests, which is the
// correct behaviour for public APIs.
//
// The returned provider holds httpClient when it needs to make its own
// requests (token exchange, oauth2 client_credentials). httpClient must
// not be nil for those schemes.
func NewAuthProvider(cfg AuthConfig, httpClient *http.Client) (AuthProvider, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Scheme)) {
	case "", "none":
		return noopAuth{}, nil
	case "bearer":
		return &bearerAuth{cfg: cfg}, nil
	case "basic":
		return &basicAuth{cfg: cfg}, nil
	case "api_key", "apikey":
		return &apiKeyAuth{cfg: cfg}, nil
	case "oauth2_client_credentials":
		if httpClient == nil {
			return nil, fmt.Errorf("openapi: oauth2_client_credentials requires an http client")
		}
		return &oauth2CCAuth{cfg: cfg, http: httpClient, tok: &cachedToken{}}, nil
	case "token_exchange":
		if httpClient == nil {
			return nil, fmt.Errorf("openapi: token_exchange requires an http client")
		}
		return &tokenExchangeAuth{cfg: cfg, http: httpClient, tok: &cachedToken{}}, nil
	default:
		return nil, fmt.Errorf("openapi: unknown auth scheme %q", cfg.Scheme)
	}
}

// noopAuth is the auth provider used when no auth is configured. It
// exists primarily so the resolver doesn't need to nil-check provider
// references on the hot path.
type noopAuth struct{}

func (noopAuth) Apply(context.Context, *http.Request, http.Header) error { return nil }
func (noopAuth) OnUnauthorized(context.Context) error                    { return nil }

// bearerAuth attaches a static or pass-through bearer token. The static
// case covers most APIs that authenticate with a long-lived API key;
// the TokenFromRequest case enables multi-tenant deployments where the
// per-user token rides on the incoming GraphJin request.
type bearerAuth struct {
	cfg AuthConfig
}

func (b *bearerAuth) Apply(_ context.Context, req *http.Request, hdrIn http.Header) error {
	tok, err := resolveToken(b.cfg, hdrIn)
	if err != nil {
		return err
	}
	attachToken(req, b.cfg, tok, "Authorization", "Bearer {token}")
	return nil
}

func (b *bearerAuth) OnUnauthorized(context.Context) error { return nil }

// basicAuth produces a base64-encoded "Authorization: Basic ..." header.
// Both username and password are taken straight from configuration —
// this scheme doesn't support per-request credentials because real-world
// basic auth setups use a single service identity.
type basicAuth struct {
	cfg AuthConfig
}

func (b *basicAuth) Apply(_ context.Context, req *http.Request, _ http.Header) error {
	if b.cfg.Username == "" {
		return fmt.Errorf("openapi: basic auth requires username")
	}
	cred := base64.StdEncoding.EncodeToString([]byte(b.cfg.Username + ":" + b.cfg.Password))
	req.Header.Set("Authorization", "Basic "+cred)
	return nil
}

func (b *basicAuth) OnUnauthorized(context.Context) error { return nil }

// apiKeyAuth attaches a key as either a header or a query parameter.
// OpenAPI distinguishes these via the securityScheme's `in` field, but
// users supply it directly in the GraphJin config so we don't need to
// thread that through.
type apiKeyAuth struct {
	cfg AuthConfig
}

func (a *apiKeyAuth) Apply(_ context.Context, req *http.Request, hdrIn http.Header) error {
	val := a.cfg.KeyValue
	if val == "" && a.cfg.TokenFromRequest != nil {
		// Allow apiKey to also pass through from request headers — useful
		// when each tenant has their own key.
		var err error
		val, err = passThroughToken(*a.cfg.TokenFromRequest, hdrIn)
		if err != nil {
			return err
		}
	}
	if val == "" {
		return fmt.Errorf("openapi: api_key requires key_value")
	}
	name := a.cfg.KeyName
	if name == "" {
		return fmt.Errorf("openapi: api_key requires key_name")
	}
	switch strings.ToLower(a.cfg.KeyIn) {
	case "", "header":
		req.Header.Set(name, val)
	case "query":
		q := req.URL.Query()
		q.Set(name, val)
		req.URL.RawQuery = q.Encode()
	default:
		return fmt.Errorf("openapi: api_key key_in %q invalid (use header or query)", a.cfg.KeyIn)
	}
	return nil
}

func (a *apiKeyAuth) OnUnauthorized(context.Context) error { return nil }

// resolveToken returns either the configured static token or the
// pass-through value extracted from the incoming request, in that order
// of preference. Callers that need to enforce the presence of a token
// must check for the empty-string return.
func resolveToken(cfg AuthConfig, hdrIn http.Header) (string, error) {
	if cfg.Token != "" {
		return cfg.Token, nil
	}
	if cfg.TokenFromRequest != nil {
		return passThroughToken(*cfg.TokenFromRequest, hdrIn)
	}
	return "", fmt.Errorf("openapi: bearer auth requires token or token_from_request")
}

// passThroughToken extracts the token from the incoming GraphJin request
// according to TokenFromRequest. Header lookups are case-insensitive
// because http.Header canonicalises on Get; query lookups aren't yet
// supported because the resolver doesn't currently see the inbound
// query string.
func passThroughToken(tfr TokenFromRequest, hdrIn http.Header) (string, error) {
	if tfr.Header != "" {
		v := hdrIn.Get(tfr.Header)
		if v == "" {
			return "", fmt.Errorf("openapi: pass-through header %q absent on incoming request", tfr.Header)
		}
		return v, nil
	}
	return "", fmt.Errorf("openapi: token_from_request requires header field (query not yet supported)")
}

// attachToken applies a resolved token to req using the configured
// AttachAs / AttachName / AttachFormat — or a sensible default header
// when those are empty. The default mirrors the most common shape:
// `Authorization: Bearer <token>` on a header named Authorization.
func attachToken(req *http.Request, cfg AuthConfig, tok, defaultName, defaultFormat string) {
	name := cfg.AttachName
	if name == "" {
		name = defaultName
	}
	format := cfg.AttachFormat
	if format == "" {
		format = defaultFormat
	}
	value := strings.ReplaceAll(format, "{token}", tok)

	switch strings.ToLower(cfg.AttachAs) {
	case "query":
		q := req.URL.Query()
		q.Set(name, value)
		req.URL.RawQuery = q.Encode()
	default:
		req.Header.Set(name, value)
	}
}

// cachedToken is the shared TTL-aware token cache used by the
// authentication providers that fetch tokens themselves
// (oauth2_client_credentials, token_exchange).
//
// The fetch closure does the actual network call and returns the token
// + its TTL; the cache handles concurrency and a 30-second pre-expiry
// refresh to avoid the race where a token expires mid-request.
type cachedToken struct {
	mu      sync.Mutex
	token   string
	expires time.Time
}

// get returns a valid token, fetching a new one when the cached value is
// missing or near expiry. Concurrent callers serialise behind the mutex;
// the cost of holding the lock across a network call is acceptable
// because mutex contention only happens at startup and right after
// expiry, not on the hot path.
func (c *cachedToken) get(ctx context.Context, fetch func(ctx context.Context) (string, time.Duration, error)) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	const refreshGuard = 30 * time.Second
	if c.token != "" && time.Now().Before(c.expires.Add(-refreshGuard)) {
		return c.token, nil
	}

	tok, ttl, err := fetch(ctx)
	if err != nil {
		return "", err
	}
	c.token = tok
	if ttl > 0 {
		c.expires = time.Now().Add(ttl)
	} else {
		// Fallback when the auth response doesn't expose an expiry:
		// cache for an hour so we don't hammer the auth endpoint, but
		// don't cache forever in case the token actually expires.
		c.expires = time.Now().Add(time.Hour)
	}
	return c.token, nil
}

// invalidate clears the cached token. Called from OnUnauthorized so the
// next request goes through fetch().
func (c *cachedToken) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = ""
	c.expires = time.Time{}
}
