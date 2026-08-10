package agent

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
)

func profileWithRoleAndRoots(role string, roots ...string) *CapabilityProfile {
	actions := []string{
		CapabilityActionDataInsert,
		CapabilityActionDataUpdate,
		CapabilityActionDataDelete,
		CapabilityActionCodeWrite,
	}
	for _, root := range roots {
		for _, action := range []string{"insert", "update", "delete"} {
			actions = append(actions, root+"."+action)
		}
	}
	return &CapabilityProfile{
		RoleClass:            role,
		AllowedActions:       actions,
		AvailableTools:       []string{toolGraphQLHelp, toolQueryCatalog, toolValidateWhere, toolExecuteSavedQuery, toolExecuteGraphQL},
		AvailableSystemRoots: roots,
	}
}

func skillIDs(definitions []skillDefinition) []string {
	ids := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		ids = append(ids, definition.id)
	}
	return ids
}

func optionSkillIDs(t *testing.T, value ax.Value) []string {
	t.Helper()
	items := anySlice(normalizeValue(value))
	ids := make([]string, 0, len(items))
	for _, item := range items {
		card, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("skill value type = %T, want object", item)
		}
		id, _ := card["id"].(string)
		ids = append(ids, id)
	}
	return ids
}

func TestBuiltinSkillsAreTheOrderedFourteenFocusedGuides(t *testing.T) {
	want := []string{
		skillDataDiscovery,
		skillDataAggregation,
		skillDataWrite,
		skillCodeRead,
		skillCodeWrite,
		skillWorkflowRead,
		skillWorkflowExec,
		skillWorkflowWrite,
		skillWatchRead,
		skillWatchWrite,
		skillTaskRead,
		skillTaskWrite,
		skillAdminRead,
		skillAdminWrite,
	}
	if got := skillIDs(builtinSkills); !reflect.DeepEqual(got, want) {
		t.Fatalf("skill order = %v, want %v", got, want)
	}

	seen := map[string]bool{}
	for _, definition := range builtinSkills {
		if strings.TrimSpace(definition.id) == "" ||
			strings.TrimSpace(definition.name) == "" ||
			strings.TrimSpace(definition.content) == "" {
			t.Fatalf("incomplete skill definition: %+v", definition)
		}
		if seen[definition.id] {
			t.Fatalf("duplicate skill id %q", definition.id)
		}
		seen[definition.id] = true
	}
}

