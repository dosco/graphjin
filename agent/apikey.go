package agent

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"
)

// maxAPIKeyFileBytes bounds what a key file may hold. Provider keys are a few
// hundred bytes at most; anything larger is not a key file.
const maxAPIKeyFileBytes = 16 * 1024

// ErrAPIKeySourceConflict reports a config that names both key sources. One
// source keeps "where did this request's key come from?" answerable.
var ErrAPIKeySourceConflict = errors.New("agent.api_key_env and agent.api_key_file are mutually exclusive; set only one")

// ValidateKeySource rejects a config that names both an environment variable
// and a file for the provider key.
func (c Config) ValidateKeySource() error {
	if strings.TrimSpace(c.APIKeyEnv) != "" && strings.TrimSpace(c.APIKeyFile) != "" {
		return ErrAPIKeySourceConflict
	}
	return nil
}

// KeySource names where the provider key is read from, for operator-facing
// messages: "file <path>" or the environment variable's name. It never
// includes the key itself.
func (c Config) KeySource() string {
	c = c.withDefaults()
	if path := strings.TrimSpace(c.APIKeyFile); path != "" {
		return "file " + path
	}
	return c.APIKeyEnv
}

// ResolveAPIKey reads the provider key from its configured source. It reads
// fresh on every call — that is the point of api_key_file: an operator
// rewrites the file and the next client built picks the new key up, with no
// restart. A missing or empty key wraps ErrMissingAPIKey so every caller's
// existing fail-closed handling applies.
func (c Config) ResolveAPIKey() (string, error) {
	if err := c.ValidateKeySource(); err != nil {
		return "", err
	}
	c = c.withDefaults()
	if path := strings.TrimSpace(c.APIKeyFile); path != "" {
		return readAPIKeyFile(path)
	}
	key := strings.TrimSpace(os.Getenv(c.APIKeyEnv))
	if key == "" {
		return "", fmt.Errorf("%w: set %s", ErrMissingAPIKey, c.APIKeyEnv)
	}
	return key, nil
}

// APIKeyConfigured reports whether ResolveAPIKey would currently succeed.
func (c Config) APIKeyConfigured() bool {
	_, err := c.ResolveAPIKey()
	return err == nil
}

// readAPIKeyFile reads one key from path. The file must hold a single token:
// surrounding whitespace (a trailing newline from `echo`) is trimmed, but
// whitespace or control characters inside it mean the path names something
// other than a key file, and its contents are never sent to a provider.
// Errors name the path, never the contents.
func readAPIKeyFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("%w: agent.api_key_file %s is not readable", ErrMissingAPIKey, path)
	}
	defer f.Close() //nolint:errcheck
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("%w: agent.api_key_file %s is not a regular file", ErrMissingAPIKey, path)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxAPIKeyFileBytes+1))
	if err != nil {
		return "", fmt.Errorf("%w: agent.api_key_file %s is not readable", ErrMissingAPIKey, path)
	}
	if len(data) > maxAPIKeyFileBytes {
		return "", fmt.Errorf("%w: agent.api_key_file %s is larger than %d bytes", ErrMissingAPIKey, path, maxAPIKeyFileBytes)
	}
	key := strings.TrimSpace(string(data))
	if key == "" {
		return "", fmt.Errorf("%w: agent.api_key_file %s is empty", ErrMissingAPIKey, path)
	}
	for _, r := range key {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return "", fmt.Errorf("%w: agent.api_key_file %s must hold a single key", ErrMissingAPIKey, path)
		}
	}
	return key, nil
}
