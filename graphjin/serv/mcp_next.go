package serv

import (
	"sort"
	"strings"
)

// NextOption is a machine-readable next-step option for MCP orchestration.
type NextOption struct {
	Tool         string   `json:"tool"`
	Priority     int      `json:"priority"`
	Reason       string   `json:"reason,omitempty"`
	When         string   `json:"when,omitempty"`
	RequiredArgs []string `json:"required_args,omitempty"`
	OptionalArgs []string `json:"optional_args,omitempty"`
}

// NextGuidance contains the recommended next MCP tool call and alternatives.
type NextGuidance struct {
	StateCode       string       `json:"state_code"`
	RecommendedTool string       `json:"recommended_tool,omitempty"`
	Options         []NextOption `json:"options,omitempty"`
}

func nextOption(
	tool string,
	priority int,
	reason string,
	when string,
	requiredArgs []string,
	optionalArgs []string,
) NextOption {
	return NextOption{
		Tool:         tool,
		Priority:     priority,
		Reason:       reason,
		When:         when,
		RequiredArgs: requiredArgs,
		OptionalArgs: optionalArgs,
	}
}

func (ms *mcpServer) toolAvailable(tool string) bool {
	switch tool {
	case "discover_databases", "plan_database_setup", "test_database_connection",
		"get_onboarding_status", "list_databases", "check_health":
		return ms.service.conf.MCP.AllowDevTools
	case "apply_database_setup":
		return ms.service.conf.MCP.AllowDevTools && ms.service.conf.MCP.AllowConfigUpdates
	case "update_current_config":
		return ms.service.conf.MCP.AllowConfigUpdates
	case "reload_schema":
		return ms.service.conf.MCP.AllowSchemaReload
	case "list_tables":
		return true
	default:
		return false
	}
}

func (ms *mcpServer) newNextGuidance(stateCode string, options []NextOption) *NextGuidance {
	out := &NextGuidance{StateCode: stateCode}

	for _, opt := range options {
		if ms.toolAvailable(opt.Tool) {
			out.Options = append(out.Options, opt)
		}
	}

	sort.SliceStable(out.Options, func(i, j int) bool {
		if out.Options[i].Priority != out.Options[j].Priority {
			return out.Options[i].Priority < out.Options[j].Priority
		}
		return out.Options[i].Tool < out.Options[j].Tool
	})

	if len(out.Options) > 0 {
		out.RecommendedTool = out.Options[0].Tool
	}

	return out
}

func (ms *mcpServer) nextForDiscover(result DiscoverResult) *NextGuidance {
	if len(result.Databases) == 0 {
		return ms.newNextGuidance("no_candidates", []NextOption{
			nextOption(
				"plan_database_setup",
				1,
				"No candidates were discovered in this scan.",
				"Use guided discovery with explicit targets or scan settings.",
				nil,
				[]string{"scan_local", "scan_unix_sockets", "targets"},
			),
			nextOption(
				"discover_databases",
				2,
				"Retry discovery with different options.",
				"Add explicit credentials or target hosts to improve discovery.",
				nil,
				[]string{"targets", "user", "password", "scan_unix_sockets"},
			),
		})
	}

	top := result.Databases[0]

	switch {
	case top.AuthStatus == "ok":
		return ms.newNextGuidance("candidate_ready", []NextOption{
			nextOption(
				"apply_database_setup",
				1,
				"Top-ranked candidate is verified and ready to apply.",
				"Apply selected candidate to config and initialize schema.",
				[]string{"candidate_id"},
				[]string{"database_alias", "create_if_not_exists"},
			),
			nextOption(
				"plan_database_setup",
				2,
				"Review ranked candidates before apply.",
				"Use guided checklist flow before applying config.",
				nil,
				nil,
			),
		})

	case top.AuthStatus == "auth_failed":
		return ms.newNextGuidance("candidate_auth_failed", []NextOption{
			nextOption(
				"test_database_connection",
				1,
				"Candidate endpoint is reachable but credentials failed.",
				"Retest with explicit credentials.",
				nil,
				[]string{"candidate_id", "config", "discovery_options", "candidate_snapshot"},
			),
			nextOption(
				"plan_database_setup",
				2,
				"Guided candidate selection helps compare alternatives.",
				"Pick a different candidate if credentials are unknown.",
				nil,
				nil,
			),
		})

	case top.ProbeStatus == "network_unreachable" || top.ProbeStatus == "timeout" || top.ProbeStatus == "tls_required":
		return ms.newNextGuidance("candidate_probe_error", []NextOption{
			nextOption(
				"test_database_connection",
				1,
				"Candidate probe failed due to connectivity or TLS requirements.",
				"Retest with explicit host/port/user/password settings.",
				nil,
				[]string{"candidate_id", "config", "discovery_options", "candidate_snapshot"},
			),
			nextOption(
				"discover_databases",
				2,
				"Retry discovery after connectivity fixes.",
				"Use explicit targets to avoid broad local scanning.",
				nil,
				[]string{"targets", "scan_unix_sockets"},
			),
		})

	default:
		return ms.newNextGuidance("candidate_needs_verification", []NextOption{
			nextOption(
				"test_database_connection",
				1,
				"Candidate has not been fully verified.",
				"Run an explicit connection test before apply.",
				nil,
				[]string{"candidate_id", "config", "discovery_options", "candidate_snapshot"},
			),
			nextOption(
				"plan_database_setup",
				2,
				"Guided flow provides ranked options and checklist.",
				"Use plan flow to choose candidate deterministically.",
				nil,
				nil,
			),
		})
	}
}

