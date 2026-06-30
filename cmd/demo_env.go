package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/subosito/gotenv"
)

func loadDemoEnv(configPath string, out io.Writer) error {
	paths := []string{".env", filepath.Join(configPath, ".env")}
	seen := make(map[string]bool, len(paths))
	for _, envPath := range paths {
		if abs, err := filepath.Abs(envPath); err == nil {
			envPath = abs
		}
		if seen[envPath] {
			continue
		}
		seen[envPath] = true
		if err := loadDemoEnvFile(envPath, out); err != nil {
			return err
		}
	}
	applyDemoAgentEnvDefaults(out)
	return nil
}

func loadDemoEnvFile(envPath string, out io.Writer) error {
	if abs, err := filepath.Abs(envPath); err == nil {
		envPath = abs
	}

	info, err := os.Stat(envPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("demo env path is a directory: %s", envPath)
	}

	if err := gotenv.Load(envPath); err != nil {
		return err
	}
	if out != nil {
		fmt.Fprintf(out, "demo env %-18s loaded %s\n", ".env", envPath)
	}
	return nil
}

func applyDemoAgentEnvDefaults(out io.Writer) {
	keyEnv, provider := demoAgentKeyEnv()
	if keyEnv == "" {
		return
	}
	changed := false
	if _, ok := os.LookupEnv("GO_ENV"); !ok {
		_ = os.Setenv("GO_ENV", "agentic")
		changed = true
	}
	if _, ok := os.LookupEnv("GJ_AGENT_ENABLED"); !ok {
		_ = os.Setenv("GJ_AGENT_ENABLED", "true")
		changed = true
	}
	if _, ok := os.LookupEnv("GJ_AGENT_API_KEY_ENV"); !ok {
		_ = os.Setenv("GJ_AGENT_API_KEY_ENV", keyEnv)
		changed = true
	}
	if provider != "" {
		if _, ok := os.LookupEnv("GJ_AGENT_PROVIDER"); !ok {
			_ = os.Setenv("GJ_AGENT_PROVIDER", provider)
			changed = true
		}
	}
	if _, ok := os.LookupEnv("GJ_AGENT_MAX_STEPS"); !ok {
		_ = os.Setenv("GJ_AGENT_MAX_STEPS", "10")
		changed = true
	}
	if _, ok := os.LookupEnv("GJ_AGENT_TIMEOUT_SECONDS"); !ok {
		_ = os.Setenv("GJ_AGENT_TIMEOUT_SECONDS", "150")
		changed = true
	}
	if changed && out != nil {
		fmt.Fprintf(out, "demo env %-18s enabled agentic mode with %s\n", "agent", keyEnv)
	}
}

func demoAgentKeyEnv() (string, string) {
	for _, candidate := range []struct {
		key      string
		provider string
	}{
		{key: "GOOGLE_APIKEY", provider: "google-gemini"},
		{key: "OPENAI_API_KEY", provider: "openai"},
		{key: "ANTHROPIC_API_KEY", provider: "anthropic"},
	} {
		if os.Getenv(candidate.key) != "" {
			return candidate.key, candidate.provider
		}
	}
	return "", ""
}
