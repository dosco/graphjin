package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ax "github.com/ax-llm/ax/packages/go"
)

type recordedCall struct {
	values  map[string]ax.Value
	options map[string]ax.Value
}

// recordingClient implements ax.AIClient and captures every Chat request ax renders,
// so we can inspect exactly what the model receives per pipeline stage.
type recordingClient struct {
	calls     []recordedCall
	responses []string
}

func (c *recordingClient) Chat(_ context.Context, values map[string]ax.Value, options map[string]ax.Value) (ax.Value, error) {
	c.calls = append(c.calls, recordedCall{values: values, options: options})
	if len(c.calls) > 6 { // safety cap: don't loop forever
		return nil, fmt.Errorf("capture cap reached")
	}
	// Return a minimal valid result so ax advances to the next pipeline stage: ax reads
	// results[].content (axllm.go:1096) and parses it against the requested response_format.
	// Every JS-runtime stage requests a { javascriptCode } output; final() ends the stage.
	content := `{"javascriptCode":"await final('done', {})"}`
	if index := len(c.calls) - 1; index < len(c.responses) {
		content = c.responses[index]
	}
	return map[string]ax.Value{
		"results": []ax.Value{
			map[string]ax.Value{"content": content},
		},
	}, nil
}

func (c *recordingClient) Embed(context.Context, map[string]ax.Value, map[string]ax.Value) (ax.Value, error) {
	return nil, nil
}

func (c *recordingClient) Stream(context.Context, map[string]ax.Value, map[string]ax.Value) ([]ax.Value, error) {
	return nil, nil
}

