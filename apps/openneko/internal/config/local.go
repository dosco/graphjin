package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/open-neko/neko/apps/openneko/internal/instance"
)

// LocalPg mirrors the `pg` block of ~/.config/openneko/config.json, written
// by the web /setup wizard (see packages/db/src/local-config.ts). On a fresh
// install the file doesn't exist and every consumer falls back to its env-
// var defaults; once /setup runs the rotated password lands here and every
// reader picks it up from this single source.
type LocalPg struct {
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	User     string `json:"user,omitempty"`
	Password string `json:"password,omitempty"`
	Database string `json:"database,omitempty"`
	SSLMode  string `json:"sslmode,omitempty"`
}

type Local struct {
	// DatabaseURL is a postgres:// URL for OpenNeko's database. It replaces Pg.
	DatabaseURL string   `json:"database_url,omitempty"`
	Pg          *LocalPg `json:"pg,omitempty"`
}

// ReadLocal returns the local config, plus the path it was read from (empty
// if no file was found). A missing or malformed file is not an error — the
// caller layers what it finds over its own defaults.
func ReadLocal(override string) (Local, string) {
	candidates := []string{filepath.Join(Dir(override), "config.json")}
	// Pre-rebrand fallback path; matches the TS reader.
	if override == "" && instance.Current() == "" {
		base := os.Getenv("XDG_CONFIG_HOME")
		if base == "" {
			if home, err := os.UserHomeDir(); err == nil && home != "" {
				base = filepath.Join(home, ".config")
			}
		}
		if base != "" {
			candidates = append(candidates, filepath.Join(base, "neko", "config.json"))
		}
	}
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var lc Local
		if err := json.Unmarshal(data, &lc); err != nil {
			continue
		}
		// pg.password is encrypted at rest (enc:v1) by the web /setup
		// writer; legacy plaintext passes through unchanged.
		if lc.Pg != nil && lc.Pg.Password != "" {
			if plain, err := MaybeDecryptValue(override, lc.Pg.Password); err == nil {
				lc.Pg.Password = plain
			}
		}
		if lc.DatabaseURL != "" {
			if plain, err := MaybeDecryptValue(override, lc.DatabaseURL); err == nil {
				lc.DatabaseURL = plain
			}
		}
		return lc, path
	}
	return Local{}, ""
}

// WriteLocalPgPassword persists the rotated DB password into the host config
// (~/.config/openneko/config.json), encrypted at rest (enc:v1) with the host
// secret-key — the single source ReadLocal reads.
//
// The web /setup wizard writes the same field, but it runs in a container and
// can only reach the in-container config volume, never the host file. The CLI
// is the one component that both holds the plaintext password and can write the
// host path, so it bridges the gap here. Existing fields are preserved.
func WriteLocalPgPassword(override, password string) error {
	enc, err := EncryptValue(override, password)
	if err != nil {
		return err
	}
	dir := Dir(override)
	path := filepath.Join(dir, "config.json")

	// Merge over the raw file so only pg.password changes and every other
	// key, known or not, stays as written.
	doc := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &doc)
	}
	pg, _ := doc["pg"].(map[string]any)
	if pg == nil {
		pg = map[string]any{}
	}
	pg["password"] = enc
	doc["pg"] = pg

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o600)
}
