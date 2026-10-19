package serv

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHandleUpdateCurrentConfig_ReloadFromDisk(t *testing.T) {
	mainPath := createSQLiteDBFile(t, "main.sqlite3", true)
	confPath := filepath.Join(t.TempDir(), "agentic.yml")
	write := func(read string) {
		t.Helper()
		body := fmt.Sprintf("disable_allow_list: true\nmcp:\n  allow_config_updates: true\nsources:\n  - name: main\n    kind: database\n    type: sqlite\n    path: %s\n    default: true\n    access:\n      read: %s\n", mainPath, read)
		if err := os.WriteFile(confPath, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("authenticated")
	conf, err := readInConfig(confPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := newGraphJinService(conf, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeTestService(svc) })
	ms := &mcpServer{service: svc, ctx: context.Background(), readOnlyDBs: map[string]bool{}}

	// An unchanged file still reloads, since secrets may have changed.
	if unchanged := applySourceModeConfigUpdate(t, ms, map[string]any{"reload_from_disk": true}); !unchanged.Success || !unchanged.Applied {
		t.Fatalf("expected reload of an unchanged file to apply, got %+v", unchanged)
	}

	write("admin")
	out := applySourceModeConfigUpdate(t, ms, map[string]any{"reload_from_disk": true})
	if !out.Success || !out.Applied {
		t.Fatalf("expected reload from disk to apply, got %+v", out)
	}
	if got := ms.service.conf.Core.Sources[0].Access.Read; got != "admin" {
		t.Fatalf("access.read after reload = %q, want admin", got)
	}
	if data, _ := os.ReadFile(confPath); !strings.Contains(string(data), "read: admin") {
		t.Fatalf("reload rewrote the config file: %s", data)
	}

	combined := applyConfigUpdate(t, ms, map[string]any{
		"reload_from_disk":          true,
		"roles":                     []any{map[string]any{"name": "og_x"}},
		"mode":                      "preview",
		"expected_catalog_revision": ms.currentConfigCatalogRevision(context.Background()),
	})
	if combined.Success && combined.Valid {
		t.Fatalf("reload_from_disk combined with other changes should be rejected, got %+v", combined)
	}
}

func TestManagedStoreWatchAfterLiveConfigChange(t *testing.T) {
	mainPath := createSQLiteDBFile(t, "main.sqlite3", true)
	confPath := filepath.Join(t.TempDir(), "agentic.yml")
	body := fmt.Sprintf("disable_allow_list: true\nmcp:\n  allow_config_updates: true\nroles:\n  - name: analyst\nsources:\n  - name: main\n    kind: database\n    type: sqlite\n    path: %s\n    default: true\n    read_only: true\n    access:\n      read: authenticated\n", mainPath)
	if err := os.WriteFile(confPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	conf, err := readInConfig(confPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := newGraphJinService(conf, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeTestService(svc) })
	if !svc.conf.managedArtifactStore {
		t.Fatal("expected the managed artifact store")
	}
	ms := &mcpServer{service: svc, ctx: context.Background(), readOnlyDBs: map[string]bool{}}

	// A live change beside the read-only source must keep the managed store usable.
	out := applySourceModeConfigUpdate(t, ms, map[string]any{
		"source_patches": []any{map[string]any{"name": "main", "access": map[string]any{"read": "account", "namespace_column": "id"}}},
	})
	if !out.Success || !out.Applied {
		t.Fatalf("live change = %+v", out)
	}
	ctx := contextWithUserRole(artifactUserCtx("user_1"), "analyst")
	res, err := svc.gj.GraphQL(ctx, `mutation { gj_watch(insert: { name: "after_change", query: "query q { users { id } }", poll_json: { interval_seconds: 60 } }) { id status } }`, nil, nil)
	if err != nil {
		t.Fatalf("insert watch after a live config change: %v", err)
	}
	if !strings.Contains(string(res.Data), "watch:") {
		t.Fatalf("watch insert returned %s", res.Data)
	}
	// The runner, started before the change, still picks the new watch up.
	deadline := time.Now().Add(30 * time.Second)
	for {
		rows, err := svc.internalStoreAllRows(context.Background(), "watch_events", "", "id", nil)
		if err == nil && len(rows) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no watch event after a live config change (err=%v)", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
