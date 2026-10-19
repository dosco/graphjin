package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
	gjagent "github.com/dosco/graphjin/agent/v3"
)

// labelledClient records that it was the one asked.
type labelledClient struct {
	name  string
	calls int
}

func (c *labelledClient) Chat(context.Context, map[string]ax.Value, map[string]ax.Value) (ax.Value, error) {
	c.calls++
	return map[string]ax.Value{"results": []ax.Value{map[string]ax.Value{"content": c.name}}}, nil
}

func (c *labelledClient) Embed(context.Context, map[string]ax.Value, map[string]ax.Value) (ax.Value, error) {
	c.calls++
	return nil, nil
}

func (c *labelledClient) Stream(context.Context, map[string]ax.Value, map[string]ax.Value) ([]ax.Value, error) {
	c.calls++
	return nil, nil
}

func promptWith(opening string) map[string]ax.Value {
	return map[string]ax.Value{"chat_prompt": []ax.Value{
		map[string]ax.Value{"role": "system", "content": opening + "\nthe rest of the prompt"},
	}}
}

// The policy answers the calls it is credited for; a fixed model answers the
// rest. Getting this backwards is the failure that matters: a stronger model
// writing the answer would score its care as the policy's grounding.
func TestStageRoutingSendsEachStageToTheRightModel(t *testing.T) {
	cases := map[string]struct {
		opening string
		want    string
	}{
		"executor":           {"You (`executor`) write the code.", "policy"},
		"distiller":          {"You (`distiller`) condense the result.", "support"},
		"responder":          {"You synthesize the final answer for the user.", "support"},
		"out of steps":       {"No more tool calls are available.", "policy"},
		"ungrounded draft":   {"The draft answer named identifiers that were never observed.", "policy"},
		"nothing recognised": {"Some prompt from a future version of the pipeline.", "policy"},
	}
	for name, item := range cases {
		policy := &labelledClient{name: "policy"}
		support := &labelledClient{name: "support"}
		var log bytes.Buffer
		router := &routingClient{policy: policy, support: support, log: &log}

		result, err := router.Chat(context.Background(), promptWith(item.opening), nil)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		results, _ := result.(map[string]ax.Value)["results"].([]ax.Value)
		got, _ := results[0].(map[string]ax.Value)["content"].(string)
		if got != item.want {
			t.Fatalf("%s: answered by %q, want %q", name, got, item.want)
		}
	}
}

// A call nobody can place goes to the policy and is counted. Sending it to the
// support model instead would quietly credit the policy with work a stronger
// model did; saying nothing at all would hide that the pipeline has a stage
// this does not know about.
func TestUnplaceableCallsGoToThePolicyAndAreReported(t *testing.T) {
	policy := &labelledClient{name: "policy"}
	support := &labelledClient{name: "support"}
	var log bytes.Buffer
	router := &routingClient{policy: policy, support: support, log: &log}

	for i := 0; i < 4; i++ {
		if _, err := router.Chat(context.Background(), promptWith("A stage from the future."), nil); err != nil {
			t.Fatal(err)
		}
	}
	if support.calls != 0 {
		t.Fatalf("unplaceable calls reached the support model %d times", support.calls)
	}
	if router.unknown.Load() != 4 {
		t.Fatalf("unplaceable calls counted = %d, want 4", router.unknown.Load())
	}
	if !strings.Contains(log.String(), "could not be placed") {
		t.Fatalf("nothing was reported: %q", log.String())
	}
}

// Embedding and streaming are not stage-shaped, and belong to the run's own
// model rather than to a model brought in for two of its stages.
func TestRoutingKeepsNonChatCallsOnThePolicy(t *testing.T) {
	policy := &labelledClient{name: "policy"}
	support := &labelledClient{name: "support"}
	router := &routingClient{policy: policy, support: support}
	if _, err := router.Embed(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := router.Stream(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
	if policy.calls != 2 || support.calls != 0 {
		t.Fatalf("policy %d, support %d", policy.calls, support.calls)
	}
}

// The stage names the router switches on are the agent package's, so a rename
// there cannot silently stop matching here.
func TestRoutingUsesTheAgentsStageNames(t *testing.T) {
	router := &routingClient{policy: &labelledClient{name: "policy"}, support: &labelledClient{name: "support"}}
	for stage, want := range map[string]string{
		gjagent.StageExecutor:  "policy",
		gjagent.StageDistiller: "support",
		gjagent.StageResponder: "support",
		gjagent.StageFinalize:  "policy",
		gjagent.StageUnknown:   "policy",
	} {
		client, _ := router.clientFor(stage).(*labelledClient)
		if client == nil || client.name != want {
			t.Fatalf("stage %q routed to %v, want %q", stage, client, want)
		}
	}
}

// featureReportingClient answers capability questions and records who was asked.
type featureReportingClient struct {
	labelledClient
	features map[string]ax.Value
	asked    []string
}

func (c *featureReportingClient) GetFeatures(model string) map[string]ax.Value {
	c.asked = append(c.asked, model)
	return c.features
}

// ax type-asserts GetFeatures to pick a structured-output mechanism, and a
// wrapper that swallows it gets the permissive default — which once sent
// DeepSeek a request it rejects, 71 times in a row. A split run has two
// providers, so the answer has to come from whichever will actually serve the
// model being asked about.
func TestRoutingForwardsCapabilitiesToTheModelThatServes(t *testing.T) {
	policy := &featureReportingClient{features: map[string]ax.Value{"who": "policy"}}
	support := &featureReportingClient{features: map[string]ax.Value{"who": "support"}}
	router := &routingClient{policy: policy, support: support, supportModel: "fast-model"}

	var _ interface {
		GetFeatures(string) map[string]ax.Value
	} = router

	if got := router.GetFeatures("the-policy"); got["who"] != "policy" {
		t.Fatalf("a question about the policy's model reached %v", got)
	}
	if got := router.GetFeatures("fast-model"); got["who"] != "support" {
		t.Fatalf("a question about the support model must reach the support client, got %v", got)
	}
	if len(support.asked) != 1 || support.asked[0] != "fast-model" {
		t.Fatalf("the support client was asked about %v", support.asked)
	}
	// With no support model configured everything belongs to the policy.
	solo := &routingClient{policy: policy}
	if got := solo.GetFeatures("anything"); got["who"] != "policy" {
		t.Fatalf("without a support model every question is the policy's: %v", got)
	}
	// And a provider that reports nothing must not become a panic.
	bare := &routingClient{policy: &labelledClient{name: "bare"}}
	if bare.GetFeatures("m") != nil {
		t.Fatal("a featureless provider must report nothing rather than something invented")
	}
}
