package serv

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/core/v3"
)

type watchFlowTestClient struct {
	content     string
	calls       int
	lastOptions map[string]ax.Value
}

func (c *watchFlowTestClient) Chat(_ context.Context, _ map[string]ax.Value, options map[string]ax.Value) (ax.Value, error) {
	c.calls++
	c.lastOptions = options
	return map[string]ax.Value{
		"results":     []ax.Value{map[string]ax.Value{"content": c.content}},
		"model_usage": map[string]ax.Value{"tokens": map[string]ax.Value{"prompt": 10, "completion": 5}},
	}, nil
}

func (*watchFlowTestClient) Embed(context.Context, map[string]ax.Value, map[string]ax.Value) (ax.Value, error) {
	return nil, nil
}

func (*watchFlowTestClient) Stream(context.Context, map[string]ax.Value, map[string]ax.Value) ([]ax.Value, error) {
	return nil, nil
}

func TestCanonicalWatchFlowRequiresTriageContract(t *testing.T) {
	canonical, hash, err := canonicalWatchFlow("default_watch_triage")
	if err != nil {
		t.Fatalf("canonicalWatchFlow: %v", err)
	}
	if canonical == "" || len(hash) != 64 || !watchFlowHasTriageContract(canonical) {
		t.Fatalf("unexpected canonical flow/hash: %q %q", canonical, hash)
	}
	if again, againHash, err := canonicalWatchFlow(canonical); err != nil || again != canonical || againHash != hash {
		t.Fatalf("flow is not stable: canonical=%q hash=%q err=%v", again, againHash, err)
	}
	if _, _, err := canonicalWatchFlow(`flowchart TD
  %%ax summarize: event:json -> summary:string
  summarize`); err == nil || !strings.Contains(err.Error(), "must return verdict") {
		t.Fatalf("missing contract error = %v", err)
	}
}

func TestNormalizeWatchEnrichmentJSONCanonicalizesInlineFlow(t *testing.T) {
	raw := `{"enabled":true,"kind":"flow","flow":"flowchart TD\n  %%ax triage: event:json, watch:json, evidence:json -> verdict:class \"notify, digest, discard\", severity:class \"info, warn, critical\", summary:string(max 280)\n  triage"}`
	normalized, cfg, enabled, err := normalizeWatchEnrichmentJSON(raw)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if !enabled || cfg.Kind != "flow" || cfg.FlowHash == "" || !strings.Contains(normalized, `"flow_hash"`) {
		t.Fatalf("normalized=%s cfg=%+v enabled=%v", normalized, cfg, enabled)
	}
}

func TestRunWatchFlowReturnsFixedVerdict(t *testing.T) {
	client := &watchFlowTestClient{content: `{"verdict":"digest","severity":"warn","summary":"Roast is drifting slowly."}`}
	conf := &Config{Serv: Serv{Agent: AgentConfig{Enabled: true, Provider: "openai", APIKeyEnv: "IGNORED", TimeoutSeconds: 5, ServiceTier: gjagent.ServiceTierStandard}}}
	svc := &graphjinService{
		conf:               conf,
		agentClientFactory: func(gjagent.Config) (ax.AIClient, error) { return client, nil },
	}
	_, cfg, enabled, err := normalizeWatchEnrichmentJSON(`{"enabled":true,"kind":"flow","flow":"default_watch_triage"}`)
	if err != nil || !enabled {
		t.Fatalf("normalize builtin: enabled=%v err=%v", enabled, err)
	}
	run, err := svc.runWatchFlow(context.Background(), cfg, map[string]ax.Value{
		"event":    map[string]any{"temperature": 410},
		"watch":    map[string]any{"id": "watch:coffee", "name": "coffee"},
		"evidence": map[string]any{},
	})
	if err != nil {
		t.Fatalf("runWatchFlow: %v", err)
	}
	if run.Verdict != "digest" || run.Severity != "warn" || run.Summary != "Roast is drifting slowly." || run.ModelCalls != 1 {
		t.Fatalf("unexpected run: %+v", run)
	}
	if client.calls != 1 {
		t.Fatalf("client calls = %d, want 1", client.calls)
	}
	if got := client.lastOptions["service_tier"]; got != gjagent.ServiceTierStandard {
		t.Fatalf("watch flow service_tier = %#v, want %q", got, gjagent.ServiceTierStandard)
	}
}

