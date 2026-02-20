package serv

import "testing"

func TestNextForDiscover_NoCandidates(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{
		AllowDevTools:      true,
		AllowConfigUpdates: true,
	})

	next := ms.nextForDiscover(DiscoverResult{})
	if next == nil {
		t.Fatal("expected next guidance")
	}
	if next.StateCode != "no_candidates" {
		t.Fatalf("expected state_code=no_candidates, got %s", next.StateCode)
	}
	if next.RecommendedTool != "plan_database_setup" {
		t.Fatalf("expected recommended_tool=plan_database_setup, got %s", next.RecommendedTool)
	}
}

func TestNextForSetupPlan_ReadyCandidate(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{
		AllowDevTools:      true,
		AllowConfigUpdates: true,
	})

	next := ms.nextForSetupPlan(SetupPlanResult{
		Candidates: []DiscoveredDatabase{
			{CandidateID: "db-1", AuthStatus: "ok"},
		},
		Recommended: "db-1",
	})
	if next == nil {
		t.Fatal("expected next guidance")
	}
	if next.StateCode != "candidate_selected_ready" {
		t.Fatalf("expected state_code=candidate_selected_ready, got %s", next.StateCode)
	}
	if next.RecommendedTool != "apply_database_setup" {
		t.Fatalf("expected recommended_tool=apply_database_setup, got %s", next.RecommendedTool)
	}
}

func TestNextForSetupPlan_ReadyCandidateWithoutConfigUpdates(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{
		AllowDevTools:      true,
		AllowConfigUpdates: false,
	})

	next := ms.nextForSetupPlan(SetupPlanResult{
		Candidates: []DiscoveredDatabase{
			{CandidateID: "db-1", AuthStatus: "ok"},
		},
		Recommended: "db-1",
	})
	if next == nil {
		t.Fatal("expected next guidance")
	}
	if next.RecommendedTool != "test_database_connection" {
		t.Fatalf("expected recommended_tool=test_database_connection, got %s", next.RecommendedTool)
	}
}

func TestNextForConnectionTest_AuthFailed(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{
		AllowDevTools: true,
	})

	next := ms.nextForConnectionTest(ConnectionTestResult{
		Success: false,
		Candidate: DiscoveredDatabase{
			ProbeStatus: "auth_failed",
		},
	})
	if next == nil {
		t.Fatal("expected next guidance")
	}
	if next.StateCode != "connection_auth_failed" {
		t.Fatalf("expected state_code=connection_auth_failed, got %s", next.StateCode)
	}
	if next.RecommendedTool != "test_database_connection" {
		t.Fatalf("expected recommended_tool=test_database_connection, got %s", next.RecommendedTool)
	}
}

func TestNextForApplyDatabaseSetup_SchemaReady(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{})

	next := ms.nextForApplyDatabaseSetup(ApplyDatabaseSetupResult{
		Applied:    true,
		Success:    true,
		TableCount: 2,
	})
	if next == nil {
		t.Fatal("expected next guidance")
	}
	if next.StateCode != "apply_success_schema_ready" {
		t.Fatalf("expected state_code=apply_success_schema_ready, got %s", next.StateCode)
	}
	if next.RecommendedTool != "list_tables" {
		t.Fatalf("expected recommended_tool=list_tables, got %s", next.RecommendedTool)
	}
}

func TestNextForOnboardingStatus_NoConfiguredDatabases(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{
		AllowDevTools: true,
	})

	next := ms.nextForOnboardingStatus(OnboardingStatusResult{})
	if next == nil {
		t.Fatal("expected next guidance")
	}
	if next.StateCode != "no_configured_databases" {
		t.Fatalf("expected state_code=no_configured_databases, got %s", next.StateCode)
	}
	if next.RecommendedTool != "discover_databases" {
		t.Fatalf("expected recommended_tool=discover_databases, got %s", next.RecommendedTool)
	}
}

func TestNextForConfigUpdate_ConnectionFailed(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{
		AllowDevTools: true,
	})

	next := ms.nextForConfigUpdate(ConfigUpdateResult{
		Success: false,
		Message: "Database connection test failed — config changes not applied",
	})
	if next == nil {
		t.Fatal("expected next guidance")
	}
	if next.StateCode != "config_connection_failed" {
		t.Fatalf("expected state_code=config_connection_failed, got %s", next.StateCode)
	}
	if next.RecommendedTool != "discover_databases" {
		t.Fatalf("expected recommended_tool=discover_databases, got %s", next.RecommendedTool)
	}
}
