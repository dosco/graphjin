// Package compose is a thin wrapper around `docker compose`. It materializes
// the embedded compose files under OPENNEKO_WORKSPACE_ROOT when set, otherwise
// under the current directory, picks the right overlay per mode, and forwards
// I/O + signals to the child.
package compose

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/open-neko/neko/apps/openneko/internal/config"
	"github.com/open-neko/neko/apps/openneko/internal/installation"
	"github.com/open-neko/neko/apps/openneko/internal/instance"
)

type Mode string

const (
	ModeProd Mode = "prod"
	ModeDev  Mode = "dev"
	ModeDemo Mode = "demo"
)

const (
	projectNameMarker    = ".project-name"
	imageVersionMarker   = ".image-version"
	installationIDMarker = ".installation-id"
)

// Supervisor is the host-side controller; assets are the embedded files the
// supervisor materializes before running compose.
type Supervisor struct {
	// AssetsFS holds the embedded compose files. Layout:
	//   compose/core.yml
	//   compose/dev.yml
	//   compose/demo.yml
	//   compose/openshell.yml
	AssetsFS fs.FS
	// RuntimeDir is where compose files are written. Defaults to
	// $OPENNEKO_WORKSPACE_ROOT/.openneko/runtime/ when that variable is set,
	// otherwise <cwd>/.openneko/runtime/.
	RuntimeDir string
	// GOOS lets tests stub the platform.
	GOOS string
}

func New(assets fs.FS) *Supervisor {
	return &Supervisor{
		AssetsFS: assets,
		GOOS:     runtime.GOOS,
	}
}