func TestRunWatchFlowUsesSharedProviderRateLimiterForInjectedClient(t *testing.T) {
	client := &watchFlowTestClient{content: `{"verdict":"digest","severity":"warn","summary":"Roast is drifting slowly."}`}
	conf := &Config{Serv: Serv{Agent: AgentConfig{
		Enabled: true, Provider: "openai", APIKeyEnv: "IGNORED", TimeoutSeconds: 5,
		RateLimit: gjagent.RateLimitConfig{RequestsPerMinute: 1},
	}}}
	svc := &graphjinService{
		conf:               conf,
		agentClientFactory: func(gjagent.Config) (ax.AIClient, error) { return client, nil },
	}
	_, cfg, enabled, err := normalizeWatchEnrichmentJSON(`{"enabled":true,"kind":"flow","flow":"default_watch_triage"}`)
	if err != nil || !enabled {
		t.Fatalf("normalize builtin: enabled=%v err=%v", enabled, err)
	}
	inputs := map[string]ax.Value{
		"event": map[string]any{"temperature": 410}, "watch": map[string]any{"id": "watch:coffee"}, "evidence": map[string]any{},
	}
	if _, err := svc.runWatchFlow(context.Background(), cfg, inputs); err != nil {
		t.Fatalf("first flow: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.runWatchFlow(ctx, cfg, inputs); err == nil || !strings.Contains(strings.ToLower(err.Error()), "canceled") {
		t.Fatalf("second flow should stop while waiting for provider capacity, got %v", err)
	}
	if client.calls != 1 {
		t.Fatalf("provider calls=%d, want the canceled second call blocked locally", client.calls)
	}
}

func TestValidateWatchFlowResultRejectsUnsafeOutput(t *testing.T) {
	for _, result := range []watchFlowResult{
		{Verdict: "silence", Severity: "warn", Summary: "x"},
		{Verdict: "notify", Severity: "fatal", Summary: "x"},
		{Verdict: "notify", Severity: "warn", Summary: strings.Repeat("x", 281)},
	} {
		if err := validateWatchFlowResult(result); err == nil {
			t.Fatalf("expected rejection for %+v", result)
		}
	}
}

func TestWatchDeliveryPayloadIncludesTriageFields(t *testing.T) {
	payload := watchDeliveryPayload(watchRuntimeDefinition{ID: "watch:coffee", Name: "coffee"}, watchDeliveryEvent{
		ID: "event:1", WatchID: "watch:coffee",
		EnrichmentJSON: `{"status":"ok","verdict":"notify","severity":"critical","summary":"Roast is too hot."}`,
	})
	event := mapFromAny(payload["event"])
	if event["verdict"] != "notify" || event["severity"] != "critical" || event["summary"] != "Roast is too hot." {
		t.Fatalf("delivery payload missing triage fields: %+v", payload)
	}
}

func TestWatchFlowReviewApprovesCurrentInlineFlow(t *testing.T) {
	db, svc := newSQLiteWatchService(t, 2)
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)
	client := &watchFlowTestClient{content: `{"verdict":"notify","severity":"critical","summary":"Roast temperature crossed the safe limit."}`}
	svc.conf.Agent = AgentConfig{Enabled: true, Provider: "openai", APIKeyEnv: "IGNORED", TimeoutSeconds: 5}
	svc.agentClientFactory = func(gjagent.Config) (ax.AIClient, error) { return client, nil }

	cp := newWatchControlPlane(svc)
	ctx := artifactUserCtx("flow_owner")
	flowJSON, _ := json.Marshal(map[string]any{"enabled": true, "kind": "flow", "flow": "default_watch_triage"})
	watch, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: watchesRootTable, Operation: "insert",
		Input: map[string]interface{}{
			"name": "coffee_flow", "query": cursorOrdersWatchQuery("coffee_flow"),
			"enrich_json": string(flowJSON),
		},
	})
	if err != nil {
		t.Fatalf("insert flow watch: %v", err)
	}
	if watch["approval"] != "pending" || watch["status"] != "paused" || watch["enabled"] != false {
		t.Fatalf("new flow watch must await preview: %+v", watch)
	}
	watchID := stringFromAny(watch["id"])
	reviewed, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: watchesRootTable, Operation: "update",
		Where: map[string]interface{}{"id": map[string]interface{}{"eq": watchID}},
		Input: map[string]interface{}{
			"flow_review_json": map[string]any{
				"decision":           "approve",
				"expected_flow_hash": stringFromAny(watch["flow_hash"]),
				"samples_json": []any{map[string]any{
					"batch_id": "batch-7", "temperature": 421, "phase": "development",
				}},
			},
		},
	})
	if err != nil {
		t.Fatalf("review flow: %v", err)
	}
	preview := mapFromAny(reviewed["flow_preview_json"])
	if reviewed["flow_approval"] != "approved" || preview["status"] != "ok" || intFromAny(preview["notify_count"]) != 1 {
		t.Fatalf("unexpected review: %+v", reviewed)
	}
	stored, err := svc.internalWatchStoreRow(ctx, watchID)
	if err != nil {
		t.Fatalf("load watch: %v", err)
	}
	if stringMapValue(stored, "approval") != "approved" || stringMapValue(stored, "status") != "active" || !boolMapValue(stored, "enabled") {
		t.Fatalf("preview did not activate watch: %+v", stored)
	}
	evidence := mapFromAny(parseJSONValue(jsonMapString(stored, "evidence_json")))
	flowReview := mapFromAny(evidence[watchFlowReviewKey])
	flowPreview := mapFromAny(flowReview["preview"])
	if flowPreview["status"] != "ok" || stringFromAny(flowPreview["flow_hash"]) == "" {
		t.Fatalf("missing review evidence: %+v", evidence)
	}
}
