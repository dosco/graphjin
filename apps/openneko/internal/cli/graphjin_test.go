package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-neko/neko/apps/openneko/internal/instance"
)

func TestGraphjinImportUpdate(t *testing.T) {
	update, err := graphjinImportUpdate([]byte("sources:\n  - name: customer\n    type: postgres\ntables:\n  - name: orders\n"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string][]map[string]any
	if err := json.Unmarshal(update, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["update_sources"][0]["name"] != "customer" || parsed["tables"][0]["name"] != "orders" {
		t.Fatalf("unexpected import update: %s", update)
	}
	for _, input := range []string{
		"auth:\n  type: none\n",
		"secrets:\n  keystore: {}\n",
		"sources: {}\n",
		"sources: []\nsources: []\n",
	} {
		if _, err := graphjinImportUpdate([]byte(input)); err == nil {
			t.Errorf("accepted unsafe or invalid import %q", strings.TrimSpace(input))
		}
	}
}

func TestGraphjinImportRefusesUnselectedInstallation(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv(instance.EnvName, "")
	file := filepath.Join(t.TempDir(), "customer.yml")
	if err := os.WriteFile(file, []byte("sources: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := NewRoot()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"graphjin", "import", file})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "selected production installation") {
		t.Fatalf("expected production installation guard, got %v", err)
	}
}