// TestCaptureRenderedPromptPerStage asserts the RLM prompt boundary and also
// logs the remaining staged payloads. Set PROMPT_CAPTURE_DIR to write full
// per-call dumps for manual auditing.
//
//	go test ./agent -run TestCaptureRenderedPromptPerStage -v
func TestCaptureRenderedPromptPerStage(t *testing.T) {
	const (
		seedID          = "saved_query:seed_prompt_leak_sentinel"
		seedSummary     = "SEED_SUMMARY_MUST_STAY_IN_RUNTIME_7F3A"
		facetKindAudit  = "facet_digest_sentinel_kind"
		toolResultAudit = "TOOL_RESULT_STAGING_AUDIT_91C2"
	)
	rec := &recordingClient{responses: []string{
		`{"javascriptCode":"await final('Inspect the GraphJin catalog and answer from evidence.', {})"}`,
		`{"javascriptCode":"void graphjinDistilledContext.count; const audit = await query_catalog({id:'help:discovery'}); console.log(audit);"}`,
		`{"javascriptCode":"await final('Answer from the inspected catalog.', {ok:true})"}`,
	}}
	runtime := &fakeRuntime{}
	runtime.catalogOverride = func(args map[string]any) any {
		if stringArg(args, "id") == "help:discovery" {
			return map[string]any{
				"count": 1,
				"cards": []any{map[string]any{
					"id": "help:discovery", "kind": "help", "summary": toolResultAudit,
				}},
			}
		}
		return map[string]any{
			"count":  1,
			"facets": map[string]any{facetKindAudit: 17},
			"cards": []any{map[string]any{
				"id": seedID, "kind": "saved_query", "summary": seedSummary,
			}},
		}
	}
	runner := newAgent(
		Config{Provider: "openai", APIKeyEnv: "GRAPHJIN_UNUSED", TimeoutSeconds: 50, MaxSteps: 4},
		runtime,
		WithClientFactory(func(Config) (ax.AIClient, error) { return rec, nil }),
		// Fixed clock so the current_date input is a deterministic sentinel.
		WithNow(func() time.Time { return time.Date(2031, 5, 17, 9, 0, 0, 0, time.UTC) }),
		// Intentionally NO WithProgramFactory: we want ax's real prompt assembly.
	)

	// Ignore Run's outcome; we only need the captured Chat requests. A normal
	// writable profile preloads data_write alongside the other core guides.
	_, _ = runner.Run(context.Background(), Request{Instruction: "create a new order for a customer and update product inventory"})

	if len(rec.calls) == 0 {
		t.Fatal("no Chat calls captured — ax did not reach the client (Seed may have failed)")
	}

	markers := []struct{ name, marker string }{
		{"base:evidence-loop", "evidence loop"},
		{"base:breadth", "Favor BREADTH"},
		{"base:count0", "count: 0"},
		{"base:next.args.id", "next.args.id"},
		{"runtimeUsage", "goja runtime profile"},
		{"SKILL:data_write", "Skill: data_write"},
		{"SKILL:data_aggregation", "Skill: data_aggregation"},
		{"responder:markdown", "markdown table"},
		{"toolDesc:saved_query", "pre-approved saved query"},
		{"toolDesc:execute_graphql", "Execute raw GraphJin GraphQL"},
		{"primitive:final", "final("},
		{"primitive:askClarification", "askClarification"},
		{"input:current_date", "2031-05-17"},
	}

	outDir := os.Getenv("PROMPT_CAPTURE_DIR")
	t.Logf("captured %d Chat call(s)", len(rec.calls))
	var runtimeReached, skillReached, markdownReached, toolResultStaged bool
	var rendered strings.Builder
	for i, call := range rec.calls {
		blob := "===VALUES===\n" + dumpAXValue(call.values) + "\n===OPTIONS===\n" + dumpAXValue(call.options)
		rendered.WriteString(blob)
		var present []string
		for _, m := range markers {
			if strings.Contains(blob, m.marker) {
				present = append(present, m.name)
			}
		}
		if strings.Contains(blob, "goja runtime profile") {
			runtimeReached = true
		}
		if strings.Contains(blob, "Skill: data_write") {
			skillReached = true
		}
		if strings.Contains(blob, "markdown table") { // responder answer-field formatting guidance
			markdownReached = true
		}
		if strings.Contains(blob, toolResultAudit) {
			toolResultStaged = true
		}
		t.Logf("call #%d: len=%d markers=%v", i, len(blob), present)
		if outDir != "" {
			path := filepath.Join(outDir, fmt.Sprintf("prompt_call_%d.txt", i))
			if err := os.WriteFile(path, []byte(blob), 0o644); err == nil {
				t.Logf("  full dump -> %s", path)
			}
		}
	}

	// Regression guards. Universal GraphJin guidance arrives through
	// runtime.usageInstructions; capability-filtered guides arrive through Ax's
	// constructor skills. Both channels must reach the rendered prompts.
	if !runtimeReached {
		t.Error("runtime usage instructions reached no stage — the live prompt channel is broken")
	}
	if !skillReached {
		t.Error("the data_write skill fragment reached no stage — skills are not wired to the live channel")
	}
	if !markdownReached {
		t.Error("markdown answer-formatting guidance reached no stage — the responder answer-field guidance regressed")
	}
	if strings.Contains(rendered.String(), seedID) || strings.Contains(rendered.String(), seedSummary) {
		t.Error("catalog seed card content leaked into a staged prompt instead of staying on inputs.context")
	}
	if !strings.Contains(rendered.String(), "catalog_facets") || !strings.Contains(rendered.String(), facetKindAudit) {
		t.Error("the small catalog facet digest did not reach a staged prompt")
	}
	if !strings.Contains(rendered.String(), "2031-05-17") {
		t.Error("the current_date input did not reach a staged prompt — date anchoring guidance has no value to anchor on")
	}
	loadedSkillBytes := 0
	loadedSkills := allowedSkills(false, nil)
	for _, definition := range loadedSkills {
		loadedSkillBytes += len(definition.content)
	}
	t.Logf(
		"payload audit: staged_bytes=%d runtime_usage_bytes=%d loaded_skill_bytes=%d capability_addendum_bytes=%d tool_result_staged=%t",
		rendered.Len(), len(runtimeUsageInstructions), loadedSkillBytes, len(capabilityCompletionInstructions(loadedSkills)), toolResultStaged,
	)
}

