package setup

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeriveEndpoints(t *testing.T) {
	cases := map[string]string{
		"http://openneko-serve:8080":                "http://openneko-serve:8080",
		"http://openneko-serve:8080/":               "http://openneko-serve:8080",
		"http://openneko-serve:8080/api/v1/graphql": "http://openneko-serve:8080",
		"http://openneko-serve:8080/api/v1/mcp":     "http://openneko-serve:8080",
		"http://host.docker.internal:8080//":        "http://host.docker.internal:8080",
		"HTTP://Up.Example/API/V1/GRAPHQL":          "HTTP://Up.Example", // case-insensitive suffix strip
	}
	for in, wantRoot := range cases {
		gql, mcp := DeriveEndpoints(in)
		if gql != wantRoot+graphqlSuffix {
			t.Errorf("%q: graphql = %q, want %q", in, gql, wantRoot+graphqlSuffix)
		}
		if mcp != wantRoot+mcpSuffix {
			t.Errorf("%q: mcp = %q, want %q", in, mcp, wantRoot+mcpSuffix)
		}
	}
}

func TestClientErrorSurfacing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "that password is too common"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	err := c.ChangePassword(context.Background(), "secret")
	if err == nil || err.Error() != "that password is too common" {
		t.Fatalf("want surfaced server error, got %v", err)
	}
}

func TestClientTestDataSourceMcpOk(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mcpOk := false
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "mcpOk": &mcpOk})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	ok, err := c.TestDataSource(context.Background(), "http://x/api/v1/graphql", "http://x/api/v1/mcp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected mcpOk=false to propagate")
	}
}

func TestClientReady(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/admin/change-password" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"changed": false})
	}))
	defer up.Close()

	if !NewClient(up.URL).Ready(context.Background()) {
		t.Fatal("expected Ready=true for a serving app")
	}
	// A closed server isn't reachable.
	down := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	addr := down.URL
	down.Close()
	if NewClient(addr).Ready(context.Background()) {
		t.Fatal("expected Ready=false for a down app")
	}
}

// An SSO-gated install answers 401/403 to an unauthenticated CLI probe.
// That is a SERVING stack: treating it as down made re-running setup burn
// the whole WaitReady budget and then fail with a misleading port error.
func TestReadyCountsAuthGatedResponses(t *testing.T) {
	for _, tc := range []struct {
		status int
		ready  bool
	}{
		{http.StatusOK, true},
		{http.StatusUnauthorized, true},
		{http.StatusForbidden, true},
		{http.StatusBadGateway, false},
		{http.StatusNotFound, false},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
		}))
		got := NewClient(srv.URL).Ready(context.Background())
		srv.Close()
		if got != tc.ready {
			t.Errorf("status %d: Ready = %v, want %v", tc.status, got, tc.ready)
		}
	}
}
