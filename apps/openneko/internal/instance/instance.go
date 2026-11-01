// Package instance defines the host-side identity and filesystem boundaries
// for a named OpenNeko installation. Containers do not receive
// OPENNEKO_INSTANCE: their Compose project already gives them isolated named
// volumes, while the host CLI uses this package to isolate its own state.
package instance

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	// EnvName selects a named OpenNeko installation for the current CLI process.
	EnvName = "OPENNEKO_INSTANCE"
	maxName = 32
)

var validName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Current returns the selected instance name, or the empty string for the
// legacy unnamed installation.
func Current() string {
	return strings.TrimSpace(os.Getenv(EnvName))
}

// Validate checks the stable identifier used in Docker project names and host
// paths. Keeping the alphabet deliberately small avoids ambiguous or unsafe
// filesystem and Compose interpolation behavior.
func Validate(name string) error {
	if name == "" {
		return nil
	}
	if len(name) > maxName {
		return fmt.Errorf("instance name must be at most %d characters", maxName)
	}
	if !validName.MatchString(name) {
		return fmt.Errorf("instance name %q must use lowercase letters, numbers, and single hyphen separators", name)
	}
	return nil
}

// Select validates and activates a named instance for this process. An empty
// value deliberately selects the backward-compatible unnamed installation.
func Select(name string) error {
	name = strings.TrimSpace(name)
	if err := Validate(name); err != nil {
		return err
	}
	if name == "" {
		return os.Unsetenv(EnvName)
	}
	return os.Setenv(EnvName, name)
}

// ScopeConfigDir adds the per-instance namespace beneath the normal OpenNeko
// config directory. Explicit config overrides bypass this helper in config.Dir.
func ScopeConfigDir(base string) string {
	if name := Current(); name != "" {
		return filepath.Join(base, "instances", name)
	}
	return base
}

// StateHome returns the XDG-compatible host state root.
func StateHome() (string, error) {
	if base := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); base != "" {
		return filepath.Abs(base)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state"), nil
}

// StateDir returns the durable host state directory for the selected named
// instance. The bool is false for the legacy unnamed installation.
func StateDir() (string, bool, error) {
	name := Current()
	if name == "" {
		return "", false, nil
	}
	base, err := StateHome()
	if err != nil {
		return "", false, err
	}
	return filepath.Join(base, "openneko", "instances", name), true, nil
}
