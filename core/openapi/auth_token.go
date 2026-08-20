package openapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// oauth2CCAuth implements the OAuth2 client_credentials grant. The token
// endpoint receives a form-encoded POST with grant_type=client_credentials
// plus client_id, client_secret, and (optionally) scope. The response
// must be JSON with at minimum an "access_token" field; "expires_in"
// (seconds) drives the cache TTL when present.
type oauth2CCAuth struct {
	cfg  AuthConfig
	http *http.Client
	tok  *cachedToken
}

func (o *oauth2CCAuth) Apply(ctx context.Context, req *http.Request, _ http.Header) error {
	tok, err := o.tok.get(ctx, o.fetch)
	if err != nil {
		return err
	}
	attachToken(req, o.cfg, tok, "Authorization", "Bearer {token}")
	return nil
}

func (o *oauth2CCAuth) OnUnauthorized(context.Context) error {
	o.tok.invalidate()
	return nil
}

// fetch performs the token-endpoint POST. Diagnostics identify only the
// endpoint origin so credentials embedded in userinfo, paths, or query values
// never appear in error messages.
func (o *oauth2CCAuth) fetch(ctx context.Context) (string, time.Duration, error) {
	if o.cfg.TokenURL == "" {
		return "", 0, fmt.Errorf("openapi: oauth2_client_credentials requires token_url")
	}
	if o.cfg.ClientID == "" || o.cfg.ClientSecret == "" {
		return "", 0, fmt.Errorf("openapi: oauth2_client_credentials requires client_id and client_secret")
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", o.cfg.ClientID)
	form.Set("client_secret", o.cfg.ClientSecret)
	if len(o.cfg.Scopes) > 0 {
		form.Set("scope", strings.Join(o.cfg.Scopes, " "))
	}

	req, err := http.NewRequestWithContext(ctx, "POST", o.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, fmt.Errorf("openapi: invalid token endpoint URL")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	res, err := o.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", 0, ctx.Err()
		}
		return "", 0, fmt.Errorf("openapi: token endpoint %s transport error", safeEndpoint(o.cfg.TokenURL))
	}
	defer res.Body.Close()

	body, err := io.ReadAll(io.LimitReader(res.Body, DefaultMaxResponseBytes+1))
	if err != nil {
		return "", 0, err
	}
	if int64(len(body)) > DefaultMaxResponseBytes {
		return "", 0, fmt.Errorf("openapi: token endpoint response exceeds %d bytes", DefaultMaxResponseBytes)
	}
	if res.StatusCode/100 != 2 {
		return "", 0, fmt.Errorf("openapi: token endpoint %s returned %d (response_bytes=%d)", safeEndpoint(o.cfg.TokenURL), res.StatusCode, len(body))
	}

	// OAuth2 spec mandates access_token + token_type at minimum;
	// expires_in is recommended but optional. We accept both numeric and
	// string expiry values because real-world implementations vary.
	var resp struct {
		AccessToken string      `json:"access_token"`
		ExpiresIn   json.Number `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", 0, fmt.Errorf("openapi: token endpoint response not JSON: %w", err)
	}
	if resp.AccessToken == "" {
		return "", 0, fmt.Errorf("openapi: token endpoint response missing access_token")
	}
	ttl := parseTTL(string(resp.ExpiresIn), o.cfg.CacheTTL)
	return resp.AccessToken, ttl, nil
}

// tokenExchangeAuth implements vendor-specific "POST credentials → JSON
// with token" flows that don't conform to OAuth2. This covers Salesforce
// Marketing Cloud Personalization (apiKeyId + apiKeySecret → access_token)
// and similar shapes. The request body and response field names are
// fully configurable so a single implementation handles many vendors.
type tokenExchangeAuth struct {
	cfg  AuthConfig
	http *http.Client
	tok  *cachedToken
}

func (t *tokenExchangeAuth) Apply(ctx context.Context, req *http.Request, _ http.Header) error {
	tok, err := t.tok.get(ctx, t.fetch)
	if err != nil {
		return err
	}
	attachToken(req, t.cfg, tok, "Authorization", "Bearer {token}")
	return nil
}

func (t *tokenExchangeAuth) OnUnauthorized(context.Context) error {
	t.tok.invalidate()
	return nil
}

func (t *tokenExchangeAuth) fetch(ctx context.Context) (string, time.Duration, error) {
	if t.cfg.TokenURL == "" {
		return "", 0, fmt.Errorf("openapi: token_exchange requires token_url")
	}
	r := t.cfg.Request
	if r == nil {
		return "", 0, fmt.Errorf("openapi: token_exchange requires request body config")
	}
	method := r.Method
	if method == "" {
		method = "POST"
	}

	body, contentType, err := encodeTokenRequestBody(r)
	if err != nil {
		return "", 0, err
	}

	req, err := http.NewRequestWithContext(ctx, method, t.cfg.TokenURL, body)
	if err != nil {
		return "", 0, fmt.Errorf("openapi: invalid token endpoint URL")
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range r.Headers {
		req.Header.Set(k, v)
	}

	res, err := t.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", 0, ctx.Err()
		}
		return "", 0, fmt.Errorf("openapi: token endpoint %s transport error", safeEndpoint(t.cfg.TokenURL))
	}
	defer res.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(res.Body, DefaultMaxResponseBytes+1))
	if err != nil {
		return "", 0, err
	}
	if int64(len(respBody)) > DefaultMaxResponseBytes {
		return "", 0, fmt.Errorf("openapi: token endpoint response exceeds %d bytes", DefaultMaxResponseBytes)
	}
	if res.StatusCode/100 != 2 {
		return "", 0, fmt.Errorf("openapi: token endpoint %s returned %d (response_bytes=%d)", safeEndpoint(t.cfg.TokenURL), res.StatusCode, len(respBody))
	}

	tokField := "access_token"
	expiresField := "expires_in"
	if t.cfg.Response != nil {
		if t.cfg.Response.TokenField != "" {
			tokField = t.cfg.Response.TokenField
		}
		if t.cfg.Response.ExpiresField != "" {
			expiresField = t.cfg.Response.ExpiresField
		}
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return "", 0, fmt.Errorf("openapi: token endpoint response not JSON: %w", err)
	}

	tok, ok := raw[tokField].(string)
	if !ok || tok == "" {
		return "", 0, fmt.Errorf("openapi: token endpoint response missing %q (got keys: %v)", tokField, mapKeys(raw))
	}

	var ttlStr string
	if v, ok := raw[expiresField]; ok {
		ttlStr = fmt.Sprint(v)
	}
	ttl := parseTTL(ttlStr, t.cfg.CacheTTL)
	return tok, ttl, nil
}

func safeEndpoint(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<configured>"
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	u.Path = ""
	u.RawPath = ""
	return u.String()
}

// encodeTokenRequestBody renders the request body in either JSON or
// form encoding. JSON is the default because most modern vendor APIs
// use it; form is provided for legacy systems that mimic OAuth2.
func encodeTokenRequestBody(r *TokenExchangeRequest) (io.Reader, string, error) {
	switch strings.ToLower(r.BodyFormat) {
	case "", "json":
		buf, err := json.Marshal(r.Body)
		if err != nil {
			return nil, "", fmt.Errorf("openapi: encode token request body: %w", err)
		}
		return bytes.NewReader(buf), "application/json", nil
	case "form":
		form := url.Values{}
		for k, v := range r.Body {
			form.Set(k, fmt.Sprint(v))
		}
		return strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", nil
	default:
		return nil, "", fmt.Errorf("openapi: unknown body_format %q (use json or form)", r.BodyFormat)
	}
}

// parseTTL turns the response expires field (which may be int or string
// seconds, depending on the API) plus an optional config override into
// a Duration. The override wins when set, since it lets users handle
// APIs that lie about their expiry windows.
func parseTTL(fromResp, override string) time.Duration {
	if override != "" {
		if d, err := time.ParseDuration(override); err == nil {
			return d
		}
	}
	if fromResp != "" {
		if secs, err := strconv.ParseFloat(strings.TrimSpace(fromResp), 64); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return 0
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}

func mapKeys(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
