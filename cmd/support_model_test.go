package main

import (
	"strings"
	"testing"
)

func clearSupportEnv(t *testing.T) {
	t.Helper()
	clearGeneratorEnv(t)
	for _, name := range []string{
		"GJ_SUPPORT_PROVIDER", "GJ_SUPPORT_MODEL", "GJ_SUPPORT_API_KEY_ENV",
		"GJ_SUPPORT_BASE_URL", "GJ_SUPPORT_REASONING", "GJ_SUPPORT_SERVICE_TIER",
		"GJ_SUPPORT_STRUCTURED_OUTPUT_MODE",
	} {
		t.Setenv(name, "")
	}
}

// The support model inherits the same credential discipline the authoring model
// has: a pinned provider resolves its own key or fails saying which one it
// wanted. Sending one provider's key to another comes back as a rejected
// credential, which reads like an expired key rather than a misconfiguration.
func TestSupportConfigNeverInheritsAnUnrelatedProvidersKey(t *testing.T) {
	clearSupportEnv(t)
	t.Setenv("OPENAI_API_KEY", "sk-openai")
	t.Setenv("GJ_SUPPORT_PROVIDER", "google-gemini")
	t.Setenv("GJ_SUPPORT_MODEL", "gemini-2.5-flash")

	_, _, err := resolveSupportConfig(generatorFlags{})
	if err == nil {
		t.Fatal("a Gemini provider must not resolve to the OpenAI key that happens to be set")
	}
	if !strings.Contains(err.Error(), "GOOGLE_API_KEY") {
		t.Fatalf("the error should name the credential it wanted, got %v", err)
	}
	if !strings.Contains(err.Error(), "support") {
		t.Fatalf("the error should say which role it was resolving, got %v", err)
	}
	if !strings.Contains(err.Error(), "--support-api-key-env") {
		t.Fatalf("the error should name the flag that fixes it, got %v", err)
	}

	t.Setenv("GOOGLE_API_KEY", "gemini-key")
	cfg, label, err := resolveSupportConfig(generatorFlags{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIKeyEnv != "GOOGLE_API_KEY" || cfg.Model != "gemini-2.5-flash" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if label != "google-gemini/gemini-2.5-flash" {
		t.Fatalf("label = %q", label)
	}
}

// The support model must never silently become the model under evaluation.
// Inheriting the agent's settings is exactly the arrangement this flag exists
// to avoid: the policy would be serving the stages it is not credited for while
// appearing to have been relieved of them.
func TestSupportConfigDoesNotInheritTheAgentsModel(t *testing.T) {
	clearSupportEnv(t)
	t.Setenv("OPENAI_API_KEY", "sk-openai")
	t.Setenv("GJ_AGENT_MODEL", "the-policy-under-test")
	t.Setenv("GJ_AGENT_PROVIDER", "openai")

	if _, _, err := resolveSupportConfig(generatorFlags{}); err == nil {
		t.Fatal("an unconfigured support model must be an error, not the agent's own")
	} else if !strings.Contains(err.Error(), "GJ_SUPPORT_MODEL") {
		t.Fatalf("the error should say what to set, got %v", err)
	}

	// The authoring model, by contrast, is meant to fall back — someone who has
	// configured one model should not have to configure a second to try it.
	cfg, _, err := resolveGeneratorConfig(generatorFlags{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "the-policy-under-test" {
		t.Fatalf("the authoring model should inherit, got %q", cfg.Model)
	}
}

// Nothing changes unless a support model was actually asked for.
func TestSupportModelIsOnlyUsedWhenRequested(t *testing.T) {
	clearSupportEnv(t)
	if supportModelRequested(generatorFlags{}) {
		t.Fatal("no flags and no variables must mean no support model")
	}
	if !supportModelRequested(generatorFlags{Model: "gemini-2.5-flash"}) {
		t.Fatal("a flag must request one")
	}
	t.Setenv("GJ_SUPPORT_MODEL", "gemini-2.5-flash")
	if !supportModelRequested(generatorFlags{}) {
		t.Fatal("a variable must request one")
	}
}
