package agent

import (
	"context"
	"strings"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
)

func TestSemanticCatalogGuidanceAndToolSchemaAreConditional(t *testing.T) {
	features := CatalogSearchFeatures{SemanticRecall: true, CoverageBatch: true}
	if got := catalogSearchInstruction(runtimeUsageInstructions, CatalogSearchFeatures{}); got != runtimeUsageInstructions {
		t.Fatal("lexical-only prompt changed")
	}
	semanticPrompt := catalogSearchInstruction(runtimeUsageInstructions, features)
	for _, phrase := range []string{"short noun-and-intent phrases", "combined relationship intent", "recall candidates", "at most one query_catalog({searches:", "same JavaScript block", "coverage.next.args.ids", "never call searches again", "never derive or pass an empty ids array", "without another catalog call"} {
		if !strings.Contains(semanticPrompt, phrase) {
			t.Fatalf("semantic prompt missing %q", phrase)
		}
	}

	queryCatalog := func(agent *Agent) map[string]struct{} {
		fields := map[string]struct{}{}
		for _, tool := range agent.tools(context.Background(), Request{}, agent.runtime) {
			if tool.Name != "query_catalog" {
				continue
			}
			for name := range tool.Args {
				fields[name] = struct{}{}
			}
			return fields
		}
		t.Fatal("query_catalog tool missing")
		return nil
	}
	lexical := newAgent(Config{}, &fakeRuntime{})
	if _, exists := queryCatalog(lexical)["searches"]; exists {
		t.Fatal("lexical-only internal tool exposed searches")
	}
	semantic := newAgent(Config{}, &fakeRuntime{}, WithCatalogSearchFeatures(features))
	if _, exists := queryCatalog(semantic)["searches"]; !exists {
		t.Fatal("semantic internal tool did not expose searches")
	}

	capturePrompt := func(features CatalogSearchFeatures) string {
		t.Helper()
		program := &fakeProgram{output: map[string]ax.Value{"status": StatusAnswered, "answer": "done"}}
		options := []Option{
			WithClientFactory(func(Config) (ax.AIClient, error) { return fakeClient{}, nil }),
			WithProgramFactory(func(_ string, values map[string]ax.Value) Program {
				program.options = values
				program.onForward = func(p *fakeProgram) {
					callProgramTool(t, p, "query_catalog", map[string]ax.Value{"id": "help:discovery"})
				}
				return program
			}),
		}
		if features.enabled() {
			options = append(options, WithCatalogSearchFeatures(features))
		}
		runner := newAgent(Config{TimeoutSeconds: 5}, &fakeRuntime{}, options...)
		if _, err := runner.Run(context.Background(), Request{Instruction: "find customers"}); err != nil {
			t.Fatal(err)
		}
		runtime, ok := program.options["runtime"].(map[string]ax.Value)
		if !ok {
			t.Fatalf("runtime option = %T", program.options["runtime"])
		}
		prompt, _ := runtime["usageInstructions"].(string)
		return prompt
	}
	marker := "Semantic catalog recall is available"
	if strings.Contains(capturePrompt(CatalogSearchFeatures{}), marker) {
		t.Fatal("lexical-only runtime prompt contained semantic guidance")
	}
	if !strings.Contains(capturePrompt(features), marker) {
		t.Fatal("semantic guidance did not reach runtime.usageInstructions")
	}
}

func TestRuntimeSeedUsageInstructionsNameExactCodePathsAndShapes(t *testing.T) {
	for _, phrase := range []string{
		"inputs.context._graphjin_discovery",
		"do not require one saved query per requested entity",
		"const card = saved[0]",
		"never put them in Promise.then callbacks",
		"inputs.distilledContext does not exist",
		"result.cards[0]",
		"execute_saved_query({name: card.title})",
		"execution.data",
		"never make another catalog call",
		"identical repeated query_catalog request is rejected before execution",
		"returns recovery.execution instead",
		"globalThis.graphjinLastExecution",
		"graphjinLastExecution.result.data",
		"Never infer that business data is absent from catalog-card prose",
		// Dynamic-first identity: authoring is the primary path, saved
		// queries a governed shortcut, and aggregate questions re-author
		// past row-page saved results.
		"primary path is dynamic authoring",
		"re-author with aggregate fields",
		"current_date",
	} {
		if !strings.Contains(runtimeSeedUsageInstructions, phrase) {
			t.Fatalf("runtime seed guidance missing %q", phrase)
		}
	}
	if strings.Contains(runtimeSeedUsageInstructions, "The normal governed path is") {
		t.Fatal("runtime seed guidance regressed to saved-query-first choreography")
	}
}

