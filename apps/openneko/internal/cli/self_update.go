package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"

	opennekoversion "github.com/open-neko/neko/apps/openneko/internal/version"
)

const maxReleaseAssetBytes = 128 << 20

var (
	latestOpenNekoReleaseURL = "https://github.com/open-neko/openneko/releases/latest"
	openNekoReleaseBaseURL   = "https://github.com/open-neko/openneko/releases/download"
	releaseTagPattern        = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$`)
	upgradeHTTPClient        = &http.Client{Timeout: 2 * time.Minute}
	upgradeExecutable        = os.Executable
	upgradeExec              = syscall.Exec
)

func resolveUpgradeTarget(ctx context.Context, requested string, stackOnly bool) (string, error) {
	target := normalizeUpgradeImageVersion(requested)
	if target == "latest" {
		resolved, err := resolveLatestReleaseTag(ctx)
		if err != nil {
			return "", err
		}
		target = resolved
	}
	if !stackOnly && !releaseTagPattern.MatchString(target) {
		return "", fmt.Errorf(
			"CLI upgrades require a released semantic version, got %q; use --stack-only for a custom image tag",
			target,
		)
	}
	return target, nil
}

func resolveLatestReleaseTag(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, latestOpenNekoReleaseURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "openneko-upgrade")
	res, err := upgradeHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("resolve latest OpenNeko release: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("resolve latest OpenNeko release: unexpected HTTP status %s", res.Status)
	}
	parts := strings.Split(strings.Trim(res.Request.URL.Path, "/"), "/")
	if len(parts) < 2 || parts[len(parts)-2] != "tag" {
		return "", fmt.Errorf("resolve latest OpenNeko release: unexpected redirect %q", res.Request.URL.String())
	}
	tag := parts[len(parts)-1]
	if !releaseTagPattern.MatchString(tag) {
		return "", fmt.Errorf("resolve latest OpenNeko release: invalid tag %q", tag)
	}
	return tag, nil
}

func cliVersionMatchesTarget(current, target string) bool {
	return strings.TrimPrefix(strings.TrimSpace(current), "v") ==
		strings.TrimPrefix(strings.TrimSpace(target), "v")
}

func upgradeReexecArgs(args []string, target string) []string {
	out := make([]string, 0, len(args)+2)
	replaced := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--version" && i+1 < len(args) {
			out = append(out, arg, target)
			i++
			replaced = true
			continue
		}
		if strings.HasPrefix(arg, "--version=") {
			out = append(out, "--version="+target)
			replaced = true
			continue
		}
		out = append(out, arg)
	}
	if !replaced {
		out = append(out, "--version", target)
	}
	return out
}

func upgradeCLIAndReexec(
	ctx context.Context,
	target string,
	args []string,
	stdout io.Writer,
	stderr io.Writer,
) error {
	if cliVersionMatchesTarget(opennekoversion.Version, target) {
		fmt.Fprintf(stdout, "OpenNeko CLI already at %s.\n", target)
		return nil
	}
	if opennekoversion.Version == "0.0.0-dev" {
		return errors.New("a development-build CLI cannot self-update; install a released CLI or re-run with --stack-only")
	}

	executable, err := upgradeExecutable()
	if err != nil {
		return fmt.Errorf("locate current OpenNeko CLI: %w", err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return fmt.Errorf("resolve current OpenNeko CLI path: %w", err)
	}

	fmt.Fprintf(stdout, "Upgrading OpenNeko CLI %s -> %s...\n", opennekoversion.Version, target)
	nextExecutable := executable
	brew, managed, err := homebrewManagedCLI(ctx, executable)
	if err != nil {
		return err
	}
	if managed {
		nextExecutable, err = upgradeHomebrewCLI(ctx, brew, target, stdout, stderr)
	} else {
		err = installStandaloneCLI(ctx, target, executable, stdout, stderr)
	}
	if err != nil {
		return err
	}

	reexecArgs := upgradeReexecArgs(args, target)
	argv := append([]string{nextExecutable}, reexecArgs...)
	fmt.Fprintf(stdout, "Re-running upgrade with OpenNeko CLI %s...\n", target)
	if err := upgradeExec(nextExecutable, argv, os.Environ()); err != nil {
		return fmt.Errorf("re-execute upgraded OpenNeko CLI: %w", err)
	}
	return nil
}

func homebrewManagedCLI(ctx context.Context, executable string) (string, bool, error) {
	looksManaged := strings.Contains(filepath.ToSlash(executable), "/Caskroom/openneko/")
	brew, err := exec.LookPath("brew")
	if err != nil {
		if looksManaged {
			return "", false, errors.New("current CLI is Homebrew-managed, but brew is unavailable")
		}
		return "", false, nil
	}
	candidates, err := homebrewCLIExecutables(ctx, brew)
	if err != nil {
		if looksManaged {
			return "", false, fmt.Errorf("inspect Homebrew OpenNeko cask: %w", err)
		}
		return "", false, nil
	}
	executableInfo, err := os.Stat(executable)
	if err != nil {
		return "", false, err
	}
	for _, candidate := range candidates {
		candidateInfo, err := os.Stat(candidate)
		if err == nil && os.SameFile(executableInfo, candidateInfo) {
			return brew, true, nil
		}
	}
	if looksManaged {
		return "", false, errors.New("current CLI is inside Homebrew Caskroom but is not owned by the installed openneko cask")
	}
	return "", false, nil
}

func upgradeHomebrewCLI(
	ctx context.Context,
	brew string,
	target string,
	stdout io.Writer,
	stderr io.Writer,
) (string, error) {
	cmd := exec.CommandContext(ctx, brew, "upgrade", "--cask", "openneko")
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("upgrade Homebrew OpenNeko CLI: %w", err)
	}
	executables, err := homebrewCLIExecutables(ctx, brew)
	if err != nil {
		return "", fmt.Errorf("locate Homebrew OpenNeko CLI after upgrade: %w", err)
	}
	if len(executables) == 0 {
		return "", errors.New("locate Homebrew OpenNeko CLI after upgrade: cask contains no executable")
	}
	executable := executables[0]
	if err := verifyCLIExecutable(ctx, executable, target); err != nil {
		return "", fmt.Errorf("Homebrew did not install requested OpenNeko release: %w", err)
	}
	return executable, nil
}

func homebrewCLIExecutables(ctx context.Context, brew string) ([]string, error) {
	cmd := exec.CommandContext(ctx, brew, "list", "--cask", "openneko")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var executables []string
	for _, candidate := range strings.Split(string(out), "\n") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || filepath.Base(candidate) != "openneko" {
			continue
		}
		info, err := os.Stat(candidate)
		if err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
			executables = append(executables, candidate)
		}
	}
	return executables, nil
}

func installStandaloneCLI(
	ctx context.Context,
	target string,
	executable string,
	stdout io.Writer,
	stderr io.Writer,
) error {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return fmt.Errorf("self-update is unsupported on %s; update the CLI with its package manager", runtime.GOOS)
	}
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		return fmt.Errorf("self-update is unsupported on %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	versionNumber := strings.TrimPrefix(target, "v")
	archiveName := fmt.Sprintf("openneko_%s_%s_%s.tar.gz", versionNumber, runtime.GOOS, runtime.GOARCH)
	base := strings.TrimSuffix(openNekoReleaseBaseURL, "/") + "/" + target

	checksums, err := downloadReleaseAsset(ctx, base+"/checksums.txt")
	if err != nil {
		return fmt.Errorf("download OpenNeko checksums: %w", err)
	}
	expected, err := checksumForAsset(checksums, archiveName)
	if err != nil {
		return err
	}
	archive, err := downloadReleaseAsset(ctx, base+"/"+archiveName)
	if err != nil {
		return fmt.Errorf("download OpenNeko CLI archive: %w", err)
	}
	actual := sha256.Sum256(archive)
	if !bytes.Equal(actual[:], expected) {
		return fmt.Errorf("checksum mismatch for %s", archiveName)
	}
	binary, err := openNekoBinaryFromArchive(archive)
	if err != nil {
		return err
	}

	directory := filepath.Dir(executable)
	candidate, err := os.CreateTemp(directory, ".openneko-upgrade-*")
	privilegedInstall := false
	if errors.Is(err, os.ErrPermission) {
		candidate, err = os.CreateTemp("", ".openneko-upgrade-*")
		privilegedInstall = true
	}
	if err != nil {
		return fmt.Errorf("prepare CLI update beside %s: %w; update it with the original installer or use --stack-only", executable, err)
	}
	candidatePath := candidate.Name()
	defer os.Remove(candidatePath)
	if err := candidate.Chmod(0o755); err != nil {
		candidate.Close()
		return err
	}
	if _, err := candidate.Write(binary); err != nil {
		candidate.Close()
		return fmt.Errorf("write upgraded OpenNeko CLI: %w", err)
	}
	if err := candidate.Sync(); err != nil {
		candidate.Close()
		return fmt.Errorf("sync upgraded OpenNeko CLI: %w", err)
	}
	if err := candidate.Close(); err != nil {
		return fmt.Errorf("close upgraded OpenNeko CLI: %w", err)
	}
	if err := verifyCLIExecutable(ctx, candidatePath, target); err != nil {
		return fmt.Errorf("verify downloaded OpenNeko CLI: %w", err)
	}
	if privilegedInstall {
		if err := installStandaloneCLIWithSudo(ctx, candidatePath, executable, target, stdout, stderr); err != nil {
			return err
		}
	} else if err := os.Rename(candidatePath, executable); err != nil {
		if !errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("replace OpenNeko CLI at %s: %w; update it with the original installer or use --stack-only", executable, err)
		}
		if err := installStandaloneCLIWithSudo(ctx, candidatePath, executable, target, stdout, stderr); err != nil {
			return err
		}
	}
	if dir, err := os.Open(directory); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

func installStandaloneCLIWithSudo(
	ctx context.Context,
	candidate string,
	executable string,
	target string,
	stdout io.Writer,
	stderr io.Writer,
) error {
	sudo, err := exec.LookPath("sudo")
	if err != nil {
		return fmt.Errorf("replace protected OpenNeko CLI at %s: sudo is unavailable; update it with the original installer or use --stack-only", executable)
	}
	installCommand, err := trustedSystemCommand("install")
	if err != nil {
		return err
	}
	moveCommand, err := trustedSystemCommand("mv")
	if err != nil {
		return err
	}
	removeCommand, err := trustedSystemCommand("rm")
	if err != nil {
		return err
	}
	privilegedCandidate := filepath.Join(
		filepath.Dir(executable),
		"."+filepath.Base(candidate),
	)
	fmt.Fprintf(stdout, "Elevating to replace protected CLI at %s...\n", executable)
	install := exec.CommandContext(ctx, sudo, installCommand, "-m", "0755", candidate, privilegedCandidate)
	install.Stdout = stdout
	install.Stderr = stderr
	if err := install.Run(); err != nil {
		return fmt.Errorf("stage protected OpenNeko CLI update: %w", err)
	}
	cleanup := func() {
		remove := exec.CommandContext(context.Background(), sudo, removeCommand, "-f", privilegedCandidate)
		remove.Stdout = stdout
		remove.Stderr = stderr
		_ = remove.Run()
	}
	moved := false
	defer func() {
		if !moved {
			cleanup()
		}
	}()
	if err := verifyCLIExecutable(ctx, privilegedCandidate, target); err != nil {
		return fmt.Errorf("verify staged protected OpenNeko CLI update: %w", err)
	}
	replace := exec.CommandContext(ctx, sudo, moveCommand, "-f", privilegedCandidate, executable)
	replace.Stdout = stdout
	replace.Stderr = stderr
	if err := replace.Run(); err != nil {
		return fmt.Errorf("replace protected OpenNeko CLI: %w", err)
	}
	moved = true
	if err := verifyCLIExecutable(ctx, executable, target); err != nil {
		return fmt.Errorf("verify protected OpenNeko CLI update: %w", err)
	}
	return nil
}

func trustedSystemCommand(name string) (string, error) {
	for _, directory := range []string{"/usr/bin", "/bin"} {
		candidate := filepath.Join(directory, name)
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("required system command %s is unavailable", name)
}

func verifyCLIExecutable(ctx context.Context, executable, target string) error {
	verifyCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(verifyCtx, executable, "version", "--short").CombinedOutput()
	if err != nil {
		return fmt.Errorf("run %s version --short: %w (%s)", executable, err, strings.TrimSpace(string(out)))
	}
	got := strings.TrimSpace(string(out))
	if !cliVersionMatchesTarget(got, target) {
		return fmt.Errorf("got CLI version %q, want %q", got, strings.TrimPrefix(target, "v"))
	}
	return nil
}

func downloadReleaseAsset(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "openneko-upgrade")
	res, err := upgradeHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("unexpected HTTP status %s", res.Status)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxReleaseAssetBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxReleaseAssetBytes {
		return nil, errors.New("release asset exceeds size limit")
	}
	return data, nil
}

func checksumForAsset(checksums []byte, asset string) ([]byte, error) {
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != asset {
			continue
		}
		checksum, err := hex.DecodeString(fields[0])
		if err != nil || len(checksum) != sha256.Size {
			return nil, fmt.Errorf("invalid checksum for %s", asset)
		}
		return checksum, nil
	}
	return nil, fmt.Errorf("checksums.txt does not contain %s", asset)
}

func openNekoBinaryFromArchive(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("open OpenNeko CLI archive: %w", err)
	}
	defer gz.Close()
	tarReader := tar.NewReader(gz)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read OpenNeko CLI archive: %w", err)
		}
		if header.Typeflag != tar.TypeReg || filepath.Base(header.Name) != "openneko" {
			continue
		}
		if header.Size <= 0 || header.Size > maxReleaseAssetBytes {
			return nil, errors.New("OpenNeko CLI archive contains an invalid binary size")
		}
		binary, err := io.ReadAll(io.LimitReader(tarReader, maxReleaseAssetBytes+1))
		if err != nil {
			return nil, fmt.Errorf("extract OpenNeko CLI: %w", err)
		}
		if int64(len(binary)) != header.Size {
			return nil, errors.New("OpenNeko CLI archive contains a truncated binary")
		}
		return binary, nil
	}
	return nil, errors.New("OpenNeko CLI archive does not contain the openneko binary")
}
