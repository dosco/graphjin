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
	content string
	calls   int
}

func (c *watchFlowTestClient) Chat(context.Context, map[string]ax.Value, map[string]ax.Value) (ax.Value, error) {
	c.calls++
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
	conf := &Config{Serv: Serv{Agent: AgentConfig{Enabled: true, Provider: "openai", APIKeyEnv: "IGNORED", TimeoutSeconds: 5}}}
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

func TestWatchFlowPreviewApprovesCurrentInlineFlow(t *testing.T) {
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
	preview, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: watchFlowPreviewRootTable, Operation: "insert",
		Input: map[string]interface{}{
			"watch_id": watchID,
			"samples_json": `[{
			  "batch_id":"batch-7","temperature":421,"phase":"development"
			}]`,
			"approve": true,
		},
	})
	if err != nil {
		t.Fatalf("preview flow: %v", err)
	}
	if preview["status"] != "ok" || preview["approved"] != true || intFromAny(preview["notify_count"]) != 1 {
		t.Fatalf("unexpected preview: %+v", preview)
	}
	stored, err := svc.internalWatchStoreRow(ctx, watchID)
	if err != nil {
		t.Fatalf("load watch: %v", err)
	}
	if stringMapValue(stored, "approval") != "approved" || stringMapValue(stored, "status") != "active" || !boolMapValue(stored, "enabled") {
		t.Fatalf("preview did not activate watch: %+v", stored)
	}
	evidence := mapFromAny(parseJSONValue(jsonMapString(stored, "evidence_json")))
	flowPreview := mapFromAny(evidence["flow_preview"])
	if flowPreview["status"] != "ok" || stringFromAny(flowPreview["flow_hash"]) == "" {
		t.Fatalf("missing preview evidence: %+v", evidence)
	}
}