func TestCapabilityCompletionGuidanceIsExecutorOnly(t *testing.T) {
	const capabilityMarker = "Governed per-run capability facts"
	rec := &recordingClient{responses: []string{
		`{"javascriptCode":"await final('Carry out the requested write and reactive work.', {})"}`,
		`{"javascriptCode":"await final({status:'answered',answer:'done'}, {})"}`,
	}}
	runner := newAgent(
		Config{Provider: "openai", APIKeyEnv: "GRAPHJIN_UNUSED", TimeoutSeconds: 50, MaxSteps: 4},
		&fakeRuntime{},
		WithClientFactory(func(Config) (ax.AIClient, error) { return rec, nil }),
	)

	_, _ = runner.Run(context.Background(), Request{
		Instruction:  "create a payment watch and record the matching payment",
		Capabilities: profileWithRoleAndRoots("user", systemRootWatch, systemRootWatchEvent),
	})
	if len(rec.calls) < 2 {
		t.Fatalf("captured %d calls, want distiller and executor prompts", len(rec.calls))
	}

	distiller := dumpAXValue(rec.calls[0].values) + dumpAXValue(rec.calls[0].options)
	if strings.Contains(distiller, "Skill: data_write") || strings.Contains(distiller, "Skill: watch_write") {
		t.Fatal("distiller unexpectedly received executor skill documents")
	}
	if strings.Contains(distiller, capabilityMarker) {
		t.Fatal("distiller received capability-denial guidance without the executor's authoritative skill documents")
	}

	var executorWithCapabilities bool
	for _, call := range rec.calls[1:] {
		rendered := dumpAXValue(call.values) + dumpAXValue(call.options)
		if strings.Contains(rendered, "Skill: data_write") && strings.Contains(rendered, "Skill: watch_write") {
			executorWithCapabilities = true
			if !strings.Contains(rendered, capabilityMarker) {
				t.Fatal("write-capable executor omitted capability-completion guidance")
			}
			if !strings.Contains(rendered, "watch_write is loaded for this run") || strings.Contains(rendered, "watch_write is not loaded") {
				t.Fatal("write-capable executor received the wrong Go-selected watch branch")
			}
			break
		}
	}
	if !executorWithCapabilities {
		t.Fatal("no executor prompt combined data_write, watch_write, and capability-completion guidance")
	}
}