func TestRuntimeUsageInstructionsCarryTheTruncationContract(t *testing.T) {
	// The runtime blob carries only the mechanical result-shape contract;
	// the domain teaching lives in the data_aggregation skill.
	for _, phrase := range []string{
		"result.truncation",
		"reached its row limit",
		"data_aggregation",
	} {
		if !strings.Contains(runtimeUsageInstructions, phrase) {
			t.Fatalf("runtime usage guidance missing %q", phrase)
		}
	}
}

func TestDataAggregationSkillTeachesEngineSideComputation(t *testing.T) {
	for _, phrase := range []string{
		"The model plans, the database computes",
		"products { count_id sum_price avg_price min_price max_price }",
		"products_aggregate { aggregate { count sum { price } avg { price } min { price } max { price } } }",
		"products_aggregate { count }",
		"copy its Supported form or Native equivalent",
		"do not repeat it or restart discovery",
		"max_<date_col>",
		"order_by aggregate desc",
		"inputs.current_date (UTC)",
		"state the window",
		"result.truncation",
	} {
		if !strings.Contains(dataAggregationInstruction, phrase) {
			t.Fatalf("data_aggregation skill missing %q", phrase)
		}
	}
	// Rankings are computed database-side: the compiler preserves GROUP BY
	// under aggregate order_by (core/internal/qcode
	// TestOrderByAggregateKeepsGrouping), so the skill steers models to
	// order_by + limit. Fetch-all-groups-and-compare is the client-side
	// arithmetic failure mode this skill exists to prevent.
	if strings.Contains(dataAggregationInstruction, "compare the complete grouped rows") {
		t.Fatal("data_aggregation skill regressed to client-side group comparison for rankings")
	}
}

func TestExecutorHandoffInstructionsRequireExplicitDiscovery(t *testing.T) {
	for _, phrase := range []string{
		"initial catalog seed is orientation only",
		"never satisfies the required model-driven discovery action",
		`query_catalog({kind:"saved_query", limit:10})`,
		"globalThis.graphjinDistilledContext",
		"globalThis.graphjinHistory",
		"most recent entries",
		"rejected until its JavaScript references graphjinHistory",
		"globalThis.graphjinExecutorRequest",
		"normalized user request",
		"runtime-only seed fallback",
		"Never treat a draft answer from the distiller as final evidence",
		"globalThis.graphjinLastExecution",
		"graphjinLastExecution.result.data",
	} {
		if !strings.Contains(executorHandoffInstructions, phrase) {
			t.Fatalf("executor handoff guidance missing %q", phrase)
		}
	}
}

func TestProtocolRejectsIdenticalCatalogRequestLoopsAndReplaysExecution(t *testing.T) {
	base := &fakeRuntime{}
	runtime := newProtocolRuntime(base, "find customers", "", 40, nil, nil, CatalogSearchFeatures{})
	args := map[string]any{"kind": "saved_query", "search": "daily planning", "limit": 10}

	if _, err := runtime.QueryCatalog(context.Background(), cloneMap(args)); err != nil {
		t.Fatalf("first query_catalog call: %v", err)
	}
	if _, err := runtime.QueryCatalog(context.Background(), cloneMap(args)); err == nil || !strings.Contains(err.Error(), "duplicate query_catalog call rejected") {
		t.Fatalf("duplicate error = %v", err)
	}
	if got := len(base.calls); got != 1 {
		t.Fatalf("runtime calls = %d, want one successful catalog dispatch", got)
	}

	execution := map[string]any{"data": map[string]any{"production_orders": []any{map[string]any{"product_name": "Northstar"}}}}
	runtime.state.recordExecution("execute_saved_query", map[string]any{"name": "daily_roast_context"}, execution)
	replayed, err := runtime.QueryCatalog(context.Background(), cloneMap(args))
	if err != nil {
		t.Fatalf("post-execution duplicate recovery: %v", err)
	}
	recovery := mapValue(mapValue(replayed)["recovery"])
	replayedExecution := mapValue(recovery["execution"])
	if got := mapValue(mapValue(replayedExecution["result"])["data"]); got == nil || got["production_orders"] == nil {
		t.Fatalf("post-execution recovery did not replay live data: %+v", replayed)
	}
	if got := len(base.calls); got != 1 {
		t.Fatalf("runtime calls after recovery = %d, want one catalog dispatch", got)
	}
}

