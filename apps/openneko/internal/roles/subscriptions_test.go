package roles

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	gjserv "github.com/dosco/graphjin/serv/v3"
	"github.com/jackc/pgx/v5"
)

func TestRenderSubscriptionsConfigReadsAsGraphJinConfig(t *testing.T) {
	url := "postgres://neko_subscriptions:p%22w%3Ad@db.example:6432/neko?sslmode=require"
	config, err := renderSubscriptionsConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GJ_DATABASE_HOST", "adventureworks-db")
	conf, err := gjserv.NewConfig(config, "yaml")
	if err != nil {
		t.Fatal(err)
	}
	db, ok := conf.Databases["neko"]
	if !ok {
		t.Fatalf("databases = %v, want neko", conf.Databases)
	}
	if db.ConnString != url || db.DBName != "neko" || db.Host != "" {
		t.Fatalf("database = %+v", db)
	}
	if conf.WatchAndReload {
		t.Fatal("reload_on_config_change would re-execute serve")
	}
}

func TestRenderSubscriptionsConfigRejectsOtherRoles(t *testing.T) {
	for _, url := range []string{"", "postgres://neko:secret@neko-db:5432/neko", "not a url"} {
		if _, err := renderSubscriptionsConfig(url); err == nil {
			t.Fatalf("%q accepted", url)
		}
	}
}

func TestReadSubscriptionsURL(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, subscriptionsPassword), []byte("p@ss:w\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	url, err := readSubscriptionsURL(dir)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := pgx.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Host != "neko-db" || parsed.Port != 5432 || parsed.User != "neko_subscriptions" || parsed.Password != "p@ss:w" || parsed.Database != "neko" {
		t.Fatalf("fallback URL = %+v", parsed)
	}

	want := "postgres://neko_subscriptions:pw@db.example:5432/neko?sslmode=verify-full"
	if err := os.WriteFile(filepath.Join(dir, subscriptionsURL), []byte(want+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := readSubscriptionsURL(dir); err != nil || got != want {
		t.Fatalf("subscriptions.url should win: %q %v", got, err)
	}
}

func TestSubscriptionsServerAnswers503UntilGraphJinIsUp(t *testing.T) {
	s := &subscriptionsServer{dir: t.TempDir()}
	s.refresh() // no password file yet: stays down, never panics
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/graphql", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 while no GraphJin is up", rec.Code)
	}
	if s.lastErr == "" {
		t.Fatal("missing password file was not reported")
	}
}