func TestExecutorLoopRepairsMutationEvidenceWithExactContinuation(t *testing.T) {
	const detailSentinel = "PRODUCT_MUTATION_COLUMNS_ID_NAME"
	rec := &recordingClient{responses: []string{
		`{"javascriptCode":"await final('Add the product through the governed write path.', {target:'products'})"}`,
		`{"javascriptCode":"void graphjinDistilledContext.target; const security = await query_catalog({id:'help:security'}); console.log(security);"}`,
		`{"javascriptCode":"const execution = await execute_graphql({query:'mutation { products(insert:{name:\"x\"}) { id } }'}); console.log(execution);"}`,
		`{"javascriptCode":"await final('blocked before mutation evidence', {})"}`,
		`{"javascriptCode":"const execution = await execute_graphql({query:'mutation { products(insert:{name:\"x\"}) { id name } }'}); await final({status:'answered',answer:'Added.',data:execution.data},{execution});"}`,
	}}
	runtime := &successfulExecutionRuntime{}
	runtime.catalogOverride = func(args map[string]any) any {
		if len(detailIDsFromArgs(args)) != 0 {
			result := cloneAnyMap(mapValue(fakeCatalogResult(args)))
			result["details"] = []any{map[string]any{"section": "mutation", "content": detailSentinel}}
			return result
		}
		return map[string]any{
			"count": 1,
			"cards": []any{map[string]any{
				"id": "table:app:main.products", "kind": "table", "table_name": "products",
			}},
		}
	}
	runner := newAgent(
		Config{Provider: "openai", APIKeyEnv: "GRAPHJIN_UNUSED", TimeoutSeconds: 50, MaxSteps: 8},
		runtime,
		WithClientFactory(func(Config) (ax.AIClient, error) { return rec, nil }),
	)

	_, _ = runner.Run(context.Background(), Request{Instruction: "add product x"})
	if len(rec.calls) < 5 {
		t.Fatalf("captured %d calls, want distiller plus repair/retry executor turns", len(rec.calls))
	}
	if runtime.graphqlCalls != 1 {
		t.Fatalf("raw GraphQL calls = %d, rejected mutation must not reach the base runtime", runtime.graphqlCalls)
	}
	repairPrompt := dumpAXValue(rec.calls[3].values) + dumpAXValue(rec.calls[3].options)
	if !strings.Contains(repairPrompt, "mutation_evidence_required") || !strings.Contains(repairPrompt, "graphjin_repair") {
		t.Fatal("mutation rejection did not reach the next executor turn as structured repair evidence")
	}
	postContinuationPrompt := dumpAXValue(rec.calls[4].values) + dumpAXValue(rec.calls[4].options)
	if !strings.Contains(postContinuationPrompt, detailSentinel) {
		t.Fatal("the exact mutation detail continuation did not reach the retry turn")
	}
}

func TestCaptureRenderedSkillsPerCapabilityProfile(t *testing.T) {
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
	}{
		{name: "anonymous"},
		{name: "user", profile: profileWithRoleAndRoots("user")},
		{name: "read-only", readOnly: true, profile: profileWithRoleAndRoots("user")},
		{name: "watch-enabled", profile: profileWithRoleAndRoots("user", systemRootWatch, systemRootWatchEvent)},
		{name: "workflow-enabled", profile: profileWithRoleAndRoots("user", systemRootWorkflow, systemRootWorkflowExec)},
		{name: "admin", profile: profileWithRoleAndRoots("admin", allRoots...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recordingClient{}
			runner := newAgent(
				Config{Provider: "openai", APIKeyEnv: "GRAPHJIN_UNUSED", ReadOnly: tc.readOnly, TimeoutSeconds: 50, MaxSteps: 4},
				&fakeRuntime{},
				WithClientFactory(func(Config) (ax.AIClient, error) { return rec, nil }),
			)
			_, _ = runner.Run(context.Background(), Request{
				Instruction:  "inspect the available GraphJin capabilities",
				Capabilities: tc.profile,
			})
			if len(rec.calls) == 0 {
				t.Fatal("no rendered Ax prompt captured")
			}
			var prompt strings.Builder
			for _, call := range rec.calls {
				prompt.WriteString(dumpAXValue(call.values))
				prompt.WriteString(dumpAXValue(call.options))
			}
			rendered := prompt.String()
			allowed := skillIDs(allowedSkills(tc.readOnly, tc.profile))
			for _, definition := range builtinSkills {
				marker := "Skill: " + definition.id
				if containsString(allowed, definition.id) && !strings.Contains(rendered, marker) {
					t.Errorf("allowed skill %q did not reach an Ax prompt", definition.id)
				}
				if !containsString(allowed, definition.id) && strings.Contains(rendered, marker) {
					t.Errorf("forbidden skill %q leaked into an Ax prompt", definition.id)
				}
			}
			for _, retired := range []string{"Skill: watch_flow", "Skill: watch_delivery"} {
				if strings.Contains(rendered, retired) {
					t.Errorf("retired skill marker %q leaked into an Ax prompt", retired)
				}
			}
		})
	}
}

func dumpAXValue(v any) string {
	data, err := json.MarshalIndent(normalizeValue(v), "", "  ")
	if err != nil {
		return fmt.Sprintf("%#v", v)
	}
	return string(data)
}