func TestProtocolEscalatesConsecutiveEmptyCatalogSearches(t *testing.T) {
	base := &fakeRuntime{catalogOverride: func(map[string]any) any {
		return map[string]any{"count": 0, "cards": []any{}}
	}}
	runtime := newProtocolRuntime(base, "total usage", "", 40, nil, nil, CatalogSearchFeatures{})
	knownID := "table:app:main.usage_events"
	runtime.state.catalogIDs[knownID] = true

	first, err := runtime.QueryCatalog(context.Background(), map[string]any{"search": "usage total"})
	if err != nil {
		t.Fatalf("first empty search: %v", err)
	}
	firstResult := mapValue(first)
	firstRecovery := mapValue(firstResult["recovery"])
	if len(catalogCards(first)) != 0 || executionFailed(first) || stringFromMap(firstRecovery, "kind") != "empty_search" {
		t.Fatalf("first empty search = %+v, want cards:[] plus non-error recovery", first)
	}
	if got := len(base.calls); got != 1 {
		t.Fatalf("base calls after first empty search = %d, want 1", got)
	}
	firstSummary := runtime.state.actions[len(runtime.state.actions)-1].Summary
	if !containsString(evidenceStringSlice(firstSummary["recovery_codes"]), "empty_search") ||
		firstSummary["recovery_tool"] != toolQueryCatalog {
		t.Fatalf("first empty search summary = %+v", firstSummary)
	}

	second, err := runtime.QueryCatalog(context.Background(), map[string]any{"search": "quantity"})
	if err != nil {
		t.Fatalf("second empty search refusal: %v", err)
	}
	refusal, ok := second.(executeResult)
	if !ok || len(refusal.Errors) != 1 {
		t.Fatalf("second empty search = %#v, want structured refusal", second)
	}
	extensions := refusal.Errors[0].Extensions
	if stringFromMap(extensions, "code") != "empty_search_exhausted" || extensions["retryable"] != true {
		t.Fatalf("second empty search extensions = %+v", extensions)
	}
	repair := mapValue(extensions["graphjin_repair"])
	next := mapValue(repair["next"])
	if !containsString(stringSliceArg(next, "known_ids"), knownID) ||
		stringFromMap(next, "recommended_tool") != toolQueryCatalog ||
		stringFromMap(mapValue(next["args"]), "kind") != "table" {
		t.Fatalf("second empty search repair = %+v, want known id and table enumeration", repair)
	}
	if got := len(base.calls); got != 1 {
		t.Fatalf("second blind search reached base runtime: calls=%d, want 1", got)
	}
	summary := runtime.state.actions[len(runtime.state.actions)-1].Summary
	if !containsString(evidenceStringSlice(summary["error_codes"]), "empty_search_exhausted") ||
		!containsString(evidenceStringSlice(summary["recovery_codes"]), "empty_search_exhausted") {
		t.Fatalf("structured refusal summary = %+v", summary)
	}
}

func TestProtocolNonEmptyCatalogResultResetsEmptySearchStreak(t *testing.T) {
	base := &fakeRuntime{catalogOverride: func(args map[string]any) any {
		if stringArg(args, "kind") == "table" {
			return map[string]any{
				"count": 1,
				"cards": []any{map[string]any{
					"id": "table:app:main.usage_events", "kind": "table", "table_name": "usage_events",
				}},
			}
		}
		return map[string]any{"count": 0, "cards": []any{}}
	}}
	runtime := newProtocolRuntime(base, "total usage", "", 40, nil, nil, CatalogSearchFeatures{})

	if _, err := runtime.QueryCatalog(context.Background(), map[string]any{"search": "missing usage"}); err != nil {
		t.Fatal(err)
	}
	if runtime.state.emptySearchStreak != 1 {
		t.Fatalf("empty streak after first miss = %d, want 1", runtime.state.emptySearchStreak)
	}
	// The recovery-prescribed enumerate-by-kind call is not another blind
	// lexical search. A non-empty result resets the guard.
	if _, err := runtime.QueryCatalog(context.Background(), map[string]any{"kind": "table", "limit": 20}); err != nil {
		t.Fatal(err)
	}
	if runtime.state.emptySearchStreak != 0 {
		t.Fatalf("empty streak after non-empty enumeration = %d, want 0", runtime.state.emptySearchStreak)
	}
	later, err := runtime.QueryCatalog(context.Background(), map[string]any{"search": "still missing"})
	if err != nil {
		t.Fatal(err)
	}
	if executionFailed(later) || stringFromMap(mapValue(mapValue(later)["recovery"]), "kind") != "empty_search" {
		t.Fatalf("later empty search = %+v, want a fresh first-empty recovery", later)
	}
	if got := len(base.calls); got != 3 {
		t.Fatalf("base calls = %d, want all three non-refused requests dispatched", got)
	}
}

