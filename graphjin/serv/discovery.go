package serv

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

const (
	routeDiscovery = "/api/v1/discovery"
)

func discoveryHandler(s1 *HttpService) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		s := s1.Load().(*graphjinService)
		if !s.conf.legacyDiscoveryEnabled() {
			http.NotFound(w, r)
			return
		}
		if s.disc == nil {
			http.Error(w, "Discovery not available. Schema may not be ready yet.", http.StatusServiceUnavailable)
			return
		}

		db := defaultDiscoveryDB(s)
		if qdb := r.URL.Query().Get("database"); qdb != "" {
			db = qdb
		}
		if db == "" {
			http.Error(w, "Discovery not available. Schema may not be ready yet.", http.StatusServiceUnavailable)
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		writeDiscoveryJSON(w, s.disc.Payload(db))
	})
}

func discoveryWildcardHandler(s1 *HttpService) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		section := strings.TrimPrefix(r.URL.Path, routeDiscovery)
		section = strings.Trim(section, "/")

		s := s1.Load().(*graphjinService)
		if !s.conf.legacyDiscoveryEnabled() {
			http.NotFound(w, r)
			return
		}
		if s.disc == nil {
			http.Error(w, "Discovery not available. Schema may not be ready yet.", http.StatusServiceUnavailable)
			return
		}
		db := defaultDiscoveryDB(s)
		if qdb := r.URL.Query().Get("database"); qdb != "" {
			db = qdb
		}
		if db == "" {
			http.Error(w, "Discovery not available. Schema may not be ready yet.", http.StatusServiceUnavailable)
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		switch section {
		case "", "/":
			writeDiscoveryJSON(w, s.disc.Payload(db))
		case "tables":
			writeDiscoveryJSON(w, s.disc.TableIndexPage(r.Context(), db, tableListOptionsFromRequest(r)))
		case "tables/full":
			payload := s.disc.FullPayload(db)
			payload.Tables = s.disc.FullTablesPage(db, tableListOptionsFromRequest(r))
			writeDiscoveryJSON(w, payload)
		case "namespaces":
			rollup := s.disc.Namespaces(r.Context(), db)
			writeDiscoveryJSON(w, ListNamespacesResult{
				Database:   db,
				Namespaces: rollup,
				Count:      len(rollup),
			})
		case "insights":
			writeDiscoveryJSON(w, s.disc.Insights(db))
		case "syntax":
			writeDiscoveryJSON(w, map[string]any{
				"query":    querySyntaxReference,
				"mutation": mutationSyntaxReference,
			})
		case "schema":
			writeDiscoveryJSON(w, discoverySchema())
		default:
			http.NotFound(w, r)
		}
	})
}

func tableListOptionsFromRequest(r *http.Request) TableListOptions {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	return TableListOptions{
		Search: q.Get("search"),
		Schema: q.Get("schema"),
		Limit:  limit,
		Cursor: q.Get("cursor"),
	}
}

func defaultDiscoveryDB(s *graphjinService) string {
	if s.gj == nil {
		return ""
	}
	if name := s.gj.DefaultDatabase(); name != "" {
		return name
	}
	names := s.gj.DatabaseNames()
	if len(names) > 0 {
		return names[0]
	}
	return ""
}

func writeDiscoveryJSON(w http.ResponseWriter, payload any) {
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