func (s *Supervisor) runtimeDir() (string, error) {
	if s.RuntimeDir != "" {
		return s.RuntimeDir, nil
	}
	if workspaceRoot := strings.TrimSpace(os.Getenv("OPENNEKO_WORKSPACE_ROOT")); workspaceRoot != "" {
		root, err := filepath.Abs(workspaceRoot)
		if err != nil {
			return "", err
		}
		if name := instance.Current(); name != "" {
			return filepath.Join(root, ".openneko", "instances", name, "runtime"), nil
		}
		return filepath.Join(root, ".openneko", "runtime"), nil
	}
	if stateDir, ok, err := instance.StateDir(); err != nil {
		return "", err
	} else if ok {
		return filepath.Join(stateDir, "runtime"), nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(cwd, ".openneko", "runtime"), nil
}

// Materialize writes the embedded compose files for the given mode into the
// runtime dir and returns the list of `-f` paths to pass to `docker compose`.
func (s *Supervisor) Materialize(mode Mode) ([]string, error) {
	rt, err := s.runtimeDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(rt, 0o755); err != nil {
		return nil, err
	}
	files := []string{"compose/core.yml"}
	switch mode {
	case ModeDev:
		files = append(files, "compose/dev.yml")
	case ModeDemo:
		files = append(files, "compose/demo.yml")
	case ModeProd, "":
		// core only
	default:
		return nil, fmt.Errorf("compose: unknown mode %q (want prod|dev|demo)", mode)
	}
	// SEC9: OpenShell is the only runtime — its overlay always applies.
	files = append(files, "compose/openshell.yml")
	var out []string
	for _, name := range files {
		raw, err := fs.ReadFile(s.AssetsFS, name)
		if err != nil {
			return nil, fmt.Errorf("compose: missing embedded asset %s: %w", name, err)
		}
		dst := filepath.Join(rt, filepath.Base(name))
		if err := os.WriteFile(dst, raw, 0o644); err != nil {
			return nil, err
		}
		out = append(out, dst)
	}
	override := filepath.Join(config.Dir(""), "compose.override.yml")
	if _, err := os.Stat(override); err == nil {
		out = append(out, override)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return out, nil
}

// ProjectName returns the compose project name to use for the current
// invocation. On `start`, callers should pass the mode so containers/
// volumes/networks land as openneko-<mode>-*. start persists the chosen
// project name to .openneko/runtime/.project-name so stop/logs/status
// pick the same project up without needing --mode every time. A named install
// safely derives its own project when the marker is absent; the unnamed legacy
// install falls back to "openneko".
func (s *Supervisor) ProjectName(modeIfStarting Mode) (string, error) {
	rt, err := s.runtimeDir()
	if err != nil {
		return "", err
	}
	marker := filepath.Join(rt, projectNameMarker)
	if modeIfStarting != "" {
		name := ProjectNameForMode(modeIfStarting)
		_ = os.MkdirAll(rt, 0o755)
		_ = os.WriteFile(marker, []byte(name+"\n"), 0o644)
		return name, nil
	}
	if b, err := os.ReadFile(marker); err == nil {
		v := strings.TrimSpace(string(b))
		if v != "" {
			return v, nil
		}
	}
	if name := instance.Current(); name != "" {
		settings, ok, err := installation.Load(config.Dir(""))
		if err != nil {
			return "", err
		}
		if ok {
			return ProjectNameForModeAndInstance(Mode(settings.Mode), name), nil
		}
		// A named command must never fall through to the legacy "openneko"
		// project: even an incomplete setup should remain inside its namespace.
		return ProjectNameForModeAndInstance(ModeProd, name), nil
	}
	return "openneko", nil
}

// InstallationID returns a random identity tied to this runtime directory.
// It separates two OpenNeko stacks that use the same mode/project name from
// different working directories. Reinstalling in the same directory reuses
// it; a genuinely new installation gets a new backup repository by default.
func (s *Supervisor) InstallationID() (string, error) {
	rt, err := s.runtimeDir()
	if err != nil {
		return "", err
	}
	marker := filepath.Join(rt, installationIDMarker)
	if raw, err := os.ReadFile(marker); err == nil {
		value := strings.TrimSpace(string(raw))
		decoded, decodeErr := hex.DecodeString(value)
		if decodeErr != nil || len(decoded) != 16 {
			return "", fmt.Errorf("compose: invalid installation identity %s", marker)
		}
		return value, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if err := os.MkdirAll(rt, 0o755); err != nil {
		return "", err
	}
	value, err := generateInstallationID()
	if err != nil {
		return "", err
	}
	file, err := os.OpenFile(marker, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return s.InstallationID()
	}
	if err != nil {
		return "", err
	}
	if _, err := file.WriteString(value + "\n"); err != nil {
		_ = file.Close()
		_ = os.Remove(marker)
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(marker)
		return "", err
	}
	return value, nil
}

// RotateInstallationID gives the next set of newly created database volumes a
// fresh backup namespace. The previous repository is deliberately retained so
// it can still be restored with its exported key.
func (s *Supervisor) RotateInstallationID() (string, string, error) {
	rt, err := s.runtimeDir()
	if err != nil {
		return "", "", err
	}
	oldID, err := s.InstallationID()
	if err != nil {
		return "", "", err
	}
	newID, err := generateInstallationID()
	if err != nil {
		return "", "", err
	}
	staging, err := os.CreateTemp(rt, ".installation-id-*")
	if err != nil {
		return "", "", err
	}
	stagingPath := staging.Name()
	if err := staging.Chmod(0o600); err != nil {
		_ = staging.Close()
		_ = os.Remove(stagingPath)
		return "", "", err
	}
	if _, err := staging.WriteString(newID + "\n"); err != nil {
		_ = staging.Close()
		_ = os.Remove(stagingPath)
		return "", "", err
	}
	if err := staging.Close(); err != nil {
		_ = os.Remove(stagingPath)
		return "", "", err
	}
	if err := os.Rename(stagingPath, filepath.Join(rt, installationIDMarker)); err != nil {
		_ = os.Remove(stagingPath)
		return "", "", err
	}
	return oldID, newID, nil
}

func generateInstallationID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// ProjectNameForMode is the canonical compose project name for a stack mode.
func ProjectNameForMode(mode Mode) string {
	return ProjectNameForModeAndInstance(mode, instance.Current())
}

// ProjectNameForModeAndInstance returns the stable Compose project name. The
// empty instance retains the pre-multi-instance project names exactly.
func ProjectNameForModeAndInstance(mode Mode, name string) string {
	if mode == "" {
		mode = ModeProd
	}
	if name != "" {
		return "openneko-" + name + "-" + string(mode)
	}
	return "openneko-" + string(mode)
}

// ModeFromProjectName recovers the stack mode from a project marker written by
// ProjectName. The bool is false for legacy names or unrelated projects.
func ModeFromProjectName(project string) (Mode, bool) {
	mode, _, ok := IdentityFromProjectName(project)
	return mode, ok
}

// IdentityFromProjectName parses both legacy openneko-<mode> projects and
// named openneko-<instance>-<mode> projects.
func IdentityFromProjectName(project string) (Mode, string, bool) {
	const prefix = "openneko-"
	if !strings.HasPrefix(project, prefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(project, prefix)
	for _, mode := range []Mode{ModeProd, ModeDev, ModeDemo} {
		if rest == string(mode) {
			return mode, "", true
		}
		suffix := "-" + string(mode)
		if strings.HasSuffix(rest, suffix) {
			name := strings.TrimSuffix(rest, suffix)
			if instance.Validate(name) == nil && name != "" {
				return mode, name, true
			}
		}
	}
	return "", "", false
}

// ImageVersion reads the persisted image tag for this install. An empty string
// means callers should use their normal default.
func (s *Supervisor) ImageVersion() (string, error) {
	rt, err := s.runtimeDir()
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(filepath.Join(rt, imageVersionMarker))
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// WriteImageVersion persists the image tag subsequent starts should use.
func (s *Supervisor) WriteImageVersion(version string) error {
	rt, err := s.runtimeDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(rt, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(rt, imageVersionMarker), []byte(strings.TrimSpace(version)+"\n"), 0o644)
}

// Run shells out to `docker compose -p <project> -f <files…> <args…>`,
// forwarding I/O and signals. Returns the child's exit code. Pass
// projectName as the empty string to let docker compose default to the
// runtime dir name (not recommended).
func (s *Supervisor) Run(ctx context.Context, projectName string, files, args []string, stdout, stderr *os.File) (int, error) {
	dockerArgs := []string{"compose"}
	if projectName != "" {
		dockerArgs = append(dockerArgs, "-p", projectName)
	}
	for _, f := range files {
		dockerArgs = append(dockerArgs, "-f", f)
	}
	dockerArgs = append(dockerArgs, args...)
	cmd := exec.CommandContext(ctx, "docker", dockerArgs...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Start(); err != nil {
		return 1, err
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigs)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	for {
		select {
		case sig := <-sigs:
			if cmd.Process != nil {
				_ = cmd.Process.Signal(sig)
			}
		case err := <-done:
			if err != nil {
				var exitErr *exec.ExitError
				if errors.As(err, &exitErr) {
					return exitErr.ExitCode(), nil
				}
				return 1, err
			}
			return 0, nil
		}
	}
}

// EnsureImage pulls image unless it's already present locally. Used to warm the
// agent sandbox image at install time so the gateway's first sandbox-create
// (i.e. the user's first chat) doesn't block on a multi-hundred-MB pull.
func (s *Supervisor) EnsureImage(ctx context.Context, image string, stdout, stderr *os.File) error {
	if exec.CommandContext(ctx, "docker", "image", "inspect", image).Run() == nil {
		return nil // already local
	}
	return s.PullImage(ctx, image, stdout, stderr)
}

// PullImage always asks Docker for the image ref, even if a local tag exists.
func (s *Supervisor) PullImage(ctx context.Context, image string, stdout, stderr *os.File) error {
	cmd := exec.CommandContext(ctx, "docker", "pull", image)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}
