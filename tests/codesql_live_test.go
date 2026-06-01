package tests_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/serv/v3"
)

func TestCodeSQLServiceLiveIndexAndWatch(t *testing.T) {
	sourceRoot := t.TempDir()
	configRoot := filepath.Join(t.TempDir(), "config")

	writeCodeSQLFixture(t, filepath.Join(sourceRoot, "main.go"), `package main

import "fmt"

// Handler is the entrypoint.
func Handler() {
	fmt.Println("ready")
}
`)

	gjs, err := serv.NewGraphJinService(&serv.Config{
		Core: core.Config{
			DisableAllowList: true,
			Sources: []core.SourceConfig{
				{Name: "code", Kind: "code", Path: sourceRoot},
			},
		},
		Serv: serv.Serv{
			ConfigPath: configRoot,
			MCP:        serv.MCPConfig{Disable: true},
		},
	})
	if err != nil {
		t.Fatalf("new codesql service: %v", err)
	}
	t.Cleanup(func() { _ = gjs.Close() })

	matches, err := filepath.Glob(filepath.Join(configRoot, "codesql", "code-*.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one codesql cache with database-name prefix, got %d: %v", len(matches), matches)
	}

	h := gjs.GraphQL(nil)
	assertCodeSQLSymbol(t, h, "Handler", "function")

	writeCodeSQLFixture(t, filepath.Join(sourceRoot, "worker.ts"), `export function watchedWorker() {
  return 42;
}
`)
	waitForCodeSQLSymbol(t, h, "watchedWorker", "function")
}

func TestMetadataGraphLiveLinksCodeSQLRefs(t *testing.T) {
	appPath := createLiveMetadataAppDB(t)
	sourceRoot := t.TempDir()
	configRoot := filepath.Join(t.TempDir(), "config")

	writeCodeSQLFixture(t, filepath.Join(sourceRoot, "main.go"), `package main

func LookupUser() {
	query := "SELECT users.email FROM users WHERE users.id = ?"
	_ = query
}
`)

	gjs, err := serv.NewGraphJinService(&serv.Config{
		Core: core.Config{
			DisableAllowList: true,
			Sources: []core.SourceConfig{
				{Name: "app", Kind: "database", Type: "sqlite", Path: appPath, Default: true, Access: core.SourceAccessConfig{
					Read: core.AccessModeAuthenticated,
				}},
				{Name: "code", Kind: "code", Path: sourceRoot},
				{Name: "graphjin", Kind: "graphjin"},
			},
		},
		Serv: serv.Serv{
			ConfigPath: configRoot,
			MCP:        serv.MCPConfig{Disable: true},
		},
	})
	if err != nil {
		t.Fatalf("new metadata/codesql service: %v", err)
	}
	t.Cleanup(func() { _ = gjs.Close() })

	resData, err := gjs.GetGraphJin().GraphQL(sourceModeIntegrationUserContext(), `query {
		gj_catalog(where: { kind: { eq: "column" }, table_name: { eq: "users" }, column_name: { eq: "email" } }, limit: 1) {
			table_name
			column_name
			gj_code { kind ref_kind path symbol_id }
		}
	}`, nil, nil)
	if err != nil {
		t.Fatalf("metadata graphql query: %v", err)
	}

	var out struct {
		Data struct {
			GJCatalog []struct {
				TableName  string `json:"table_name"`
				ColumnName string `json:"column_name"`
				GJCode     []struct {
					Kind     string `json:"kind"`
					RefKind  string `json:"ref_kind"`
					Path     string `json:"path"`
					SymbolID string `json:"symbol_id"`
				} `json:"gj_code"`
			} `json:"gj_catalog"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	res, err := json.Marshal(resData)
	if err != nil {
		t.Fatalf("marshal metadata response: %v", err)
	}
	if err := json.Unmarshal(res, &out); err != nil {
		t.Fatalf("decode metadata response: %v\n%s", err, string(res))
	}
	if len(out.Errors) != 0 {
		t.Fatalf("graphql errors: %+v", out.Errors)
	}
	if len(out.Data.GJCatalog) != 1 {
		t.Fatalf("gj_catalog len = %d, want 1: %s", len(out.Data.GJCatalog), string(res))
	}
	col := out.Data.GJCatalog[0]
	if col.TableName != "users" || col.ColumnName != "email" {
		t.Fatalf("column = %s.%s, want users.email", col.TableName, col.ColumnName)
	}
	for _, ref := range col.GJCode {
		if ref.Kind == "db_reference" && ref.RefKind == "sql_string" && ref.Path == "main.go" && ref.SymbolID != "" {
			return
		}
	}
	t.Fatalf("missing sql_string ref to LookupUser in response: %s", string(res))
}

func assertCodeSQLSymbol(t *testing.T, h http.Handler, name, kind string) {
	t.Helper()
	gotKind := queryCodeSQLSymbol(t, h, name)
	if gotKind != kind {
		t.Fatalf("symbol %q kind = %q, want %q", name, gotKind, kind)
	}
}

func waitForCodeSQLSymbol(t *testing.T, h http.Handler, name, kind string) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	var gotKind string
	for time.Now().Before(deadline) {
		gotKind = queryCodeSQLSymbol(t, h, name)
		if gotKind == kind {
			return
		}
		time.Sleep(75 * time.Millisecond)
	}
	t.Fatalf("symbol %q kind = %q after watcher wait, want %q", name, gotKind, kind)
}

func queryCodeSQLSymbol(t *testing.T, h http.Handler, name string) string {
	t.Helper()
	body := []byte(`{"query":"query($name: String!) { gj_code(where: { kind: { eq: \"symbol\" }, name: { eq: $name } }, limit: 1) { name symbol_kind language } }","variables":{"name":` + strconvQuote(name) + `}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("graphql status = %d, body = %s", res.Code, res.Body.String())
	}

	var out struct {
		Data struct {
			GJCode []struct {
				Name string `json:"name"`
				Kind string `json:"symbol_kind"`
			} `json:"gj_code"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode graphql response: %v\n%s", err, res.Body.String())
	}
	if len(out.Errors) != 0 {
		t.Fatalf("graphql errors: %+v", out.Errors)
	}
	if len(out.Data.GJCode) == 0 {
		return ""
	}
	return out.Data.GJCode[0].Kind
}

func postGraphQL(t *testing.T, h http.Handler, body []byte) []byte {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("graphql status = %d, body = %s", res.Code, res.Body.String())
	}
	return res.Body.Bytes()
}

func createLiveMetadataAppDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE users (
		id integer primary key,
		email text not null unique
	);`); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeCodeSQLFixture(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
