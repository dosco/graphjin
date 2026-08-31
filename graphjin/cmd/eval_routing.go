package main

import (
	"context"
	"fmt"
	"io"
	"sync/atomic"

	ax "github.com/ax-llm/ax/packages/go"
	gjagent "github.com/dosco/graphjin/agent/v3"
)

// Serving one agent run from two models.
//
// A run is several calls with different jobs. The executor writes the code that
// does the work and is what a training run is teaching; the distiller and the
// responder condense and phrase. Making a small policy do all three measures it
// through bottlenecks it did not create, and makes a trainer pay for tokens it
// is not learning from.
//
// So the policy serves the executor and a fixed model serves the rest. The one
// thing that must not happen is the reverse of what this is for: a call the
// policy should have answered being quietly answered by a stronger model, which
// would launder the policy's failures into the support model's competence.

type routingClient struct {
	policy  ax.AIClient
	support ax.AIClient
	// unknown counts calls that could not be placed. They go to the policy,
	// which is the conservative direction — the policy is what is being
	// measured, so an unplaceable call is credited to it rather than handed to
	// a model that would make it look better.
	unknown atomic.Int64
	// supportModel names the model the support client serves, so a capability
	// question about it reaches the client that can answer it.
	supportModel string
	log          io.Writer
}

func (c *routingClient) Chat(ctx context.Context, values map[string]ax.Value, options map[string]ax.Value) (ax.Value, error) {
	return c.clientFor(gjagent.StageOfChatRequest(values)).Chat(ctx, values, options)
}

// clientFor picks which model answers a stage.
//
// The finalizers go to the policy. They write the answer the policy is credited
// for — one of them exists precisely because a draft answer named something the
// run never observed — so letting a stronger model write them would score the
// support model's care as the policy's grounding.
func (c *routingClient) clientFor(stage string) ax.AIClient {
	switch stage {
	case gjagent.StageDistiller, gjagent.StageResponder:
		return c.support
	case gjagent.StageUnknown:
		if count := c.unknown.Add(1); c.log != nil && count <= 3 {
			fmt.Fprintf(c.log,
				"eval: a model call could not be placed in the pipeline and was served by the policy (%d so far)\n", count)
		}
	}
	return c.policy
}

// GetFeatures forwards the provider's capability report.
//
// ax type-asserts this to decide the structured-output mechanism, and a
// wrapper that swallows it gets the permissive default instead — which once
// sent DeepSeek a request it rejects, 71 times in a row. A split run has two
// providers, so the report has to come from whichever one will answer: asking
// the policy about the support model's capabilities would describe the wrong
// model.
func (c *routingClient) GetFeatures(model string) map[string]ax.Value {
	client := c.policy
	if c.support != nil && c.supportModel != "" && model == c.supportModel {
		client = c.support
	}
	if inner, ok := client.(interface {
		GetFeatures(string) map[string]ax.Value
	}); ok {
		return inner.GetFeatures(model)
	}
	return nil
}

// Embedding and streaming are not stage-shaped: nothing in them says which part
// of the pipeline asked, and the policy is who the run is about.
func (c *routingClient) Embed(ctx context.Context, values map[string]ax.Value, options map[string]ax.Value) (ax.Value, error) {
	return c.policy.Embed(ctx, values, options)
}

func (c *routingClient) Stream(ctx context.Context, values map[string]ax.Value, options map[string]ax.Value) ([]ax.Value, error) {
	return c.policy.Stream(ctx, values, options)
}

// newStageRoutingFactory builds the client factory that splits a run between
// two models. The policy is built from whatever configuration the service
// itself resolved, so the model under evaluation is unchanged by this.
func newStageRoutingFactory(support gjagent.Config, log io.Writer) func(gjagent.Config) (ax.AIClient, error) {
	return func(policy gjagent.Config) (ax.AIClient, error) {
		policyClient, err := gjagent.DefaultClientFactory(policy)
		if err != nil {
			return nil, err
		}
		supportClient, err := gjagent.DefaultClientFactory(support)
		if err != nil {
			return nil, fmt.Errorf("support model: %w", err)
		}
		return &routingClient{policy: policyClient, support: supportClient, supportModel: support.Model, log: log}, nil
	}
}
