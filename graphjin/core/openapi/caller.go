package openapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Caller executes a single OpenAPI operation. One Caller is constructed
// per operation at boot time and reused for every call to that
// operation. The construction cost (URL parsing, escape-table init) is
// amortised across the request lifetime.
//
// Callers are safe for concurrent use — the auth provider, limiter, and
// http client all guard their own state. The Caller itself only reads
// from its fields after construction.
type Caller struct {
	op      *OpDescriptor
	auth    AuthProvider
	limiter *limiter
	http    *http.Client
	baseURL string
}

// CallParams carries the per-call inputs. PathValues populates the URL
// template's {placeholders}; QueryValues and HeaderValues populate
// non-path parameters (query strings and HTTP headers respectively);
// IncomingHeaders is the inbound GraphJin request's header set, used
// only for pass-through auth and ignored otherwise.
type CallParams struct {
	PathValues      map[string]string
	QueryValues     map[string]string
	HeaderValues    map[string]string
	IncomingHeaders http.Header
}

// NewCaller wires up everything a single operation needs to execute.
// httpClient must be non-nil; auth and limiter must already be
// initialised by the SpecRuntime that holds them.
func NewCaller(op *OpDescriptor, baseURL string, auth AuthProvider, lim *limiter, httpClient *http.Client) *Caller {
	return &Caller{
		op:      op,
		auth:    auth,
		limiter: lim,
		http:    httpClient,
		baseURL: strings.TrimRight(baseURL, "/"),
	}
}

// Call executes the operation and returns the response body, with the
// configured ResultPath stripped. A 401 triggers exactly one retry
// after invalidating the auth provider's cached token — this handles
// the common case of a service-credential token expiring mid-flight.
func (c *Caller) Call(ctx context.Context, p CallParams) ([]byte, error) {
	body, status, err := c.doOnce(ctx, p)
	if err != nil {
		return nil, err
	}
	if status == http.StatusUnauthorized {
		// Cached-token providers invalidate; stateless ones no-op.
		// Either way, we retry exactly once. Repeat 401s after refresh
		// surface as a real auth failure rather than a retry loop.
		if rerr := c.auth.OnUnauthorized(ctx); rerr != nil {
			return nil, fmt.Errorf("openapi: auth invalidate: %w", rerr)
		}
		body, status, err = c.doOnce(ctx, p)
		if err != nil {
			return nil, err
		}
	}
	if status/100 != 2 {
		return nil, fmt.Errorf("openapi: %s %s returned %d: %s",
			c.op.Method, c.op.PathTemplate, status, truncate(body, 200))
	}

	if len(c.op.ResultPath) > 0 {
		body, err = stripResultPath(body, c.op.ResultPath)
		if err != nil {
			return nil, fmt.Errorf("openapi: %s strip result_path: %w", c.op.OperationID, err)
		}
	}
	return body, nil
}

// doOnce builds and executes a single HTTP request. Separated out so
// the 401-retry path can call it twice without re-building the URL
// every time we need a defensive copy of the response body.
func (c *Caller) doOnce(ctx context.Context, p CallParams) ([]byte, int, error) {
	if err := c.limiter.acquire(ctx); err != nil {
		return nil, 0, err
	}
	defer c.limiter.release()

	urlStr, err := c.buildURL(p)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, c.op.Method, urlStr, nil)
	if err != nil {
		return nil, 0, err
	}

	for name, val := range p.HeaderValues {
		req.Header.Set(name, val)
	}
	req.Header.Set("Accept", "application/json")

	if err := c.auth.Apply(ctx, req, p.IncomingHeaders); err != nil {
		return nil, 0, fmt.Errorf("openapi: auth apply: %w", err)
	}

	res, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("openapi: %s: %w", urlStr, err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, res.StatusCode, err
	}
	return body, res.StatusCode, nil
}

// buildURL substitutes {path params} and appends query string. Path
// values are URL-escaped because they may contain characters (slashes,
// spaces) that would break the URL otherwise. Missing required path
// params produce an explicit error rather than a malformed request.
func (c *Caller) buildURL(p CallParams) (string, error) {
	path := c.op.PathTemplate
	for _, ps := range c.op.PathParams {
		val, ok := p.PathValues[ps.Name]
		if !ok || val == "" {
			if ps.Required {
				return "", fmt.Errorf("openapi: missing path parameter %q for %s", ps.Name, c.op.OperationID)
			}
			continue
		}
		placeholder := "{" + ps.Name + "}"
		path = strings.ReplaceAll(path, placeholder, url.PathEscape(val))
	}

	full := c.baseURL + path
	u, err := url.Parse(full)
	if err != nil {
		return "", fmt.Errorf("openapi: invalid URL %q: %w", full, err)
	}

	if len(p.QueryValues) > 0 {
		q := u.Query()
		for k, v := range p.QueryValues {
			if v == "" {
				continue
			}
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
	}

	return u.String(), nil
}

// stripResultPath walks a sequence of object keys and returns the
// re-encoded value at the end of the path. We deliberately round-trip
// through map[string]interface{} rather than streaming — the round-trip
// is a few microseconds on payloads of any sane size, and the streaming
// alternative is much harder to make correct against arbitrary input.
func stripResultPath(body []byte, path []string) ([]byte, error) {
	var v interface{}
	if err := json.Unmarshal(body, &v); err != nil {
		return nil, err
	}
	for _, key := range path {
		m, ok := v.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("expected object at result_path step %q", key)
		}
		next, ok := m[key]
		if !ok {
			return nil, fmt.Errorf("result_path key %q not found in response", key)
		}
		v = next
	}
	return json.Marshal(v)
}
