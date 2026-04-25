package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ClientConfig is the persisted CLI client configuration — the server to talk
// to plus the JWT minted for this user. Stored at
// `$XDG_CONFIG_HOME/graphjin/client.json` (falls back to
// `~/.config/graphjin/client.json`).
type ClientConfig struct {
	Server    string    `json:"server"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
	Issuer    string    `json:"issuer,omitempty"`
	Subject   string    `json:"subject,omitempty"`
	Email     string    `json:"email,omitempty"`
}

// ClientConfigPath returns the absolute path to the client.json file,
// honoring XDG_CONFIG_HOME via os.UserConfigDir().
func ClientConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "graphjin", "client.json"), nil
}

// LoadClientConfig reads the config file. Returns (nil, nil) if the file does
// not exist — callers treat that as "not yet signed in."
func LoadClientConfig() (*ClientConfig, error) {
	p, err := ClientConfigPath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var c ClientConfig
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	return &c, nil
}

// SaveClientConfig atomically writes the config with 0600 perms.
func SaveClientConfig(c *ClientConfig) error {
	if c == nil {
		return errors.New("nil ClientConfig")
	}
	p, err := ClientConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".client.json.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		// Best-effort cleanup on failure; Rename below removes the file on success.
		if _, err := os.Stat(tmpName); err == nil {
			_ = os.Remove(tmpName)
		}
	}()
	if err := os.Chmod(tmpName, 0o600); err != nil {
		tmp.Close() //nolint:errcheck
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close() //nolint:errcheck
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, p)
}

// DeleteClientConfig removes the config file. Returns nil if it doesn't exist.
func DeleteClientConfig() error {
	p, err := ClientConfigPath()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
