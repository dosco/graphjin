// Package installation persists the non-secret host settings needed to target
// the same OpenNeko stack on every later CLI invocation.
package installation

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/open-neko/neko/apps/openneko/internal/instance"
)

const (
	Filename       = "installation.json"
	CurrentVersion = 1
)

// Settings is intentionally free of credentials. Secret material remains in
// the existing encrypted per-instance config and repository-bound key stores.
type Settings struct {
	Version        int    `json:"version"`
	Instance       string `json:"instance,omitempty"`
	Mode           string `json:"mode"`
	WebPort        int    `json:"web_port"`
	WebBindAddress string `json:"web_bind_address"`
}

func Path(configDir string) string {
	return filepath.Join(configDir, Filename)
}

func Load(configDir string) (Settings, bool, error) {
	raw, err := os.ReadFile(Path(configDir))
	if errors.Is(err, fs.ErrNotExist) {
		return Settings{}, false, nil
	}
	if err != nil {
		return Settings{}, false, err
	}
	var settings Settings
	if err := json.Unmarshal(raw, &settings); err != nil {
		return Settings{}, false, fmt.Errorf("read installation settings: %w", err)
	}
	if err := settings.Validate(); err != nil {
		return Settings{}, false, fmt.Errorf("invalid installation settings %s: %w", Path(configDir), err)
	}
	return settings, true, nil
}

func Save(configDir string, settings Settings) error {
	if settings.Version == 0 {
		settings.Version = CurrentVersion
	}
	if err := settings.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp, err := os.CreateTemp(configDir, ".installation-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, Path(configDir))
}

func (s Settings) Validate() error {
	if s.Version != CurrentVersion {
		return fmt.Errorf("unsupported settings version %d", s.Version)
	}
	if err := instance.Validate(s.Instance); err != nil {
		return err
	}
	switch s.Mode {
	case "prod", "dev", "demo":
	default:
		return fmt.Errorf("mode must be prod, dev, or demo")
	}
	if err := validatePort("web port", s.WebPort); err != nil {
		return err
	}
	address, err := netip.ParseAddr(strings.TrimSpace(s.WebBindAddress))
	if err != nil || !address.Is4() {
		return fmt.Errorf("web bind address must be an IPv4 address")
	}
	return nil
}

func validatePort(label string, port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("%s must be between 1 and 65535", label)
	}
	return nil
}

// ApplyEnvironment makes persisted settings visible to the existing Compose
// and CLI code. When overwrite is false, an explicit operator environment
// override retains precedence.
func ApplyEnvironment(settings Settings, overwrite bool) error {
	values := map[string]string{
		"OPENNEKO_PORT":             strconv.Itoa(settings.WebPort),
		"OPENNEKO_WEB_BIND_ADDRESS": settings.WebBindAddress,
	}
	for key, value := range values {
		if !overwrite && strings.TrimSpace(os.Getenv(key)) != "" {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return nil
}
