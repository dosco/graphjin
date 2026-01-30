package serv

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed web/build
var webBuild embed.FS

// webuiHandler serves the web UI with SPA routing support
func webuiHandler(routePrefix string, gqlEndpoint string) http.Handler {
	webRoot, _ := fs.Sub(webBuild, "web/build")
	fileServer := http.FileServer(http.FS(webRoot))

	h := func(w http.ResponseWriter, r *http.Request) {
		// Redirect root to include graphql endpoint
		if r.URL.Path == "" && r.URL.RawQuery == "" {
			rt := (r.URL.Path + "?endpoint=" + gqlEndpoint)
			w.Header().Set("Location", rt)
			w.WriteHeader(http.StatusMovedPermanently)
			return
		}

		// Try to open the requested file
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		f, err := webRoot.Open(path)
		if err == nil {
			defer f.Close() //nolint:errcheck
			stat, statErr := f.Stat()
			if statErr == nil && !stat.IsDir() {
				// File exists - serve it normally
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		// File not found or is a directory - serve index.html for SPA routing
		indexFile, err := webRoot.Open("index.html")
		if err != nil {
			http.Error(w, "index.html not found", http.StatusNotFound)
			return
		}
		defer indexFile.Close() //nolint:errcheck

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.Copy(w, indexFile) //nolint:errcheck
	}

	if !strings.HasSuffix(routePrefix, "/") {
		routePrefix += "/"
	}

	return http.StripPrefix(routePrefix, http.HandlerFunc(h))
}
