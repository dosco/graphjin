package tests_test

import (
	"bytes"
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
			Databases: map[string]core.DatabaseConfig{
				"code": {
					Type: "codesql",
					Path: sourceRoot,
				},
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
	body := []byte(`{"query":"query($name: String!) { code_symbols(where: { name: { eq: $name } }, limit: 1) { name kind language } }","variables":{"name":` + strconvQuote(name) + `}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("graphql status = %d, body = %s", res.Code, res.Body.String())
	}

	var out struct {
		Data struct {
			CodeSymbols []struct {
				Name string `json:"name"`
				Kind string `json:"kind"`
			} `json:"code_symbols"`
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
	if len(out.Data.CodeSymbols) == 0 {
		return ""
	}
	return out.Data.CodeSymbols[0].Kind
}

func writeCodeSQLFixture(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