func TestProtocolEmptyCatalogDetailLookupDoesNotConsumeSearchGuard(t *testing.T) {
	base := &fakeRuntime{catalogOverride: func(map[string]any) any {
		return map[string]any{"count": 0, "cards": []any{}}
	}}
	runtime := newProtocolRuntime(base, "total usage", "", 40, nil, nil, CatalogSearchFeatures{})
	runtime.state.emptySearchStreak = 1

	out, err := runtime.QueryCatalog(context.Background(), map[string]any{"id": "table:app:main.missing"})
	if err != nil {
		t.Fatalf("empty detail lookup: %v", err)
	}
	recovery := mapValue(mapValue(out)["recovery"])
	if executionFailed(out) || stringFromMap(recovery, "kind") != "empty_detail" {
		t.Fatalf("empty detail lookup = %+v, want empty-detail recovery", out)
	}
	if got := len(base.calls); got != 1 {
		t.Fatalf("empty detail lookup base calls = %d, want 1", got)
	}
	if runtime.state.emptySearchStreak != 1 {
		t.Fatalf("empty detail lookup changed search streak to %d", runtime.state.emptySearchStreak)
	}
	if runtime.state.emptyDetailStreak != 1 {
		t.Fatalf("empty detail streak = %d, want 1", runtime.state.emptyDetailStreak)
	}
	if containsString(runtime.state.catalogDetails, "table:app:main.missing") {
		t.Fatalf("empty detail lookup became protocol evidence: %v", runtime.state.catalogDetails)
	}
}

func TestProtocolEscalatesConsecutiveUnknownCatalogDetails(t *testing.T) {
	base := &fakeRuntime{catalogOverride: func(map[string]any) any {
		return map[string]any{"count": 0, "cards": []any{}}
	}}
	runtime := newProtocolRuntime(base, "total usage", "", 40, nil, nil, CatalogSearchFeatures{})
	knownID := "table:app:main.usage_events"
	runtime.state.catalogIDs[knownID] = true

	firstID := "table:app:main.guessed_usage"
	first, err := runtime.QueryCatalog(context.Background(), map[string]any{"id": firstID})
	if err != nil {
		t.Fatalf("first empty detail: %v", err)
	}
	firstRecovery := mapValue(mapValue(first)["recovery"])
	if executionFailed(first) || stringFromMap(firstRecovery, "kind") != "empty_detail" ||
		stringFromMap(firstRecovery, "missed_id") != firstID ||
		!containsString(stringSliceArg(firstRecovery, "known_ids"), knownID) {
		t.Fatalf("first empty detail = %+v, want missed and known ids", first)
	}
	firstSummary := runtime.state.actions[len(runtime.state.actions)-1].Summary
	if !containsString(evidenceStringSlice(firstSummary["recovery_codes"]), "empty_detail") ||
		firstSummary["recovery_tool"] != toolQueryCatalog {
		t.Fatalf("first empty detail summary = %+v", firstSummary)
	}

	secondID := "table:app:main.guessed_totals"
	second, err := runtime.QueryCatalog(context.Background(), map[string]any{"id": secondID})
	if err != nil {
		t.Fatalf("second empty detail refusal: %v", err)
	}
	refusal, ok := second.(executeResult)
	if !ok || len(refusal.Errors) != 1 {
		t.Fatalf("second empty detail = %#v, want structured refusal", second)
	}
	extensions := refusal.Errors[0].Extensions
	if stringFromMap(extensions, "code") != "empty_detail_exhausted" || extensions["retryable"] != true {
		t.Fatalf("second empty detail extensions = %+v", extensions)
	}
	repair := mapValue(extensions["graphjin_repair"])
	if stringFromMap(repair, "missed_id") != secondID ||
		!containsString(stringSliceArg(repair, "known_ids"), knownID) {
		t.Fatalf("second empty detail repair = %+v, want missed and known ids", repair)
	}
	if got := len(base.calls); got != 1 {
		t.Fatalf("second unknown detail reached base runtime: calls=%d, want 1", got)
	}
	summary := runtime.state.actions[len(runtime.state.actions)-1].Summary
	if !containsString(evidenceStringSlice(summary["error_codes"]), "empty_detail_exhausted") ||
		!containsString(evidenceStringSlice(summary["recovery_codes"]), "empty_detail_exhausted") {
		t.Fatalf("structured refusal summary = %+v", summary)
	}
}