func (ms *mcpServer) nextForSetupPlan(result SetupPlanResult) *NextGuidance {
	if len(result.Candidates) == 0 {
		return ms.newNextGuidance("no_candidates", []NextOption{
			nextOption(
				"discover_databases",
				1,
				"No setup candidates were found.",
				"Run broader discovery first, then re-plan setup.",
				nil,
				[]string{"targets", "scan_local", "scan_unix_sockets"},
			),
		})
	}

	recommended := result.Candidates[0]
	if result.Recommended != "" {
		for _, c := range result.Candidates {
			if c.CandidateID == result.Recommended {
				recommended = c
				break
			}
		}
	}

	if recommended.AuthStatus == "ok" {
		return ms.newNextGuidance("candidate_selected_ready", []NextOption{
			nextOption(
				"apply_database_setup",
				1,
				"Recommended candidate is already verified.",
				"Apply setup using candidate_id and alias.",
				[]string{"candidate_id"},
				[]string{"database_alias", "create_if_not_exists"},
			),
			nextOption(
				"test_database_connection",
				2,
				"Optional verification step before apply.",
				"Retest when credentials or endpoint details changed.",
				nil,
				[]string{"candidate_id"},
			),
		})
	}

	return ms.newNextGuidance("candidate_needs_test", []NextOption{
		nextOption(
			"test_database_connection",
			1,
			"Recommended candidate is not verified.",
			"Verify credentials before applying setup.",
			nil,
			[]string{"candidate_id", "config", "discovery_options", "candidate_snapshot"},
		),
		nextOption(
			"discover_databases",
			2,
			"Find alternate candidates if verification fails.",
			"Scan additional targets or ports.",
			nil,
			[]string{"targets", "scan_unix_sockets"},
		),
	})
}

