package serv

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	core "github.com/dosco/graphjin/core/v3"
)

func newTestHttpService(t *testing.T, databaseSchemas map[string]string) *HttpService {
	t.Helper()

	gj, err := core.NewTestGraphJin(databaseSchemas)
	if err != nil {
		t.Fatalf("create test GraphJin: %v", err)
	}

	svc := &graphjinService{gj: gj}
	hs := &HttpService{}
	hs.Store(svc)
	return hs
}

// TestAdminTableSchema_WithDatabaseParam verifies that passing ?database=
// disambiguates a table name that exists in multiple databases.
func TestAdminTableSchema_WithDatabaseParam(t *testing.T) {
	hs := newTestHttpService(t, map[string]string{
		"primary":   "default",
		"analytics": "default",
	})

	handler := adminTableSchemaHandler(hs)

	// Without database param → ambiguity error (users exists in both)
	req := httptest.NewRequest("GET", "/api/v1/admin/tables/users", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for ambiguous table, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "multiple databases") {
		t.Fatalf("expected ambiguity error, got: %s", rec.Body.String())
	}

	// With database param → success
	req = httptest.NewRequest("GET", "/api/v1/admin/tables/users?database=primary", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with database param, got %d: %s", rec.Code, rec.Body.String())
	}

	var schema core.TableSchema
	if err := json.Unmarshal(rec.Body.Bytes(), &schema); err != nil {
		t.Fatalf("failed to parse schema response: %v", err)
	}
	if schema.Database != "primary" {
		t.Fatalf("expected database primary, got %q", schema.Database)
	}
}

// TestAdminTableSchema_WithoutDatabaseParam_NoConflict verifies that omitting
// ?database= works when the table name is unique across all databases.
func TestAdminTableSchema_WithoutDatabaseParam_NoConflict(t *testing.T) {
	hs := newTestHttpService(t, map[string]string{
		"primary":   "default",
		"secondary": "with_database",
	})

	handler := adminTableSchemaHandler(hs)

	// "events" only exists in secondary → should resolve without database param
	req := httptest.NewRequest("GET", "/api/v1/admin/tables/events", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for unique table, got %d: %s", rec.Code, rec.Body.String())
	}

	var schema core.TableSchema
	if err := json.Unmarshal(rec.Body.Bytes(), &schema); err != nil {
		t.Fatalf("failed to parse schema response: %v", err)
	}
	if schema.Database != "secondary" {
		t.Fatalf("expected database secondary, got %q", schema.Database)
	}
}
