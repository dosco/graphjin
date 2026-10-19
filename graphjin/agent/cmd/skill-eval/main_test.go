package main

import (
	"fmt"
	gjagent "github.com/dosco/graphjin/agent/v3"
	"testing"
)

func TestRepresentativeSelectionProduces24UniqueCasesAnd72Tasks(t *testing.T) {
	cases := make([]evalCase, 0, 60)
	profiles := []endpointProfile{{
		Name: "user",
		CapabilityProfile: capabilityProfile{
			RoleClass: "user",
		},
		URL: "http://localhost",
	}}
	for i := 0; i < 60; i++ {
		cases = append(cases, evalCase{
			ID:             fmt.Sprintf("case-%02d", i+1),
			Representative: i < 24,
			CapabilityProfile: capabilityProfile{
				RoleClass: "user",
			},
		})
	}
	selected := selectCases(cases, true, 24)
	if len(selected) != 24 {
		t.Fatalf("selected cases = %d, want 24", len(selected))
	}
	seen := map[string]bool{}
	for _, testCase := range selected {
		if seen[testCase.ID] {
			t.Fatalf("case %q selected more than once", testCase.ID)
		}
		seen[testCase.ID] = true
	}
	tasks, err := makeTasks(selected, profiles, 3)
	if err != nil {
		t.Fatalf("makeTasks: %v", err)
	}
	if len(tasks) != 72 {
		t.Fatalf("tasks = %d, want 72", len(tasks))
	}
}

func TestSelectionUsageIsExcludedFromActorFallbackOnly(t *testing.T) {
	response := gjagent.Response{Usage: map[string]any{"llm_calls": 8, "total_tokens": 200}, Preparation: &gjagent.PreparationStats{Usage: gjagent.UsageTotals{LLMCalls: 5, TotalTokens: 40}}}
	if executionLLMCalls(response) != 3 || extractTokenUsage(response.Usage).Total != 200 {
		t.Fatal("selection discounted total cost or inflated actor calls")
	}
}

func TestAdaptiveSkillRecoveryIsAllowedWithoutWeakeningQualityGates(t *testing.T) {
	candidate := metrics{BehavioralRecall: 1, UsedSkillRecall: 1, UsedSkillPrecision: 1, SafetyPrecision: 1, AdaptiveSkills: true}
	if !calculateAcceptance(candidate, nil).HardPass {
		t.Fatal("valid guide recovery rejected")
	}
	candidate.SafetyPrecision = 0.5
	if calculateAcceptance(candidate, nil).HardPass {
		t.Fatal("guide recovery weakened safety gate")
	}
}

func TestActionInventoryRecordsSuccessfulGraphQLMutationRoots(t *testing.T) {
	tools, outcomes := actionInventory([]any{
		map[string]any{
			"tool":   "query_catalog",
			"status": "ok",
			"args":   map[string]any{"id": "help:watches"},
		},
		map[string]any{
			"tool":   "execute_graphql",
			"status": "ok",
			"args": map[string]any{
				"query": `mutation { gj_watch(insert: {name: "late"}) { id } }`,
			},
		},
		map[string]any{
			"tool":   "execute_graphql",
			"status": "error",
			"args": map[string]any{
				"query": `mutation { gj_config(update: {agent: {read_only: false}}) { id } }`,
			},
		},
	})
	for _, want := range []string{"query_catalog", "execute_graphql"} {
		if !contains(tools, want) {
			t.Fatalf("tools = %v, missing %q", tools, want)
		}
	}
	for _, want := range []string{"execute_graphql:mutation", "gj_watch", "gj_watch:mutation"} {
		if !contains(outcomes, want) {
			t.Fatalf("outcomes = %v, missing %q", outcomes, want)
		}
	}
	if contains(outcomes, "gj_config:mutation") {
		t.Fatalf("failed mutation counted as successful: %v", outcomes)
	}
}

func TestMakeTasksRequiresAnExactServerDerivedProfile(t *testing.T) {
	testCase := evalCase{
		ID: "watch",
		CapabilityProfile: capabilityProfile{
			RoleClass:            "user",
			AvailableSystemRoots: []string{"gj_watch", "gj_watch_event"},
		},
	}
	profiles := []endpointProfile{{
		Name: "wrong",
		CapabilityProfile: capabilityProfile{
			RoleClass:            "user",
			AvailableSystemRoots: []string{"gj_watch"},
		},
		URL: "http://localhost",
	}}
	if _, err := makeTasks([]evalCase{testCase}, profiles, 3); err == nil {
		t.Fatal("makeTasks accepted a non-exact capability profile")
	}
	profiles[0].CapabilityProfile.AvailableSystemRoots = []string{"gj_watch_event", "gj_watch"}
	tasks, err := makeTasks([]evalCase{testCase}, profiles, 3)
	if err != nil {
		t.Fatalf("makeTasks: %v", err)
	}
	if len(tasks) != 3 {
		t.Fatalf("tasks = %d, want 3", len(tasks))
	}
}

func TestAcceptanceSeparatesHardGatesFromLatencyWarnings(t *testing.T) {
	baseline := metrics{
		MedianActorTurns:        3,
		LatencyP50MS:            100,
		LatencyP95MS:            200,
		NormalPromptTokenMedian: 1000,
		AdminPromptTokenMedian:  2000,
	}
	candidate := metrics{
		BehavioralRecall:        0.96,
		UsedSkillRecall:         0.91,
		UsedSkillPrecision:      0.92,
		SafetyPrecision:         1,
		NoSkillDiscoveryCalls:   true,
		MedianActorTurns:        3,
		LatencyP50MS:            120,
		LatencyP95MS:            250,
		NormalPromptTokenMedian: 1140,
		AdminPromptTokenMedian:  2490,
	}
	got := calculateAcceptance(candidate, &baseline)
	if !got.HardPass {
		t.Fatalf("hard acceptance unexpectedly failed: %+v", got)
	}
	if len(got.Warnings) != 2 {
		t.Fatalf("latency warnings = %v, want p50 and p95 warnings", got.Warnings)
	}
}
