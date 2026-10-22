package roles

import (
	"strings"
	"testing"

	gjserv "github.com/dosco/graphjin/serv/v3"
)

func TestRenderSubscriptionsConfigReadsAsGraphJinConfig(t *testing.T) {
	config, err := renderSubscriptionsConfig("neko-db:5432/neko", "0.0.0.0:8089", `p"w:d`)
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

func TestRenderSubscriptionsConfigRejectsBadInput(t *testing.T) {
	for _, target := range []string{"", "neko-db/neko", "neko-db:x/neko", "neko-db:5432", ":5432/neko"} {
		if _, err := renderSubscriptionsConfig(target, "0.0.0.0:8089", "pw"); err == nil || !strings.Contains(err.Error(), subscriptionsDBEnv) {
			t.Fatalf("target %q: err = %v", target, err)
		}
	}
	if _, err := renderSubscriptionsConfig("neko-db:5432/neko", "0.0.0.0:8089", ""); err == nil {
		t.Fatal("empty password accepted")
	}
}
