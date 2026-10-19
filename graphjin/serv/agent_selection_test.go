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
	base := AgentStatusResponse{Provider: "openai", Model: "main"}
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
	if _, err := svc.saveUserArtifact(ctx, artifactKindSavedQuery, "private_selector_query", `query private_selector_query { users { id name } }`, map[string]any{"operation": "query"}); err != nil {
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
			if strings.Contains(card.ID, "private_selector_query") {
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
