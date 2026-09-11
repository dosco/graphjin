package openapi

import (
	"context"
	"net/http"
)

type requestHeadersKey struct{}
type requestHeaders struct {
	request context.Context
	headers http.Header
}

// WithRequestHeaders supplies trusted, explicitly selected upstream credential
// headers to OpenAPI calls. They are copied and never stored on a shared caller.
// The original request must remain active, including when a child detaches its
// cancellation. Hosts must not populate these headers from GraphQL arguments.
func WithRequestHeaders(ctx context.Context, headers http.Header) context.Context {
	return context.WithValue(ctx, requestHeadersKey{}, requestHeaders{ctx, headers.Clone()})
}

func incomingRequestHeaders(ctx context.Context) (http.Header, error) {
	value, ok := ctx.Value(requestHeadersKey{}).(requestHeaders)
	if !ok {
		return nil, nil
	}
	if err := value.request.Err(); err != nil {
		return nil, err
	}
	return value.headers, nil
}

// UsesRequestCredentials reports whether calls require a personal request token.
func (c *Caller) UsesRequestCredentials() bool {
	switch auth := c.auth.(type) {
	case *bearerAuth:
		return auth.cfg.TokenFromRequest != nil
	case *apiKeyAuth:
		return auth.cfg.TokenFromRequest != nil
	}
	return false
}
