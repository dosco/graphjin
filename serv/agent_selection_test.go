package serv

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gjagent "github.com/dosco/graphjin/agent/v3"
)

func TestAgentSelectionEnvironmentAndValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dev.yml")
	if err := os.WriteFile(path, []byte("app_name: selection-test\nhost_port: 0.0.0.0:8080\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GJ_AGENT_SELECTION_ENABLED", "true")
	t.Setenv("GJ_AGENT_SELECTION_PROVIDER", "typesafe")
	t.Setenv("GJ_AGENT_SELECTION_MODEL", "jev-latest")
	t.Setenv("GJ_AGENT_SELECTION_API_KEY_ENV", "JEV_API_KEY")
	t.Setenv("GJ_AGENT_SELECTION_MODE", "catalog")
	t.Setenv("GJ_AGENT_SELECTION_CONCURRENCY", "2")
	conf, err := ReadInConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	selection := conf.Agent.Selection
	if !selection.Enabled || selection.Provider != "typesafe" || selection.Model != "jev-latest" || selection.APIKeyEnv != "JEV_API_KEY" || selection.Mode != "catalog" || selection.Concurrency != 2 || selection.StartupBudgetMS != 2000 {
		t.Fatalf("config: %+v", selection)
	}
	t.Setenv("GJ_AGENT_SELECTION_CONCURRENCY", "17")
	if _, err := ReadInConfig(path); err == nil {
		t.Fatal("invalid concurrency accepted")
	}
	t.Setenv("GJ_AGENT_SELECTION_CONCURRENCY", "2")
	t.Setenv("GJ_AGENT_SELECTION_PROVIDER", "")
	if _, err := ReadInConfig(path); err == nil {
		t.Fatal("enabled selector inherited execution provider")
	}
}

func TestAgentSelectionChangesEvaluationFingerprint(t *testing.T) {
	base := agentStatusResponse{Provider: "openai", Model: "main"}
	selected := base
	selected.Selection = &gjagent.SelectionConfig{Enabled: true, Provider: "typesafe", Mode: "both"}
	if agentEvalFingerprint(base) == agentEvalFingerprint(selected) {
		t.Fatal("selection omitted from fingerprint")
	}
	other := *selected.Selection
	other.Mode = "catalog"
	changed := selected
	changed.Selection = &other
	if agentEvalFingerprint(changed) == agentEvalFingerprint(selected) {
		t.Fatal("selection mode omitted from fingerprint")
	}
}

func TestAgentDiscoveryIndexKeepsAccountArtifactsPrivate(t *testing.T) {
	svc := newArtifactOverlayTestService(t, map[string]string{"/queries/global_query.gql": `query global_query { users { id } }`})
	ctx := artifactUserCtx("selector_owner")
	// A workflow, because the agent's catalog leaves saved queries out
	// (agent/catalog_view.go) and a private one would prove nothing here.
	if _, err := svc.saveUserArtifact(ctx, artifactKindWorkflow, "private_selector_flow", `function main() { return { ok: true }; }`, workflowMetaMap(WorkflowMeta{Description: "Private selector flow"})); err != nil {
		t.Fatal(err)
	}
	runtime := &serviceAgentRuntime{service: svc}
	owner, err := runtime.CatalogDiscovery(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	other, err := runtime.CatalogDiscovery(artifactUserCtx("another_owner"), nil)
	if err != nil {
		t.Fatal(err)
	}
	contains := func(index *gjagent.CatalogDiscoveryIndex) bool {
		for _, card := range index.Cards {
			if strings.Contains(card.ID, "private_selector_flow") {
				return true
			}
		}
		return false
	}
	if !contains(owner) || contains(other) {
		t.Fatalf("account visibility: owner=%v other=%v", contains(owner), contains(other))
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := runtime.CatalogDiscovery(cancelled, nil); err == nil {
		t.Fatal("cancelled discovery proceeded")
	}
}

// The data agent authors every query itself, so the service runtime it reads
// the catalog through never returns a saved query — by kind, by id or by
// search — while the service's own catalog, which MCP and REST read, keeps it.
func TestAgentRuntimeCatalogLeavesOutSavedQueries(t *testing.T) {
	svc := newArtifactOverlayTestService(t, map[string]string{"/queries/global_query.gql": `query global_query { users { id } }`})
	ctx := artifactUserCtx("catalog_owner")
	full, err := svc.catalogSnapshotForContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := full.Card("saved_query:global_query"); !ok {
		t.Fatal("fixture: the service catalog must list the saved query")
	}
	runtime := &serviceAgentRuntime{service: svc}
	for _, args := range []map[string]any{
		{"kind": "saved_query"},
		{"id": "saved_query:global_query"},
		{"search": "global_query"},
	} {
		out, err := runtime.QueryCatalog(ctx, args)
		if err != nil {
			t.Fatal(err)
		}
		result, ok := out.(agentCatalogResult)
		if !ok {
			t.Fatalf("result type %T", out)
		}
		for _, card := range result.Cards {
			if card.Kind == "saved_query" || strings.Contains(card.ID, "global_query") {
				t.Fatalf("%v returned %s", args, card.ID)
			}
		}
	}
	if index, err := runtime.CatalogDiscovery(ctx, nil); err != nil {
		t.Fatal(err)
	} else {
		for _, card := range index.Cards {
			if strings.Contains(card.ID, "saved_query:") {
				t.Fatalf("discovery index listed %s", card.ID)
			}
		}
	}
}
