package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
)

// The defining property of `graphjin config set/unset` is that it edits values
// while preserving the surrounding comments and structure — a plain
// marshal/unmarshal would destroy them.

const commentedConfig = `# yaml-language-server: $schema=./config.schema.json
app_name: "Test App" # friendly name
mode: dev

# Logging level must be one of debug, error, warn, info
log_level: "info"

# API rate limits
rate_limiter:
  # events per second
  rate: 10
  bucket: 5
`

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "dev.yml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return p
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(b)
}

func TestConfigSet_PreservesCommentsAndEditsValue(t *testing.T) {
	log = zap.NewNop().Sugar()
	p := writeConfig(t, commentedConfig)
	confFile = p
	t.Cleanup(func() { confFile = "" })

	editConfigFile("log_level", "debug", false)

	out := readFile(t, p)
	for _, want := range []string{
		"# yaml-language-server: $schema=./config.schema.json",
		"# Logging level must be one of debug, error, warn, info",
		"# events per second",
		"# friendly name",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("comment %q was lost:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "log_level: debug") && !strings.Contains(out, `log_level: "debug"`) {
		t.Fatalf("log_level not updated:\n%s", out)
	}
}

func TestConfigSet_NestedValueParsedAsYAML(t *testing.T) {
	log = zap.NewNop().Sugar()
	p := writeConfig(t, commentedConfig)
	confFile = p
	t.Cleanup(func() { confFile = "" })

	editConfigFile("rate_limiter.rate", "42", false)

	out := readFile(t, p)
	if !strings.Contains(out, "rate: 42") {
		t.Fatalf("nested rate not set to numeric 42:\n%s", out)
	}
	if !strings.Contains(out, "# events per second") {
		t.Fatalf("nested comment lost:\n%s", out)
	}
}

func TestConfigUnset_RemovesKeyKeepingSiblings(t *testing.T) {
	log = zap.NewNop().Sugar()
	p := writeConfig(t, commentedConfig)
	confFile = p
	t.Cleanup(func() { confFile = "" })

	editConfigFile("rate_limiter.bucket", "", true)

	out := readFile(t, p)
	if strings.Contains(out, "bucket:") {
		t.Fatalf("bucket should have been removed:\n%s", out)
	}
	if !strings.Contains(out, "rate: 10") {
		t.Fatalf("sibling rate should remain:\n%s", out)
	}
}
