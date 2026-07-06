package agent

import (
	"context"
	"strings"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
)

func seedWithKinds(kinds ...string) any {
	cards := make([]any, 0, len(kinds))
	for _, k := range kinds {
		cards = append(cards, map[string]any{"kind": k})
	}
	return map[string]any{"cards": cards}
}

func profileWithRoots(roots ...string) *CapabilityProfile {
	return &CapabilityProfile{AvailableSystemRoots: roots}
}

func seedWithCodeSource() any {
	return map[string]any{"cards": []any{map[string]any{"kind": "table", "source_kind": "code", "name": "gj_code"}}}
}

func adminProfile() *CapabilityProfile {
	return profileWithRoots(systemRootConfig, systemRootSecurity, systemRootRuntime)
}

func workflowProfile() *CapabilityProfile {
	return profileWithRoots(systemRootWorkflow, systemRootWorkflowExec)
}

func watchProfile() *CapabilityProfile {
	return profileWithRoots(systemRootWatch, systemRootWatchEvent)
}

// controlPlaneSeedRuntime returns a control-plane (config) seed so selectSkill picks an
// admin skill, and reflects id lookups back so help:security sets the security/runtime
// evidence flag in the protocol guard.
func controlPlaneSeedRuntime() *fakeRuntime {
	return &fakeRuntime{
		catalogOverride: func(args map[string]any) any {
			if id := stringArg(args, "id"); id != "" {
				return map[string]any{
					"count": 1,
					"cards": []any{map[string]any{"id": id, "kind": "help", "name": strings.TrimPrefix(id, "help:")}},
				}
			}
			return map[string]any{
				"count": 1,
				"cards": []any{map[string]any{"id": "system:gj_config", "kind": "config", "name": "gj_config"}},
			}
		},
	}
}

func evidenceSelectedSkill(t *testing.T, resp Response) string {
	t.Helper()
	ev, ok := resp.Evidence.(map[string]any)
	if !ok {
		t.Fatalf("evidence type = %T", resp.Evidence)
	}
	name, _ := ev["selected_skill"].(string)
	return name
}

func TestHasWriteIntent(t *testing.T) {
	for _, tc := range []struct {
		instruction string
		want        bool
	}{
		{"show me recent orders", false},
		{"review the security posture", false},
		{"list the workflows that exist", false},
		{"find the function that reserves green coffee", false},
		{"what does the daily roast workflow do", false},
		{"create a new order for the customer", true},
		{"add a new admin role to the config", true},
		{"fix the bug in the roast plan function", true},
		{"run the daily roast plan workflow", true},
		{"update the subscription quantity", true},
		{"delete the stale ticket", true},
		{"pause the failed orders watch", true},
		{"resume my inventory watch", true},
		{"mark the watch events as seen", true},
	} {
		if got := hasWriteIntent(tc.instruction); got != tc.want {
			t.Fatalf("hasWriteIntent(%q) = %v, want %v", tc.instruction, got, tc.want)
		}
	}
}