func TestProtocolPartiallyResolvedCatalogDetailBatchIsNotEmpty(t *testing.T) {
	resolvedID := "table:app:main.usage_events"
	missingID := "table:app:main.usage_totals"
	base := &fakeRuntime{catalogOverride: func(map[string]any) any {
		return map[string]any{
			"count": 1,
			"cards": []any{map[string]any{
				"id": resolvedID, "kind": "table", "table_name": "usage_events",
			}},
		}
	}}
	runtime := newProtocolRuntime(base, "total usage", "", 40, nil, nil, CatalogSearchFeatures{})
	runtime.state.emptyDetailStreak = 1
	runtime.state.catalogIDs[resolvedID] = true

	out, err := runtime.QueryCatalog(context.Background(), map[string]any{"ids": []any{resolvedID, missingID}})
	if err != nil {
		t.Fatal(err)
	}
	if executionFailed(out) || mapValue(mapValue(out)["recovery"]) != nil {
		t.Fatalf("partially resolved detail batch = %+v, want ordinary result", out)
	}
	if runtime.state.emptyDetailStreak != 0 {
		t.Fatalf("empty detail streak = %d, want reset", runtime.state.emptyDetailStreak)
	}
	if !containsString(runtime.state.catalogDetails, resolvedID) || containsString(runtime.state.catalogDetails, missingID) {
		t.Fatalf("detail evidence = %v, want only returned id", runtime.state.catalogDetails)
	}
	if got := len(base.calls); got != 1 {
		t.Fatalf("base calls = %d, want 1", got)
	}
}

func TestProtocolKnownCatalogDetailResolvesAfterEmptyGuess(t *testing.T) {
	knownID := "table:app:main.usage_events"
	base := &fakeRuntime{catalogOverride: func(args map[string]any) any {
		if stringArg(args, "id") == knownID {
			return map[string]any{
				"count": 1,
				"cards": []any{map[string]any{
					"id": knownID, "kind": "table", "table_name": "usage_events",
				}},
			}
		}
		return map[string]any{"count": 0, "cards": []any{}}
	}}
	runtime := newProtocolRuntime(base, "total usage", "", 40, nil, nil, CatalogSearchFeatures{})
	runtime.state.catalogIDs[knownID] = true

	if _, err := runtime.QueryCatalog(context.Background(), map[string]any{"id": "table:app:main.guess"}); err != nil {
		t.Fatal(err)
	}
	out, err := runtime.QueryCatalog(context.Background(), map[string]any{"id": knownID})
	if err != nil {
		t.Fatal(err)
	}
	if executionFailed(out) || len(catalogCards(out)) != 1 {
		t.Fatalf("known detail result = %+v, want resolved card", out)
	}
	if runtime.state.emptyDetailStreak != 0 {
		t.Fatalf("empty detail streak = %d, want reset", runtime.state.emptyDetailStreak)
	}
	if got := len(base.calls); got != 2 {
		t.Fatalf("base calls = %d, want both first miss and known detail", got)
	}
}

