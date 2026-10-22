package roles

import (
	"net/http"
	"net/http/httptest"
	"testing"

	gjserv "github.com/dosco/graphjin/serv/v3"
)

func TestRenderSubscriptionsConfigReadsAsGraphJinConfig(t *testing.T) {
	config, err := renderSubscriptionsConfig(`p"w:d`)
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
	if db.Host != "neko-db" || db.Port != 5432 || db.DBName != "neko" || db.User != "neko_subscriptions" || db.Password != `p"w:d` {
		t.Fatalf("database = %+v", db)
	}
	if conf.WatchAndReload {
		t.Fatal("reload_on_config_change would re-execute serve")
	}
}

func TestRenderSubscriptionsConfigRejectsEmptyPassword(t *testing.T) {
	if _, err := renderSubscriptionsConfig(""); err == nil {
		t.Fatal("empty password accepted")
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
