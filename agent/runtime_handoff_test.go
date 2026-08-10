package agent

import (
	"fmt"
	"strings"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
)

func TestGraphJinRuntimeCarriesExecutionAcrossAxStagePatch(t *testing.T) {
	execution := map[string]any{
		"tool": "execute_saved_query",
		"args": map[string]any{"name": "daily_roast_context"},
		"result": map[string]any{
			"data": map[string]any{
				"production_orders": []any{map[string]any{"product_name": "Northstar House Blend 340g"}},
			},
		},
	}
	runtime := newGraphJinCodeRuntime(func() any { return execution }, nil, nil, nil)
	session, err := runtime.CreateSession(map[string]ax.Value{
		"inputs": map[string]any{"instruction": "plan production"},
	}, map[string]ax.Value{
		"reservedNames": []any{"inputs", "instruction", "distilledContext"},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	defer session.Close()

	session.PatchGlobals(map[string]any{
		"version": 1,
		"bindings": map[string]any{
			"runtime": map[string]any{"language": "JavaScript"},
		},
		"globals": map[string]any{
			"runtime": map[string]any{"language": "JavaScript"},
		},
	}, nil)
	visible := mapValue(session.Inspect(nil))
	if got := fmt.Sprint(visible[runtimeLastExecutionKey]); got != runtimeHandoffRedaction(runtimeLastExecutionKey) {
		t.Fatalf("runtime inspection %s = %q, want redacted handoff metadata", runtimeLastExecutionKey, got)
	}

	step := mapValue(session.Execute(`await final("use carried execution", {result: graphjinLastExecution.result.data});`, nil))
	payload := step
	if normalized := mapValue(step["completion_payload"]); normalized != nil {
		payload = normalized
	}
	args := anySlice(payload["args"])
	if len(args) < 2 {
		t.Fatalf("runtime step = %+v", step)
	}
	evidence := mapValue(args[1])
	result := mapValue(evidence["result"])
	if result["production_orders"] == nil {
		t.Fatalf("carried execution result = %+v", result)
	}
}

func TestGraphJinRuntimeAutoFinalizesAfterDuplicateGraceTurn(t *testing.T) {
	execution := map[string]any{
		"tool":   toolExecuteGraphQL,
		"result": map[string]any{"data": map[string]any{"invoices": []any{map[string]any{"id": "INV-1"}}}},
	}
	ready := false
	runtime := newGraphJinCodeRuntime(
		func() any { return execution }, nil, nil, nil,
		func() string {
			if !ready {
				return ""
			}
			ready = false
			return `await final("GraphJin duplicate execution recovery completed.", {execution: globalThis.graphjinLastExecution});`
		},
	)
	session, err := runtime.CreateSession(map[string]ax.Value{
		"inputs": map[string]any{"instruction": "Show invoice INV-1"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	session.Execute(`await final("distilled", {});`, nil)
	session.PatchGlobals(map[string]any{"version": 1, "bindings": map[string]any{}}, nil)
	ready = true
	completed := session.Execute(`console.log("cached duplicate observed");`, nil)
	if runtimeCompletionType(completed) != "final" {
		t.Fatalf("protocol completion = %+v, want final", completed)
	}
	evidence, ok := runtimeFinalEvidence(completed)
	if !ok || mapValue(mapValue(evidence)["execution"])["tool"] != toolExecuteGraphQL {
		t.Fatalf("protocol completion evidence = %+v", evidence)
	}
}

func TestGraphJinRuntimeExecutesDeterministicSystemRootRepair(t *testing.T) {
	ready := false
	catalogCalls := 0
	executionCalls := 0
	runtime := newGraphJinCodeRuntime(
		nil, nil, nil, nil,
		func() string {
			if !ready {
				return ""
			}
			ready = false
			return `globalThis.graphjinSystemRootPrerequisites = await query_catalog({ids:["help:security","help:runtime"]}); globalThis.graphjinSystemRootRepair = await execute_graphql({"query":"query { gj_watch_event(where: { seen: { eq: false } }) { id watch_id data_json seen } }"}); console.log(globalThis.graphjinSystemRootRepair);`
		},
	)
	runtime.RegisterCallable(toolQueryCatalog, func(params ax.Value) (ax.Value, error) {
		catalogCalls++
		ids := anySlice(mapValue(params)["ids"])
		if len(ids) != 2 || ids[0] != "help:security" || ids[1] != "help:runtime" {
			t.Fatalf("catalog repair args = %+v", params)
		}
		return map[string]any{"cards": []any{map[string]any{"id": "help:security"}, map[string]any{"id": "help:runtime"}}}, nil
	})
	runtime.RegisterCallable(toolExecuteGraphQL, func(params ax.Value) (ax.Value, error) {
		executionCalls++
		query := stringFromMap(mapValue(params), "query")
		for _, fragment := range []string{"gj_watch_event", "data_json", "seen"} {
			if !strings.Contains(query, fragment) {
				t.Fatalf("repaired query missing %q: %s", fragment, query)
			}
		}
		return map[string]any{"data": map[string]any{"gj_watch_event": []any{map[string]any{"id": "we:1", "seen": false}}}}, nil
	})
	session, err := runtime.CreateSession(map[string]ax.Value{
		"inputs": map[string]any{"instruction": "Review the unseen watch event."},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	session.Execute(`await final("distilled", {});`, nil)
	session.PatchGlobals(map[string]any{"version": 1, "bindings": map[string]any{}}, nil)

	ready = true
	result := session.Execute(`console.log("model attempted watch_events_unseen");`, nil)
	if catalogCalls != 1 || executionCalls != 1 {
		t.Fatalf("deterministic repair calls catalog=%d execution=%d result=%+v", catalogCalls, executionCalls, result)
	}
}

func TestGraphJinRuntimeCarriesDistillerPayloadAcrossAxStagePatch(t *testing.T) {
	var distilled any
	runtime := newGraphJinCodeRuntime(nil, func(value any) { distilled = value }, nil, nil)
	session, err := runtime.CreateSession(map[string]ax.Value{
		"inputs": map[string]any{
			"instruction": "inventory saved queries",
			"history": []any{
				map[string]any{"role": "assistant", "content": "FIRST-TRAIL-123"},
			},
		},
	}, map[string]ax.Value{
		"reservedNames": []any{"inputs", "executorRequest", "distilledContext"},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	defer session.Close()

	distillerStep := mapValue(session.Execute(`await final("inventory approved saved queries", {saved_query_ids: ["saved_query:daily_roast_context"]});`, nil))
	if mapValue(distillerStep["completion_payload"]) == nil && stringFromMap(distillerStep, "type") != "final" {
		t.Fatalf("distiller runtime step = %+v", distillerStep)
	}

	// Ax's real shared-session path sanitizes the reserved handoff inputs out of
	// the patch snapshot. The wrapper therefore restores the payload captured
	// from the preceding distiller completion.
	session.PatchGlobals(map[string]any{
		"version": 1,
		"bindings": map[string]any{
			"runtime": map[string]any{"language": "JavaScript"},
		},
	}, nil)
	visible := mapValue(session.Inspect(nil))
	for _, key := range []string{runtimeDistilledContextKey, runtimeExecutorRequestKey, runtimeHistoryKey} {
		if got := fmt.Sprint(visible[key]); got != runtimeHandoffRedaction(key) {
			t.Fatalf("runtime inspection %s = %q, want redacted handoff metadata", key, got)
		}
	}
	if len(anySlice(mapValue(distilled)["saved_query_ids"])) != 1 {
		t.Fatalf("distilled callback = %+v, want model-narrowed evidence", distilled)
	}

	step := mapValue(session.Execute(`await final(graphjinExecutorRequest, {ids: graphjinDistilledContext.saved_query_ids, trail: graphjinHistory[0].content});`, nil))
	payload := step
	if normalized := mapValue(step["completion_payload"]); normalized != nil {
		payload = normalized
	}
	args := anySlice(payload["args"])
	if len(args) < 2 {
		t.Fatalf("runtime step = %+v", step)
	}
	evidence := mapValue(args[1])
	if len(anySlice(evidence["ids"])) != 1 || strings.TrimSpace(fmt.Sprint(evidence["trail"])) != "FIRST-TRAIL-123" {
		t.Fatalf("runtime step = %+v", step)
	}
}

func TestGraphJinRuntimeRepairsMalformedDistillerDraftWithOriginalRequestAndSeed(t *testing.T) {
	runtime := newGraphJinCodeRuntime(nil, nil, nil, nil)
	session, err := runtime.CreateSession(map[string]ax.Value{
		"inputs": map[string]any{
			"instruction": "Inventory the approved saved queries. Do discovery only.",
			"context": map[string]any{
				protocolContextKey: map[string]any{
					"cards": []any{map[string]any{"id": "saved_query:daily_roast_context", "kind": "saved_query"}},
				},
			},
		},
	}, map[string]ax.Value{
		"reservedNames": []any{"inputs", "executorRequest", "distilledContext"},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	defer session.Close()

	// This is the real failure shape: the distiller violated final(request,
	// evidence) and forwarded a premature draft response object instead.
	session.Execute(`await final({status: "answered", answer: "No approved saved queries found.", evidence: []});`, nil)
	session.PatchGlobals(map[string]any{
		"version": 1,
		"bindings": map[string]any{
			"runtime": map[string]any{"language": "JavaScript"},
		},
	}, nil)
	visible := mapValue(session.Inspect(nil))
	for _, key := range []string{runtimeExecutorRequestKey, runtimeDistilledContextKey} {
		if got := fmt.Sprint(visible[key]); got != runtimeHandoffRedaction(key) {
			t.Fatalf("runtime inspection %s = %q, want redacted handoff metadata", key, got)
		}
	}
	guarded := mapValue(session.Execute(`await final("No approved saved queries found.", {});`, nil))
	if stringFromMap(mapValue(guarded["result"]), "graphjin_protocol") != "runtime_handoff_read_required" {
		t.Fatalf("handoff-blind executor step = %+v, want runtime_handoff_read_required", guarded)
	}
	step := mapValue(session.Execute(`await final(graphjinExecutorRequest, {cards: graphjinDistilledContext.cards});`, nil))
	payload := step
	if normalized := mapValue(step["completion_payload"]); normalized != nil {
		payload = normalized
	}
	args := anySlice(payload["args"])
	if len(args) < 2 || strings.TrimSpace(fmt.Sprint(args[0])) != "Inventory the approved saved queries. Do discovery only." || len(anySlice(mapValue(args[1])["cards"])) != 1 {
		t.Fatalf("runtime handoff = %+v, want repaired request and runtime-only seed fallback", step)
	}
}

func TestGraphJinRuntimeRequiresExplicitHandoffReadBeforeToolCall(t *testing.T) {
	calls := 0
	runtime := newGraphJinCodeRuntime(nil, nil, nil, nil)
	runtime.RegisterCallable(toolQueryCatalog, func(params ax.Value) (ax.Value, error) {
		calls++
		return map[string]any{"cards": []any{map[string]any{"id": stringFromMap(mapValue(params), "id")}}}, nil
	})
	session, err := runtime.CreateSession(map[string]ax.Value{
		"inputs": map[string]any{"instruction": "Compare the CRM and support sources."},
	}, map[string]ax.Value{
		"reservedNames": []any{"inputs", "executorRequest", "distilledContext"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	session.Execute(`await final("compare sources", {sources: [{id: "source:crm"}, {id: "source:support"}]});`, nil)
	session.PatchGlobals(map[string]any{"version": 1, "bindings": map[string]any{}}, nil)
	guarded := mapValue(session.Execute(`await query_catalog({id: "source:crm"});`, nil))
	if calls != 0 || stringFromMap(mapValue(guarded["result"]), "graphjin_protocol") != "runtime_handoff_read_required" {
		t.Fatalf("handoff-blind tool call = %+v calls=%d", guarded, calls)
	}
	accepted := mapValue(session.Execute(`const selected = graphjinDistilledContext.sources; await query_catalog({id: selected[0].id});`, nil))
	if calls != 1 || stringFromMap(accepted, "kind") != "result" {
		t.Fatalf("handoff-grounded tool call = %+v calls=%d", accepted, calls)
	}
}

func TestGraphJinRuntimeUsesGovernedDistillerDetailFallback(t *testing.T) {
	runtime := newGraphJinCodeRuntime(nil, nil, nil, nil).WithHandoffFallback(func() any {
		return map[string]any{
			"catalog_detail_ids": []any{"source:account_health_api", "table:app:main.account_health"},
			"catalog_detail": map[string]any{
				"cards": []any{map[string]any{"id": "source:account_health_api"}},
				"details": []any{map[string]any{
					"section": "query_shape",
					"content": `query { accounts { account_health { health open_risk_count } } }`,
				}},
			},
		}
	})
	session, err := runtime.CreateSession(map[string]ax.Value{
		"inputs": map[string]any{"instruction": "Combine the account and health API."},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	session.Execute(`await final("combine sources", {});`, nil)
	session.PatchGlobals(map[string]any{"version": 1, "bindings": map[string]any{}}, nil)
	guarded := mapValue(session.Execute(`await final("guessed", {});`, nil))
	if stringFromMap(mapValue(guarded["result"]), "graphjin_protocol") != "runtime_handoff_read_required" {
		t.Fatalf("fallback-blind final = %+v", guarded)
	}
	accepted := session.Execute(`const shape = graphjinDistilledContext.catalog_detail.details[0].content; await final("use shape", {shape});`, nil)
	if runtimeCompletionType(accepted) != "final" {
		t.Fatalf("fallback-grounded final = %+v", accepted)
	}
}

func TestGraphJinRuntimeRejectsPrematureExecutorFinalForRequiredSavedQuery(t *testing.T) {
	pending := `const detail = await query_catalog({id:"saved_query:daily_roast_context"}); const execution = await execute_saved_query({name:"daily_roast_context"});`
	runtime := newGraphJinCodeRuntime(nil, nil, func() string { return pending }, nil)
	session, err := runtime.CreateSession(map[string]ax.Value{
		"inputs": map[string]any{"instruction": "Then execute_saved_query({name:\"daily_roast_context\"})."},
	}, map[string]ax.Value{
		"reservedNames": []any{"inputs", "executorRequest", "distilledContext"},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	defer session.Close()

	distiller := mapValue(session.Execute(`await final("execute daily_roast_context", {});`, nil))
	if runtimeCompletionType(distiller) != "final" {
		t.Fatalf("distiller final was incorrectly guarded: %+v", distiller)
	}
	session.PatchGlobals(map[string]any{"version": 1, "bindings": map[string]any{}}, nil)

	rejected := mapValue(session.Execute(`await final("answer from detail only", {});`, nil))
	if stringFromMap(rejected, "kind") != "result" || stringFromMap(mapValue(rejected["result"]), "graphjin_protocol") != "saved_query_execution_required" {
		t.Fatalf("executor final = %+v, want continuation result", rejected)
	}
	if got := stringFromMap(mapValue(rejected["result"]), "next"); got != pending {
		t.Fatalf("executor continuation = %q, want exact executable guidance", got)
	}
	clarification := mapValue(session.Execute(`await askClarification("Which saved query should I use?");`, nil))
	if stringFromMap(clarification, "kind") != "result" || stringFromMap(mapValue(clarification["result"]), "graphjin_protocol") != "saved_query_execution_required" {
		t.Fatalf("executor clarification = %+v, want continuation result", clarification)
	}
	pending = ""
	accepted := mapValue(session.Execute(`await final("answer from execution", {ok:true});`, nil))
	if runtimeCompletionType(accepted) != "final" {
		t.Fatalf("completed executor final = %+v, want final", accepted)
	}
}

func TestGraphJinRuntimeRejectsFinalUntilExecutionRepair(t *testing.T) {
	pending := "execution_repair_required: retry with a distinct repaired query"
	runtime := newGraphJinCodeRuntime(nil, nil, func() string { return pending }, nil)
	session, err := runtime.CreateSession(map[string]ax.Value{
		"inputs": map[string]any{"instruction": "How many accounts are active?"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	session.Execute(`await final("distilled", {});`, nil)
	session.PatchGlobals(map[string]any{"version": 1, "bindings": map[string]any{}}, nil)
	rejected := mapValue(session.Execute(`await final("blocked", {execution:{errors:[{message:"unknown root"}], recovery:{next:"query accounts instead"}}});`, nil))
	result := mapValue(rejected["result"])
	if got := stringFromMap(result, "graphjin_protocol"); got != "execution_repair_required" {
		t.Fatalf("protocol = %q, result=%+v", got, rejected)
	}
	if strings.Contains(stringFromMap(result, "message"), "execution_repair_required:") {
		t.Fatalf("public message leaked internal prefix: %+v", result)
	}
	attempt := mapValue(result["attempt"])
	if mapValue(attempt["execution"])["errors"] == nil {
		t.Fatalf("repair response discarded attempted execution evidence: %+v", result)
	}
	pending = ""
	accepted := session.Execute(`await final("answered", {count: 4});`, nil)
	if runtimeCompletionType(accepted) != "final" {
		t.Fatalf("final after repair = %+v", accepted)
	}
}

func TestGraphJinRuntimeRunsNarrowSavedQueryContinuationBeforeFinal(t *testing.T) {
	pending := "the discovered saved query must execute before final"
	continuation := `const detail = await query_catalog({id:"saved_query:daily_roast_context"}); const execution = await execute_saved_query({name:"daily_roast_context"}); await final("GraphJin continuation completed.", {detail, execution});`
	runtime := newGraphJinCodeRuntime(nil, nil, func() string { return pending }, func() string { return continuation })
	detailCalls := 0
	executionCalls := 0
	runtime.RegisterCallable("query_catalog", func(params ax.Value) (ax.Value, error) {
		detailCalls++
		return map[string]any{"cards": []any{map[string]any{"id": "saved_query:daily_roast_context"}}}, nil
	})
	runtime.RegisterCallable("execute_saved_query", func(params ax.Value) (ax.Value, error) {
		executionCalls++
		return map[string]any{"data": map[string]any{"production_orders": []any{map[string]any{"product_name": "Northstar"}}}}, nil
	})
	session, err := runtime.CreateSession(map[string]ax.Value{
		"inputs": map[string]any{"instruction": "Find live production work."},
	}, nil)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	defer session.Close()

	session.Execute(`await final("find the saved query", {});`, nil)
	session.PatchGlobals(map[string]any{"version": 1, "bindings": map[string]any{}}, nil)
	resumed := session.Execute(`await final("no live result", {});`, nil)
	if detailCalls != 1 || executionCalls != 1 {
		t.Fatalf("continuation calls = detail:%d execution:%d, want one each; result=%+v", detailCalls, executionCalls, resumed)
	}
	if runtimeCompletionType(resumed) == "final" || !strings.Contains(fmt.Sprint(normalizeValue(resumed)), "Northstar") {
		t.Fatalf("continuation result = %+v, want live execution evidence returned to the next actor step", resumed)
	}
}

func TestGraphJinRuntimeRequiresCodeReadForHistoryDependentFollowUp(t *testing.T) {
	runtime := newGraphJinCodeRuntime(nil, nil, nil, nil)
	session, err := runtime.CreateSession(map[string]ax.Value{
		"inputs": map[string]any{
			"instruction": "What is its amount?",
			"history": []any{
				map[string]any{"role": "assistant", "content": "FIRST-TRAIL-RUNTIME-ONLY"},
			},
		},
	}, map[string]ax.Value{
		"reservedNames": []any{"inputs", "executorRequest", "distilledContext"},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	defer session.Close()

	session.Execute(`await final("continue the prior task", {});`, nil)
	session.PatchGlobals(map[string]any{"version": 1, "bindings": map[string]any{}}, nil)

	guarded := mapValue(session.Execute(`await final("missing trail", {});`, nil))
	guardResult := mapValue(guarded["result"])
	if stringFromMap(guardResult, "graphjin_protocol") != "history_read_required" {
		t.Fatalf("history-blind executor step = %+v, want history_read_required", guarded)
	}
	history := anySlice(guardResult["history"])
	if len(history) != 1 || stringFromMap(mapValue(history[0]), "content") != "FIRST-TRAIL-RUNTIME-ONLY" {
		t.Fatalf("automatic history recovery = %+v", guardResult)
	}
	accepted := mapValue(session.Execute(`await final("repeat marker", {marker: "FIRST-TRAIL-RUNTIME-ONLY"});`, nil))
	if runtimeCompletionType(accepted) != "final" {
		t.Fatalf("history-grounded executor final = %+v, want final", accepted)
	}
}
