//nolint:errcheck
package etags

import (
	"net/http"
)

// Handler wraps the http.Handler h with ETag support.
func Handler(h http.Handler, weak bool) http.Handler {
	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		// Skip if websocket
		if req.Header.Get("Sec-WebSocket-Key") != "" {
			h.ServeHTTP(res, req)
			return
		}

		if IsFresh(req.Header, res.Header()) {
			res.WriteHeader(http.StatusNotModified)
		} else {
			h.ServeHTTP(res, req)
		}
	})
}