func TestSelectSkill(t *testing.T) {
	for _, tc := range []struct {
		name        string
		instruction string
		seed        any
		readOnly    bool
		profile     *CapabilityProfile
		want        string
	}{
		{
			name: "data read is the default", instruction: "show me recent orders",
			seed: seedWithKinds("table"), readOnly: false, profile: nil, want: skillDataDiscovery,
		},
		{
			name: "data write on write intent", instruction: "create a new order",
			seed: seedWithKinds("table"), readOnly: false, profile: nil, want: skillDataWrite,
		},
		{
			name: "read_only suppresses data write", instruction: "create a new order",
			seed: seedWithKinds("table"), readOnly: true, profile: nil, want: skillDataDiscovery,
		},
		{
			name: "admin read on read intent", instruction: "review the security posture",
			seed: seedWithKinds("system_capability"), readOnly: false, profile: adminProfile(), want: skillAdminRead,
		},
		{
			name: "admin write on write intent", instruction: "add a new admin role to the config",
			seed: seedWithKinds("config", "config_recipe"), readOnly: false, profile: adminProfile(), want: skillAdminWrite,
		},
		{
			name: "read_only forces admin read despite write verb", instruction: "add a new admin role",
			seed: seedWithKinds("config"), readOnly: true, profile: adminProfile(), want: skillAdminRead,
		},
		{
			name: "admin roots but data-only seed stays data read", instruction: "list products",
			seed: seedWithKinds("table", "column"), readOnly: false, profile: adminProfile(), want: skillDataDiscovery,
		},
		{
			name: "workflow read on read intent", instruction: "what workflow reviews production risk",
			seed: seedWithKinds("workflow"), readOnly: false, profile: workflowProfile(), want: skillWorkflowRead,
		},
		{
			name: "workflow write on run intent", instruction: "run the daily roast plan workflow",
			seed: seedWithKinds("workflow"), readOnly: false, profile: workflowProfile(), want: skillWorkflowWrite,
		},
		{
			name: "code read on read intent", instruction: "find the function that reserves green coffee",
			seed: seedWithCodeSource(), readOnly: false, profile: nil, want: skillCodeRead,
		},
		{
			name: "code write on write intent", instruction: "fix the bug in the roast plan function",
			seed: seedWithCodeSource(), readOnly: false, profile: nil, want: skillCodeWrite,
		},
		{
			name: "watch read on read intent", instruction: "what fired on my watches this week",
			seed: seedWithKinds("table"), readOnly: false, profile: watchProfile(), want: skillWatchRead,
		},
		{
			name: "watch write on write intent", instruction: "create a watch on failed orders",
			seed: seedWithKinds("table"), readOnly: false, profile: watchProfile(), want: skillWatchWrite,
		},
		{
			name: "read_only forces watch read despite write verb", instruction: "pause the failed orders watch",
			seed: seedWithKinds("table"), readOnly: true, profile: watchProfile(), want: skillWatchRead,
		},
		{
			name: "watch intent without watch roots stays data", instruction: "create a watch on failed orders",
			seed: seedWithKinds("table"), readOnly: false, profile: profileWithRoots(systemRootCatalog), want: skillDataWrite,
		},
		{
			name: "admin config seed outranks watch intent", instruction: "enable watches in the config",
			seed: seedWithKinds("config", "config_recipe"), readOnly: false,
			profile: profileWithRoots(systemRootConfig, systemRootSecurity, systemRootRuntime, systemRootWatch),
			want:    skillAdminWrite,
		},
		{
			name: "admin write outranks a code card", instruction: "update gj_config roles",
			seed: map[string]any{"cards": []any{
				map[string]any{"kind": "config"},
				map[string]any{"kind": "table", "source_kind": "code"},
			}},
			readOnly: false, profile: adminProfile(), want: skillAdminWrite,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := selectSkill(tc.instruction, tc.seed, tc.readOnly, tc.profile).name; got != tc.want {
				t.Fatalf("selectSkill = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuiltinSkillsAllowFullToolSurface(t *testing.T) {
	allTools := []string{toolGraphQLHelp, toolQueryCatalog, toolValidateWhere, toolExecuteSavedQuery, toolExecuteGraphQL}
	for name, sk := range builtinSkills {
		for _, tool := range allTools {
			if !sk.allowTool(tool) {
				t.Fatalf("built-in skill %q should allow %s", name, tool)
			}
		}
	}
}

func TestAllowToolSetRestricts(t *testing.T) {
	allow := allowToolSet(toolGraphQLHelp, toolQueryCatalog)
	if !allow(toolQueryCatalog) {
		t.Fatal("allowToolSet should allow a listed tool")
	}
	if allow(toolExecuteGraphQL) {
		t.Fatal("allowToolSet should remove an unlisted tool")
	}
}

func TestFilterToolsBySkill(t *testing.T) {
	tools := []ax.Tool{{Name: toolQueryCatalog}, {Name: toolExecuteGraphQL}}
	got := filterToolsBySkill(tools, skill{allowTool: allowToolSet(toolQueryCatalog)})
	if len(got) != 1 || got[0].Name != toolQueryCatalog {
		t.Fatalf("filterToolsBySkill = %+v, want only query_catalog", got)
	}
	if all := filterToolsBySkill(tools, skill{}); len(all) != 2 {
		t.Fatalf("nil allowTool should pass through, got %d tools", len(all))
	}
}

func TestSkillRequiredEvidence(t *testing.T) {
	admin := builtinSkills[skillAdminWrite]
	if admin.requiredEvidence == nil {
		t.Fatal("admin_write must declare required evidence")
	}
	if admin.requiredEvidence(&discoveryState{securityRuntimeEvidence: false}) {
		t.Fatal("admin_write requiredEvidence should be false without security/runtime evidence")
	}
	if !admin.requiredEvidence(&discoveryState{securityRuntimeEvidence: true}) {
		t.Fatal("admin_write requiredEvidence should be true with security/runtime evidence")
	}
	// Read skills impose no answer-level evidence gate (core + protocol guard enforce safety).
	for _, name := range []string{
		skillDataDiscovery, skillCodeRead, skillWorkflowRead, skillAdminRead, skillWatchRead,
	} {
		if builtinSkills[name].requiredEvidence != nil {
			t.Fatalf("read skill %q should have no required evidence", name)
		}
	}
	// Every write skill declares an answer-level evidence backstop.
	for _, name := range []string{skillDataWrite, skillCodeWrite, skillWorkflowWrite, skillWatchWrite} {
		if builtinSkills[name].requiredEvidence == nil {
			t.Fatalf("write skill %q must declare required evidence", name)
		}
	}

	if !dataWriteEvidence(newDiscoveryState("x")) {
		t.Fatal("data_write evidence should hold when no uncovered mutation executed")
	}
	uncovered := newDiscoveryState("x")
	uncovered.hasUncoveredMutation = true
	if dataWriteEvidence(uncovered) {
		t.Fatal("data_write evidence should fail after an uncovered mutation")
	}

	codeState := newDiscoveryState("x")
	codeState.codeWriteExecuted = true
	if codeWriteEvidence(codeState) {
		t.Fatal("code_write evidence should fail when a gj_code write executed without security evidence")
	}
	codeState.securityRuntimeEvidence = true
	if !codeWriteEvidence(codeState) {
		t.Fatal("code_write evidence should hold with security evidence")
	}

	wfState := newDiscoveryState("x")
	wfState.workflowExecuted = true
	if workflowWriteEvidence(wfState) {
		t.Fatal("workflow_write evidence should fail when a workflow executed without a detail inspection")
	}
	wfState.workflowsDetailed["daily_roast_plan"] = true
	if !workflowWriteEvidence(wfState) {
		t.Fatal("workflow_write evidence should hold after a workflow detail inspection")
	}
	if !workflowWriteEvidence(newDiscoveryState("x")) {
		t.Fatal("workflow_write evidence should hold when nothing executed")
	}

	watchState := newDiscoveryState("x")
	watchState.watchWriteExecuted = true
	if watchWriteEvidence(watchState) {
		t.Fatal("watch_write evidence should fail when a gj_watch write executed without security evidence")
	}
	watchState.securityRuntimeEvidence = true
	if !watchWriteEvidence(watchState) {
		t.Fatal("watch_write evidence should hold with security evidence")
	}
	if !watchWriteEvidence(newDiscoveryState("x")) {
		t.Fatal("watch_write evidence should hold when nothing executed")
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

func TestRunRecordsSelectedSkillInEvidence(t *testing.T) {
	program := &fakeProgram{output: map[string]ax.Value{"status": StatusAnswered, "answer": "ok"}}
	runner := newAgent(Config{TimeoutSeconds: 5}, &fakeRuntime{},
		WithClientFactory(func(Config) (ax.AIClient, error) { return fakeClient{}, nil }),
		WithProgramFactory(func(_ string, options map[string]ax.Value) Program {
			program.options = options
			program.onForward = func(p *fakeProgram) {
				callProgramTool(t, p, "query_catalog", map[string]ax.Value{"id": "help:discovery"})
			}
			return program
		}),
	)
	// A read instruction with no capability profile resolves to the base data_discovery mode.
	resp, err := runner.Run(context.Background(), Request{Instruction: "list things"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp.Status != StatusAnswered {
		t.Fatalf("status = %s, want answered: %+v", resp.Status, resp)
	}
	if got := evidenceSelectedSkill(t, resp); got != skillDataDiscovery {
		t.Fatalf("selected_skill = %q, want %q", got, skillDataDiscovery)
	}
}

func TestRunAdminWriteBlocksWithoutSecurityEvidence(t *testing.T) {
	program := &fakeProgram{output: map[string]ax.Value{"status": StatusAnswered, "answer": "changed config"}}
	runner := newAgent(Config{TimeoutSeconds: 5}, controlPlaneSeedRuntime(),
		WithClientFactory(func(Config) (ax.AIClient, error) { return fakeClient{}, nil }),
		WithProgramFactory(func(_ string, options map[string]ax.Value) Program {
			program.options = options
			program.onForward = func(p *fakeProgram) {
				// Discovery happened, but not security/runtime guidance.
				callProgramTool(t, p, "query_catalog", map[string]ax.Value{"id": "help:discovery"})
			}
			return program
		}),
	)
	resp, err := runner.Run(context.Background(), Request{
		Instruction:  "update the config to add an admin role",
		Capabilities: adminProfile(),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := evidenceSelectedSkill(t, resp); got != skillAdminWrite {
		t.Fatalf("selected_skill = %q, want %q", got, skillAdminWrite)
	}
	if resp.Status != StatusBlocked {
		t.Fatalf("status = %s, want blocked: %+v", resp.Status, resp)
	}
	if !responseHasProtocolError(resp, "skill_evidence_required") {
		t.Fatalf("missing skill_evidence_required error: %+v", resp.Errors)
	}
}

func TestRunAdminWriteAnswersWithSecurityEvidence(t *testing.T) {
	program := &fakeProgram{output: map[string]ax.Value{"status": StatusAnswered, "answer": "previewed config change"}}
	runner := newAgent(Config{TimeoutSeconds: 5}, controlPlaneSeedRuntime(),
		WithClientFactory(func(Config) (ax.AIClient, error) { return fakeClient{}, nil }),
		WithProgramFactory(func(_ string, options map[string]ax.Value) Program {
			program.options = options
			program.onForward = func(p *fakeProgram) {
				callProgramTool(t, p, "query_catalog", map[string]ax.Value{"id": "help:security"})
			}
			return program
		}),
	)
	resp, err := runner.Run(context.Background(), Request{
		Instruction:  "update the config to add an admin role",
		Capabilities: adminProfile(),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := evidenceSelectedSkill(t, resp); got != skillAdminWrite {
		t.Fatalf("selected_skill = %q, want %q", got, skillAdminWrite)
	}
	if resp.Status != StatusAnswered {
		t.Fatalf("status = %s, want answered: %+v", resp.Status, resp)
	}
	if len(resp.Errors) != 0 {
		t.Fatalf("unexpected errors: %+v", resp.Errors)
	}
}
