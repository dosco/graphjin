package compose

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/open-neko/neko/apps/openneko/internal/instance"
)

func isolateConfig(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(instance.EnvName, "")
}

func sampleFS() fstest.MapFS {
	return fstest.MapFS{
		"compose/core.yml": {Data: []byte("services: { web: {} }\n")},
		"compose/dev.yml":  {Data: []byte("services: { dev: {} }\n")},
		"compose/demo.yml": {Data: []byte("services: { demo: {} }\n")},
	}
}

func TestMaterializeProd(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	s := &Supervisor{AssetsFS: sampleFS(), RuntimeDir: dir, GOOS: "darwin"}
	files, err := s.Materialize(ModeProd)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, f := range files {
		got = append(got, filepath.Base(f))
	}
	want := []string{"core.yml"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestMaterializeDemo(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	s := &Supervisor{AssetsFS: sampleFS(), RuntimeDir: dir, GOOS: "darwin"}
	files, err := s.Materialize(ModeDemo)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, f := range files {
		got = append(got, filepath.Base(f))
	}
	want := []string{"core.yml", "demo.yml"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected order: %v", got)
	}
}

func TestMaterializeRemovesTheOldOpenShellOverlay(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	stale := filepath.Join(dir, "openshell.yml")
	if err := os.WriteFile(stale, []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Supervisor{AssetsFS: sampleFS(), RuntimeDir: dir, GOOS: "linux"}
	if _, err := s.Materialize(ModeProd); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("the old overlay is still in the runtime dir: %v", err)
	}
}

func TestMaterializeUnknownMode(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	s := &Supervisor{AssetsFS: sampleFS(), RuntimeDir: dir, GOOS: "linux"}
	if _, err := s.Materialize(Mode("bogus")); err == nil {
		t.Fatal("expected error for unknown mode")
	}
}

func TestRuntimeDirUsesWorkspaceRoot(t *testing.T) {
	t.Setenv(instance.EnvName, "")
	workspaceRoot := t.TempDir()
	t.Setenv("OPENNEKO_WORKSPACE_ROOT", workspaceRoot)

	s := &Supervisor{}
	got, err := s.runtimeDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(workspaceRoot, ".openneko", "runtime")
	if got != want {
		t.Fatalf("runtime dir = %q, want %q", got, want)
	}
}

func TestRuntimeDirScopesNamedInstance(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv("OPENNEKO_WORKSPACE_ROOT", workspaceRoot)
	t.Setenv(instance.EnvName, "acme")

	s := &Supervisor{}
	got, err := s.runtimeDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(workspaceRoot, ".openneko", "instances", "acme", "runtime")
	if got != want {
		t.Fatalf("runtime dir = %q, want %q", got, want)
	}
}

func TestRuntimeDirUsesNamedInstanceStateOutsideWorkspace(t *testing.T) {
	t.Setenv("OPENNEKO_WORKSPACE_ROOT", "")
	t.Setenv("XDG_STATE_HOME", "/var/state")
	t.Setenv(instance.EnvName, "acme")

	s := &Supervisor{}
	got, err := s.runtimeDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("/var/state", "openneko", "instances", "acme", "runtime")
	if got != want {
		t.Fatalf("runtime dir = %q, want %q", got, want)
	}
}

func TestExplicitRuntimeDirOverridesWorkspaceRoot(t *testing.T) {
	t.Setenv("OPENNEKO_WORKSPACE_ROOT", t.TempDir())
	explicit := t.TempDir()

	s := &Supervisor{RuntimeDir: explicit}
	got, err := s.runtimeDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != explicit {
		t.Fatalf("runtime dir = %q, want explicit %q", got, explicit)
	}
}

func TestProjectNameModeRoundTrip(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	s := &Supervisor{AssetsFS: sampleFS(), RuntimeDir: dir, GOOS: "linux"}

	project, err := s.ProjectName(ModeDemo)
	if err != nil {
		t.Fatal(err)
	}
	if project != "openneko-demo" {
		t.Fatalf("project = %q", project)
	}
	readBack, err := s.ProjectName("")
	if err != nil {
		t.Fatal(err)
	}
	if readBack != project {
		t.Fatalf("readBack = %q, want %q", readBack, project)
	}
	mode, ok := ModeFromProjectName(readBack)
	if !ok || mode != ModeDemo {
		t.Fatalf("mode = %q ok=%v", mode, ok)
	}
}

func TestNamedProjectIdentityRoundTrip(t *testing.T) {
	project := ProjectNameForModeAndInstance(ModeProd, "acme-west")
	if project != "openneko-acme-west-prod" {
		t.Fatalf("project = %q", project)
	}
	mode, name, ok := IdentityFromProjectName(project)
	if !ok || mode != ModeProd || name != "acme-west" {
		t.Fatalf("identity = %q %q %v", mode, name, ok)
	}
	if _, _, ok := IdentityFromProjectName("openneko-Bad-prod"); ok {
		t.Fatal("invalid instance project parsed successfully")
	}
}

func TestNamedProjectFallbackNeverTargetsLegacyProject(t *testing.T) {
	isolateConfig(t)
	t.Setenv(instance.EnvName, "acme")
	s := &Supervisor{RuntimeDir: t.TempDir()}
	project, err := s.ProjectName("")
	if err != nil {
		t.Fatal(err)
	}
	if project != "openneko-acme-prod" {
		t.Fatalf("named fallback project = %q", project)
	}
}

func TestInstallationIDIsStableAndRuntimeScoped(t *testing.T) {
	firstRuntime := t.TempDir()
	first := &Supervisor{RuntimeDir: firstRuntime}
	firstID, err := first.InstallationID()
	if err != nil {
		t.Fatal(err)
	}
	readBack, err := first.InstallationID()
	if err != nil {
		t.Fatal(err)
	}
	if firstID != readBack || len(firstID) != 32 {
		t.Fatalf("installation identity is not stable: first=%q second=%q", firstID, readBack)
	}
	second := &Supervisor{RuntimeDir: t.TempDir()}
	secondID, err := second.InstallationID()
	if err != nil {
		t.Fatal(err)
	}
	if secondID == firstID {
		t.Fatal("different runtime directories shared an installation identity")
	}
}

func TestImageVersionMarker(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	s := &Supervisor{AssetsFS: sampleFS(), RuntimeDir: dir, GOOS: "linux"}

	v, err := s.ImageVersion()
	if err != nil {
		t.Fatal(err)
	}
	if v != "" {
		t.Fatalf("empty marker = %q", v)
	}
	if err := s.WriteImageVersion("v1.2.3"); err != nil {
		t.Fatal(err)
	}
	v, err = s.ImageVersion()
	if err != nil {
		t.Fatal(err)
	}
	if v != "v1.2.3" {
		t.Fatalf("image version = %q", v)
	}
}
