package main

import (
	"strings"
	"testing"
)

// clearGeneratorEnv gives each case a known-empty starting point; t.Setenv
// restores everything afterwards.
func clearGeneratorEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"GJ_GENERATOR_PROVIDER", "GJ_GENERATOR_MODEL", "GJ_GENERATOR_API_KEY_ENV",
		"GJ_GENERATOR_BASE_URL", "GJ_GENERATOR_REASONING", "GJ_GENERATOR_SERVICE_TIER",
		"GJ_GENERATOR_STRUCTURED_OUTPUT_MODE",
		"GJ_AGENT_PROVIDER", "GJ_AGENT_MODEL", "GJ_AGENT_API_KEY_ENV",
		"GJ_AGENT_BASE_URL", "GJ_AGENT_REASONING", "GJ_AGENT_SERVICE_TIER",
		"GJ_AGENT_STRUCTURED_OUTPUT_MODE",
		"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GOOGLE_API_KEY", "GEMINI_API_KEY",
	} {
		t.Setenv(name, "")
	}
}

// A pinned provider must never authenticate with an unrelated provider's key.
// Sending an OpenAI key to Gemini comes back as a rejected credential, which
// reads like an expired key rather than the misconfiguration it is — this has
// cost real debugging time before.
func TestGeneratorConfigNeverInheritsAnUnrelatedProvidersKey(t *testing.T) {
	clearGeneratorEnv(t)
	t.Setenv("OPENAI_API_KEY", "sk-openai")
	t.Setenv("GJ_GENERATOR_PROVIDER", "google-gemini")
	t.Setenv("GJ_GENERATOR_MODEL", "gemini-3.5-pro")

	_, _, err := resolveGeneratorConfig(generatorFlags{})
	if err == nil {
		t.Fatal("a Gemini provider must not resolve to the OpenAI key that happens to be set")
	}
	if !strings.Contains(err.Error(), "GOOGLE_API_KEY") {
		t.Fatalf("the error should name the credential it wanted, got %v", err)
	}

	// With the matching key present it resolves, and to the right variable.
	t.Setenv("GOOGLE_API_KEY", "gemini-key")
	cfg, label, err := resolveGeneratorConfig(generatorFlags{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIKeyEnv != "GOOGLE_API_KEY" {
		t.Fatalf("resolved credential = %q, want GOOGLE_API_KEY", cfg.APIKeyEnv)
	}
	if label != "google-gemini/gemini-3.5-pro" {
		t.Fatalf("label = %q", label)
	}
}

func TestGeneratorConfigPrecedence(t *testing.T) {
	clearGeneratorEnv(t)
	t.Setenv("OPENAI_API_KEY", "sk-openai")
	t.Setenv("GJ_AGENT_MODEL", "small-agent-model")
	t.Setenv("GJ_AGENT_PROVIDER", "openai")

	// Nothing generator-specific: fall back to the agent's model so a single
	// configured model still works.
	cfg, _, err := resolveGeneratorConfig(generatorFlags{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "small-agent-model" {
		t.Fatalf("expected the agent model as fallback, got %q", cfg.Model)
	}

	// GJ_GENERATOR_* wins over the agent's.
	t.Setenv("GJ_GENERATOR_MODEL", "big-authoring-model")
	cfg, _, err = resolveGeneratorConfig(generatorFlags{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "big-authoring-model" {
		t.Fatalf("GJ_GENERATOR_MODEL should win, got %q", cfg.Model)
	}

	// A flag wins over both.
	cfg, _, err = resolveGeneratorConfig(generatorFlags{Model: "flag-model"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "flag-model" {
		t.Fatalf("the flag should win, got %q", cfg.Model)
	}
}

func TestGeneratorConfigRequiresAModelAndAPresentCredential(t *testing.T) {
	clearGeneratorEnv(t)
	if _, _, err := resolveGeneratorConfig(generatorFlags{}); err == nil {
		t.Fatal("expected a missing model to be refused")
	}

	// A named credential variable that is not actually set must fail here
	// rather than as an opaque provider rejection mid-authoring.
	t.Setenv("GJ_GENERATOR_MODEL", "big-model")
	t.Setenv("GJ_GENERATOR_API_KEY_ENV", "NOT_SET_ANYWHERE")
	_, _, err := resolveGeneratorConfig(generatorFlags{})
	if err == nil || !strings.Contains(err.Error(), "NOT_SET_ANYWHERE") {
		t.Fatalf("expected the unset credential to be named, got %v", err)
	}
}

func TestGeneratorConfigRejectsUnsupportedPortableControls(t *testing.T) {
	clearGeneratorEnv(t)
	t.Setenv("OPENAI_API_KEY", "sk-openai")
	t.Setenv("GJ_GENERATOR_MODEL", "big-model")
	t.Setenv("GJ_GENERATOR_SERVICE_TIER", "platinum")
	if _, _, err := resolveGeneratorConfig(generatorFlags{}); err == nil {
		t.Fatal("an unsupported service tier must be refused before any spend")
	}
}
