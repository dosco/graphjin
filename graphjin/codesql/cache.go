package codesql

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var safeNameRE = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

// CachePath returns config/codesql/<database-name>-<source-root-hash>.sqlite.
func CachePath(name, root, cacheDir string) (string, error) {
	if strings.TrimSpace(name) == "" {
		name = "default"
	}
	if strings.TrimSpace(cacheDir) == "" {
		return "", fmt.Errorf("codesql cache dir is required")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	name = safeNameRE.ReplaceAllString(name, "_")
	sum := sha256.Sum256([]byte(absRoot))
	short := hex.EncodeToString(sum[:])[:16]
	return filepath.Join(cacheDir, fmt.Sprintf("%s-%s.sqlite", name, short)), nil
}

func ensureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}