func TestAllowedSkillsCapabilityMatrix(t *testing.T) {
	allRoots := []string{
		systemRootCatalog,
		systemRootSecurity,
		systemRootRuntime,
		systemRootConfig,
		systemRootWorkflow,
		systemRootWorkflowExec,
		systemRootWatch,
		systemRootWatchEvent,
		systemRootTask,
		systemRootTaskEntry,
	}
	baseRead := []string{skillDataDiscovery, skillDataAggregation, skillCodeRead}
	baseWrite := []string{skillDataDiscovery, skillDataAggregation, skillDataWrite, skillCodeRead, skillCodeWrite}

	for _, tc := range []struct {
		name     string
		readOnly bool
		profile  *CapabilityProfile
		want     []string
	}{
		{name: "missing profile fails closed", want: baseRead},
		{name: "anonymous read only", readOnly: true, want: baseRead},
		{
			name:    "workflow execution only",
			profile: profileWithRoleAndRoots("user", systemRootWorkflowExec),
			want:    append(append([]string{}, baseWrite...), skillWorkflowRead, skillWorkflowExec),
		},
		{
			name:    "workflow authoring only",
			profile: profileWithRoleAndRoots("user", systemRootWorkflow),
			want:    append(append([]string{}, baseWrite...), skillWorkflowRead, skillWorkflowWrite),
		},
		{
			name:    "watch event read only root",
			profile: profileWithRoleAndRoots("user", systemRootWatchEvent),
			want:    append(append([]string{}, baseWrite...), skillWatchRead),
		},
		{
			name:    "watch lifecycle",
			profile: profileWithRoleAndRoots("user", systemRootWatch),
			want:    append(append([]string{}, baseWrite...), skillWatchRead, skillWatchWrite),
		},
		{
			name:    "task entries read only root",
			profile: profileWithRoleAndRoots("user", systemRootTaskEntry),
			want:    append(append([]string{}, baseWrite...), skillTaskRead),
		},
		{
			name:    "task lifecycle",
			profile: profileWithRoleAndRoots("user", systemRootTask),
			want:    append(append([]string{}, baseWrite...), skillTaskRead, skillTaskWrite),
		},
		{
			name:    "admin roots do not make a non admin",
			profile: profileWithRoleAndRoots("support", systemRootSecurity, systemRootRuntime, systemRootConfig),
			want:    baseWrite,
		},
		{
			name:    "admin config",
			profile: profileWithRoleAndRoots("admin", systemRootConfig),
			want:    append(append([]string{}, baseWrite...), skillAdminRead, skillAdminWrite),
		},
		{
			name:     "full admin read only",
			readOnly: true,
			profile:  profileWithRoleAndRoots("admin", allRoots...),
			want: []string{
				skillDataDiscovery,
				skillDataAggregation,
				skillCodeRead,
				skillWorkflowRead,
				skillWatchRead,
				skillTaskRead,
				skillAdminRead,
			},
		},
		{
			name:    "full admin",
			profile: profileWithRoleAndRoots("admin", allRoots...),
			want:    skillIDs(builtinSkills),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := skillIDs(allowedSkills(tc.readOnly, tc.profile)); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("allowed skills = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSkillPayloadBudgets(t *testing.T) {
	allRoots := []string{
		systemRootSecurity,
		systemRootRuntime,
		systemRootConfig,
		systemRootWorkflow,
		systemRootWorkflowExec,
		systemRootWatch,
		systemRootWatchEvent,
		systemRootTask,
		systemRootTaskEntry,
	}
	for _, tc := range []struct {
		name    string
		profile *CapabilityProfile
		max     int
	}{
		// Budgets ratchet both ways. The current allowance includes the exact
		// discovery prerequisites enforced by the write and cross-source guards.
		// Keep modest headroom so any further prompt growth remains deliberate.
		{name: "normal user", profile: profileWithRoleAndRoots("user"), max: 3 * 1024},
		{name: "full admin", profile: profileWithRoleAndRoots("admin", allRoots...), max: 18 * 512},
	} {
		t.Run(tc.name, func(t *testing.T) {
			definitions := allowedSkills(false, tc.profile)
			for _, definition := range definitions {
				t.Logf("%s content: %d bytes", definition.id, len(definition.content))
			}
			payload, err := json.Marshal(skillValues(definitions))
			if err != nil {
				t.Fatalf("marshal skills: %v", err)
			}
			if len(payload) > tc.max {
				t.Fatalf("skill payload = %d bytes, max %d", len(payload), tc.max)
			}
			t.Logf("skill payload: %d bytes", len(payload))
		})
	}
}

func TestCapabilityCompletionInstructionBudgets(t *testing.T) {
	allRoots := []string{
		systemRootSecurity,
		systemRootRuntime,
		systemRootConfig,
		systemRootWorkflow,
		systemRootWorkflowExec,
		systemRootWatch,
		systemRootWatchEvent,
		systemRootTask,
		systemRootTaskEntry,
	}
	for _, tc := range []struct {
		name     string
		readOnly bool
		profile  *CapabilityProfile
		max      int
	}{
		{name: "read-only user", readOnly: true, profile: profileWithRoleAndRoots("user"), max: 4 * 1024},
		{name: "watch user", profile: profileWithRoleAndRoots("user", systemRootWatch, systemRootWatchEvent), max: 4 * 1024},
		{name: "full admin", profile: profileWithRoleAndRoots("admin", allRoots...), max: 4 * 1024},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := capabilityCompletionInstructions(allowedSkills(tc.readOnly, tc.profile))
			if len(payload) > tc.max {
				t.Fatalf("capability addendum = %d bytes, max %d", len(payload), tc.max)
			}
			t.Logf("capability addendum: %d bytes", len(payload))
		})
	}
}

func TestWatchSkillsTeachAllFourDecisionBranches(t *testing.T) {
	// The merged watch_write guide must retain the behavioral contract formerly
	// split across watch_write, watch_flow, and watch_delivery.
	for _, fragment := range []string{
		"deterministic",
		"semantic",
		"noisy",
		"only asks to be told",
		"explicitly asks GraphJin to act",
		"notification request",
		"is not permission to act",
		"Deterministic action triggers",
		"semantic or noisy action triggers",
		"help:security",
		"help:runtime",
		"help:watches",
		`delivery_json: { kind: "inbox", digest: { window: "1h" } }`,
		"condition_js never executes",
		"default_watch_triage",
		"inline AxFlow Mermaid",
		"create no flow artifact or flows/ file",
		"Tool-free flows",
		"notify/digest/discard",
		"info/warn/critical",
		"280 characters",
		"delivery_json.digest.window for noise",
		"Failure sends raw inbox notification, never workflow/webhook",
		"Inspect a named workflow",
		"flow_approval/action_approval pending",
		"flow_review_json",
		"successful preview of the current flow_hash",
		"action_review_json",
		"only that hash in a later run",
		"Never create and approve an action in the same run",
	} {
		if !strings.Contains(watchWriteInstruction, fragment) {
			t.Fatalf("watch lifecycle guidance missing %q", fragment)
		}
	}
}

func TestWatchReadSkillUsesCanonicalEventAndAcknowledgementShapes(t *testing.T) {
	for _, fragment := range []string{
		"help:security",
		"help:runtime",
		"help:watches",
		"gj_watch_event(where:",
		"data_json",
		"not event_json or payload_json",
		`eq: "<event_id>"`,
		"update: { seen: true }",
		"quote its exact string id",
	} {
		if !strings.Contains(watchReadInstruction, fragment) {
			t.Fatalf("watch read guidance missing %q", fragment)
		}
	}
}

func TestSkillsExposeEnforcedDiscoveryPrerequisites(t *testing.T) {
	for _, tc := range []struct {
		name      string
		guidance  string
		fragments []string
	}{
		{
			name:      "cross-source reads",
			guidance:  dataDiscoveryInstruction,
			fragments: []string{"source:<name>", "exact catalog IDs", "joined field"},
		},
		{
			name:      "data writes",
			guidance:  dataWriteInstruction,
			fragments: []string{"help:security", "help:runtime", "exact catalog IDs", "mutation_pattern", "table details", "table-root mutation syntax", "never invent Hasura"},
		},
		{
			name:      "watch reads",
			guidance:  watchReadInstruction,
			fragments: []string{"help:watches", "exact examples", "never invent table IDs"},
		},
		{
			name:      "watch writes",
			guidance:  watchWriteInstruction,
			fragments: []string{"help:security", "help:runtime", "help:watches", "exact catalog IDs"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, fragment := range tc.fragments {
				if !strings.Contains(tc.guidance, fragment) {
					t.Fatalf("skill guidance missing enforced prerequisite %q: %s", fragment, tc.guidance)
				}
			}
		})
	}
}

func TestRunPassesOnlyCapabilityFilteredConstructorSkills(t *testing.T) {
	allRoots := []string{
		systemRootSecurity,
		systemRootRuntime,
		systemRootConfig,
		systemRootWorkflow,
		systemRootWorkflowExec,
		systemRootWatch,
		systemRootWatchEvent,
		systemRootTask,
		systemRootTaskEntry,
	}
	for _, tc := range []struct {
		name        string
		instruction string
		readOnly    bool
		profile     *CapabilityProfile
	}{
		{name: "anonymous", instruction: "show recent orders"},
		{name: "user", instruction: "show recent orders", profile: profileWithRoleAndRoots("user")},
		{name: "read only", instruction: "change an order", readOnly: true, profile: profileWithRoleAndRoots("user")},
		{name: "watch enabled", instruction: "let me know if a PO slips", profile: profileWithRoleAndRoots("user", systemRootWatch)},
		{name: "workflow enabled", instruction: "run the daily roast plan", profile: profileWithRoleAndRoots("user", systemRootWorkflowExec)},
		{name: "admin", instruction: "review configuration", profile: profileWithRoleAndRoots("admin", allRoots...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			program := &fakeProgram{output: map[string]ax.Value{"status": StatusAnswered, "answer": "ok"}}
			var captured map[string]ax.Value
			runner := newAgent(Config{ReadOnly: tc.readOnly, TimeoutSeconds: 5}, &fakeRuntime{},
				WithClientFactory(func(Config) (ax.AIClient, error) { return fakeClient{}, nil }),
				WithProgramFactory(func(_ string, options map[string]ax.Value) Program {
					captured = options
					program.options = options
					program.onForward = func(p *fakeProgram) {
						callProgramTool(t, p, toolQueryCatalog, map[string]ax.Value{"id": "help:discovery"})
					}
					return program
				}),
			)
			resp, err := runner.Run(context.Background(), Request{Instruction: tc.instruction, Capabilities: tc.profile})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if resp.Status != StatusAnswered {
				t.Fatalf("status = %s, want answered: %+v", resp.Status, resp)
			}
			want := skillIDs(allowedSkills(tc.readOnly, tc.profile))
			if got := optionSkillIDs(t, captured["skills"]); !reflect.DeepEqual(got, want) {
				t.Fatalf("constructor skills = %v, want %v", got, want)
			}
			for _, forbidden := range []string{"skillsCatalog", "skills_catalog", "onSkillsSearch", "on_skills_search", "skillsMode", "skills_mode"} {
				if _, ok := captured[forbidden]; ok {
					t.Fatalf("unexpected skill discovery option %q", forbidden)
				}
			}
			if _, ok := captured["onUsedSkills"].(ax.AxAgentObserverFn); !ok {
				t.Fatalf("onUsedSkills type = %T, want ax.AxAgentObserverFn", captured["onUsedSkills"])
			}
		})
	}
}

func TestWatchParaphrasesReceiveWatchWriteWithoutRouting(t *testing.T) {
	profile := profileWithRoleAndRoots("user", systemRootWatch)
	for _, instruction := range []string{
		"watch the roast telemetry",
		"let me know if a PO slips",
		"keep an eye on late production orders",
	} {
		t.Run(instruction, func(t *testing.T) {
			program := &fakeProgram{output: map[string]ax.Value{"status": StatusAnswered, "answer": "ok"}}
			var captured map[string]ax.Value
			runner := newAgent(Config{TimeoutSeconds: 5}, &fakeRuntime{},
				WithClientFactory(func(Config) (ax.AIClient, error) { return fakeClient{}, nil }),
				WithProgramFactory(func(_ string, options map[string]ax.Value) Program {
					captured = options
					program.options = options
					program.onForward = func(p *fakeProgram) {
						callProgramTool(t, p, toolQueryCatalog, map[string]ax.Value{"id": "help:discovery"})
					}
					return program
				}),
			)
			if _, err := runner.Run(context.Background(), Request{Instruction: instruction, Capabilities: profile}); err != nil {
				t.Fatalf("Run: %v", err)
			}
			ids := optionSkillIDs(t, captured["skills"])
			if !containsString(ids, skillWatchWrite) {
				t.Fatalf("watch_write not loaded for %q: %v", instruction, ids)
			}
			if _, ok := captured["skillsCatalog"]; ok {
				t.Fatal("watch skill should be preloaded, not discovered through a catalog")
			}
		})
	}
}

func TestUsedSkillObserverProducesPluralTelemetryAndAlias(t *testing.T) {
	program := &fakeProgram{output: map[string]ax.Value{"status": StatusAnswered, "answer": "done"}}
	runner := newAgent(Config{TimeoutSeconds: 5}, &fakeRuntime{},
		WithClientFactory(func(Config) (ax.AIClient, error) { return fakeClient{}, nil }),
		WithProgramFactory(func(_ string, options map[string]ax.Value) Program {
			program.options = options
			program.onForward = func(p *fakeProgram) {
				callProgramTool(t, p, toolQueryCatalog, map[string]ax.Value{"id": "help:discovery"})
				observer := p.options["onUsedSkills"].(ax.AxAgentObserverFn)
				observer([]ax.Value{
					ax.Object("id", skillWatchRead, "name", "Watch inbox", "reason", "Reviewed events", "stage", "distiller"),
					ax.Object("id", skillWatchWrite, "name", "Watch lifecycle", "reason", "Created the watch", "stage", "executor"),
				})
			}
			return program
		}),
	)

	resp, err := runner.Run(context.Background(), Request{
		Instruction:  "review my alerts then create another watch",
		Capabilities: profileWithRoleAndRoots("user", systemRootWatch, systemRootWatchEvent),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []SkillUsage{
		{ID: skillWatchRead, Name: "Watch inbox", Reason: "Reviewed events", Stage: "distiller"},
		{ID: skillWatchWrite, Name: "Watch lifecycle", Reason: "Created the watch", Stage: "executor"},
	}
	if !reflect.DeepEqual(resp.Skills, want) {
		t.Fatalf("skills = %+v, want %+v", resp.Skills, want)
	}
	if resp.Skill != skillWatchRead {
		t.Fatalf("deprecated skill alias = %q, want first used id %q", resp.Skill, skillWatchRead)
	}
}

func TestNoUsedSkillsLeavesTelemetryEmpty(t *testing.T) {
	program := &fakeProgram{output: map[string]ax.Value{
		"status": StatusAnswered,
		"answer": "done",
		"skill":  "model_supplied_value_must_not_count",
	}}
	runner := newAgent(Config{TimeoutSeconds: 5}, &fakeRuntime{},
		WithClientFactory(func(Config) (ax.AIClient, error) { return fakeClient{}, nil }),
		WithProgramFactory(func(_ string, options map[string]ax.Value) Program {
			program.options = options
			program.onForward = func(p *fakeProgram) {
				callProgramTool(t, p, toolQueryCatalog, map[string]ax.Value{"id": "help:discovery"})
			}
			return program
		}),
	)
	resp, err := runner.Run(context.Background(), Request{Instruction: "list things"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(resp.Skills) != 0 || resp.Skill != "" {
		t.Fatalf("unused skills leaked into response: %+v / %q", resp.Skills, resp.Skill)
	}
}

func TestExecuteGraphQLRequiresWorkflowDetailBeforeWorkflowExecution(t *testing.T) {
	program := &fakeProgram{output: map[string]ax.Value{"status": StatusAnswered, "answer": "done"}}
	rt := &fakeRuntime{}
	runner := newAgent(Config{TimeoutSeconds: 5}, rt,
		WithClientFactory(func(Config) (ax.AIClient, error) { return fakeClient{}, nil }),
		WithProgramFactory(func(_ string, options map[string]ax.Value) Program {
			program.options = options
			program.onForward = func(p *fakeProgram) {
				callProgramTool(t, p, toolQueryCatalog, map[string]ax.Value{"id": "help:security"})
				_, _ = callProgramToolError(p, toolExecuteGraphQL, map[string]ax.Value{
					"query": `mutation { gj_workflow_execution(insert: {name: "daily_roast"}) { id } }`,
				})
			}
			return program
		}),
	)
	resp, err := runner.Run(context.Background(), Request{
		Instruction:  "run daily roast",
		Capabilities: profileWithRoleAndRoots("user", systemRootWorkflowExec),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp.Status != StatusBlocked || !responseHasProtocolError(resp, "workflow_detail_required") {
		t.Fatalf("response = %+v, want workflow_detail_required", resp)
	}
	for _, call := range rt.calls {
		if call == toolExecuteGraphQL {
			t.Fatal("workflow execution reached the runtime without detail evidence")
		}
	}
}

func TestExecuteGraphQLAllowsWorkflowExecutionAfterDetail(t *testing.T) {
	program := &fakeProgram{output: map[string]ax.Value{"status": StatusAnswered, "answer": "done"}}
	rt := &fakeRuntime{}
	runner := newAgent(Config{TimeoutSeconds: 5}, rt,
		WithClientFactory(func(Config) (ax.AIClient, error) { return fakeClient{}, nil }),
		WithProgramFactory(func(_ string, options map[string]ax.Value) Program {
			program.options = options
			program.onForward = func(p *fakeProgram) {
				callProgramTool(t, p, toolQueryCatalog, map[string]ax.Value{"id": "help:security"})
				callProgramTool(t, p, toolQueryCatalog, map[string]ax.Value{"id": "workflow:daily_roast"})
				callProgramTool(t, p, toolExecuteGraphQL, map[string]ax.Value{
					"query": `mutation { gj_workflow_execution(insert: {name: "daily_roast"}) { id } }`,
				})
			}
			return program
		}),
	)
	resp, err := runner.Run(context.Background(), Request{
		Instruction:  "run daily roast",
		Capabilities: profileWithRoleAndRoots("user", systemRootWorkflowExec),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp.Status != StatusAnswered {
		t.Fatalf("status = %s, want answered: %+v", resp.Status, resp)
	}
	if !containsString(rt.calls, toolExecuteGraphQL) {
		t.Fatalf("calls = %v, want execute_graphql", rt.calls)
	}
}

func TestWriteLikeGraphQLCoversSystemWriteRoots(t *testing.T) {
	for _, query := range []string{
		`mutation { products(insert: {name: "x"}) { id } }`,
		`query { gj_config { id } }`,
		`query { gj_workflow { id } }`,
		`query { gj_code { id } }`,
		`query { gj_watch { id } }`,
		`query { gj_watch_event { id } }`,
	} {
		if !writeLikeGraphQL(query) {
			t.Fatalf("writeLikeGraphQL(%q) = false, want true", query)
		}
	}
	if writeLikeGraphQL(`query { products { id } }`) {
		t.Fatal("ordinary read query should not be write-like")
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func BenchmarkAllowedSkills(b *testing.B) {
	profile := profileWithRoleAndRoots("admin",
		systemRootSecurity,
		systemRootRuntime,
		systemRootConfig,
		systemRootWorkflow,
		systemRootWorkflowExec,
		systemRootWatch,
		systemRootWatchEvent,
	)
	b.ReportAllocs()
	for b.Loop() {
		_ = allowedSkills(false, profile)
	}
}

func BenchmarkSkillPromptConstruction(b *testing.B) {
	profile := profileWithRoleAndRoots("admin",
		systemRootSecurity,
		systemRootRuntime,
		systemRootConfig,
		systemRootWorkflow,
		systemRootWorkflowExec,
		systemRootWatch,
		systemRootWatchEvent,
	)
	definitions := allowedSkills(false, profile)
	b.ReportAllocs()
	for b.Loop() {
		_ = ax.NewAgent(agentSignature, map[string]ax.Value{
			"contextFields": []ax.Value{"history", "context"},
			"skills":        skillValues(definitions),
		})
	}
}
