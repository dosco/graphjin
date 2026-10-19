package serv

import (
	"net/http"
	"net/http/httptest"
	"strings"
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
		gj:   &core.GraphJin{},
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

func TestIsSourcesUsedHidesLegacyRESTDiscoveryAndWorkflows(t *testing.T) {
	handler := newLegacySurfaceRouteHandler(t, &Config{
		Core: core.Config{Mode: "agentic", Sources: []core.SourceConfig{{Name: "graphjin", Kind: "database", Type: "sqlite"}}},
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

func TestIsSourcesUsedLegacyDiscoveryEnablesLegacyRESTSurfaces(t *testing.T) {
	handler := newLegacySurfaceRouteHandler(t, &Config{
		Core: core.Config{Mode: "agentic", Sources: []core.SourceConfig{{Name: "graphjin", Kind: "database", Type: "sqlite"}}, System: core.SystemConfig{Capabilities: map[string]bool{"legacy_discovery.read": true}}},
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

func TestIsSourcesUsedMCPOnlyHidesLegacyRESTSurfaces(t *testing.T) {
	handler := newLegacySurfaceRouteHandler(t, &Config{
		Core: core.Config{Mode: "agentic", Sources: []core.SourceConfig{{Name: "graphjin", Kind: "database", Type: "sqlite"}}},
		Serv: Serv{MCP: MCPConfig{Only: true, Disable: true}},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/daily_report", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for disabled MCP-only workflow REST surface, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAgentRouteRegistration(t *testing.T) {
	handler := newLegacySurfaceRouteHandler(t, &Config{
		Core: core.Config{Mode: "agentic", Sources: []core.SourceConfig{{Name: "graphjin", Kind: "database", Type: "sqlite"}}},
		Serv: Serv{MCP: MCPConfig{Disable: true}, Agent: AgentConfig{Enabled: true}},
	})

	req := httptest.NewRequest(http.MethodPost, routeAgent, strings.NewReader(`{"instruction":"find customers"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code == http.StatusNotFound {
		t.Fatalf("expected agent route to be registered when agent.enabled=true")
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected missing agent key to return 503, got %d: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, routeAgentStatus, nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected agent status route to be registered when agent.enabled=true, got %d: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, routeAgent, strings.NewReader(`{}`))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected empty instruction to return 400, got %d: %s", rec.Code, rec.Body.String())
	}

	disabled := newLegacySurfaceRouteHandler(t, &Config{
		Core: core.Config{Mode: "agentic", Sources: []core.SourceConfig{{Name: "graphjin", Kind: "database", Type: "sqlite"}}},
		Serv: Serv{MCP: MCPConfig{Disable: true}},
	})
	req = httptest.NewRequest(http.MethodPost, routeAgent, strings.NewReader(`{"instruction":"find customers"}`))
	rec = httptest.NewRecorder()
	disabled.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected disabled agent route to be 404, got %d: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, routeAgentStatus, nil)
	rec = httptest.NewRecorder()
	disabled.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status route to stay available when agent.enabled=false, got %d: %s", rec.Code, rec.Body.String())
	}

	mcpOnly := newLegacySurfaceRouteHandler(t, &Config{
		Core: core.Config{Mode: "agentic", Sources: []core.SourceConfig{{Name: "graphjin", Kind: "database", Type: "sqlite"}}},
		Serv: Serv{MCP: MCPConfig{Only: true, Disable: true}, Agent: AgentConfig{Enabled: true}},
	})
	req = httptest.NewRequest(http.MethodPost, routeAgent, strings.NewReader(`{"instruction":"find customers"}`))
	rec = httptest.NewRecorder()
	mcpOnly.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected mcp.only to hide agent REST route, got %d: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, routeAgentStatus, nil)
	rec = httptest.NewRecorder()
	mcpOnly.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected mcp.only to hide agent status route, got %d: %s", rec.Code, rec.Body.String())
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

func TestLegacyProductionHidesMCPRoutesByDefault(t *testing.T) {
	handler := newLegacySurfaceRouteHandler(t, &Config{
		Core: core.Config{},
		Serv: Serv{Production: true},
	})

	for _, tc := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/v1/mcp"},
		{method: http.MethodPost, path: "/api/v1/mcp/message"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("expected 404 for disabled legacy production MCP route %s, got %d: %s", tc.path, rec.Code, rec.Body.String())
			}
		})
	}
}
