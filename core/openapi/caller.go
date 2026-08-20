package openapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
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
// IncomingHeaders is an optional host-supplied inbound header set used only
// for pass-through auth. The built-in GraphQL bridge currently leaves it nil.
type CallParams struct {
	PathValues      map[string]string
	QueryValues     map[string]string
	HeaderValues    map[string]string
	IncomingHeaders http.Header
	BodyJSON        []byte
	RequestID       string
}

// CallResult carries the normalized execution metadata used by the GraphQL
// mutation envelope and structured runtime diagnostics.
type CallResult struct {
	Body          []byte
	StatusCode    int
	RequestID     string
	RequestBytes  int64
	ResponseBytes int64
	RetryCount    int
}

// NewCaller wires up everything a single operation needs to execute.
// A nil httpClient uses http.DefaultClient; auth and limiter must already be
// initialised by the SpecRuntime that holds them. Mutation callers receive a
// shallow client copy with redirects disabled.
func NewCaller(op *OpDescriptor, baseURL string, auth AuthProvider, lim *limiter, httpClient *http.Client) *Caller {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if op != nil && op.Mode == OpModeMutation {
		httpClient = clientWithoutRedirects(httpClient)
	}
	return &Caller{
		op:      op,
		auth:    auth,
		limiter: lim,
		http:    httpClient,
		baseURL: strings.TrimRight(baseURL, "/"),
	}
}

// clientWithoutRedirects returns a shallow copy that preserves the caller's
// transport, timeout, cookie jar, and tracing hooks while refusing redirects.
// Redirects are implicit additional HTTP attempts; for writes, 307/308 can
// replay the original body and any redirect can escape the configured base URL.
func clientWithoutRedirects(client *http.Client) *http.Client {
	if client == nil {
		client = http.DefaultClient
	}
	cloned := *client
	cloned.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &cloned
}

// Call executes the operation and returns the response body, with the
// configured ResultPath stripped. A 401 triggers exactly one retry
// after invalidating the auth provider's cached token — this handles
// the common case of a service-credential token expiring mid-flight.
func (c *Caller) Call(ctx context.Context, p CallParams) ([]byte, error) {
	result, err := c.call(ctx, p, false)
	if err != nil {
		return nil, err
	}
	if len(c.op.ResultPath) > 0 {
		result.Body, err = stripResultPath(result.Body, c.op.ResultPath)
		if err != nil {
			return nil, fmt.Errorf("openapi: %s strip result_path: %w", c.op.OperationID, err)
		}
	}
	return result.Body, nil
}

// CallMutation executes an explicitly classified mutation and returns status,
// size, retry, and correlation metadata for the stable GraphQL envelope.
func (c *Caller) CallMutation(ctx context.Context, p CallParams) (CallResult, error) {
	if c == nil || c.op == nil || c.op.Mode != OpModeMutation {
		return CallResult{}, fmt.Errorf("openapi: caller is not an exposed mutation")
	}
	return c.call(ctx, p, true)
}

func (c *Caller) call(ctx context.Context, p CallParams, mutation bool) (CallResult, error) {
	if p.RequestID == "" {
		p.RequestID = NewRequestID()
	}
	result, err := c.doOnce(ctx, p)
	if err != nil {
		return result, mutationCallError(ctx, mutation, err)
	}
	retry := result.StatusCode == http.StatusUnauthorized && (!mutation || c.op.RetryOnAuthFailure)
	if retry {
		if rerr := c.auth.OnUnauthorized(ctx); rerr != nil {
			return result, fmt.Errorf("openapi: auth invalidate: %w", rerr)
		}
		result, err = c.doOnce(ctx, p)
		result.RetryCount = 1
		if err != nil {
			return result, mutationCallError(ctx, mutation, err)
		}
	}
	if !c.acceptsStatus(result.StatusCode, mutation) {
		if mutation {
			return result, fmt.Errorf("openapi: %s %s returned %d (response_bytes=%d)", c.op.Method, c.op.PathTemplate, result.StatusCode, result.ResponseBytes)
		}
		return result, fmt.Errorf("openapi: %s %s returned %d: %s", c.op.Method, c.op.PathTemplate, result.StatusCode, truncate(result.Body, 200))
	}
	if mutation && result.StatusCode != http.StatusNoContent && len(result.Body) == 0 {
		return result, fmt.Errorf("openapi: %s returned an empty JSON response", c.op.OperationID)
	}
	if mutation && len(result.Body) != 0 && !json.Valid(result.Body) {
		return result, fmt.Errorf("openapi: %s returned invalid JSON (response_bytes=%d)", c.op.OperationID, result.ResponseBytes)
	}
	return result, nil
}

