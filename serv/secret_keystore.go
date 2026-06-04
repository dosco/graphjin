package serv

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/dosco/graphjin/core/v3"
	"gopkg.in/yaml.v3"
)

const (
	secretRefPrefix             = "gjsecret://"
	defaultLocalKeystoreFile    = "secrets.enc.yml"
	localKeystoreAlgorithm      = "AES-256-GCM"
	localKeystoreVersion        = 1
	localKeystoreNonceSize      = 12
	localKeystoreAdditionalData = "graphjin-keystore-v1:"
)

type localKeystore struct {
	path    string
	key     []byte
	entries map[string]localKeystoreEntry
}

type localKeystoreFile struct {
	Version   int                           `yaml:"version"`
	UpdatedAt string                        `yaml:"updated_at,omitempty"`
	Secrets   map[string]localKeystoreEntry `yaml:"secrets,omitempty"`
}

type localKeystoreEntry struct {
	Algorithm  string `yaml:"algorithm"`
	Nonce      string `yaml:"nonce"`
	Ciphertext string `yaml:"ciphertext"`
}

func newLocalKeystore(conf *Config) (*localKeystore, error) {
	ks := &localKeystore{
		path:    resolveLocalKeystorePath(conf),
		entries: make(map[string]localKeystoreEntry),
	}
	keyText := ""
	if conf != nil {
		keyText = strings.TrimSpace(conf.Secrets.Keystore.Key)
	}
	if keyText == "" {
		return ks, nil
	}
	key, err := decodeLocalKeystoreKey(keyText)
	if err != nil {
		return nil, err
	}
	ks.key = key
	if err := ks.load(); err != nil {
		return nil, err
	}
	return ks, nil
}

func resolveLocalKeystorePath(conf *Config) string {
	path := ""
	if conf != nil {
		path = strings.TrimSpace(conf.Secrets.Keystore.Path)
	}
	if path == "" {
		path = defaultLocalKeystoreFile
	}
	if filepath.IsAbs(path) {
		return path
	}
	base := "."
	if conf != nil {
		if conf.viper != nil && conf.viper.ConfigFileUsed() != "" {
			base = filepath.Dir(conf.viper.ConfigFileUsed())
		} else if conf.ConfigPath != "" {
			base = conf.ConfigPath
		}
	}
	return filepath.Clean(filepath.Join(base, path))
}

func decodeLocalKeystoreKey(value string) ([]byte, error) {
	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	var lastErr error
	for _, enc := range encodings {
		key, err := enc.DecodeString(value)
		if err != nil {
			lastErr = err
			continue
		}
		if len(key) != 32 {
			return nil, fmt.Errorf("secrets.keystore.key must decode to 32 bytes for AES-256-GCM, got %d bytes", len(key))
		}
		return key, nil
	}
	if lastErr != nil {
		return nil, fmt.Errorf("secrets.keystore.key must be a base64-encoded 32-byte key: %w", lastErr)
	}
	return nil, fmt.Errorf("secrets.keystore.key must be a base64-encoded 32-byte key")
}

func (ks *localKeystore) hasKey() bool {
	return ks != nil && len(ks.key) == 32
}

func (ks *localKeystore) load() error {
	if ks == nil || ks.path == "" {
		return nil
	}
	data, err := os.ReadFile(ks.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read secrets keystore %s: %w", ks.path, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}
	var file localKeystoreFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return fmt.Errorf("parse secrets keystore %s: %w", ks.path, err)
	}
	if file.Version != 0 && file.Version != localKeystoreVersion {
		return fmt.Errorf("unsupported secrets keystore version %d in %s", file.Version, ks.path)
	}
	if file.Secrets != nil {
		ks.entries = file.Secrets
	}
	if ks.entries == nil {
		ks.entries = make(map[string]localKeystoreEntry)
	}
	return nil
}

func (ks *localKeystore) Seal(ref, plaintext string) error {
	if _, err := parseSecretRef(ref); err != nil {
		return err
	}
	if !ks.hasKey() {
		return missingLocalKeystoreKeyError([]string{ref})
	}
	block, err := aes.NewCipher(ks.key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, localKeystoreNonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return fmt.Errorf("generate secret nonce: %w", err)
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), []byte(localKeystoreAdditionalData+ref))
	ks.entries[ref] = localKeystoreEntry{
		Algorithm:  localKeystoreAlgorithm,
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	}
	return nil
}

