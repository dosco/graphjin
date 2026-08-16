package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/subosito/gotenv"
)

const demoAgentTimeoutSeconds = 300

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
	_, keyEnvPinned := os.LookupEnv("GJ_AGENT_API_KEY_ENV")
	if pinned := strings.TrimSpace(os.Getenv("GJ_AGENT_PROVIDER")); pinned != "" && !keyEnvPinned {
		// A pinned provider must not inherit an unrelated provider's key. Sending
		// OPENAI_API_KEY to Gemini comes back as a rejected credential, which reads
		// like an expired key rather than the misconfiguration it is.
		if matched := demoAgentProviderKeyEnv(pinned); matched != "" {
			keyEnv, provider = matched, pinned
		} else if keyEnv != "" && out != nil {
			fmt.Fprintf(out, "demo env %-18s warning %s is pinned but no matching key variable is set; falling back to %s\n", "agent", pinned, keyEnv)
		}
	}
	if keyEnv == "" {
		return
	}
	changed := false
	if _, ok := os.LookupEnv("GO_ENV"); !ok {
		_ = os.Setenv("GO_ENV", "agentic")
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
		_ = os.Setenv("GJ_AGENT_MAX_STEPS", "8")
		changed = true
	}
	if _, ok := os.LookupEnv("GJ_AGENT_TIMEOUT_SECONDS"); !ok {
		// Generous cap: client-sampled runs add an MCP round trip per model call,
		// and reasoning models spend longer per call.
		_ = os.Setenv("GJ_AGENT_TIMEOUT_SECONDS", strconv.Itoa(demoAgentTimeoutSeconds))
		changed = true
	}
	if changed && out != nil {
		fmt.Fprintf(out, "demo env %-18s enabled agentic mode with %s\n", "agent", keyEnv)
	}
}

func demoAgentKeyEnv() (string, string) {
	// OpenAI stays first so multi-key environments keep their historical
	// default; the Google entries previously never matched because the
	// candidate was misspelled GOOGLE_APIKEY.
	for _, candidate := range []struct {
		key      string
		provider string
	}{
		{key: "OPENAI_API_KEY", provider: "openai"},
		{key: "ANTHROPIC_API_KEY", provider: "anthropic"},
		{key: "GOOGLE_API_KEY", provider: "google-gemini"},
		{key: "GEMINI_API_KEY", provider: "google-gemini"},
	} {
		if os.Getenv(candidate.key) != "" {
			return candidate.key, candidate.provider
		}
	}
	return "", ""
}

// demoAgentProviderKeyEnv resolves the key variable an explicitly pinned
// provider authenticates with. Unknown providers return an empty string so
// custom setups keep whatever the caller configured.
func demoAgentProviderKeyEnv(provider string) string {
	var candidates []string
	switch strings.ToLower(provider) {
	case "openai":
		candidates = []string{"OPENAI_API_KEY"}
	case "anthropic":
		candidates = []string{"ANTHROPIC_API_KEY"}
	case "google-gemini", "gemini", "google":
		candidates = []string{"GOOGLE_API_KEY", "GEMINI_API_KEY"}
	case "deepseek":
		candidates = []string{"DEEPSEEK_API_KEY"}
	}
	for _, candidate := range candidates {
		if os.Getenv(candidate) != "" {
			return candidate
		}
	}
	return ""
}
