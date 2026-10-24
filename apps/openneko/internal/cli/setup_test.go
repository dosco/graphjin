package cli

import (
	"reflect"
	"strings"
	"testing"

	"github.com/open-neko/neko/apps/openneko/internal/plugin/marketplace"
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

func TestSetupRetainsAxOnlyBackendFlagCompatibility(t *testing.T) {
	flag := newSetupCmd().Flags().Lookup("backend")
	if flag == nil || flag.Deprecated == "" {
		t.Fatal("setup must retain the deprecated --backend flag for older automation")
	}
	if err := validateLegacyAgentBackend("ax"); err != nil {
		t.Fatalf("legacy --backend ax must remain accepted: %v", err)
	}
	err := validateLegacyAgentBackend("hermes")
	if err == nil || !strings.Contains(err.Error(), "Ax is the only agent runtime") {
		t.Fatalf("removed backend must fail with an Ax-only message, got %v", err)
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

func TestSplitCSV(t *testing.T) {
	got := splitCSV("a, b ,,c ")
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitCSV = %v, want %v", got, want)
	}
	if got := splitCSV("  "); got != nil {
		t.Fatalf("splitCSV(blank) = %v, want nil", got)
	}
}

func TestResolvePluginSelection(t *testing.T) {
	plugins := []marketplace.Plugin{
		{Name: "@x/a"}, {Name: "@x/b"}, {Name: "@x/c"},
	}
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"all", []string{"@x/a", "@x/b", "@x/c"}},
		{"ALL", []string{"@x/a", "@x/b", "@x/c"}},
		{"1,3", []string{"@x/a", "@x/c"}},
		{"@x/b", []string{"@x/b"}},
		{"2, @x/a", []string{"@x/b", "@x/a"}},
		{"9", nil},                     // out of range
		{"nope", nil},                  // unknown name
		{"1,@x/a,1", []string{"@x/a"}}, // de-duped
	}
	for _, c := range cases {
		got := resolvePluginSelection(c.in, plugins)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("resolvePluginSelection(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