func (ks *localKeystore) Open(ref string) (string, error) {
	if _, err := parseSecretRef(ref); err != nil {
		return "", err
	}
	if !ks.hasKey() {
		return "", missingLocalKeystoreKeyError([]string{ref})
	}
	entry, ok := ks.entries[ref]
	if !ok {
		return "", fmt.Errorf("secret ref %s not found in keystore %s", ref, ks.path)
	}
	if entry.Algorithm != "" && entry.Algorithm != localKeystoreAlgorithm {
		return "", fmt.Errorf("secret ref %s uses unsupported algorithm %q", ref, entry.Algorithm)
	}
	nonce, err := base64.StdEncoding.DecodeString(entry.Nonce)
	if err != nil {
		return "", fmt.Errorf("secret ref %s has invalid nonce: %w", ref, err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(entry.Ciphertext)
	if err != nil {
		return "", fmt.Errorf("secret ref %s has invalid ciphertext: %w", ref, err)
	}
	block, err := aes.NewCipher(ks.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, []byte(localKeystoreAdditionalData+ref))
	if err != nil {
		return "", fmt.Errorf("decrypt secret ref %s from keystore %s: %w", ref, ks.path, err)
	}
	return string(plaintext), nil
}

func (ks *localKeystore) Save(keepRefs map[string]struct{}) error {
	if !ks.hasKey() {
		return missingLocalKeystoreKeyError(nil)
	}
	if ks.path == "" {
		return fmt.Errorf("secrets.keystore.path is empty")
	}
	if keepRefs != nil {
		for ref := range ks.entries {
			if _, keep := keepRefs[ref]; !keep {
				delete(ks.entries, ref)
			}
		}
	}
	file := localKeystoreFile{
		Version:   localKeystoreVersion,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Secrets:   ks.entries,
	}
	data, err := yaml.Marshal(file)
	if err != nil {
		return fmt.Errorf("encode secrets keystore: %w", err)
	}
	dir := filepath.Dir(ks.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create secrets keystore directory %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".secrets-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary secrets keystore: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) //nolint:errcheck
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close() //nolint:errcheck
		return fmt.Errorf("set temporary secrets keystore permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close() //nolint:errcheck
		return fmt.Errorf("write temporary secrets keystore: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close() //nolint:errcheck
		return fmt.Errorf("sync temporary secrets keystore: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary secrets keystore: %w", err)
	}
	if err := os.Rename(tmpName, ks.path); err != nil {
		return fmt.Errorf("replace secrets keystore %s: %w", ks.path, err)
	}
	if err := os.Chmod(ks.path, 0o600); err != nil {
		return fmt.Errorf("set secrets keystore permissions %s: %w", ks.path, err)
	}
	return nil
}

func parseSecretRef(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if !strings.HasPrefix(ref, secretRefPrefix) {
		return "", fmt.Errorf("invalid secret ref %q: expected %s prefix", ref, secretRefPrefix)
	}
	path := strings.TrimPrefix(ref, secretRefPrefix)
	if path == "" {
		return "", fmt.Errorf("invalid secret ref %q: missing path", ref)
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf("invalid secret ref %q: invalid path segment", ref)
		}
	}
	return ref, nil
}

func isSecretRef(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), secretRefPrefix)
}

func missingLocalKeystoreKeyError(paths []string) error {
	sort.Strings(paths)
	msg := "secrets.keystore.key must be set before setting secret config values or reading encrypted secret refs"
	if len(paths) > 0 {
		msg += " (" + strings.Join(paths, ", ") + ")"
	}
	msg += "; set secrets.keystore.key, for example with GJ_SECRETS_KEYSTORE_KEY, and retry"
	return fmt.Errorf("%s", msg)
}

func (s *graphjinService) localKeystore() (*localKeystore, error) {
	if s == nil || s.conf == nil {
		return nil, fmt.Errorf("config not available")
	}
	ks, err := newLocalKeystore(s.conf)
	if err != nil {
		return nil, err
	}
	s.secretStore = ks
	return ks, nil
}

func (s *graphjinService) hydrateCoreConfigSecrets(conf *core.Config) error {
	if conf == nil || !configContainsSecretRefs(conf) {
		return nil
	}
	ks, err := s.localKeystore()
	if err != nil {
		return err
	}
	if !ks.hasKey() {
		return missingLocalKeystoreKeyError(secretRefsInConfig(conf))
	}
	return hydrateConfigSecrets(conf, ks)
}

func (s *graphjinService) hydrateLegacyDatabaseSecrets(db *Database) error {
	if db == nil || !configContainsSecretRefs(db) {
		return nil
	}
	ks, err := s.localKeystore()
	if err != nil {
		return err
	}
	if !ks.hasKey() {
		return missingLocalKeystoreKeyError(secretRefsInConfig(db))
	}
	return hydrateConfigSecrets(db, ks)
}

func hydrateConfigSecrets(target any, ks *localKeystore) error {
	return transformConfigSecretStrings(target, func(_ []string, value string) (string, error) {
		if !isSecretRef(value) {
			return value, nil
		}
		return ks.Open(strings.TrimSpace(value))
	})
}

func sealCoreConfigSecrets(conf *core.Config, ks *localKeystore) (map[string]struct{}, error) {
	usedRefs := make(map[string]struct{})
	err := transformConfigSecretStrings(conf, func(path []string, value string) (string, error) {
		if !configSecretPathSensitive(path) {
			return value, nil
		}
		value = strings.TrimSpace(value)
		if value == "" {
			return value, nil
		}
		if isSecretRef(value) {
			ref, err := parseSecretRef(value)
			if err != nil {
				return value, err
			}
			usedRefs[ref] = struct{}{}
			return value, nil
		}
		if configValueIsExternalRef(value) {
			return value, nil
		}
		ref := secretRefForPath(path)
		if err := ks.Seal(ref, value); err != nil {
			return value, err
		}
		usedRefs[ref] = struct{}{}
		return ref, nil
	})
	if err != nil {
		return nil, err
	}
	for _, ref := range secretRefsInConfig(conf) {
		usedRefs[ref] = struct{}{}
	}
	return usedRefs, nil
}

func configContainsSecretRefs(target any) bool {
	return len(secretRefsInConfig(target)) > 0
}

func secretRefsInConfig(target any) []string {
	refs := make(map[string]struct{})
	walkConfigStrings(reflect.ValueOf(target), func(value string) {
		if isSecretRef(value) {
			if ref, err := parseSecretRef(value); err == nil {
				refs[ref] = struct{}{}
			}
		}
	})
	out := make([]string, 0, len(refs))
	for ref := range refs {
		out = append(out, ref)
	}
	sort.Strings(out)
	return out
}

func plaintextSecretUpdatePaths(target any) []string {
	var paths []string
	collectPlaintextSecretUpdatePaths(target, nil, &paths)
	sort.Strings(paths)
	return compactStrings(paths)
}

func collectPlaintextSecretUpdatePaths(v any, path []string, out *[]string) {
	switch x := v.(type) {
	case map[string]any:
		if name, _ := x["name"].(string); isSensitiveConfigKey(name) {
			if value, _ := x["value"].(string); shouldSealPlainSecretValue(value) {
				*out = append(*out, strings.Join(append(path, "value"), "."))
			}
		}
		keys := make([]string, 0, len(x))
		for key := range x {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			collectPlaintextSecretUpdatePaths(x[key], append(path, key), out)
		}
	case []any:
		for i, item := range x {
			segment := strconv.Itoa(i)
			if name := namedMapElementName(item); name != "" {
				segment = name
			}
			collectPlaintextSecretUpdatePaths(item, append(path, segment), out)
		}
	case string:
		if configSecretPathSensitive(path) && shouldSealPlainSecretValue(x) {
			*out = append(*out, strings.Join(path, "."))
		}
	}
}

func namedMapElementName(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	name, _ := m["name"].(string)
	return sanitizeSecretRefSegment(name)
}

func shouldSealPlainSecretValue(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && !isSecretRef(value) && !configValueIsExternalRef(value)
}

func configValueIsExternalRef(value string) bool {
	return strings.Contains(value, "${")
}

func secretRefForPath(path []string) string {
	segments := make([]string, 0, len(path))
	for _, segment := range path {
		if sanitized := sanitizeSecretRefSegment(segment); sanitized != "" {
			segments = append(segments, sanitized)
		}
	}
	return secretRefPrefix + strings.Join(segments, "/")
}

func sanitizeSecretRefSegment(segment string) string {
	segment = strings.TrimSpace(segment)
	if segment == "" {
		return ""
	}
	replacer := strings.NewReplacer("/", "_", "\\", "_", "\x00", "_")
	return replacer.Replace(segment)
}

func configSecretPathSensitive(path []string) bool {
	if len(path) == 0 {
		return false
	}
	last := path[len(path)-1]
	if isSensitiveConfigKey(last) {
		return true
	}
	if len(path) >= 2 && strings.EqualFold(path[len(path)-2], "headers") {
		return isSensitiveConfigKey(last)
	}
	return false
}

type secretStringTransform func(path []string, value string) (string, error)

func transformConfigSecretStrings(target any, fn secretStringTransform) error {
	if target == nil {
		return nil
	}
	_, _, err := transformConfigSecretValue(reflect.ValueOf(target), nil, fn)
	return err
}

func transformConfigSecretValue(v reflect.Value, path []string, fn secretStringTransform) (reflect.Value, bool, error) {
	if !v.IsValid() {
		return v, false, nil
	}
	if v.Kind() == reflect.Interface {
		if v.IsNil() {
			return v, false, nil
		}
		nv, changed, err := transformConfigSecretValue(v.Elem(), path, fn)
		if err != nil || !changed {
			return v, changed, err
		}
		if v.CanSet() {
			v.Set(nv)
			return v, true, nil
		}
		return nv, true, nil
	}
	if v.Kind() != reflect.Pointer && !v.CanSet() && v.CanInterface() {
		copyValue := reflect.New(v.Type()).Elem()
		copyValue.Set(v)
		return transformConfigSecretValue(copyValue, path, fn)
	}

	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return v, false, nil
		}
		_, changed, err := transformConfigSecretValue(v.Elem(), path, fn)
		return v, changed, err
	case reflect.String:
		if !configSecretPathSensitive(path) {
			return v, false, nil
		}
		next, err := fn(path, v.String())
		if err != nil {
			return v, false, err
		}
		if next == v.String() {
			return v, false, nil
		}
		if v.CanSet() {
			v.SetString(next)
			return v, true, nil
		}
		return reflect.ValueOf(next).Convert(v.Type()), true, nil
	case reflect.Struct:
		changed := false
		for i := 0; i < v.NumField(); i++ {
			field := v.Type().Field(i)
			if field.PkgPath != "" {
				continue
			}
			name := configFieldName(field)
			if name == "" {
				continue
			}
			_, fieldChanged, err := transformConfigSecretValue(v.Field(i), append(path, name), fn)
			if err != nil {
				return v, changed, err
			}
			changed = changed || fieldChanged
		}
		return v, changed, nil
	case reflect.Slice, reflect.Array:
		changed := false
		for i := 0; i < v.Len(); i++ {
			item := v.Index(i)
			segment := strconv.Itoa(i)
			if name := namedConfigElementName(item); name != "" {
				segment = name
			}
			nv, itemChanged, err := transformConfigSecretValue(item, append(path, segment), fn)
			if err != nil {
				return v, changed, err
			}
			if itemChanged && item.CanSet() && nv.IsValid() && nv.Type().AssignableTo(item.Type()) {
				item.Set(nv)
			}
			changed = changed || itemChanged
		}
		return v, changed, nil
	case reflect.Map:
		if v.IsNil() || v.Type().Key().Kind() != reflect.String {
			return v, false, nil
		}
		keys := v.MapKeys()
		sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
		changed := false
		for _, key := range keys {
			segment := sanitizeSecretRefSegment(key.String())
			nv, itemChanged, err := transformConfigSecretValue(v.MapIndex(key), append(path, segment), fn)
			if err != nil {
				return v, changed, err
			}
			if itemChanged && nv.IsValid() {
				elemType := v.Type().Elem()
				switch {
				case nv.Type().AssignableTo(elemType):
					v.SetMapIndex(key, nv)
				case nv.Type().ConvertibleTo(elemType):
					v.SetMapIndex(key, nv.Convert(elemType))
				case elemType.Kind() == reflect.Interface:
					v.SetMapIndex(key, nv)
				}
			}
			changed = changed || itemChanged
		}
		return v, changed, nil
	default:
		return v, false, nil
	}
}

