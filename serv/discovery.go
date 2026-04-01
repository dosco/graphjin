package serv

import (
	"net/http"
	"strings"
)

const (
	routeDiscovery = "/api/v1/discovery"
)

// discoveryHandler returns the combined discovery Bible for the entire graph.
// GET /api/v1/discovery
func discoveryHandler(s1 *HttpService) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		s := s1.Load().(*graphjinService)
		md := s.gj.GetCombinedDiscovery()

		if md == "" {
			http.Error(w, "Discovery not available. Schema may not be ready yet.", http.StatusServiceUnavailable)
			return
		}

		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write([]byte(md))
	})
}

// discoveryWildcardHandler handles /api/v1/discovery/* routes — redirects to main endpoint.
func discoveryWildcardHandler(s1 *HttpService) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Strip trailing path — everything is in the combined document
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/discovery")
		if path != "" && path != "/" {
			// Redirect to main discovery endpoint
			http.Redirect(w, r, "/api/v1/discovery", http.StatusMovedPermanently)
			return
		}
		discoveryHandler(s1).ServeHTTP(w, r)
	})
}
