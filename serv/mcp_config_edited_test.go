package serv

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func editedConfigService(t *testing.T) (*mcpServer, string, func(read, extra string) string) {
	t.Helper()
	mainPath := createSQLiteDBFile(t, "main.sqlite3", true)
	confPath := filepath.Join(t.TempDir(), "agentic.yml")
	body := func(read, extra string) string {
		return fmt.Sprintf("disable_allow_list: true\nlog_level: info\nmcp:\n  allow_config_updates: true\nsources:\n  - name: main\n    kind: database\n    type: sqlite\n    path: %s\n    default: true\n    access:\n      read: %s\n%s", mainPath, read, extra)
	}
	if err := os.WriteFile(confPath, []byte(body("authenticated", "")), 0o600); err != nil {
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
	return &mcpServer{service: svc, ctx: context.Background(), readOnlyDBs: map[string]bool{}}, confPath, body
}

func TestHandleUpdateCurrentConfig_ConfigYAMLAppliesDataSectionsLive(t *testing.T) {
	ms, confPath, body := editedConfigService(t)
	before, _ := os.ReadFile(confPath)

	out := applySourceModeConfigUpdate(t, ms, map[string]any{"config_yaml": body("admin", "")})
	if !out.Success || !out.Applied {
		t.Fatalf("expected the edited config to apply, got %+v", out)
	}
	if got := ms.service.conf.Core.Sources[0].Access.Read; got != "admin" {
		t.Fatalf("access.read after the edit = %q, want admin", got)
	}
	if after, _ := os.ReadFile(confPath); string(after) != string(before) {
		t.Fatalf("GraphJin wrote the config file; the caller owns that write:\n%s", after)
	}
}

func TestHandleUpdateCurrentConfig_ConfigYAMLRoutesServerSettings(t *testing.T) {
	ms, _, body := editedConfigService(t)

	// A live server setting goes through the serv rules.
	out := applySourceModeConfigUpdate(t, ms, map[string]any{"config_yaml": body("authenticated", "agent:\n  enabled: true\n  max_steps: 4\n")})
	if !out.Success || !out.Applied {
		t.Fatalf("expected the agent edit to apply, got %+v", out)
	}
	if !ms.service.conf.Agent.Enabled || ms.service.conf.Agent.MaxSteps != 4 {
		t.Fatalf("agent after the edit = %+v", ms.service.conf.Agent)
	}

	// A setting GraphJin can only take at startup is refused, not left
	// waiting for a restart.
	refused := applyConfigUpdate(t, ms, map[string]any{
		"config_yaml":               body("authenticated", "host_port: 0.0.0.0:9999\n"),
		"mode":                      "preview",
		"expected_catalog_revision": ms.currentConfigCatalogRevision(context.Background()),
	})
	if refused.Success && refused.Valid {
		t.Fatalf("a changed host_port should be refused, got %+v", refused)
	}
	if !strings.Contains(strings.Join(refused.Errors, " "), "host_port cannot be changed while GraphJin runs") {
		t.Fatalf("errors = %v", refused.Errors)
	}
}

func TestHandleUpdateCurrentConfig_ConfigYAMLRejectsBadInput(t *testing.T) {
	ms, _, body := editedConfigService(t)
	rev := ms.currentConfigCatalogRevision(context.Background())
	for name, args := range map[string]map[string]any{
		"invalid yaml":   {"config_yaml": "sources: [", "mode": "preview", "expected_catalog_revision": rev},
		"combined":       {"config_yaml": body("admin", ""), "roles": []any{map[string]any{"name": "x"}}, "mode": "preview", "expected_catalog_revision": rev},
		"removed server": {"config_yaml": strings.Replace(body("admin", ""), "log_level: info\n", "", 1), "mode": "preview", "expected_catalog_revision": rev},
	} {
		if out := applyConfigUpdate(t, ms, args); out.Success && out.Valid {
			t.Fatalf("%s: expected a rejection, got %+v", name, out)
		}
	}
	if got := ms.service.conf.Core.Sources[0].Access.Read; got != "authenticated" {
		t.Fatalf("a rejected edit changed the running config: access.read = %q", got)
	}
}

func TestCoreConfigKeysCoverDataSections(t *testing.T) {
	keys := coreConfigKeys()
	for _, key := range []string{"sources", "databases", "tables", "roles", "functions", "resolvers", "blocklist"} {
		if !keys[key] {
			t.Errorf("%s is not a core key", key)
		}
	}
	for _, key := range []string{"agent", "host_port", "mcp", "catalog_search"} {
		if keys[key] {
			t.Errorf("%s should be a server key", key)
		}
	}
}