func namedConfigElementName(v reflect.Value) string {
	for v.IsValid() && (v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer) {
		if v.IsNil() {
			return ""
		}
		v = v.Elem()
	}
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return ""
	}
	field := v.FieldByName("Name")
	if !field.IsValid() || field.Kind() != reflect.String {
		return ""
	}
	return sanitizeSecretRefSegment(field.String())
}

func configFieldName(field reflect.StructField) string {
	for _, tagName := range []string{"yaml", "json", "mapstructure"} {
		if name := configTagName(field.Tag.Get(tagName)); name != "" {
			return name
		}
	}
	return snakeCase(field.Name)
}

func configTagName(tag string) string {
	if tag == "" {
		return ""
	}
	name := strings.Split(tag, ",")[0]
	if name == "-" {
		return ""
	}
	return name
}

func snakeCase(value string) string {
	var out []rune
	for i, r := range value {
		if unicode.IsUpper(r) {
			if i > 0 {
				out = append(out, '_')
			}
			r = unicode.ToLower(r)
		}
		out = append(out, r)
	}
	return string(out)
}

func walkConfigStrings(v reflect.Value, fn func(string)) {
	if !v.IsValid() {
		return
	}
	for v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return
		}
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.String:
		fn(v.String())
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).PkgPath != "" {
				continue
			}
			walkConfigStrings(v.Field(i), fn)
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			walkConfigStrings(v.Index(i), fn)
		}
	case reflect.Map:
		for _, key := range v.MapKeys() {
			walkConfigStrings(v.MapIndex(key), fn)
		}
	}
}

func compactStrings(values []string) []string {
	if len(values) == 0 {
		return values
	}
	out := values[:0]
	var prev string
	for i, value := range values {
		if i == 0 || value != prev {
			out = append(out, value)
			prev = value
		}
	}
	return out
}
