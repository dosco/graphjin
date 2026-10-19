package installation

import (
	"os"
	"path/filepath"
	"testing"
)

func sampleSettings() Settings {
	return Settings{
		Version:        CurrentVersion,
		Instance:       "acme",
		Mode:           "prod",
		WebPort:        3100,
		WebBindAddress: "127.0.0.1",
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := sampleSettings()
	if err := Save(dir, want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := Load(dir)
	if err != nil || !ok {
		t.Fatalf("Load = %+v, %v, %v", got, ok, err)
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	info, err := os.Stat(filepath.Join(dir, Filename))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("settings mode = %v", info.Mode().Perm())
	}
}

func TestSettingsValidation(t *testing.T) {
	settings := sampleSettings()
	settings.WebPort = 0
	if err := settings.Validate(); err == nil {
		t.Fatal("expected invalid port error")
	}
	settings = sampleSettings()
	settings.WebBindAddress = "::1"
	if err := settings.Validate(); err == nil {
		t.Fatal("expected IPv4 bind address error")
	}
}

// Settings written before the agent runtime moved into core.yml carry the
// OpenShell gateway port and a fixed Docker subnet; they still load.
func TestSettingsLoadIgnoresRetiredFields(t *testing.T) {
	dir := t.TempDir()
	raw := `{"version":1,"instance":"acme","mode":"prod","web_port":3100,"web_bind_address":"127.0.0.1","openshell_port":18100,"docker_subnet":"10.224.1.0/24"}`
	if err := os.WriteFile(filepath.Join(dir, Filename), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	got, ok, err := Load(dir)
	if err != nil || !ok || got != sampleSettings() {
		t.Fatalf("Load = %+v, %v, %v", got, ok, err)
	}
}

func TestApplyEnvironmentPreservesExplicitOverrides(t *testing.T) {
	t.Setenv("OPENNEKO_PORT", "3999")
	t.Setenv("OPENNEKO_WEB_BIND_ADDRESS", "")
	if err := ApplyEnvironment(sampleSettings(), false); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("OPENNEKO_PORT"); got != "3999" {
		t.Fatalf("explicit web port replaced with %q", got)
	}
	if got := os.Getenv("OPENNEKO_WEB_BIND_ADDRESS"); got != "127.0.0.1" {
		t.Fatalf("persisted bind address not applied: %q", got)
	}
}
