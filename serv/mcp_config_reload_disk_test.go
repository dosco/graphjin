package serv

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
