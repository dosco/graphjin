package serv

import (
	"bufio"
	"net"
	"net/http"

	"github.com/dosco/graphjin/core/v3/openapi"
)

// Capture only credential headers explicitly named by server configuration.
// Neither GraphQL variables nor MCP tool arguments can select an account token.
func (s *graphjinService) withOpenAPIRequestHeaders(w http.ResponseWriter, r *http.Request) (http.ResponseWriter, *http.Request) {
	var headers http.Header
	collect := func(specs map[string]openapi.SpecConfig) {
		for _, spec := range specs {
			if tfr := spec.Auth.TokenFromRequest; tfr != nil && tfr.Header != "" {
				if headers == nil {
					headers = make(http.Header)
				}
				name := http.CanonicalHeaderKey(tfr.Header)
				headers[name] = r.Header.Values(name)
			}
		}
	}
	collect(s.conf.Core.OpenAPI)
	for _, source := range s.conf.Core.Sources {
		collect(source.Specs)
	}
	if headers == nil {
		return w, r
	}
	// ponytail: disable HTTP caching deployment-wide for request-token sources;
	// narrow to selected sources if public-response caching is needed.
	w.Header().Set("Cache-Control", "private, no-store")
	return &personalAPIResponseWriter{ResponseWriter: w}, r.WithContext(openapi.WithRequestHeaders(r.Context(), headers))
}

// MCP's SSE transport sets its own cache header when committing the response.
// Preserve no-store at that boundary while keeping ResponseController support.
type personalAPIResponseWriter struct {
	http.ResponseWriter
	written bool
}

func (w *personalAPIResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *personalAPIResponseWriter) WriteHeader(status int) {
	if w.written {
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	if status < 100 || status >= 200 || status == http.StatusSwitchingProtocols {
		w.written = true
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *personalAPIResponseWriter) Write(p []byte) (int, error) {
	if !w.written {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}
func (w *personalAPIResponseWriter) Flush() {
	if !w.written {
		w.WriteHeader(http.StatusOK)
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *personalAPIResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(w.ResponseWriter).Hijack()
}
