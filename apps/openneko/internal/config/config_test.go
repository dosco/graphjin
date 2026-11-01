package config

import (
	"path/filepath"
	"testing"

	"github.com/open-neko/neko/apps/openneko/internal/instance"
)

func TestDirOverride(t *testing.T) {
	if got := Dir("/tmp/foo"); got != "/tmp/foo" {
		t.Fatalf("override should win, got %q", got)
	}
}

func TestDirXDG(t *testing.T) {
	t.Setenv(instance.EnvName, "")
	t.Setenv("XDG_CONFIG_HOME", "/var/cfg")
	if got := Dir(""); got != filepath.Join("/var/cfg", "openneko") {
		t.Fatalf("expected XDG-derived path, got %q", got)
	}
}

func TestDirFallback(t *testing.T) {
	t.Setenv(instance.EnvName, "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/test")
	if got := Dir(""); got != "/home/test/.config/openneko" {
		t.Fatalf("expected HOME-derived path, got %q", got)
	}
}

func TestDirScopesNamedInstance(t *testing.T) {
	t.Setenv(instance.EnvName, "acme")
	t.Setenv("XDG_CONFIG_HOME", "/var/cfg")
	want := filepath.Join("/var/cfg", "openneko", "instances", "acme")
	if got := Dir(""); got != want {
		t.Fatalf("named config path = %q, want %q", got, want)
	}
	if got := Dir("/explicit"); got != "/explicit" {
		t.Fatalf("explicit override was scoped: %q", got)
	}
}

func TestRootDirIsNeverInstanceScoped(t *testing.T) {
	t.Setenv(instance.EnvName, "acme")
	t.Setenv("XDG_CONFIG_HOME", "/var/cfg")
	if got := RootDir(); got != filepath.Join("/var/cfg", "openneko") {
		t.Fatalf("root config path = %q", got)
	}
}

func TestFile(t *testing.T) {
	got := File("/etc/openneko", "secrets.json")
	if got != "/etc/openneko/secrets.json" {
		t.Fatalf("unexpected path: %q", got)
	}
}
