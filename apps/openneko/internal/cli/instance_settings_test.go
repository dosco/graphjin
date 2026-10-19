package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-neko/neko/apps/openneko/internal/config"
	"github.com/open-neko/neko/apps/openneko/internal/installation"
	"github.com/open-neko/neko/apps/openneko/internal/instance"
)

func isolateNamedInstallation(t *testing.T, name string) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("OPENNEKO_WORKSPACE_ROOT", "")
	t.Setenv("OPENNEKO_PORT", "")
	t.Setenv("OPENNEKO_WEB_BIND_ADDRESS", "")
	t.Setenv(instance.EnvName, name)
}

func TestPrepareSetupInstallationPersistsExplicitInstanceSettings(t *testing.T) {
	isolateNamedInstallation(t, "acme")
	cmd := newSetupCmd()
	for name, value := range map[string]string{
		"mode":         "prod",
		"port":         "3100",
		"bind-address": "127.0.0.1",
	} {
		if err := cmd.Flags().Set(name, value); err != nil {
			t.Fatal(err)
		}
	}
	settings, err := prepareSetupInstallation(cmd, "prod", setupHostOptions{
		webPort:        3100,
		webPortChanged: true,
		webBindAddress: "127.0.0.1",
		webBindChanged: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if settings.Instance != "acme" || settings.WebPort != 3100 {
		t.Fatalf("settings = %+v", settings)
	}
	readBack, ok, err := installation.Load(config.Dir(""))
	if err != nil || !ok || readBack != settings {
		t.Fatalf("readBack = %+v, ok=%v, err=%v", readBack, ok, err)
	}
	if got := os.Getenv("OPENNEKO_PORT"); got != "3100" {
		t.Fatalf("OPENNEKO_PORT = %q", got)
	}
}

func TestActivateInstallationRestoresPersistedPorts(t *testing.T) {
	isolateNamedInstallation(t, "acme")
	settings := installation.Settings{
		Version:        installation.CurrentVersion,
		Instance:       "acme",
		Mode:           "prod",
		WebPort:        3200,
		WebBindAddress: "127.0.0.1",
	}
	if err := installation.Save(config.Dir(""), settings); err != nil {
		t.Fatal(err)
	}
	t.Setenv(instance.EnvName, "")
	t.Setenv("OPENNEKO_PORT", "")
	if err := activateInstallation("acme"); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("OPENNEKO_PORT") != "3200" {
		t.Fatalf("persisted port not restored: web=%q", os.Getenv("OPENNEKO_PORT"))
	}
}

func TestActivateNamedInstallationDoesNotInheritAnotherStacksPorts(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(instance.EnvName, "acme")
	settings := installation.Settings{
		Version:        installation.CurrentVersion,
		Instance:       "acme",
		Mode:           "prod",
		WebPort:        3100,
		WebBindAddress: "127.0.0.1",
	}
	if err := installation.Save(config.Dir(""), settings); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENNEKO_PORT", "3999")
	if err := activateInstallation("acme"); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("OPENNEKO_PORT"); got != "3100" {
		t.Fatalf("web port = %q, want persisted 3100", got)
	}
}

func TestModeForStartRequiresNamedSetup(t *testing.T) {
	isolateNamedInstallation(t, "not-installed")
	cmd := newStartCmd()
	_, err := modeForStart(cmd, "prod")
	if err == nil || !strings.Contains(err.Error(), "openneko setup --instance not-installed") {
		t.Fatalf("error = %v", err)
	}
}

func TestModeForStartUsesPersistedMode(t *testing.T) {
	isolateNamedInstallation(t, "demo-customer")
	settings := installation.Settings{
		Version:        installation.CurrentVersion,
		Instance:       "demo-customer",
		Mode:           "demo",
		WebPort:        3300,
		WebBindAddress: "127.0.0.1",
	}
	if err := installation.Save(config.Dir(""), settings); err != nil {
		t.Fatal(err)
	}
	cmd := newStartCmd()
	mode, err := modeForStart(cmd, "prod")
	if err != nil || string(mode) != "demo" {
		t.Fatalf("mode=%q err=%v", mode, err)
	}
}

func TestSetupRejectsPortReservedByStoppedInstance(t *testing.T) {
	isolateNamedInstallation(t, "globex")
	acme := installation.Settings{
		Version:        1,
		Instance:       "acme",
		Mode:           "prod",
		WebPort:        3100,
		WebBindAddress: "127.0.0.1",
	}
	if err := installation.Save(filepath.Join(config.RootDir(), "instances", "acme"), acme); err != nil {
		t.Fatal(err)
	}
	cmd := newSetupCmd()
	if err := cmd.Flags().Set("port", "3100"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("bind-address", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	_, err := prepareSetupInstallation(cmd, "prod", setupHostOptions{
		webPort:        3100,
		webPortChanged: true,
		webBindAddress: "127.0.0.1",
		webBindChanged: true,
	})
	if err == nil || !strings.Contains(err.Error(), "reserved by instance acme") {
		t.Fatalf("error = %v", err)
	}
}

func TestNamedRuntimeAndConfigDoNotSharePaths(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(base, "state"))
	t.Setenv(instance.EnvName, "acme")
	acmeConfig := config.Dir("")
	if err := instance.Select("globex"); err != nil {
		t.Fatal(err)
	}
	globexConfig := config.Dir("")
	if acmeConfig == globexConfig {
		t.Fatalf("instances share config path %q", acmeConfig)
	}
}
