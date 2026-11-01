package instance

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	for _, name := range []string{"", "acme", "customer-12", "a"} {
		if err := Validate(name); err != nil {
			t.Fatalf("Validate(%q): %v", name, err)
		}
	}
	for _, name := range []string{"Acme", "-acme", "acme-", "acme--west", "acme_customer", "two words", strings.Repeat("a", 33)} {
		if err := Validate(name); err == nil {
			t.Fatalf("Validate(%q) unexpectedly succeeded", name)
		}
	}
}

func TestScopedDirectories(t *testing.T) {
	t.Setenv(EnvName, "acme")
	t.Setenv("XDG_STATE_HOME", "/var/state")
	if got := ScopeConfigDir("/var/config/openneko"); got != "/var/config/openneko/instances/acme" {
		t.Fatalf("config dir = %q", got)
	}
	got, ok, err := StateDir()
	if err != nil || !ok {
		t.Fatalf("StateDir = %q, %v, %v", got, ok, err)
	}
	if want := filepath.Join("/var/state", "openneko", "instances", "acme"); got != want {
		t.Fatalf("state dir = %q, want %q", got, want)
	}
}

func TestUnnamedInstanceKeepsLegacyPaths(t *testing.T) {
	t.Setenv(EnvName, "")
	if got := ScopeConfigDir("/var/config/openneko"); got != "/var/config/openneko" {
		t.Fatalf("config dir = %q", got)
	}
	if got, ok, err := StateDir(); err != nil || ok || got != "" {
		t.Fatalf("StateDir = %q, %v, %v", got, ok, err)
	}
}