func (ms *mcpServer) nextForConnectionTest(result ConnectionTestResult) *NextGuidance {
	if result.Success {
		return ms.newNextGuidance("connection_verified", []NextOption{
			nextOption(
				"apply_database_setup",
				1,
				"Connection test succeeded.",
				"Apply this candidate to GraphJin config.",
				nil,
				[]string{"candidate_id", "database_alias", "config"},
			),
			nextOption(
				"plan_database_setup",
				2,
				"Optional final comparison step.",
				"Review alternatives before apply.",
				nil,
				nil,
			),
		})
	}

	switch result.Candidate.ProbeStatus {
	case "auth_failed":
		return ms.newNextGuidance("connection_auth_failed", []NextOption{
			nextOption(
				"test_database_connection",
				1,
				"Authentication failed for tested candidate.",
				"Retry with explicit credentials.",
				nil,
				[]string{"config", "candidate_snapshot", "discovery_options"},
			),
			nextOption(
				"discover_databases",
				2,
				"Find other endpoints that may be easier to authenticate.",
				"Run discovery with explicit targets.",
				nil,
				[]string{"targets", "scan_unix_sockets"},
			),
		})
	case "network_unreachable", "timeout", "tls_required":
		return ms.newNextGuidance("connection_probe_error", []NextOption{
			nextOption(
				"test_database_connection",
				1,
				"Probe failed due to network or TLS constraints.",
				"Retry with explicit config tuned for this endpoint.",
				nil,
				[]string{"config", "candidate_snapshot", "discovery_options"},
			),
			nextOption(
				"discover_databases",
				2,
				"Re-scan after connectivity changes.",
				"Use target-based scan to confirm endpoint reachability.",
				nil,
				[]string{"targets", "scan_unix_sockets"},
			),
		})
	default:
		return ms.newNextGuidance("connection_failed", []NextOption{
			nextOption(
				"test_database_connection",
				1,
				"Connection test did not pass.",
				"Retry using explicit candidate config values.",
				nil,
				[]string{"config", "candidate_snapshot", "discovery_options"},
			),
			nextOption(
				"plan_database_setup",
				2,
				"Guided flow may suggest better candidates.",
				"Use ranked candidates before applying config.",
				nil,
				nil,
			),
		})
	}
}

func (ms *mcpServer) nextForApplyDatabaseSetup(result ApplyDatabaseSetupResult) *NextGuidance {
	if !result.Applied {
		if result.Verification.AuthStatus != "ok" {
			return ms.newNextGuidance("apply_blocked_unverified", []NextOption{
				nextOption(
					"test_database_connection",
					1,
					"Setup apply was blocked because candidate is unverified.",
					"Verify credentials or endpoint health before apply.",
					nil,
					[]string{"candidate_id", "config"},
				),
			})
		}
		return ms.newNextGuidance("apply_not_applied", []NextOption{
			nextOption(
				"get_onboarding_status",
				1,
				"Setup was not applied.",
				"Check current config/schema readiness before retry.",
				nil,
				nil,
			),
		})
	}

	if result.Success && result.TableCount > 0 {
		return ms.newNextGuidance("apply_success_schema_ready", []NextOption{
			nextOption(
				"list_tables",
				1,
				"Database setup applied and schema has tables.",
				"Continue with schema exploration.",
				nil,
				nil,
			),
			nextOption(
				"get_onboarding_status",
				2,
				"Optional status verification after setup.",
				"Confirm schema readiness and configured database list.",
				nil,
				nil,
			),
		})
	}

	if result.TableCount == 0 {
		return ms.newNextGuidance("apply_success_no_tables", []NextOption{
			nextOption(
				"get_onboarding_status",
				1,
				"Setup applied but no tables are currently discovered.",
				"Confirm schema state before changing config.",
				nil,
				nil,
			),
			nextOption(
				"list_databases",
				2,
				"Inspect available databases on configured servers.",
				"Choose a database that has user tables.",
				nil,
				nil,
			),
			nextOption(
				"update_current_config",
				3,
				"Adjust dbname/credentials or create a new DB.",
				"Use create_if_not_exists in dev mode when appropriate.",
				nil,
				[]string{"databases", "create_if_not_exists"},
			),
		})
	}

	return ms.newNextGuidance("apply_partial_success", []NextOption{
		nextOption(
			"get_onboarding_status",
			1,
			"Setup applied with errors.",
			"Check schema readiness before proceeding.",
			nil,
			nil,
		),
		nextOption(
			"update_current_config",
			2,
			"Fix remaining config issues after partial apply.",
			"Adjust connection/database settings and retry.",
			nil,
			[]string{"databases"},
		),
	})
}

