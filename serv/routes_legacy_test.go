package serv

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"go.uber.org/zap"
)

func newLegacySurfaceRouteHandler(t *testing.T, conf *Config) http.Handler {
	t.Helper()

	logger := zap.NewNop()
	svc := &graphjinService{
		conf: conf,
		log:  logger.Sugar(),
		zlog: logger,
	}
	hs := &HttpService{}
	hs.Store(svc)

	router := http.NewServeMux()
	handler, err := routesHandler(hs, router, nil)
	if err != nil {
		t.Fatalf("routes handler: %v", err)
	}
	return handler
}

func TestSourceModeHidesLegacyRESTDiscoveryAndWorkflows(t *testing.T) {
	handler := newLegacySurfaceRouteHandler(t, &Config{
		Core: core.Config{Sources: []core.SourceConfig{{Name: "graphjin", Kind: "graphjin"}}},
		Serv: Serv{MCP: MCPConfig{Disable: true}},
	})

	for _, tc := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/v1/discovery"},
		{method: http.MethodGet, path: "/api/v1/discovery/tables"},
		{method: http.MethodPost, path: "/api/v1/workflows/daily_report"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("expected 404 for disabled legacy REST surface %s, got %d: %s", tc.path, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestSourceModeLegacyDiscoveryEnablesLegacyRESTSurfaces(t *testing.T) {
	handler := newLegacySurfaceRouteHandler(t, &Config{
		Core: core.Config{Sources: []core.SourceConfig{{Name: "graphjin", Kind: "graphjin"}}},
		Serv: Serv{MCP: MCPConfig{Disable: true, LegacyDiscovery: true}},
	})

	for _, tc := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/v1/discovery"},
		{method: http.MethodPost, path: "/api/v1/workflows/daily_report"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code == http.StatusNotFound {
				t.Fatalf("expected legacy REST surface %s to be registered with mcp.legacy_discovery", tc.path)
			}
		})
	}
}

func TestSourceModeMCPOnlyHidesLegacyRESTSurfaces(t *testing.T) {
	handler := newLegacySurfaceRouteHandler(t, &Config{
		Core: core.Config{Sources: []core.SourceConfig{{Name: "graphjin", Kind: "graphjin"}}},
		Serv: Serv{MCP: MCPConfig{Only: true, Disable: true}},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/daily_report", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for disabled MCP-only workflow REST surface, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestLegacyModeKeepsLegacyRESTSurfaces(t *testing.T) {
	handler := newLegacySurfaceRouteHandler(t, &Config{
		Core: core.Config{},
		Serv: Serv{MCP: MCPConfig{Disable: true}},
	})

	for _, tc := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/v1/discovery"},
		{method: http.MethodPost, path: "/api/v1/workflows/daily_report"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code == http.StatusNotFound {
				t.Fatalf("expected legacy mode to register REST surface %s", tc.path)
			}
		})
	}
}