func TestProtocolEmptyDetailDoesNotAffectLexicalSearchGuard(t *testing.T) {
	base := &fakeRuntime{catalogOverride: func(map[string]any) any {
		return map[string]any{"count": 0, "cards": []any{}}
	}}
	runtime := newProtocolRuntime(base, "total usage", "", 40, nil, nil, CatalogSearchFeatures{})

	if _, err := runtime.QueryCatalog(context.Background(), map[string]any{"id": "table:app:main.guess"}); err != nil {
		t.Fatal(err)
	}
	out, err := runtime.QueryCatalog(context.Background(), map[string]any{"search": "usage totals"})
	if err != nil {
		t.Fatal(err)
	}
	if executionFailed(out) || stringFromMap(mapValue(mapValue(out)["recovery"]), "kind") != "empty_search" {
		t.Fatalf("first lexical miss after detail miss = %+v, want ordinary empty-search recovery", out)
	}
	if got := len(base.calls); got != 2 {
		t.Fatalf("lexical search was blocked by detail guard: calls=%d, want 2", got)
	}
}

func TestProtocolUnreturnedCatalogDetailIDsDoNotAuthorizeGuards(t *testing.T) {
	tests := []struct {
		name   string
		id     string
		seed   func(*discoveryState)
		assert func(*testing.T, *discoveryState)
	}{
		{
			name: "saved query",
			id:   "saved_query:usage_events_total",
			seed: func(state *discoveryState) { state.catalogKinds["saved_query"] = true },
			assert: func(t *testing.T, state *discoveryState) {
				if state.savedQueryDetailed("usage_events_total") {
					t.Fatal("unreturned saved-query id authorized execution")
				}
			},
		},
		{
			name: "table mutation shape",
			id:   "table:app:main.usage_events",
			assert: func(t *testing.T, state *discoveryState) {
				if state.tablesDetailed["usage_events"] || len(state.missingMutationEvidence([]string{"usage_events"})) == 0 {
					t.Fatal("unreturned table id authorized mutation-shape evidence")
				}
			},
		},
		{
			name: "workflow",
			id:   "workflow:daily_usage_rollup",
			assert: func(t *testing.T, state *discoveryState) {
				if state.workflowsDetailed["daily_usage_rollup"] || state.hasWorkflowDetailEvidence() {
					t.Fatal("unreturned workflow id authorized workflow execution")
				}
			},
		},
		{
			name: "security runtime",
			id:   "help:security",
			assert: func(t *testing.T, state *discoveryState) {
				if state.securityRuntimeEvidence {
					t.Fatal("unreturned security id authorized write-capable GraphQL")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := &fakeRuntime{catalogOverride: func(map[string]any) any {
				return map[string]any{"count": 0, "cards": []any{}}
			}}
			runtime := newProtocolRuntime(base, "inspect governed evidence", "", 40, nil, nil, CatalogSearchFeatures{})
			if tt.seed != nil {
				tt.seed(runtime.state)
			}
			if _, err := runtime.QueryCatalog(context.Background(), map[string]any{"id": tt.id}); err != nil {
				t.Fatal(err)
			}
			if containsString(runtime.state.catalogDetails, tt.id) {
				t.Fatalf("unreturned id became catalog detail evidence: %v", runtime.state.catalogDetails)
			}
			tt.assert(t, runtime.state)
		})
	}
}

func TestProtocolEmptySavedQueryDetailDoesNotAuthorizeExecution(t *testing.T) {
	base := &fakeRuntime{catalogOverride: func(map[string]any) any {
		return map[string]any{"count": 0, "cards": []any{}}
	}}
	runtime := newProtocolRuntime(base, "total usage", "", 40, nil, nil, CatalogSearchFeatures{})
	// Reproduce a broad seed that contained some unrelated saved query. That
	// must not make a guessed, empty detail id authoritative.
	runtime.state.catalogKinds["saved_query"] = true

	if _, err := runtime.QueryCatalog(context.Background(), map[string]any{"id": "saved_query:usage_events_total"}); err != nil {
		t.Fatal(err)
	}
	if runtime.state.savedQueryDetailed("usage_events_total") {
		t.Fatal("empty saved-query detail lookup authorized the guessed name")
	}
	before := len(base.calls)
	if _, err := runtime.ExecuteSavedQuery(context.Background(), map[string]any{"name": "usage_events_total"}); err == nil ||
		!strings.Contains(err.Error(), "inspect query_catalog") {
		t.Fatalf("guessed saved query execution error = %v", err)
	}
	if got := len(base.calls); got != before {
		t.Fatalf("guessed saved query reached base runtime: calls=%d want=%d", got, before)
	}
}

func TestProtocolSuccessfulExecutionResetsEmptyCatalogStreaks(t *testing.T) {
	base := &successfulExecutionRuntime{}
	runtime := newProtocolRuntime(base, "show invoice", "", 40, nil, nil, CatalogSearchFeatures{})
	runtime.state.seedOK = true
	runtime.state.modelDiscoveryAction = true
	runtime.state.catalogDetails = []string{"table:app:main.invoices"}
	runtime.state.emptySearchStreak = 1
	runtime.state.emptyDetailStreak = 1

	if _, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{
		"query": "query { invoices(limit: 1) { id status } }",
	}); err != nil {
		t.Fatal(err)
	}
	if runtime.state.emptySearchStreak != 0 {
		t.Fatalf("empty streak after successful execution = %d, want 0", runtime.state.emptySearchStreak)
	}
	if runtime.state.emptyDetailStreak != 0 {
		t.Fatalf("empty detail streak after successful execution = %d, want 0", runtime.state.emptyDetailStreak)
	}
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func TestSemanticCoverageProtocolValidationAndOneBatchLimit(t *testing.T) {
	features := CatalogSearchFeatures{SemanticRecall: true, CoverageBatch: true}
	newRuntime := func(enabled bool) (*protocolRuntime, *fakeRuntime) {
		base := &fakeRuntime{}
		profile := CatalogSearchFeatures{}
		if enabled {
			profile = features
		}
		return newProtocolRuntime(base, "find customers", "", 20, nil, nil, profile), base
	}
	seedRuntime, seedBase := newRuntime(true)
	if _, err := seedRuntime.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The seed's own search must stay one unexpanded lexical/semantic call;
	// coverage batching remains the model's explicit one-shot choice. No hidden
	// saved-query lookup may expand the seed.
	seedArgs := seedRuntime.state.actions[0].Args
	if stringArg(seedArgs, "search") != "find customers" || seedArgs["searches"] != nil {
		t.Fatalf("semantic seed changed or expanded automatically: args=%+v", seedArgs)
	}
	for _, args := range []map[string]any{seedBase.args} {
		if args["searches"] != nil {
			t.Fatalf("no automatic call may use coverage batching: %+v", args)
		}
	}

	for _, test := range []struct {
		name string
		args map[string]any
		want string
	}{
		{name: "too few", args: map[string]any{"searches": []any{"customers"}}, want: "two or three"},
		{name: "too many", args: map[string]any{"searches": []any{"a", "b", "c", "d"}}, want: "two or three"},
		{name: "empty", args: map[string]any{"searches": []any{"customers", "  "}}, want: "cannot be empty"},
		{name: "duplicate", args: map[string]any{"searches": []any{"Customers", " customers "}}, want: "unique"},
		{name: "non string", args: map[string]any{"searches": []any{"customers", 42}}, want: "must be strings"},
		{name: "too long", args: map[string]any{"searches": []any{"customers", strings.Repeat("x", MaxCatalogCoverageSearchBytes+1)}}, want: "at most"},
		{name: "with search", args: map[string]any{"searches": []any{"customers", "products"}, "search": "orders"}, want: "mutually exclusive"},
		{name: "with id", args: map[string]any{"searches": []any{"customers", "products"}, "id": "table:customers"}, want: "mutually exclusive"},
		{name: "with ids", args: map[string]any{"searches": []any{"customers", "products"}, "ids": []any{"table:customers"}}, want: "mutually exclusive"},
		{name: "with order", args: map[string]any{"searches": []any{"customers", "products"}, "order_by": map[string]any{"title": "asc"}}, want: "mutually exclusive"},
	} {
		t.Run(test.name, func(t *testing.T) {
			runtime, base := newRuntime(true)
			if _, err := runtime.QueryCatalog(context.Background(), test.args); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
			if len(base.calls) != 0 {
				t.Fatalf("invalid coverage reached runtime: %v", base.calls)
			}
		})
	}

	lexical, _ := newRuntime(false)
	if _, err := lexical.QueryCatalog(context.Background(), map[string]any{"searches": []any{"customers", "products"}}); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("lexical-only error = %v", err)
	}

	runtime, base := newRuntime(true)
	args := map[string]any{
		"searches": []any{" customers and products bought ", " customers ", "products purchased"},
		"where":    map[string]any{"database_name": map[string]any{"eq": "app"}},
		"limit":    20,
	}
	if _, err := runtime.QueryCatalog(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	if got, ok := base.args["explain"].(bool); !ok || !got {
		t.Fatalf("explain = %#v, want true", base.args["explain"])
	}
	searches := stringSliceArg(base.args, "searches")
	if len(searches) != 3 || searches[0] != "customers and products bought" {
		t.Fatalf("normalized searches = %#v", searches)
	}
	if _, err := runtime.QueryCatalog(context.Background(), map[string]any{"searches": []any{"orders", "products"}}); err == nil || !strings.Contains(err.Error(), "already used") {
		t.Fatalf("second coverage error = %v", err)
	}
	if len(base.calls) != 1 {
		t.Fatalf("runtime calls = %v, want one coverage dispatch", base.calls)
	}
}

type semanticAgentFlowRuntime struct {
	*fakeRuntime
	catalogArgs []map[string]any
}

func (r *semanticAgentFlowRuntime) QueryCatalog(ctx context.Context, args map[string]any) (any, error) {
	copyArgs, _ := normalizeValue(args).(map[string]any)
	r.catalogArgs = append(r.catalogArgs, copyArgs)
	return r.fakeRuntime.QueryCatalog(ctx, args)
}

func TestSemanticAgentInspectsCoveragePathBeforeExecution(t *testing.T) {
	base := &fakeRuntime{}
	base.catalogOverride = func(args map[string]any) any {
		if len(detailIDsFromArgs(args)) != 0 {
			return fakeCatalogResult(args)
		}
		if len(stringSliceArg(args, "searches")) != 0 {
			return map[string]any{
				"count": 3,
				"cards": []any{
					map[string]any{"id": "table:customers", "kind": "table", "table_name": "customers"},
					map[string]any{"id": "relationship:orders_products", "kind": "relationship", "summary": "real foreign-key path"},
					map[string]any{"id": "table:products", "kind": "table", "table_name": "products"},
				},
				"retrieval": map[string]any{"mode": "coverage_hybrid", "relationship_path_count": 1},
			}
		}
		return fakeCatalogResult(args)
	}
	runtime := &semanticAgentFlowRuntime{fakeRuntime: base}
	program := &fakeProgram{output: map[string]ax.Value{"status": StatusAnswered, "answer": "done"}}
	runner := newAgent(Config{TimeoutSeconds: 5}, runtime,
		WithCatalogSearchFeatures(CatalogSearchFeatures{SemanticRecall: true, CoverageBatch: true}),
		WithClientFactory(func(Config) (ax.AIClient, error) { return fakeClient{}, nil }),
		WithProgramFactory(func(_ string, options map[string]ax.Value) Program {
			program.options = options
			program.onForward = func(p *fakeProgram) {
				callProgramTool(t, p, "query_catalog", map[string]ax.Value{"searches": []any{
					"customers and products bought", "customer buyers", "products purchased",
				}})
				callProgramTool(t, p, "query_catalog", map[string]ax.Value{"ids": []any{
					"table:customers", "relationship:orders_products", "table:products",
				}})
				callProgramTool(t, p, "execute_graphql", map[string]ax.Value{
					"query": "query { customers { id products { id } } }",
				})
			}
			return program
		}),
	)
	response, err := runner.Run(context.Background(), Request{Instruction: "show customers and products they bought"})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != StatusAnswered {
		t.Fatalf("response = %+v", response)
	}
	// Seed, model coverage batch, and detail; the raw execution then proceeds
	// directly on that discovery evidence without a hidden supplement lookup.
	if len(runtime.catalogArgs) != 3 {
		t.Fatalf("catalog calls = %+v, want seed, coverage, detail", runtime.catalogArgs)
	}
	if len(stringSliceArg(runtime.catalogArgs[1], "searches")) != 3 {
		t.Fatalf("second catalog call was not coverage: %+v", runtime.catalogArgs[1])
	}
	wantDetails := []string{"table:customers", "relationship:orders_products", "table:products"}
	gotDetails := detailIDsFromArgs(runtime.catalogArgs[2])
	if strings.Join(gotDetails, "|") != strings.Join(wantDetails, "|") {
		t.Fatalf("detail inspection = %v, want returned endpoints and real path %v", gotDetails, wantDetails)
	}
	wantCalls := "query_catalog|query_catalog|query_catalog|execute_graphql"
	if got := strings.Join(base.calls, "|"); got != wantCalls {
		t.Fatalf("tool order = %s, want %s", got, wantCalls)
	}
}
