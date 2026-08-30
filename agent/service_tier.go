package agent

import (
	"context"
	"fmt"
	"strings"

	ax "github.com/ax-llm/ax/packages/go"
)

// Portable inference service tiers are Ax vocabulary. GraphJin validates the
// operator-facing value and lets Ax resolve it against the selected deployment
// profile and model, including provider-specific wire mappings.
const (
	ServiceTierAuto     = "auto"
	ServiceTierStandard = "standard"
	ServiceTierFlex     = "flex"
	ServiceTierPriority = "priority"
)

// EffectiveServiceTier returns the canonical requested tier. Auto preserves
// provider-owned selection and is the default for both parsed and direct Go
// configurations.
func EffectiveServiceTier(tier string) string {
	tier = strings.ToLower(strings.TrimSpace(tier))
	if tier == "" {
		return ServiceTierAuto
	}
	return tier
}

// ValidateServiceTier catches typos before a model client or transport is
// created. Provider/model compatibility remains Ax's responsibility so its
// profile metadata stays the single source of truth.
func ValidateServiceTier(tier string) error {
	switch EffectiveServiceTier(tier) {
	case ServiceTierAuto, ServiceTierStandard, ServiceTierFlex, ServiceTierPriority:
		return nil
	default:
		return fmt.Errorf("agent.service_tier must be one of %s, %s, %s, %s",
			ServiceTierAuto, ServiceTierStandard, ServiceTierFlex, ServiceTierPriority)
	}
}

// wrapServiceTierAIClient enforces an explicit tier at the provider boundary.
// Ax carries forward options through its ordinary agent stages, but its
// runtime llmQuery primitive starts a nested AxGen call with fresh options.
// Decorating the client keeps that internal model call on the same requested
// tier without adding provider-specific mappings to GraphJin. Auto remains
// unwrapped so the provider owns tier selection completely.
func wrapServiceTierAIClient(client ax.AIClient, tier string) ax.AIClient {
	tier = EffectiveServiceTier(tier)
	if client == nil || tier == ServiceTierAuto {
		return client
	}
	return &serviceTierAIClient{inner: client, tier: tier}
}

type serviceTierAIClient struct {
	inner ax.AIClient
	tier  string
}

func (c *serviceTierAIClient) withTier(options map[string]ax.Value) map[string]ax.Value {
	withTier := make(map[string]ax.Value, len(options)+1)
	for key, value := range options {
		withTier[key] = value
	}
	withTier["service_tier"] = c.tier
	return withTier
}

func (c *serviceTierAIClient) Chat(ctx context.Context, request, options map[string]ax.Value) (ax.Value, error) {
	return c.inner.Chat(ctx, request, c.withTier(options))
}

func (c *serviceTierAIClient) Embed(ctx context.Context, request, options map[string]ax.Value) (ax.Value, error) {
	return c.inner.Embed(ctx, request, options)
}

func (c *serviceTierAIClient) Stream(ctx context.Context, request, options map[string]ax.Value) ([]ax.Value, error) {
	return c.inner.Stream(ctx, request, c.withTier(options))
}

// GetFeatures preserves Ax's deployment-capability seam so explicit tiers are
// still checked against the selected profile and model before transport.
func (c *serviceTierAIClient) GetFeatures(model string) map[string]ax.Value {
	if inner, ok := c.inner.(interface {
		GetFeatures(string) map[string]ax.Value
	}); ok {
		return inner.GetFeatures(model)
	}
	return nil
}
