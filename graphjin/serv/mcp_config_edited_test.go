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

func editedConfigHost(ms *mcpServer) *HttpService {
	hs := &HttpService{}
	hs.Store(ms.service)
	return hs
}

func editOK(out map[string]any) bool {
	return out["success"] == true && out["valid"] == true
}

func TestEditConfig_AppliesDataSectionsLive(t *testing.T) {
	ms, confPath, body := editedConfigService(t)
	before, _ := os.ReadFile(confPath)

	out := editedConfigHost(ms).EditConfig(context.Background(), body("admin", ""), true)
	if !editOK(out) || out["applied"] != true {
		t.Fatalf("expected the edited config to apply, got %+v", out)
	}
	if got := ms.service.conf.Core.Sources[0].Access.Read; got != "admin" {
		t.Fatalf("access.read after the edit = %q, want admin", got)
	}
	if after, _ := os.ReadFile(confPath); string(after) != string(before) {
		t.Fatalf("GraphJin wrote the config file; the caller owns that write:\n%s", after)
	}
}

func TestEditConfig_CheckChangesNothing(t *testing.T) {
	ms, _, body := editedConfigService(t)
	out := editedConfigHost(ms).EditConfig(context.Background(), body("admin", ""), false)
	if !editOK(out) || out["applied"] == true {
		t.Fatalf("expected a valid check that applies nothing, got %+v", out)
	}
	if got := ms.service.conf.Core.Sources[0].Access.Read; got != "authenticated" {
		t.Fatalf("a check changed the running config: access.read = %q", got)
	}
}

func TestEditConfig_RoutesServerSettings(t *testing.T) {
	ms, _, body := editedConfigService(t)
	host := editedConfigHost(ms)

	// A live server setting goes through the serv rules.
	out := host.EditConfig(context.Background(), body("authenticated", "agent:\n  enabled: true\n  max_steps: 4\n"), true)
	if !editOK(out) || out["applied"] != true {
		t.Fatalf("expected the agent edit to apply, got %+v", out)
	}
	if !ms.service.conf.Agent.Enabled || ms.service.conf.Agent.MaxSteps != 4 {
		t.Fatalf("agent after the edit = %+v", ms.service.conf.Agent)
	}

	// A setting GraphJin can only take at startup is refused, not left
	// waiting for a restart.
	refused := host.EditConfig(context.Background(), body("authenticated", "host_port: 0.0.0.0:9999\n"), false)
	if editOK(refused) {
		t.Fatalf("a changed host_port should be refused, got %+v", refused)
	}
	if !strings.Contains(fmt.Sprint(refused["errors"]), "host_port cannot be changed while GraphJin runs") {
		t.Fatalf("errors = %v", refused["errors"])
	}
}

func TestEditConfig_RejectsBadInput(t *testing.T) {
	ms, _, body := editedConfigService(t)
	host := editedConfigHost(ms)
	for name, text := range map[string]string{
		"invalid yaml":   "sources: [",
		"removed server": strings.Replace(body("admin", ""), "log_level: info\n", "", 1),
	} {
		if out := host.EditConfig(context.Background(), text, true); editOK(out) {
			t.Fatalf("%s: expected a rejection, got %+v", name, out)
		}
	}
	if got := ms.service.conf.Core.Sources[0].Access.Read; got != "authenticated" {
		t.Fatalf("a rejected edit changed the running config: access.read = %q", got)
	}
}

// config_yaml replaces the whole configuration, so only the host that owns
// the file may send it: gj_config over GraphQL or MCP refuses it.
func TestHandleUpdateCurrentConfig_RefusesConfigYAMLFromRequests(t *testing.T) {
	ms, _, body := editedConfigService(t)
	out := applyConfigUpdate(t, ms, map[string]any{
		"config_yaml":               body("admin", ""),
		"mode":                      "preview",
		"expected_catalog_revision": ms.currentConfigCatalogRevision(context.Background()),
	})
	if out.Success || !strings.Contains(strings.Join(out.Errors, " "), "config_yaml is not accepted here") {
		t.Fatalf("expected config_yaml to be refused, got %+v", out)
	}
	if got := ms.service.conf.Core.Sources[0].Access.Read; got != "authenticated" {
		t.Fatalf("a refused config_yaml changed the running config: access.read = %q", got)
	}
	for _, column := range configNanoColumns() {
		if column.Name == "config_yaml" {
			t.Fatal("gj_config exposes a config_yaml column")
		}
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
