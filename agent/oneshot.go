package agent

import (
	"context"
	"fmt"
	"strings"

	ax "github.com/ax-llm/ax/packages/go"
)

// One-shot model calls.
//
// The agent loop is the thing being measured; the work that *prepares* a
// measurement — authoring tasks, describing a world — is a different job with
// different needs. It wants one call to a capable model and a structured reply,
// with no tools, no discovery protocol and no step budget, and it wants to use a
// model chosen independently of whichever one is under evaluation.
//
// This is that surface. It reuses the same client construction and the same
// portable forward options as the agent, so an authoring call honours reasoning,
// service tier and structured-output settings exactly as an agent call would.

// OneShot runs a single tool-less model call and returns the decoded fields.
//
// cfg is a full agent Config, but only the model-selection fields are read:
// Provider, Model, APIKeyEnv, BaseURL, Reasoning, ShowThoughts, ServiceTier,
// StructuredOutputMode and ResponseFormat. A Config assembled in code has not
// been through the service's config normalization, so this validates the
// portable controls itself rather than letting an unsupported value reach the
// provider as a confusing transport error.
func OneShot(ctx context.Context, cfg Config, signature string, values map[string]any) (map[string]any, error) {
	client, resolved, err := oneShotClient(cfg)
	if err != nil {
		return nil, err
	}
	return OneShotWithClient(ctx, client, resolved, signature, values)
}

// OneShotWithClient runs the call against an already-built client, so a test can
// exercise the decode path without a provider.
func OneShotWithClient(ctx context.Context, client ax.AIClient, cfg Config, signature string, values map[string]any) (map[string]any, error) {
	if client == nil {
		return nil, fmt.Errorf("one-shot call needs a model client")
	}
	if strings.TrimSpace(signature) == "" {
		return nil, fmt.Errorf("one-shot call needs a signature")
	}
	options := modelForwardOptions(cfg, nil)
	input := make(map[string]ax.Value, len(values))
	for key, value := range values {
		input[key] = value
	}
	out, err := ax.NewAx(signature, options).Forward(ctx, client, input, options)
	if err != nil {
		return nil, err
	}
	fields := mapValue(normalizeValue(out))
	if len(fields) == 0 {
		return nil, fmt.Errorf("one-shot call returned no fields")
	}
	return fields, nil
}

// oneShotClient validates the config and builds the model client.
func oneShotClient(cfg Config) (ax.AIClient, Config, error) {
	cfg = cfg.withDefaults()
	if err := ValidateStructuredOutputMode(cfg.StructuredOutputMode, cfg.ResponseFormat); err != nil {
		return nil, cfg, err
	}
	if err := ValidateServiceTier(cfg.ServiceTier); err != nil {
		return nil, cfg, err
	}
	if err := cfg.RateLimit.Validate(); err != nil {
		return nil, cfg, err
	}
	client, err := DefaultClientFactory(cfg)
	if err != nil {
		return nil, cfg, err
	}
	return client, cfg, nil
}

// OneShotText reads one string field out of a one-shot reply.
func OneShotText(fields map[string]any, field string) string {
	return strings.TrimSpace(stringValue(fields[field]))
}
