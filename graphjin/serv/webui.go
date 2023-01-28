package serv

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed web/build
var webBuild embed.FS

func webuiHandler(routePrefix string, gqlEndpoint string) http.Handler {
	webRoot, _ := fs.Sub(webBuild, "web/build")
	fs := http.FileServer(http.FS(webRoot))

	h := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "" && r.URL.RawQuery == "" {
			rt := (r.URL.Path + "?endpoint=" + gqlEndpoint)
			w.Header().Set("Location", rt)
			w.WriteHeader(http.StatusMovedPermanently)
			return
		}
		fs.ServeHTTP(w, r)
	}

	if !strings.HasSuffix(routePrefix, "/") {
		routePrefix += "/"
	}

	return http.StripPrefix(routePrefix, http.HandlerFunc(h))
}
