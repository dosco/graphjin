package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/open-neko/neko/apps/openneko/internal/compose"
	"github.com/open-neko/neko/apps/openneko/internal/config"
	"github.com/open-neko/neko/apps/openneko/internal/installation"
	"github.com/open-neko/neko/apps/openneko/internal/instance"
	opennekoversion "github.com/open-neko/neko/apps/openneko/internal/version"
)

func TestUpgradeCLIOnlyStopsBeforeStack(t *testing.T) {
	previousVersion := opennekoversion.Version
	t.Cleanup(func() { opennekoversion.Version = previousVersion })
	opennekoversion.Version = "9.8.7"
	cmd := newUpgradeCmd()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs([]string{"--cli-only", "--version", "v9.8.7"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); !strings.Contains(got, "CLI upgrade complete at v9.8.7") {
		t.Fatalf("output = %q", got)
	}
}

func TestUpgradeRejectsConflictingScopes(t *testing.T) {
	cmd := newUpgradeCmd()
	cmd.SetArgs([]string{"--cli-only", "--stack-only", "--version", "v9.8.7"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("error = %v, want mutually exclusive", err)
	}
}

func TestNormalizeUpgradeImageVersion(t *testing.T) {
	tests := map[string]string{
		"":                      "latest",
		"  ":                    "latest",
		"latest":                "latest",
		"v1.2.3":                "v1.2.3",
		"1.2.3":                 "v1.2.3",
		"1.2.3-rc.1+build.123":  "v1.2.3-rc.1+build.123",
		"sha-1234567890abcdef":  "sha-1234567890abcdef",
		"release-candidate-tag": "release-candidate-tag",
	}
	for in, want := range tests {
		if got := normalizeUpgradeImageVersion(in); got != want {
			t.Fatalf("normalizeUpgradeImageVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUpgradeWaitsForEveryServiceReadinessCheck(t *testing.T) {
	want := []string{
		"up", "-d", "--wait", "--wait-timeout", "180",
		"--pull", "never", "--remove-orphans", "--force-recreate",
	}
	if got := upgradeStackUpArgs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("upgrade stack args = %v, want %v", got, want)
	}
}

func TestResolveUpgradeMode(t *testing.T) {
	s := &compose.Supervisor{RuntimeDir: t.TempDir()}
	stubComposeProjects(t, []string{"openneko-prod"}, []string{"openneko-prod"})
	if _, err := s.ProjectName(compose.ModeDemo); err != nil {
		t.Fatal(err)
	}
	mode, defaulted, err := resolveUpgradeMode(context.Background(), "auto", s)
	if err != nil {
		t.Fatal(err)
	}
	if mode != compose.ModeDemo || defaulted {
		t.Fatalf("mode=%q defaulted=%v, want demo false", mode, defaulted)
	}

	mode, defaulted, err = resolveUpgradeMode(context.Background(), "prod", s)
	if err != nil {
		t.Fatal(err)
	}
	if mode != compose.ModeProd || defaulted {
		t.Fatalf("mode=%q defaulted=%v, want prod false", mode, defaulted)
	}

	_, _, err = resolveUpgradeMode(context.Background(), "bogus", s)
	if err == nil {
		t.Fatal("expected invalid mode error")
	}
}

func TestResolveUpgradeModeDefaultsProdWithoutMarker(t *testing.T) {
	s := &compose.Supervisor{RuntimeDir: t.TempDir()}
	stubComposeProjects(t, nil, nil)
	mode, defaulted, err := resolveUpgradeMode(context.Background(), "auto", s)
	if err != nil {
		t.Fatal(err)
	}
	if mode != compose.ModeProd || !defaulted {
		t.Fatalf("mode=%q defaulted=%v, want prod true", mode, defaulted)
	}
}

func TestResolveUpgradeModeDetectsRunningLegacyStack(t *testing.T) {
	s := &compose.Supervisor{RuntimeDir: t.TempDir()}
	stubComposeProjects(t, []string{"openneko-demo"}, []string{"openneko-demo"})

	mode, defaulted, err := resolveUpgradeMode(context.Background(), "auto", s)
	if err != nil {
		t.Fatal(err)
	}
	if mode != compose.ModeDemo || defaulted {
		t.Fatalf("mode=%q defaulted=%v, want demo false", mode, defaulted)
	}
}

func TestResolveUpgradeModeDetectsStoppedLegacyStack(t *testing.T) {
	s := &compose.Supervisor{RuntimeDir: t.TempDir()}
	stubComposeProjects(t, nil, []string{"openneko-dev"})

	mode, defaulted, err := resolveUpgradeMode(context.Background(), "auto", s)
	if err != nil {
		t.Fatal(err)
	}
	if mode != compose.ModeDev || defaulted {
		t.Fatalf("mode=%q defaulted=%v, want dev false", mode, defaulted)
	}
}

func TestResolveUpgradeModeDetectsLegacyProdProject(t *testing.T) {
	s := &compose.Supervisor{RuntimeDir: t.TempDir()}
	stubComposeProjects(t, []string{"openneko"}, []string{"openneko"})

	mode, defaulted, err := resolveUpgradeMode(context.Background(), "auto", s)
	if err != nil {
		t.Fatal(err)
	}
	if mode != compose.ModeProd || defaulted {
		t.Fatalf("mode=%q defaulted=%v, want prod false", mode, defaulted)
	}
}

func TestResolveUpgradeModeErrorsOnMultipleStacks(t *testing.T) {
	t.Setenv(instance.EnvName, "")
	s := &compose.Supervisor{RuntimeDir: t.TempDir()}
	stubComposeProjects(t, []string{"openneko-dev", "openneko-demo"}, []string{"openneko-dev", "openneko-demo"})

	_, _, err := resolveUpgradeMode(context.Background(), "auto", s)
	if err == nil {
		t.Fatal("expected multiple stack error")
	}
}

func TestResolveUpgradeModeRefusesUnnamedGuessForNamedStack(t *testing.T) {
	t.Setenv(instance.EnvName, "")
	s := &compose.Supervisor{RuntimeDir: t.TempDir()}
	stubComposeProjects(t, []string{"openneko-acme-prod"}, []string{"openneko-acme-prod"})
	_, _, err := resolveUpgradeMode(context.Background(), "auto", s)
	if err == nil || !strings.Contains(err.Error(), "--instance") {
		t.Fatalf("error = %v, want named-instance guidance", err)
	}
}

func TestUpgradeRefusesUnconfiguredNamedInstanceBeforeResolvingRelease(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(instance.EnvName, "missing")
	cmd := newUpgradeCmd()
	cmd.SetArgs([]string{"--stack-only", "--version", "v9.8.7"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "has not been configured") {
		t.Fatalf("error = %v, want unconfigured instance error", err)
	}
}

func TestSeedRefusesNamedProductionInstance(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(instance.EnvName, "acme")
	settings := installation.Settings{
		Version:        installation.CurrentVersion,
		Instance:       "acme",
		Mode:           "prod",
		WebPort:        3101,
		WebBindAddress: "127.0.0.1",
		OpenShellPort:  18101,
		DockerSubnet:   "10.224.1.0/24",
	}
	if err := os.MkdirAll(filepath.Dir(installation.Path(config.Dir(""))), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := installation.Save(config.Dir(""), settings); err != nil {
		t.Fatal(err)
	}
	cmd := newSeedCmd()
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "requires a demo instance") {
		t.Fatalf("error = %v, want demo-mode requirement", err)
	}
}

func TestModeFromExistingProjectsTargetsSelectedInstance(t *testing.T) {
	t.Setenv(instance.EnvName, "acme")
	mode, ok, err := modeFromExistingProjects([]string{
		"openneko-globex-demo",
		"openneko-acme-prod",
	})
	if err != nil || !ok || mode != compose.ModeProd {
		t.Fatalf("mode = %q, ok=%v, err=%v", mode, ok, err)
	}
}

func TestOldOpenNekoImageRefs(t *testing.T) {
	in := `
ghcr.io/open-neko/neko-web:v1.0.0
ghcr.io/open-neko/neko-web:v2.0.0
ghcr.io/open-neko/neko-worker:v1.0.0
ghcr.io/open-neko/records-graphjin:v1.0.0
ghcr.io/open-neko/agent:<none>
postgres:16-alpine
ghcr.io/open-neko/plugin-base:v0.9.0
ghcr.io/open-neko/plugin-base:v0.9.0
`
	want := []string{
		"ghcr.io/open-neko/neko-web:v1.0.0",
		"ghcr.io/open-neko/neko-worker:v1.0.0",
		"ghcr.io/open-neko/records-graphjin:v1.0.0",
		"ghcr.io/open-neko/plugin-base:v0.9.0",
	}
	if got := oldOpenNekoImageRefs(in, "v2.0.0"); !reflect.DeepEqual(got, want) {
		t.Fatalf("oldOpenNekoImageRefs = %#v, want %#v", got, want)
	}
}

func TestExtraUpgradeImageRefsDedupesOverrides(t *testing.T) {
	t.Setenv("OPENNEKO_AGENT_IMAGE", "custom/agent:v1")
	t.Setenv("OPENNEKO_PLUGIN_BASE_IMAGE", "custom/agent:v1")
	t.Setenv("OPENNEKO_SUPERVISOR_IMAGE", "custom/agent:v1")
	got := extraUpgradeImageRefs("v2.0.0")
	want := []string{"custom/agent:v1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("extraUpgradeImageRefs = %#v, want %#v", got, want)
	}
}

func stubComposeProjects(t *testing.T, running, all []string) {
	t.Helper()
	previous := listDockerComposeProjectNames
	t.Cleanup(func() {
		listDockerComposeProjectNames = previous
	})
	listDockerComposeProjectNames = func(_ context.Context, includeStopped bool) ([]string, error) {
		if includeStopped {
			return all, nil
		}
		return running, nil
	}
}
