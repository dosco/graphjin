package serv

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/server"
	"go.uber.org/zap/zaptest"
)

func TestRegisterOnboardingTools_Gating(t *testing.T) {
	t.Run("not registered when AllowDevTools is false", func(t *testing.T) {
		ms := mockMcpServerWithConfig(MCPConfig{
			AllowDevTools:      false,
			AllowConfigUpdates: true,
		})
		ms.srv = server.NewMCPServer("test", "0.0.0")
		ms.registerOnboardingTools()
		tools := ms.srv.ListTools()
		if _, exists := tools["plan_database_setup"]; exists {
			t.Fatal("plan_database_setup should not be registered")
		}
	})

	t.Run("apply tool requires config updates permission", func(t *testing.T) {
		ms := mockMcpServerWithConfig(MCPConfig{
			AllowDevTools:      true,
			AllowConfigUpdates: false,
		})
		ms.srv = server.NewMCPServer("test", "0.0.0")
		ms.registerOnboardingTools()
		tools := ms.srv.ListTools()
		if _, exists := tools["plan_database_setup"]; !exists {
			t.Fatal("plan_database_setup should be registered")
		}
		if _, exists := tools["apply_database_setup"]; exists {
			t.Fatal("apply_database_setup should not be registered")
		}
	})
}

func TestRegisterOnboardingTools_SchemaIncludesScanUnixSockets(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{
		AllowDevTools: true,
	})
	ms.srv = server.NewMCPServer("test", "0.0.0")
	ms.registerOnboardingTools()

	tool, ok := ms.srv.ListTools()["plan_database_setup"]
	if !ok {
		t.Fatal("plan_database_setup should be registered")
	}
	if _, ok := tool.Tool.InputSchema.Properties["scan_unix_sockets"]; !ok {
		t.Fatal("plan_database_setup schema should include scan_unix_sockets")
	}
}

func TestResolveCandidate_FromExplicitConfig(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{AllowDevTools: true})
	args := map[string]any{
		"config": map[string]any{
			"type":   "postgres",
			"host":   "127.0.0.1",
			"port":   5432.0,
			"user":   "postgres",
			"dbname": "app",
		},
	}
	c, err := ms.resolveCandidate(args)
	if err != nil {
		t.Fatalf("resolveCandidate returned error: %v", err)
	}
	if c.CandidateID == "" {
		t.Fatal("expected candidate_id")
	}
	if c.Type != "postgres" {
		t.Fatalf("expected postgres, got %s", c.Type)
	}
}

func TestHandlePlanDatabaseSetup_ReturnsChecklist(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{AllowDevTools: true})
	req := newToolRequest(map[string]any{
		"scan_local":  true,
		"skip_docker": true,
		"skip_probe":  true,
		"scan_dir":    "/nonexistent/path",
	})
	res, err := ms.handlePlanDatabaseSetup(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected non-error result: %v", res.Content)
	}
	text := assertToolSuccess(t, res)
	var out SetupPlanResult
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(out.Checklist) == 0 {
		t.Fatal("expected checklist entries")
	}
	if out.Next == nil {
		t.Fatal("expected next guidance in setup plan response")
	}
	if out.Next.StateCode == "" {
		t.Fatal("expected non-empty next.state_code")
	}
}

func TestResolveCandidate_UsesCacheWithoutDiscoveryOptions(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{AllowDevTools: true})
	candidate := DiscoveredDatabase{
		Type:          "postgres",
		Host:          "127.0.0.1",
		Port:          5432,
		Source:        "target",
		Status:        "listening",
		ConfigSnippet: buildConfigSnippet("postgres", "127.0.0.1", 5432, ""),
	}
	enrichDiscoveredDatabase(&candidate)
	ms.cacheCandidates([]DiscoveredDatabase{candidate})

	got, err := ms.resolveCandidate(map[string]any{"candidate_id": candidate.CandidateID})
	if err != nil {
		t.Fatalf("resolveCandidate returned error: %v", err)
	}
	if got.CandidateID != candidate.CandidateID {
		t.Fatalf("expected candidate_id %s, got %s", candidate.CandidateID, got.CandidateID)
	}
}

func TestResolveCandidate_FallsBackToSnapshot(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{AllowDevTools: true})
	got, err := ms.resolveCandidate(map[string]any{
		"candidate_snapshot": map[string]any{
			"type": "postgres",
			"host": "localhost",
			"port": 5432.0,
			"config_snippet": map[string]any{
				"user":   "postgres",
				"dbname": "app",
			},
		},
	})
	if err != nil {
		t.Fatalf("resolveCandidate returned error: %v", err)
	}
	if got.Type != "postgres" {
		t.Fatalf("expected postgres, got %s", got.Type)
	}
}

func TestHandleApplyDatabaseSetup_BlocksUnverifiedByDefault(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{
		AllowDevTools:      true,
		AllowConfigUpdates: true,
	})

	req := newToolRequest(map[string]any{
		"config": map[string]any{
			"type": "cockroachdb",
			"host": "localhost",
			"port": 26257.0,
		},
		"database_alias": "bad_db",
	})

	res, err := ms.handleApplyDatabaseSetup(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := assertToolSuccess(t, res)

	var out ApplyDatabaseSetupResult
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if out.Applied {
		t.Fatal("expected applied=false for unverified candidate")
	}
	if out.Next == nil {
		t.Fatal("expected next guidance")
	}
	if out.Next.RecommendedTool != "test_database_connection" {
		t.Fatalf("expected recommended tool test_database_connection, got %s", out.Next.RecommendedTool)
	}
	if len(ms.service.conf.Core.Databases) != 0 {
		t.Fatal("expected no config mutation when candidate is unverified")
	}
}

func TestHandleApplyDatabaseSetup_AllowsUnverifiedOverride(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{
		AllowDevTools:      true,
		AllowConfigUpdates: true,
	})
	ms.service.log = zaptest.NewLogger(t).Sugar()

	req := newToolRequest(map[string]any{
		"config": map[string]any{
			"type": "cockroachdb",
			"host": "localhost",
			"port": 26257.0,
		},
		"database_alias":         "bad_db",
		"allow_unverified_apply": true,
	})

	res, err := ms.handleApplyDatabaseSetup(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = assertToolSuccess(t, res)
	if len(ms.service.conf.Core.Databases) != 1 {
		t.Fatal("expected config mutation when allow_unverified_apply=true")
	}
}