// doOnce builds and executes a single HTTP request. Separated out so
// the 401-retry path can call it twice without re-building the URL
// every time we need a defensive copy of the response body.
func (c *Caller) doOnce(ctx context.Context, p CallParams) (CallResult, error) {
	result := CallResult{RequestID: p.RequestID, RequestBytes: int64(len(p.BodyJSON))}
	requestLimit := c.op.MaxRequestBytes
	if requestLimit <= 0 {
		requestLimit = DefaultMaxRequestBytes
	}
	if result.RequestBytes > requestLimit {
		return result, fmt.Errorf("openapi: request body exceeds max_request_bytes (%d > %d)", result.RequestBytes, requestLimit)
	}
	if err := c.limiter.acquire(ctx); err != nil {
		return result, err
	}
	defer c.limiter.release()

	urlStr, err := c.buildURL(p)
	if err != nil {
		return result, err
	}
	var body io.Reader
	if len(p.BodyJSON) != 0 {
		body = bytes.NewReader(p.BodyJSON)
	}
	req, err := http.NewRequestWithContext(ctx, c.op.Method, urlStr, body)
	if err != nil {
		return result, err
	}

	for name, val := range p.HeaderValues {
		if forbiddenHeader(name) {
			return result, fmt.Errorf("openapi: caller header %q is forbidden", name)
		}
		req.Header.Set(name, val)
	}
	req.Header.Set("Accept", "application/json")
	if len(p.BodyJSON) != 0 {
		req.Header.Set("Content-Type", c.op.RequestMediaType)
	}
	req.Header.Set("X-Request-ID", p.RequestID)

	if err := c.auth.Apply(ctx, req, p.IncomingHeaders); err != nil {
		return result, fmt.Errorf("openapi: auth apply: %w", err)
	}

	res, err := c.http.Do(req)
	if err != nil {
		return result, &upstreamTransportError{method: c.op.Method, operationID: c.op.OperationID, err: err}
	}
	defer res.Body.Close()

	responseLimit := c.op.MaxResponseBytes
	if responseLimit <= 0 {
		responseLimit = DefaultMaxResponseBytes
	}
	readLimit := responseLimit
	if readLimit < int64(^uint64(0)>>1) {
		readLimit++
	}
	responseBody, err := io.ReadAll(io.LimitReader(res.Body, readLimit))
	if err != nil {
		result.StatusCode = res.StatusCode
		return result, err
	}
	result.StatusCode = res.StatusCode
	result.ResponseBytes = int64(len(responseBody))
	if result.ResponseBytes > responseLimit {
		result.Body = nil
		return result, fmt.Errorf("openapi: response exceeds max_response_bytes (%d > %d)", result.ResponseBytes, responseLimit)
	}
	result.Body = responseBody
	return result, nil
}

type upstreamTransportError struct {
	method      string
	operationID string
	err         error
}

func (e *upstreamTransportError) Error() string {
	return fmt.Sprintf("openapi: %s %s transport error", e.method, e.operationID)
}

func (e *upstreamTransportError) Unwrap() error { return e.err }

func mutationCallError(ctx context.Context, mutation bool, err error) error {
	var transportErr *upstreamTransportError
	if mutation && ctx.Err() == nil && errors.As(err, &transportErr) {
		return fmt.Errorf("openapi: mutation outcome may be ambiguous: %w", err)
	}
	return err
}

func (c *Caller) acceptsStatus(status int, mutation bool) bool {
	if !mutation {
		return status/100 == 2
	}
	for _, accepted := range c.op.SuccessStatuses {
		if status == accepted {
			return true
		}
	}
	return false
}

// NewRequestID returns a correlation identifier suitable for propagating to an
// upstream API and recording in redacted completion metadata.
func NewRequestID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "gj-openapi-request"
	}
	const alphabet = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = alphabet[v>>4]
		out[i*2+1] = alphabet[v&0x0f]
	}
	return "gj-" + string(out)
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
		return "", fmt.Errorf("openapi: invalid URL for operation %s", c.op.OperationID)
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
