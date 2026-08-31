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

// The support model is a third role, and a different question again.
//
// An agent run is several model calls with different jobs. Deciding what to
// keep from a large tool result, and turning findings into prose, are not the
// work being trained — but they still have to be done well, or a policy is
// measured through a bottleneck it did not create. So they can be served by a
// fixed capable model while the policy answers only the calls it is credited
// for.
func addSupportFlags(cmd *cobra.Command, flags *generatorFlags) {
	cmd.Flags().StringVar(&flags.Provider, "support-provider", "", "provider for the distiller and responder stages (default: GJ_SUPPORT_PROVIDER)")
	cmd.Flags().StringVar(&flags.Model, "support-model", "", "model that serves the distiller and responder stages while the policy serves the executor")
	cmd.Flags().StringVar(&flags.APIKeyEnv, "support-api-key-env", "", "environment variable holding the support model's key")
	cmd.Flags().StringVar(&flags.BaseURL, "support-base-url", "", "base URL for the support model")
	cmd.Flags().StringVar(&flags.Reasoning, "support-reasoning", "", "reasoning effort for the support model")
	cmd.Flags().StringVar(&flags.OutputMode, "support-structured-output-mode", "", "structured output mechanism for the support model")
}

// supportModelRequested reports whether anything asked for a separate support
// model. Nothing here changes unless it did: an unrequested split would put a
// second model in front of every run that never asked for one.
func supportModelRequested(flags generatorFlags) bool {
	return firstNonEmptyValue(flags.Provider, flags.Model, flags.APIKeyEnv, flags.BaseURL,
		flags.Reasoning, flags.OutputMode,
		os.Getenv("GJ_SUPPORT_PROVIDER"), os.Getenv("GJ_SUPPORT_MODEL"), os.Getenv("GJ_SUPPORT_BASE_URL")) != ""
}

// resolveSupportConfig decides which model serves the stages the policy is not
// being credited for.
//
// Unlike the authoring model, this does not fall back to the agent's own
// settings. Falling back would silently serve the support stages with the very
// model under evaluation, which is the one arrangement this flag exists to
// avoid — so an incomplete configuration is an error rather than a default.
func resolveSupportConfig(flags generatorFlags) (gjagent.Config, string, error) {
	return resolveRoleModelConfig("support", "GJ_SUPPORT", flags, false)
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
	return resolveRoleModelConfig("authoring", "GJ_GENERATOR", flags, true)
}

// resolveRoleModelConfig builds one role's model configuration.
//
// The roles differ only in which variables they read and whether they may fall
// back to the agent's own settings. Everything that matters — above all the
// credential pinning below — is shared, because getting that wrong once was
// enough.
func resolveRoleModelConfig(role, envPrefix string, flags generatorFlags, inheritAgent bool) (gjagent.Config, string, error) {
	// inherited returns the agent's own value, but only for a role allowed to
	// borrow it.
	inherited := func(name string) string {
		if !inheritAgent {
			return ""
		}
		return os.Getenv("GJ_AGENT_" + name)
	}
	cfg := gjagent.Config{
		Provider:             firstNonEmptyValue(flags.Provider, os.Getenv(envPrefix+"_PROVIDER"), inherited("PROVIDER")),
		Model:                firstNonEmptyValue(flags.Model, os.Getenv(envPrefix+"_MODEL"), inherited("MODEL")),
		APIKeyEnv:            firstNonEmptyValue(flags.APIKeyEnv, os.Getenv(envPrefix+"_API_KEY_ENV")),
		BaseURL:              firstNonEmptyValue(flags.BaseURL, os.Getenv(envPrefix+"_BASE_URL"), inherited("BASE_URL")),
		Reasoning:            firstNonEmptyValue(flags.Reasoning, os.Getenv(envPrefix+"_REASONING"), inherited("REASONING")),
		ServiceTier:          firstNonEmptyValue(os.Getenv(envPrefix+"_SERVICE_TIER"), inherited("SERVICE_TIER")),
		StructuredOutputMode: firstNonEmptyValue(flags.OutputMode, os.Getenv(envPrefix+"_STRUCTURED_OUTPUT_MODE"), inherited("STRUCTURED_OUTPUT_MODE")),
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
	// Flag names follow the variable prefix: GJ_GENERATOR_* pairs with
	// --generator-*, GJ_SUPPORT_* with --support-*.
	flagPrefix := strings.ToLower(strings.TrimPrefix(envPrefix, "GJ_"))
	keyFlag := "--" + flagPrefix + "-api-key-env"
	keyEnv := envPrefix + "_API_KEY_ENV"
	switch {
	case cfg.APIKeyEnv != "":
		// Explicitly named; nothing to resolve.
	case cfg.Provider != "":
		cfg.APIKeyEnv = demoAgentProviderKeyEnv(cfg.Provider)
		if cfg.APIKeyEnv == "" {
			wanted := demoAgentProviderKeyCandidates(cfg.Provider)
			if len(wanted) == 0 {
				return gjagent.Config{}, "", fmt.Errorf(
					"provider %q has no known credential variable; set %s or %s", cfg.Provider, keyFlag, keyEnv)
			}
			return gjagent.Config{}, "", fmt.Errorf(
				"provider %q is set for %s but none of %s is; set one, or name a different variable with %s",
				cfg.Provider, role, strings.Join(wanted, ", "), keyFlag)
		}
	default:
		// Auto-detection sets the provider and its key together, so it cannot
		// produce a mismatched pair. Borrowing the agent's key variable can, so
		// only a role allowed to inherit does it.
		if agentKeyEnv := strings.TrimSpace(os.Getenv("GJ_AGENT_API_KEY_ENV")); inheritAgent && agentKeyEnv != "" {
			cfg.APIKeyEnv = agentKeyEnv
		} else if detectedKey, detectedProvider := demoAgentKeyEnv(); detectedKey != "" {
			cfg.APIKeyEnv, cfg.Provider = detectedKey, detectedProvider
		}
	}
	if strings.TrimSpace(cfg.Model) == "" {
		fallback := ""
		if inheritAgent {
			fallback = " (GJ_AGENT_MODEL is used when neither is set)"
		}
		return gjagent.Config{}, "", fmt.Errorf("no %s model configured; set --%s-model or %s_MODEL%s",
			role, flagPrefix, envPrefix, fallback)
	}
	if strings.TrimSpace(cfg.APIKeyEnv) == "" {
		return gjagent.Config{}, "", fmt.Errorf(
			"no credential variable for %s provider %q; set %s or %s", role, cfg.Provider, keyFlag, keyEnv)
	}
	if os.Getenv(cfg.APIKeyEnv) == "" {
		return gjagent.Config{}, "", fmt.Errorf("%s credential %s is not set", role, cfg.APIKeyEnv)
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
