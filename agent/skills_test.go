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
	return &CapabilityProfile{RoleClass: role, AvailableSystemRoots: roots}
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

func TestBuiltinSkillsAreTheOrderedTwelveFocusedGuides(t *testing.T) {
	want := []string{
		skillDataDiscovery,
		skillDataWrite,
		skillCodeRead,
		skillCodeWrite,
		skillWorkflowRead,
		skillWorkflowExec,
		skillWorkflowWrite,
		skillWatchRead,
		skillWatchWrite,
		skillWatchFlow,
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
		systemRootWatchFlowPreview,
	}
	baseRead := []string{skillDataDiscovery, skillCodeRead}
	baseWrite := []string{skillDataDiscovery, skillDataWrite, skillCodeRead, skillCodeWrite}

	for _, tc := range []struct {
		name     string
		readOnly bool
		profile  *CapabilityProfile
		want     []string
	}{
		{name: "anonymous", want: baseWrite},
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
			name:    "watch flow needs both roots",
			profile: profileWithRoleAndRoots("user", systemRootWatch, systemRootWatchFlowPreview),
			want:    append(append([]string{}, baseWrite...), skillWatchRead, skillWatchWrite, skillWatchFlow),
		},
		{
			name:    "preview alone is read guidance only",
			profile: profileWithRoleAndRoots("user", systemRootWatchFlowPreview),
			want:    append(append([]string{}, baseWrite...), skillWatchRead),
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
				skillCodeRead,
				skillWorkflowRead,
				skillWatchRead,
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
		systemRootWatchFlowPreview,
	}
	for _, tc := range []struct {
		name    string
		profile *CapabilityProfile
		max     int
	}{
		{name: "normal user", profile: profileWithRoleAndRoots("user"), max: 3 * 1024},
		{name: "full admin", profile: profileWithRoleAndRoots("admin", allRoots...), max: 8 * 1024},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := json.Marshal(skillValues(allowedSkills(false, tc.profile)))
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

func TestRunPassesOnlyCapabilityFilteredConstructorSkills(t *testing.T) {
	allRoots := []string{
		systemRootSecurity,
		systemRootRuntime,
		systemRootConfig,
		systemRootWorkflow,
		systemRootWorkflowExec,
		systemRootWatch,
		systemRootWatchEvent,
		systemRootWatchFlowPreview,
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
		`query { gj_watch_flow_preview { id } }`,
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
		systemRootWatchFlowPreview,
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
		systemRootWatchFlowPreview,
	)
	definitions := allowedSkills(false, profile)
	b.ReportAllocs()
	for b.Loop() {
		_ = ax.NewAgent(agentSignature, map[string]ax.Value{
			"contextFields": []ax.Value{"history"},
			"skills":        skillValues(definitions),
		})
	}
}
