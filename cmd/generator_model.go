package main

import (
	"fmt"
	"os"
	"strings"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/spf13/cobra"
)

// The generator model is the one that writes and describes; the agent model is
// the one being measured. They are usually not the same model and should not be
// the same configuration.
//
// Authoring wants judgement — which state of the business is worth alerting on,
// how a colleague would phrase the need — and that is worth a capable model
// whether or not the model under evaluation is a small one. Keeping the two
// settings apart is what lets a 3B model be trained against tasks a frontier
// model wrote.

type generatorFlags struct {
	Provider   string
	Model      string
	APIKeyEnv  string
	BaseURL    string
	Reasoning  string
	OutputMode string
}

func addGeneratorFlags(cmd *cobra.Command, flags *generatorFlags) {
	cmd.Flags().StringVar(&flags.Provider, "generator-provider", "", "provider for the authoring model (default: GJ_GENERATOR_PROVIDER, else the agent's)")
	cmd.Flags().StringVar(&flags.Model, "generator-model", "", "model used for authoring (default: GJ_GENERATOR_MODEL, else the agent's)")
	cmd.Flags().StringVar(&flags.APIKeyEnv, "generator-api-key-env", "", "environment variable holding the authoring model's key")
	cmd.Flags().StringVar(&flags.BaseURL, "generator-base-url", "", "base URL for the authoring model")
	cmd.Flags().StringVar(&flags.Reasoning, "generator-reasoning", "", "reasoning effort for the authoring model")
	cmd.Flags().StringVar(&flags.OutputMode, "generator-structured-output-mode", "", "structured output mechanism for the authoring model")
}

// resolveGeneratorConfig decides which model does the authoring.
//
// Precedence is flags, then GJ_GENERATOR_*, then the agent's own GJ_AGENT_*
// settings, then whichever provider key happens to be present. Falling back to
// the agent's configuration is deliberate: someone who has only set up one model
// should not have to configure a second one to try authoring.
//
// The returned label is what gets shown before spending and recorded on the
// tasks, so a task that turns out to be wrong can be traced to the model that
// wrote it.
func resolveGeneratorConfig(flags generatorFlags) (gjagent.Config, string, error) {
	cfg := gjagent.Config{
		Provider:             firstNonEmptyValue(flags.Provider, os.Getenv("GJ_GENERATOR_PROVIDER"), os.Getenv("GJ_AGENT_PROVIDER")),
		Model:                firstNonEmptyValue(flags.Model, os.Getenv("GJ_GENERATOR_MODEL"), os.Getenv("GJ_AGENT_MODEL")),
		APIKeyEnv:            firstNonEmptyValue(flags.APIKeyEnv, os.Getenv("GJ_GENERATOR_API_KEY_ENV")),
		BaseURL:              firstNonEmptyValue(flags.BaseURL, os.Getenv("GJ_GENERATOR_BASE_URL"), os.Getenv("GJ_AGENT_BASE_URL")),
		Reasoning:            firstNonEmptyValue(flags.Reasoning, os.Getenv("GJ_GENERATOR_REASONING"), os.Getenv("GJ_AGENT_REASONING")),
		ServiceTier:          firstNonEmptyValue(os.Getenv("GJ_GENERATOR_SERVICE_TIER"), os.Getenv("GJ_AGENT_SERVICE_TIER")),
		StructuredOutputMode: firstNonEmptyValue(flags.OutputMode, os.Getenv("GJ_GENERATOR_STRUCTURED_OUTPUT_MODE"), os.Getenv("GJ_AGENT_STRUCTURED_OUTPUT_MODE")),
	}

	// A pinned provider must never inherit an unrelated provider's key.
	//
	// Auto-detection picks whichever credential happens to be in the
	// environment, which is helpful when nothing is pinned and actively harmful
	// when something is: sending an OpenAI key to Gemini comes back as a
	// rejected credential, which reads like an expired key rather than the
	// misconfiguration it is. So detection runs only for an unpinned provider,
	// and a pinned one that cannot find its own credential fails saying which
	// variable it wanted.
	switch {
	case cfg.APIKeyEnv != "":
		// Explicitly named; nothing to resolve.
	case cfg.Provider != "":
		cfg.APIKeyEnv = demoAgentProviderKeyEnv(cfg.Provider)
		if cfg.APIKeyEnv == "" {
			wanted := demoAgentProviderKeyCandidates(cfg.Provider)
			if len(wanted) == 0 {
				return gjagent.Config{}, "", fmt.Errorf(
					"provider %q has no known credential variable; set --generator-api-key-env or GJ_GENERATOR_API_KEY_ENV", cfg.Provider)
			}
			return gjagent.Config{}, "", fmt.Errorf(
				"provider %q is set for authoring but none of %s is; set one, or name a different variable with --generator-api-key-env",
				cfg.Provider, strings.Join(wanted, ", "))
		}
	default:
		if agentKeyEnv := strings.TrimSpace(os.Getenv("GJ_AGENT_API_KEY_ENV")); agentKeyEnv != "" {
			cfg.APIKeyEnv = agentKeyEnv
		} else if detectedKey, detectedProvider := demoAgentKeyEnv(); detectedKey != "" {
			cfg.APIKeyEnv, cfg.Provider = detectedKey, detectedProvider
		}
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return gjagent.Config{}, "", fmt.Errorf(
			"no authoring model configured; set --generator-model or GJ_GENERATOR_MODEL (GJ_AGENT_MODEL is used when neither is set)")
	}
	if strings.TrimSpace(cfg.APIKeyEnv) == "" {
		return gjagent.Config{}, "", fmt.Errorf(
			"no credential variable for authoring provider %q; set --generator-api-key-env or GJ_GENERATOR_API_KEY_ENV", cfg.Provider)
	}
	if os.Getenv(cfg.APIKeyEnv) == "" {
		return gjagent.Config{}, "", fmt.Errorf(
			"authoring credential %s is not set", cfg.APIKeyEnv)
	}
	if err := gjagent.ValidateStructuredOutputMode(cfg.StructuredOutputMode, cfg.ResponseFormat); err != nil {
		return gjagent.Config{}, "", err
	}
	if err := gjagent.ValidateServiceTier(cfg.ServiceTier); err != nil {
		return gjagent.Config{}, "", err
	}
	provider := cfg.Provider
	if provider == "" {
		provider = "openai"
	}
	return cfg, provider + "/" + cfg.Model, nil
}

func firstNonEmptyValue(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