func (ms *mcpServer) nextForOnboardingStatus(result OnboardingStatusResult) *NextGuidance {
	if len(result.ConfiguredDatabases) == 0 {
		return ms.newNextGuidance("no_configured_databases", []NextOption{
			nextOption(
				"discover_databases",
				1,
				"No databases are configured yet.",
				"Discover reachable databases first.",
				nil,
				nil,
			),
			nextOption(
				"plan_database_setup",
				2,
				"Guided setup provides ranked candidates.",
				"Use checklist-based onboarding flow.",
				nil,
				nil,
			),
		})
	}

	if !result.SchemaReady {
		return ms.newNextGuidance("schema_not_ready", []NextOption{
			nextOption(
				"update_current_config",
				1,
				"Database is configured but schema is not ready.",
				"Fix dbname/credentials or add schema-aware settings.",
				nil,
				[]string{"databases"},
			),
			nextOption(
				"reload_schema",
				2,
				"Refresh schema after external database changes.",
				"Use when tables were recently created.",
				nil,
				nil,
			),
			nextOption(
				"list_databases",
				3,
				"Inspect server databases to pick one with user tables.",
				"Use before changing active database config.",
				nil,
				nil,
			),
		})
	}

	return ms.newNextGuidance("schema_ready", []NextOption{
		nextOption(
			"list_tables",
			1,
			"Onboarding is complete and schema is ready.",
			"Continue with table/schema exploration.",
			nil,
			nil,
		),
	})
}

func (ms *mcpServer) nextForConfigUpdate(result ConfigUpdateResult) *NextGuidance {
	msg := strings.ToLower(result.Message)
	hasConnFailure := strings.Contains(msg, "connection test failed")
	hasSchemaFailure := strings.Contains(msg, "schema discovery found no tables")

	for _, e := range result.Errors {
		le := strings.ToLower(e)
		if strings.Contains(le, "connection failed") {
			hasConnFailure = true
		}
		if strings.Contains(le, "schema not ready") {
			hasSchemaFailure = true
		}
	}

	if hasConnFailure {
		return ms.newNextGuidance("config_connection_failed", []NextOption{
			nextOption(
				"discover_databases",
				1,
				"Config update failed due to connection issues.",
				"Discover reachable endpoints before retrying config update.",
				nil,
				[]string{"targets", "user", "password", "scan_unix_sockets"},
			),
			nextOption(
				"test_database_connection",
				2,
				"Validate explicit endpoint credentials before applying config.",
				"Use candidate_id or config payload for verification.",
				nil,
				[]string{"candidate_id", "config"},
			),
			nextOption(
				"update_current_config",
				3,
				"Retry config update with corrected connection details.",
				"Use explicit databases host/port/user/password/dbname values.",
				nil,
				[]string{"databases"},
			),
		})
	}

	if hasSchemaFailure {
		return ms.newNextGuidance("config_schema_not_ready", []NextOption{
			nextOption(
				"list_databases",
				1,
				"Config reloaded but no tables were discovered.",
				"Inspect available databases and pick one with user tables.",
				nil,
				nil,
			),
			nextOption(
				"update_current_config",
				2,
				"Adjust dbname or create a database.",
				"Use create_if_not_exists in dev mode if needed.",
				nil,
				[]string{"databases", "create_if_not_exists"},
			),
			nextOption(
				"get_onboarding_status",
				3,
				"Check current schema readiness.",
				"Confirm state after config adjustments.",
				nil,
				nil,
			),
		})
	}

	if result.Success {
		if ms.service.gj != nil && ms.service.gj.SchemaReady() {
			return ms.newNextGuidance("config_applied_schema_ready", []NextOption{
				nextOption(
					"list_tables",
					1,
					"Configuration applied and schema is ready.",
					"Proceed to table discovery and query planning.",
					nil,
					nil,
				),
			})
		}
		return ms.newNextGuidance("config_applied_schema_pending", []NextOption{
			nextOption(
				"get_onboarding_status",
				1,
				"Configuration applied but schema readiness is not confirmed.",
				"Check onboarding status before querying.",
				nil,
				nil,
			),
			nextOption(
				"reload_schema",
				2,
				"Refresh schema if database objects changed recently.",
				"Use when tables were created after service startup.",
				nil,
				nil,
			),
		})
	}

	return ms.newNextGuidance("config_update_with_errors", []NextOption{
		nextOption(
			"get_onboarding_status",
			1,
			"Configuration update returned errors.",
			"Inspect current readiness before next change.",
			nil,
			nil,
		),
		nextOption(
			"update_current_config",
			2,
			"Retry with corrected configuration values.",
			"Apply targeted updates to the failed sections.",
			nil,
			nil,
		),
	})
}
