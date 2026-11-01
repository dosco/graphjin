package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	opennekoversion "github.com/open-neko/neko/apps/openneko/internal/version"
)

func TestResolveUpgradeTarget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/latest" {
			http.Redirect(w, r, "/open-neko/openneko/releases/tag/v9.8.7", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	stubUpgradeReleaseEndpoints(t, server.URL+"/latest", server.URL)

	got, err := resolveUpgradeTarget(context.Background(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	if got != "v9.8.7" {
		t.Fatalf("latest target = %q, want v9.8.7", got)
	}
	got, err = resolveUpgradeTarget(context.Background(), "9.7.6-rc.1", false)
	if err != nil || got != "v9.7.6-rc.1" {
		t.Fatalf("exact target = %q, %v", got, err)
	}
	if _, err := resolveUpgradeTarget(context.Background(), "sha-deadbeef", false); err == nil {
		t.Fatal("expected custom image tag to require --stack-only")
	}
	got, err = resolveUpgradeTarget(context.Background(), "sha-deadbeef", true)
	if err != nil || got != "sha-deadbeef" {
		t.Fatalf("stack-only custom target = %q, %v", got, err)
	}
}

func TestUpgradeReexecArgsPinsResolvedTarget(t *testing.T) {
	tests := []struct {
		in   []string
		want []string
	}{
		{[]string{"upgrade"}, []string{"upgrade", "--version", "v9.8.7"}},
		{[]string{"upgrade", "--version", "latest"}, []string{"upgrade", "--version", "v9.8.7"}},
		{[]string{"upgrade", "--version=v9.7.0"}, []string{"upgrade", "--version=v9.8.7"}},
	}
	for _, test := range tests {
		if got := upgradeReexecArgs(test.in, "v9.8.7"); !reflect.DeepEqual(got, test.want) {
			t.Fatalf("upgradeReexecArgs(%v) = %v, want %v", test.in, got, test.want)
		}
	}
}

func TestInstallStandaloneCLIVerifiesAndAtomicallyReplaces(t *testing.T) {
	archiveName, archive, checksums := fakeCLIRelease(t, "9.8.7")
	server := releaseAssetServer(t, "v9.8.7", archiveName, archive, checksums)
	defer server.Close()
	stubUpgradeReleaseEndpoints(t, server.URL+"/latest", server.URL)

	executable := filepath.Join(t.TempDir(), "openneko")
	if err := os.WriteFile(executable, []byte("old-cli"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := installStandaloneCLI(
		context.Background(), "v9.8.7", executable, &bytes.Buffer{}, &bytes.Buffer{},
	); err != nil {
		t.Fatal(err)
	}
	if err := verifyCLIExecutable(context.Background(), executable, "v9.8.7"); err != nil {
		t.Fatal(err)
	}
	if leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(executable), ".openneko-upgrade-*")); err != nil {
		t.Fatal(err)
	} else if len(leftovers) != 0 {
		t.Fatalf("temporary CLI files remain: %v", leftovers)
	}
}

func TestInstallStandaloneCLIRejectsChecksumMismatchWithoutReplacing(t *testing.T) {
	archiveName, archive, _ := fakeCLIRelease(t, "9.8.7")
	badChecksums := []byte(fmt.Sprintf("%064x  %s\n", 0, archiveName))
	server := releaseAssetServer(t, "v9.8.7", archiveName, archive, badChecksums)
	defer server.Close()
	stubUpgradeReleaseEndpoints(t, server.URL+"/latest", server.URL)

	executable := filepath.Join(t.TempDir(), "openneko")
	if err := os.WriteFile(executable, []byte("old-cli"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := installStandaloneCLI(
		context.Background(), "v9.8.7", executable, &bytes.Buffer{}, &bytes.Buffer{},
	)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("error = %v, want checksum mismatch", err)
	}
	got, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old-cli" {
		t.Fatalf("existing CLI changed after rejected update: %q", got)
	}
}

func TestUpgradeCLIAndReexecStandalone(t *testing.T) {
	archiveName, archive, checksums := fakeCLIRelease(t, "9.8.7")
	server := releaseAssetServer(t, "v9.8.7", archiveName, archive, checksums)
	defer server.Close()
	stubUpgradeReleaseEndpoints(t, server.URL+"/latest", server.URL)

	executable := filepath.Join(t.TempDir(), "openneko")
	if err := os.WriteFile(executable, []byte("old-cli"), 0o755); err != nil {
		t.Fatal(err)
	}
	previousVersion := opennekoversion.Version
	previousExecutable := upgradeExecutable
	previousExec := upgradeExec
	t.Cleanup(func() {
		opennekoversion.Version = previousVersion
		upgradeExecutable = previousExecutable
		upgradeExec = previousExec
	})
	opennekoversion.Version = "9.7.0"
	upgradeExecutable = func() (string, error) { return executable, nil }
	var reexecPath string
	var reexecArgs []string
	upgradeExec = func(path string, args []string, _ []string) error {
		reexecPath = path
		reexecArgs = append([]string(nil), args...)
		return nil
	}

	var stdout bytes.Buffer
	if err := upgradeCLIAndReexec(
		context.Background(),
		"v9.8.7",
		[]string{"upgrade", "--cli-only"},
		&stdout,
		&stdout,
	); err != nil {
		t.Fatal(err)
	}
	resolvedExecutable, err := filepath.EvalSymlinks(executable)
	if err != nil {
		t.Fatal(err)
	}
	if reexecPath != resolvedExecutable {
		t.Fatalf("reexec path = %q, want %q", reexecPath, resolvedExecutable)
	}
	wantArgs := []string{resolvedExecutable, "upgrade", "--cli-only", "--version", "v9.8.7"}
	if !reflect.DeepEqual(reexecArgs, wantArgs) {
		t.Fatalf("reexec args = %v, want %v", reexecArgs, wantArgs)
	}
	if err := verifyCLIExecutable(context.Background(), executable, "v9.8.7"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "9.7.0 -> v9.8.7") {
		t.Fatalf("upgrade output = %q", stdout.String())
	}
}

func TestUpgradeCLIRejectsDevelopmentBuild(t *testing.T) {
	previousVersion := opennekoversion.Version
	t.Cleanup(func() { opennekoversion.Version = previousVersion })
	opennekoversion.Version = "0.0.0-dev"
	err := upgradeCLIAndReexec(
		context.Background(),
		"v9.8.7",
		[]string{"upgrade"},
		&bytes.Buffer{},
		&bytes.Buffer{},
	)
	if err == nil || !strings.Contains(err.Error(), "development-build") {
		t.Fatalf("error = %v, want development-build guidance", err)
	}
}

func TestHomebrewManagedCLIRequiresSameInstalledFile(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "openneko")
	if err := os.WriteFile(executable, []byte("cli"), 0o755); err != nil {
		t.Fatal(err)
	}
	brew := filepath.Join(dir, "brew")
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = list ]; then printf '%%s\\n' %q; fi\n", executable)
	if err := os.WriteFile(brew, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	got, managed, err := homebrewManagedCLI(context.Background(), executable)
	if err != nil {
		t.Fatal(err)
	}
	if !managed || got != brew {
		t.Fatalf("homebrewManagedCLI = %q, %v; want %q, true", got, managed, brew)
	}
}

func TestHomebrewManagedCLIFailsClosedWhenBrewIsUnavailable(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "Caskroom", "openneko", "9.7.0", "openneko")
	if err := os.MkdirAll(filepath.Dir(executable), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("cli"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	_, managed, err := homebrewManagedCLI(context.Background(), executable)
	if err == nil || managed || !strings.Contains(err.Error(), "Homebrew-managed") {
		t.Fatalf("homebrewManagedCLI = managed %v, error %v", managed, err)
	}
}

func TestInstallStandaloneCLIWithSudoStagesThenAtomicallyMoves(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "openneko")
	if err := os.WriteFile(executable, []byte("old-cli"), 0o755); err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(t.TempDir(), "candidate")
	binary := []byte("#!/bin/sh\nif [ \"$1\" = version ]; then printf '9.8.7\\n'; fi\n")
	if err := os.WriteFile(candidate, binary, 0o755); err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	sudo := filepath.Join(binDir, "sudo")
	if err := os.WriteFile(sudo, []byte("#!/bin/sh\nexec \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	var output bytes.Buffer
	if err := installStandaloneCLIWithSudo(
		context.Background(), candidate, executable, "v9.8.7", &output, &output,
	); err != nil {
		t.Fatal(err)
	}
	if err := verifyCLIExecutable(context.Background(), executable, "v9.8.7"); err != nil {
		t.Fatal(err)
	}
	privilegedCandidate := filepath.Join(dir, "."+filepath.Base(candidate))
	if _, err := os.Stat(privilegedCandidate); !os.IsNotExist(err) {
		t.Fatalf("privileged staging file remains at %s", privilegedCandidate)
	}
}

func fakeCLIRelease(t *testing.T, version string) (string, []byte, []byte) {
	t.Helper()
	binary := []byte(fmt.Sprintf(
		"#!/bin/sh\nif [ \"$1\" = version ] && [ \"$2\" = --short ]; then printf '%%s\\n' %q; exit 0; fi\nexit 1\n",
		version,
	))
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{
		Name: "openneko",
		Mode: 0o755,
		Size: int64(len(binary)),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(binary); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	archiveName := fmt.Sprintf("openneko_%s_%s_%s.tar.gz", version, runtime.GOOS, runtime.GOARCH)
	checksum := sha256.Sum256(archive.Bytes())
	checksums := []byte(fmt.Sprintf("%x  %s\n", checksum, archiveName))
	return archiveName, archive.Bytes(), checksums
}

func releaseAssetServer(
	t *testing.T,
	tag string,
	archiveName string,
	archive []byte,
	checksums []byte,
) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + tag + "/checksums.txt":
			_, _ = w.Write(checksums)
		case "/" + tag + "/" + archiveName:
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
}

func stubUpgradeReleaseEndpoints(t *testing.T, latestURL, releaseBaseURL string) {
	t.Helper()
	previousLatest := latestOpenNekoReleaseURL
	previousBase := openNekoReleaseBaseURL
	previousClient := upgradeHTTPClient
	t.Cleanup(func() {
		latestOpenNekoReleaseURL = previousLatest
		openNekoReleaseBaseURL = previousBase
		upgradeHTTPClient = previousClient
	})
	latestOpenNekoReleaseURL = latestURL
	openNekoReleaseBaseURL = releaseBaseURL
	upgradeHTTPClient = http.DefaultClient
}
