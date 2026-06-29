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
		mode        string
		profile     *CapabilityProfile
		want        string
	}{
		{
			name: "data read is the default", instruction: "show me recent orders",
			seed: seedWithKinds("table"), mode: ModeSafe, profile: nil, want: skillDataDiscovery,
		},
		{
			name: "data write on write intent", instruction: "create a new order",
			seed: seedWithKinds("table"), mode: ModeSafe, profile: nil, want: skillDataWrite,
		},
		{
			name: "discovery_only suppresses data write", instruction: "create a new order",
			seed: seedWithKinds("table"), mode: ModeDiscoveryOnly, profile: nil, want: skillDataDiscovery,
		},
		{
			name: "admin read on read intent", instruction: "review the security posture",
			seed: seedWithKinds("system_capability"), mode: ModeSafe, profile: adminProfile(), want: skillAdminRead,
		},
		{
			name: "admin write on write intent", instruction: "add a new admin role to the config",
			seed: seedWithKinds("config", "config_recipe"), mode: ModeSafe, profile: adminProfile(), want: skillAdminWrite,
		},
		{
			name: "discovery_only forces admin read despite write verb", instruction: "add a new admin role",
			seed: seedWithKinds("config"), mode: ModeDiscoveryOnly, profile: adminProfile(), want: skillAdminRead,
		},
		{
			name: "admin roots but data-only seed stays data read", instruction: "list products",
			seed: seedWithKinds("table", "column"), mode: ModeSafe, profile: adminProfile(), want: skillDataDiscovery,
		},
		{
			name: "workflow read on read intent", instruction: "what workflow reviews production risk",
			seed: seedWithKinds("workflow"), mode: ModeSafe, profile: workflowProfile(), want: skillWorkflowRead,
		},
		{
			name: "workflow write on run intent", instruction: "run the daily roast plan workflow",
			seed: seedWithKinds("workflow"), mode: ModeSafe, profile: workflowProfile(), want: skillWorkflowWrite,
		},
		{
			name: "code read on read intent", instruction: "find the function that reserves green coffee",
			seed: seedWithCodeSource(), mode: ModeSafe, profile: nil, want: skillCodeRead,
		},
		{
			name: "code write on write intent", instruction: "fix the bug in the roast plan function",
			seed: seedWithCodeSource(), mode: ModeSafe, profile: nil, want: skillCodeWrite,
		},
		{
			name: "admin write outranks a code card", instruction: "update gj_config roles",
			seed: map[string]any{"cards": []any{
				map[string]any{"kind": "config"},
				map[string]any{"kind": "table", "source_kind": "code"},
			}},
			mode: ModeSafe, profile: adminProfile(), want: skillAdminWrite,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := selectSkill(tc.instruction, tc.seed, tc.mode, tc.profile).name; got != tc.want {
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
	// All other skills impose no answer-level evidence gate (core + protocol guard enforce safety).
	for _, name := range []string{
		skillDataDiscovery, skillDataWrite, skillCodeRead, skillCodeWrite,
		skillWorkflowRead, skillWorkflowWrite, skillAdminRead,
	} {
		if builtinSkills[name].requiredEvidence != nil {
			t.Fatalf("skill %q should have no required evidence", name)
		}
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
