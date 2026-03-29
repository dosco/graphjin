package serv

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/dosco/graphjin/auth/v3"
	core "github.com/dosco/graphjin/core/v3"
	"github.com/go-chi/chi/v5"
	"github.com/spf13/afero"
	"go.uber.org/zap"
	_ "modernc.org/sqlite"
)

func newSecuredTestHandler(t *testing.T) http.Handler {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.sqlite3")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
	})

	for _, stmt := range []string{
		`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)`,
		`INSERT INTO users (id, name) VALUES (1, 'Ada')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("exec %q: %v", stmt, err)
		}
	}

	logger := zap.NewNop()
	fs := newAferoFS(afero.NewMemMapFs(), "/")
	if err := fs.Put("/queries/listUsers.gql", []byte(`query listUsers { users { id name } }`)); err != nil {
		t.Fatalf("write query file: %v", err)
	}

	coreConf := core.Config{
		DBType:     "sqlite",
		Production: false,
	}
	gj, err := core.NewGraphJin(&coreConf, db,
		core.OptionSetFS(fs),
		core.OptionSetDatabases(map[string]*sql.DB{core.DefaultDBName: db}))
	if err != nil {
		t.Fatalf("create test GraphJin: %v", err)
	}

	svc := &graphjinService{
		gj:   gj,
		log:  logger.Sugar(),
		zlog: logger,
		conf: &Config{
			Serv: Serv{
				WebUI: true,
				Auth: auth.Auth{
					Type: "header",
					Header: struct {
						Name   string
						Value  string
						Exists bool
					}{
						Name:  "X-Test-Auth",
						Value: "secret",
					},
				},
			},
		},
	}

	hs := &HttpService{}
	hs.Store(svc)

	router := chi.NewRouter()
	handler, err := routesHandler(hs, router, nil)
	if err != nil {
		t.Fatalf("routes handler: %v", err)
	}

	return handler
}

func TestAdminAndOpenAPIRoutesRequireAuth(t *testing.T) {
	handler := newSecuredTestHandler(t)

	paths := []string{
		"/api/v1/admin/tables",
		"/api/v1/admin/queries",
		"/api/v1/admin/config",
		"/api/v1/openapi.json",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401 without auth for %s, got %d", path, rec.Code)
			}

			req = httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("X-Test-Auth", "secret")
			rec = httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200 with auth for %s, got %d: %s", path, rec.Code, rec.Body.String())
			}
		})
	}
}
