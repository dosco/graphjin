package cli

import (
	"strings"
	"testing"
)

func TestWebBaseURLUsesPersistedBindAddress(t *testing.T) {
	t.Setenv("OPENNEKO_PORT", "3100")
	t.Setenv("OPENNEKO_WEB_BIND_ADDRESS", "192.0.2.10")
	if got := webBaseURL(); got != "http://192.0.2.10:3100" {
		t.Fatalf("webBaseURL = %q", got)
	}
	t.Setenv("OPENNEKO_WEB_BIND_ADDRESS", "0.0.0.0")
	if got := webBaseURL(); got != "http://localhost:3100" {
		t.Fatalf("wildcard webBaseURL = %q", got)
	}
}

func TestSetupRetainsHermesOnlyBackendFlagCompatibility(t *testing.T) {
	flag := newSetupCmd().Flags().Lookup("backend")
	if flag == nil || flag.Deprecated == "" {
		t.Fatal("setup must retain the deprecated --backend flag for older automation")
	}
	if err := validateLegacyAgentBackend("hermes"); err != nil {
		t.Fatalf("legacy --backend hermes must remain accepted: %v", err)
	}
	err := validateLegacyAgentBackend("removed-runtime")
	if err == nil || !strings.Contains(err.Error(), "Hermes is the only agent runtime") {
		t.Fatalf("removed backend must fail with a Hermes-only message, got %v", err)
	}
}

func TestSetupExposesPersistentHostSettingsFlags(t *testing.T) {
	cmd := newSetupCmd()
	for _, name := range []string{"port", "openshell-port", "bind-address", "docker-subnet"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("setup is missing --%s", name)
		}
	}
}

func TestSetupKeepsRemovedPluginFlagsAsDeprecatedNoOps(t *testing.T) {
	cmd := newSetupCmd()
	for _, name := range []string{"skip-plugins", "plugins"} {
		flag := cmd.Flags().Lookup(name)
		if flag == nil || flag.Deprecated == "" {
			t.Fatalf("setup must keep --%s as a deprecated no-op for older automation", name)
		}
	}
}
