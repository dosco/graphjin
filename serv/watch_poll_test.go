package serv

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
)

func TestNormalizeWatchPollJSON(t *testing.T) {
	if out, _, ok, err := normalizeWatchPollJSON(""); err != nil || ok || out != "" {
		t.Fatalf("empty poll_json = %q %v %v", out, ok, err)
	}
	out, cfg, ok, err := normalizeWatchPollJSON(`{"interval_seconds":300,"variables":{"since":{"kind":"seconds_ago","seconds":3600},"now":{"kind":"now"}}}`)
	if err != nil || !ok || cfg.Interval != 5*time.Minute || len(cfg.Variables) != 2 {
		t.Fatalf("poll_json = %q %+v %v %v", out, cfg, ok, err)
	}
	for _, bad := range []string{
		`{"interval_seconds":1}`,
		`{"interval_seconds":999999}`,
		`{"interval_seconds":60,"variables":{"x":{"kind":"tomorrow"}}}`,
		`{"interval_seconds":60,"variables":{"x":{"kind":"seconds_ago"}}}`,
		`{"interval_seconds":60,"cron":"* * * * *"}`,
	} {
		if _, _, _, err := normalizeWatchPollJSON(bad); err == nil {
			t.Errorf("expected %s to be rejected", bad)
		}
	}

	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	vars, err := cfg.pollVariables(`{"limit":5}`, now)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(vars, &got); err != nil {
		t.Fatal(err)
	}
	if got["since"] != "2026-10-19T11:00:00Z" || got["now"] != "2026-10-19T12:00:00Z" || got["limit"] != float64(5) {
		t.Fatalf("poll variables = %v", got)
	}
}

func TestPolledWatchRecordsEachChangedResult(t *testing.T) {
	db, svc := newSQLiteWatchService(t, 20)
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)
	cp := newWatchControlPlane(svc)
	ctx := contextWithUserRole(artifactUserCtx("user_1"), "analyst")

	// An aggregate has no cursor, so only a polled watch accepts it.
	query := `query open_orders { orders_aggregate: orders(where: { status: { eq: "open" } }) { count_id } }`
	if _, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: watchesRootTable, Operation: "insert",
		Input: map[string]interface{}{"name": "open_orders_sub", "query": "subscription open_orders { orders { id } }"},
	}); err == nil || !strings.Contains(err.Error(), "cursor") {
		t.Fatalf("a subscription without a cursor should still need poll_json, got %v", err)
	}
	row, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: watchesRootTable, Operation: "insert",
		Input: map[string]interface{}{
			"name":      "open_orders",
			"query":     query,
			"poll_json": map[string]interface{}{"interval_seconds": 60},
		},
	})
	if err != nil {
		t.Fatalf("insert polled watch: %v", err)
	}
	watchID := row["id"].(string)

	defs, err := svc.loadRunnableWatches(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var def watchRuntimeDefinition
	for _, d := range defs {
		if d.ID == watchID {
			def = d
		}
	}
	if def.ID == "" || def.PollJSON == "" {
		t.Fatalf("polled watch not runnable: %+v", defs)
	}
	_, cfg, _, _ := normalizeWatchPollJSON(def.PollJSON)

	start := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	poll := func(minute int) {
		t.Helper()
		if err := svc.pollWatchOnce(ctx, &def, cfg, start.Add(time.Duration(minute)*time.Minute)); err != nil {
			t.Fatalf("poll at minute %d: %v", minute, err)
		}
	}
	events := func() int {
		t.Helper()
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM "_graphjin_watch_events" WHERE watch_id = ?`, watchID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	poll(0) // count 0
	poll(0) // a retry of the same poll
	if got := events(); got != 1 {
		t.Fatalf("events after first poll = %d, want 1", got)
	}
	if _, err := db.Exec(`INSERT INTO orders (id, status) VALUES (1, 'open')`); err != nil {
		t.Fatal(err)
	}
	poll(1) // count 1
	poll(2) // unchanged
	if _, err := db.Exec(`DELETE FROM orders WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	poll(3) // back to count 0, an earlier result
	if got := events(); got != 3 {
		t.Fatalf("events = %d, want 3 (0, 1, back to 0)", got)
	}
}

func TestWatchWebhookKeystoreSecret(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	svc := &graphjinService{conf: &Config{}}
	if _, err := svc.watchWebhookKeystoreSecret("watch:1"); err == nil {
		t.Fatal("expected an error without a keystore key")
	}
	svc.conf.Secrets.Keystore.Key = base64.StdEncoding.EncodeToString(key)
	got, err := svc.watchWebhookKeystoreSecret("watch:1")
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("graphjin-watch-webhook:v1:watch:1"))
	if want := hex.EncodeToString(mac.Sum(nil)); got != want {
		t.Fatalf("secret = %s, want %s", got, want)
	}

	if _, cfg, _, err := normalizeWatchDeliveryJSON(`{"kind":"webhook","url":"https://hooks.example.com/x","secret_source":"keystore"}`); err != nil || cfg.Webhook.SecretSource != "keystore" {
		t.Fatalf("keystore secret_source = %+v %v", cfg.Webhook, err)
	}
	for _, bad := range []string{
		`{"kind":"webhook","url":"https://hooks.example.com/x","secret_source":"vault"}`,
		`{"kind":"webhook","url":"https://hooks.example.com/x","secret_source":"keystore","secret_env":"X"}`,
	} {
		if _, _, _, err := normalizeWatchDeliveryJSON(bad); err == nil {
			t.Errorf("expected %s to be rejected", bad)
		}
	}
}

func TestManagedStoreIsWritableBesideReadOnlySources(t *testing.T) {
	conf := &Config{Core: core.Config{
		Sources:   []core.SourceConfig{{Name: "shop", Kind: "database", Type: "sqlite", Default: true, ReadOnly: true}},
		Artifacts: core.ArtifactsConfig{Enabled: true},
	}}
	if !conf.artifactSourceReadOnly() {
		t.Fatal("artifacts on a read-only source should be read-only")
	}
	conf.managedArtifactStore = true
	if conf.artifactSourceReadOnly() {
		t.Fatal("the managed store is GraphJin's own file and stays writable")
	}
}
