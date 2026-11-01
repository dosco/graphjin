package secrets

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/open-neko/neko/apps/openneko/internal/config"
)

const StoreFilename = "secrets.json"

// OperatorsKey is the worker-owned per-operator credential section. The
// CLI never parses it; Write preserves it (and any other section it
// doesn't own) byte-for-byte to avoid the dual-writer clobber.
const OperatorsKey = "_operators"

type Store map[string]map[string]string

func Path(overrideDir string) string {
	return filepath.Join(config.Dir(overrideDir), StoreFilename)
}

func Read(overrideDir string) (Store, error) {
	file := Path(overrideDir)
	raw, err := os.ReadFile(file)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Store{}, nil
		}
		return nil, err
	}
	var parsed map[string]map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("secrets store at %s is invalid JSON: %w", file, err)
	}
	out := Store{}
	for pkg, env := range parsed {
		if pkg == OperatorsKey {
			continue
		}
		m := map[string]string{}
		for k, v := range env {
			if s, ok := v.(string); ok {
				plain, err := config.MaybeDecryptValue(overrideDir, s)
				if err != nil {
					return nil, fmt.Errorf("secrets store %s/%s: %w", pkg, k, err)
				}
				m[k] = plain
			}
		}
		out[pkg] = m
	}
	return out, nil
}

func ReadSoft(overrideDir string, warn func(string)) Store {
	s, err := Read(overrideDir)
	if err != nil {
		if warn != nil {
			warn(fmt.Sprintf("secrets store unreadable; treating as empty: %v", err))
		}
		return Store{}
	}
	return s
}

func Write(store Store, overrideDir string) error {
	file := Path(overrideDir)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}

	// Sections this CLI doesn't own (the worker's _operators blobs, or
	// anything a future writer adds) are carried over verbatim.
	preserved := map[string]json.RawMessage{}
	if raw, err := os.ReadFile(file); err == nil {
		var full map[string]json.RawMessage
		if jsonErr := json.Unmarshal(raw, &full); jsonErr == nil {
			for k, v := range full {
				if k == OperatorsKey || !isAllStringObject(v) {
					preserved[k] = v
				}
			}
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	pkgs := make([]string, 0, len(store))
	for k := range store {
		if _, taken := preserved[k]; taken {
			return fmt.Errorf("secrets store section %q is not CLI-owned", k)
		}
		pkgs = append(pkgs, k)
	}
	sort.Strings(pkgs)
	preservedKeys := make([]string, 0, len(preserved))
	for k := range preserved {
		preservedKeys = append(preservedKeys, k)
	}
	sort.Strings(preservedKeys)

	var buf bytes.Buffer
	buf.WriteString("{")
	wrote := 0
	for _, pkg := range pkgs {
		if wrote > 0 {
			buf.WriteString(",")
		}
		wrote++
		buf.WriteString("\n  ")
		writeQuoted(&buf, pkg)
		buf.WriteString(": {")
		inner := store[pkg]
		keys := make([]string, 0, len(inner))
		for k := range inner {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for j, k := range keys {
			if j > 0 {
				buf.WriteString(",")
			}
			buf.WriteString("\n    ")
			writeQuoted(&buf, k)
			buf.WriteString(": ")
			enc, err := config.EncryptValue(overrideDir, inner[k])
			if err != nil {
				return fmt.Errorf("encrypt %s/%s: %w", pkg, k, err)
			}
			writeQuoted(&buf, enc)
		}
		if len(keys) > 0 {
			buf.WriteString("\n  ")
		}
		buf.WriteString("}")
	}
	for _, k := range preservedKeys {
		if wrote > 0 {
			buf.WriteString(",")
		}
		wrote++
		buf.WriteString("\n  ")
		writeQuoted(&buf, k)
		buf.WriteString(": ")
		var indented bytes.Buffer
		if err := json.Indent(&indented, preserved[k], "  ", "  "); err == nil {
			buf.Write(indented.Bytes())
		} else {
			buf.Write(preserved[k])
		}
	}
	if wrote > 0 {
		buf.WriteString("\n")
	}
	buf.WriteString("}\n")

	if err := os.WriteFile(file, buf.Bytes(), 0o600); err != nil {
		return err
	}
	_ = os.Chmod(file, 0o600)
	return nil
}

// isAllStringObject reports whether a raw JSON value is an object whose
// values are all strings — the shape of a CLI-owned env section.
func isAllStringObject(raw json.RawMessage) bool {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return false
	}
	for _, v := range obj {
		if _, ok := v.(string); !ok {
			return false
		}
	}
	return true
}

func writeQuoted(buf *bytes.Buffer, s string) {
	b, _ := json.Marshal(s)
	buf.Write(b)
}

var envKeyRX = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

func IsValidEnvKey(key string) bool {
	return envKeyRX.MatchString(key)
}

func Set(store Store, plugin, key, value string) (Store, error) {
	if !IsValidEnvKey(key) {
		return nil, fmt.Errorf(`env key %q must be UPPER_SNAKE_CASE`, key)
	}
	if store == nil {
		store = Store{}
	}
	existing := store[plugin]
	next := make(map[string]string, len(existing)+1)
	maps.Copy(next, existing)
	next[key] = value
	out := Store{}
	for p, m := range store {
		if p == plugin {
			continue
		}
		out[p] = m
	}
	out[plugin] = next
	return out, nil
}

func Unset(store Store, plugin, key string) (Store, bool) {
	existing, ok := store[plugin]
	if !ok {
		return store, false
	}
	if _, ok := existing[key]; !ok {
		return store, false
	}
	next := map[string]string{}
	for k, v := range existing {
		if k == key {
			continue
		}
		next[k] = v
	}
	out := Store{}
	for p, m := range store {
		if p == plugin {
			continue
		}
		out[p] = m
	}
	if len(next) > 0 {
		out[plugin] = next
	}
	return out, true
}

func ListKeysForSection(store Store, section string) []string {
	keys := make([]string, 0, len(store[section]))
	for k := range store[section] {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func AllValues(store Store) []string {
	set := map[string]struct{}{}
	for _, env := range store {
		for _, v := range env {
			if v != "" {
				set[v] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
