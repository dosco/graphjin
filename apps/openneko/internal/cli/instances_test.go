package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-neko/neko/apps/openneko/internal/config"
	"github.com/open-neko/neko/apps/openneko/internal/installation"
	"github.com/open-neko/neko/apps/openneko/internal/instance"
)

func TestConfiguredInstallationsListsNamedInstances(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv(instance.EnvName, "")
	for _, settings := range []installation.Settings{
		{Version: 1, Instance: "acme", Mode: "prod", WebPort: 3100, WebBindAddress: "127.0.0.1"},
		{Version: 1, Instance: "globex", Mode: "prod", WebPort: 3101, WebBindAddress: "127.0.0.1"},
	} {
		dir := filepath.Join(config.RootDir(), "instances", settings.Instance)
		if err := installation.Save(dir, settings); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := configuredInstallations()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Name != "acme" || entries[1].Name != "globex" {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestInstancesCommandOutput(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv(instance.EnvName, "")
	settings := installation.Settings{Version: 1, Instance: "acme", Mode: "prod", WebPort: 3100, WebBindAddress: "127.0.0.1"}
	if err := installation.Save(filepath.Join(config.RootDir(), "instances", "acme"), settings); err != nil {
		t.Fatal(err)
	}
	cmd := newInstancesCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"acme", "127.0.0.1:3100", "openneko-acme-prod"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, out.String())
		}
	}
}
