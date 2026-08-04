package serv

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/dosco/graphjin/core/v3"
)

func TestConsoleBootstrapDevAdvertisesUserAndAdmin(t *testing.T) {
	conf := &Config{Core: core.Config{
		Mode:      "dev",
		Artifacts: core.ArtifactsConfig{Enabled: true},
		Watches:   core.WatchesConfig{Enabled: true},
		Tasks:     core.TasksConfig{Enabled: true},
	}}
	hs := newAgentHTTPTestService(conf)
	req := httptest.NewRequest(http.MethodGet, routeConsoleBootstrap, nil)
	rec := httptest.NewRecorder()

	hs.ConsoleBootstrap(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("bootstrap status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("Cache-Control = %q, want private, no-store", got)
	}
	response := decodeConsoleBootstrap(t, rec)
	if response.SchemaVersion != consoleBootstrapSchemaVersion {
		t.Fatalf("schema version = %q, want %q", response.SchemaVersion, consoleBootstrapSchemaVersion)
	}
	if !hasConsoleWorkspace(response.Workspaces, "user") || !hasConsoleWorkspace(response.Workspaces, "admin") {
		t.Fatalf("dev workspaces = %+v, want user and admin", response.Workspaces)
	}
	if hasConsoleWorkspace(response.Workspaces, "trainer") {
		t.Fatalf("trainer must stay hidden without a backend capability: %+v", response.Workspaces)
	}
}

func TestConsoleBootstrapAgenticScopesAdminWorkspace(t *testing.T) {
	conf := &Config{Core: core.Config{
		Mode:    "agentic",
		Sources: []core.SourceConfig{{Name: "graphjin", Kind: "database", Type: "sqlite"}},
	}}
	hs := newAgentHTTPTestService(conf)

	anonReq := httptest.NewRequest(http.MethodGet, routeConsoleBootstrap, nil)
	anonRec := httptest.NewRecorder()
	hs.ConsoleBootstrap(nil).ServeHTTP(anonRec, anonReq)
	anon := decodeConsoleBootstrap(t, anonRec)
	if !hasConsoleWorkspace(anon.Workspaces, "user") || hasConsoleWorkspace(anon.Workspaces, "admin") {
		t.Fatalf("agentic anon workspaces = %+v, want user only", anon.Workspaces)
	}

	ctx := context.WithValue(context.Background(), core.UserIDKey, "admin-1")
	ctx = context.WithValue(ctx, core.UserRoleKey, "admin")
	adminReq := httptest.NewRequest(http.MethodGet, routeConsoleBootstrap, nil).WithContext(ctx)
	adminRec := httptest.NewRecorder()
	hs.ConsoleBootstrap(nil).ServeHTTP(adminRec, adminReq)
	admin := decodeConsoleBootstrap(t, adminRec)
	if !hasConsoleWorkspace(admin.Workspaces, "admin") {
		t.Fatalf("agentic admin workspaces = %+v, want admin", admin.Workspaces)
	}
	if admin.Identity.DisplayName != "admin-1" || !admin.Identity.Authenticated || admin.Identity.Role != "admin" {
		t.Fatalf("agentic admin identity = %+v", admin.Identity)
	}
}

func TestConsoleBootstrapAdvertisesTrainerWhenReportBackendExists(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "eval-state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	conf := &Config{Core: core.Config{Mode: "dev"}, Serv: Serv{EvalStateDir: stateDir}}
	hs := newAgentHTTPTestService(conf)
	req := httptest.NewRequest(http.MethodGet, routeConsoleBootstrap, nil)
	rec := httptest.NewRecorder()
	hs.ConsoleBootstrap(nil).ServeHTTP(rec, req)
	response := decodeConsoleBootstrap(t, rec)
	if !hasConsoleWorkspace(response.Workspaces, "trainer") {
		t.Fatalf("trainer workspace missing: %+v", response.Workspaces)
	}
	for _, workspace := range response.Workspaces {
		if workspace.ID == "trainer" && (workspace.DefaultPath != "/trainer/reports" || len(workspace.Capabilities) != 1 || workspace.Capabilities[0] != evalReportsCapability) {
			t.Fatalf("trainer workspace = %+v", workspace)
		}
	}
}

func TestConsoleBootstrapNamespaceAndMethod(t *testing.T) {
	hs := newAgentHTTPTestService(&Config{Core: core.Config{Mode: "dev"}})
	req := httptest.NewRequest(http.MethodGet, routeConsoleBootstrap, nil)
	rec := httptest.NewRecorder()
	hs.ConsoleBootstrapWithNS(nil, "tenant-a").ServeHTTP(rec, req)
	response := decodeConsoleBootstrap(t, rec)
	if response.Scope.Namespace != "tenant-a" {
		t.Fatalf("namespace = %q, want tenant-a", response.Scope.Namespace)
	}

	post := httptest.NewRequest(http.MethodPost, routeConsoleBootstrap, nil)
	postRec := httptest.NewRecorder()
	hs.ConsoleBootstrap(nil).ServeHTTP(postRec, post)
	if postRec.Code != http.StatusMethodNotAllowed || postRec.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("POST status/Allow = %d/%q, want 405/GET", postRec.Code, postRec.Header().Get("Allow"))
	}
}

func decodeConsoleBootstrap(t *testing.T, rec *httptest.ResponseRecorder) consoleBootstrapResponse {
	t.Helper()
	var response consoleBootstrapResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode console bootstrap: %v: %s", err, rec.Body.String())
	}
	return response
}

func hasConsoleWorkspace(workspaces []consoleBootstrapWorkspace, id string) bool {
	for _, workspace := range workspaces {
		if workspace.ID == id {
			return true
		}
	}
	return false
}
