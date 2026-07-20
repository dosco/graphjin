package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDemoEnvMissingFileIsNoop(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	unsetEnv(t, "GO_ENV", "GOOGLE_APIKEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GJ_AGENT_ENABLED", "GJ_AGENT_API_KEY_ENV", "GJ_AGENT_PROVIDER", "GJ_AGENT_MAX_STEPS", "GJ_AGENT_TIMEOUT_SECONDS")
	configDir := filepath.Join(dir, "demo")
	var out bytes.Buffer
	if err := loadDemoEnv(configDir, &out); err != nil {
		t.Fatalf("load missing .env: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("expected no output for missing .env, got %q", out.String())
	}
}

func TestLoadDemoEnvLoadsValuesWithoutOverridingShell(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	configDir := filepath.Join(dir, "demo")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("create config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, ".env"), []byte("GO_ENV=agentic\nGOOGLE_APIKEY=from-file\nEXISTING_VALUE=from-file\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	t.Setenv("GO_ENV", "")
	t.Setenv("GOOGLE_APIKEY", "")
	t.Setenv("EXISTING_VALUE", "from-shell")
	if err := os.Unsetenv("GO_ENV"); err != nil {
		t.Fatalf("unset GO_ENV: %v", err)
	}
	if err := os.Unsetenv("GOOGLE_APIKEY"); err != nil {
		t.Fatalf("unset GOOGLE_APIKEY: %v", err)
	}
	unsetEnv(t, "GJ_AGENT_ENABLED", "GJ_AGENT_API_KEY_ENV", "GJ_AGENT_PROVIDER", "GJ_AGENT_MAX_STEPS", "GJ_AGENT_TIMEOUT_SECONDS")

	var out bytes.Buffer
	if err := loadDemoEnv(configDir, &out); err != nil {
		t.Fatalf("load .env: %v", err)
	}
	if got := os.Getenv("GO_ENV"); got != "agentic" {
		t.Fatalf("GO_ENV = %q, want agentic", got)
	}
	if got := os.Getenv("GOOGLE_APIKEY"); got != "from-file" {
		t.Fatalf("GOOGLE_APIKEY = %q, want from-file", got)
	}
	if got := os.Getenv("EXISTING_VALUE"); got != "from-shell" {
		t.Fatalf("EXISTING_VALUE = %q, want shell value to win", got)
	}
	if !strings.Contains(out.String(), "demo env") || !strings.Contains(out.String(), ".env") {
		t.Fatalf("expected loaded message, got %q", out.String())
	}
}

func TestLoadDemoEnvLoadsRootBeforeDemoEnv(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	configDir := filepath.Join(dir, "examples", "coffee-roastery")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("create config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("GO_ENV=agentic\nROOT_ONLY=root\nSHARED_VALUE=root\n"), 0o600); err != nil {
		t.Fatalf("write root .env: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, ".env"), []byte("GOOGLE_APIKEY=from-demo\nDEMO_ONLY=demo\nSHARED_VALUE=demo\n"), 0o600); err != nil {
		t.Fatalf("write demo .env: %v", err)
	}
	for _, key := range []string{"GO_ENV", "ROOT_ONLY", "SHARED_VALUE", "GOOGLE_APIKEY", "DEMO_ONLY", "GJ_AGENT_ENABLED", "GJ_AGENT_API_KEY_ENV", "GJ_AGENT_PROVIDER", "GJ_AGENT_MAX_STEPS", "GJ_AGENT_TIMEOUT_SECONDS"} {
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}
	}

	var out bytes.Buffer
	if err := loadDemoEnv(configDir, &out); err != nil {
		t.Fatalf("load .env files: %v", err)
	}
	if got := os.Getenv("GO_ENV"); got != "agentic" {
		t.Fatalf("GO_ENV = %q, want agentic", got)
	}
	if got := os.Getenv("ROOT_ONLY"); got != "root" {
		t.Fatalf("ROOT_ONLY = %q, want root", got)
	}
	if got := os.Getenv("GOOGLE_APIKEY"); got != "from-demo" {
		t.Fatalf("GOOGLE_APIKEY = %q, want from-demo", got)
	}
	if got := os.Getenv("DEMO_ONLY"); got != "demo" {
		t.Fatalf("DEMO_ONLY = %q, want demo", got)
	}
	if got := os.Getenv("SHARED_VALUE"); got != "root" {
		t.Fatalf("SHARED_VALUE = %q, want root value to win", got)
	}
	if got := strings.Count(out.String(), "demo env"); got != 3 {
		t.Fatalf("expected two loaded messages plus agent defaults, got %d in %q", got, out.String())
	}
}

func TestLoadDemoEnvInfersAgentFromOpenAIKey(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	configDir := filepath.Join(dir, "examples", "coffee-roastery")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("create config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("OPENAI_API_KEY=from-openai\n"), 0o600); err != nil {
		t.Fatalf("write root .env: %v", err)
	}
	unsetEnv(t, "GO_ENV", "GOOGLE_APIKEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GJ_AGENT_ENABLED", "GJ_AGENT_API_KEY_ENV", "GJ_AGENT_PROVIDER", "GJ_AGENT_MAX_STEPS", "GJ_AGENT_TIMEOUT_SECONDS")

	var out bytes.Buffer
	if err := loadDemoEnv(configDir, &out); err != nil {
		t.Fatalf("load .env files: %v", err)
	}
	if _, ok := os.LookupEnv("GJ_AGENT_ENABLED"); ok {
		t.Fatalf("GJ_AGENT_ENABLED should be omitted; mode defaults enable the agent")
	}
	if got := os.Getenv("GJ_AGENT_API_KEY_ENV"); got != "OPENAI_API_KEY" {
		t.Fatalf("GJ_AGENT_API_KEY_ENV = %q, want OPENAI_API_KEY", got)
	}
	if got := os.Getenv("GJ_AGENT_PROVIDER"); got != "openai" {
		t.Fatalf("GJ_AGENT_PROVIDER = %q, want openai", got)
	}
	if got := os.Getenv("GO_ENV"); got != "agentic" {
		t.Fatalf("GO_ENV = %q, want agentic", got)
	}
	if got := os.Getenv("GJ_AGENT_MAX_STEPS"); got != "10" {
		t.Fatalf("GJ_AGENT_MAX_STEPS = %q, want 10", got)
	}
	if got := os.Getenv("GJ_AGENT_TIMEOUT_SECONDS"); got != "300" {
		t.Fatalf("GJ_AGENT_TIMEOUT_SECONDS = %q, want 300", got)
	}
	if !strings.Contains(out.String(), "enabled agentic mode with OPENAI_API_KEY") {
		t.Fatalf("expected agent inference message, got %q", out.String())
	}
}

func TestLoadDemoEnvDoesNotOverrideExplicitAgentEnv(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	configDir := filepath.Join(dir, "examples", "coffee-roastery")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("create config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("GO_ENV=dev\nOPENAI_API_KEY=from-openai\nGJ_AGENT_ENABLED=false\nGJ_AGENT_API_KEY_ENV=CUSTOM_AGENT_KEY\nGJ_AGENT_PROVIDER=custom\nGJ_AGENT_MAX_STEPS=4\nGJ_AGENT_TIMEOUT_SECONDS=45\n"), 0o600); err != nil {
		t.Fatalf("write root .env: %v", err)
	}
	unsetEnv(t, "GO_ENV", "GOOGLE_APIKEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GJ_AGENT_ENABLED", "GJ_AGENT_API_KEY_ENV", "GJ_AGENT_PROVIDER", "GJ_AGENT_MAX_STEPS", "GJ_AGENT_TIMEOUT_SECONDS")

	if err := loadDemoEnv(configDir, nil); err != nil {
		t.Fatalf("load .env files: %v", err)
	}
	if got := os.Getenv("GJ_AGENT_ENABLED"); got != "false" {
		t.Fatalf("GJ_AGENT_ENABLED = %q, want false", got)
	}
	if got := os.Getenv("GJ_AGENT_API_KEY_ENV"); got != "CUSTOM_AGENT_KEY" {
		t.Fatalf("GJ_AGENT_API_KEY_ENV = %q, want CUSTOM_AGENT_KEY", got)
	}
	if got := os.Getenv("GJ_AGENT_PROVIDER"); got != "custom" {
		t.Fatalf("GJ_AGENT_PROVIDER = %q, want custom", got)
	}
	if got := os.Getenv("GO_ENV"); got != "dev" {
		t.Fatalf("GO_ENV = %q, want dev", got)
	}
	if got := os.Getenv("GJ_AGENT_MAX_STEPS"); got != "4" {
		t.Fatalf("GJ_AGENT_MAX_STEPS = %q, want 4", got)
	}
	if got := os.Getenv("GJ_AGENT_TIMEOUT_SECONDS"); got != "45" {
		t.Fatalf("GJ_AGENT_TIMEOUT_SECONDS = %q, want 45", got)
	}
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(old); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
}

func unsetEnv(t *testing.T, keys ...string) {
	t.Helper()
	for _, key := range keys {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}
	}
}
