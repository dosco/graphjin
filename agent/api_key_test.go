package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigAPIKeyPrefersFileAndRereadsIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key")
	t.Setenv("GJ_TEST_AGENT_KEY", "from-env")
	cfg := Config{APIKeyEnv: "GJ_TEST_AGENT_KEY"}
	if got := cfg.APIKey(); got != "from-env" {
		t.Fatalf("env key = %q", got)
	}
	cfg.APIKeyFile = path
	if got := cfg.APIKey(); got != "" {
		t.Fatalf("missing key file = %q, want empty", got)
	}
	if err := os.WriteFile(path, []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := cfg.APIKey(); got != "first" {
		t.Fatalf("file key = %q", got)
	}
	if err := os.WriteFile(path, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := cfg.APIKey(); got != "second" {
		t.Fatalf("rotated file key = %q", got)
	}
}
