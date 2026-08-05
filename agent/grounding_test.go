package agent

import (
	"context"
	"strings"
	"testing"
)

type failingExecRuntime struct {
	*fakeRuntime
	execOut any
}

type sequencedExecRuntime struct {
	*fakeRuntime
	outputs []any
	calls   int
}

func (r *sequencedExecRuntime) ExecuteGraphQL(_ context.Context, args map[string]any) (any, error) {
	r.record(toolExecuteGraphQL, args)
	if r.calls >= len(r.outputs) {
		return nil, nil
	}
	out := r.outputs[r.calls]
	r.calls++
	return out, nil
}

func (r *failingExecRuntime) ExecuteGraphQL(_ context.Context, args map[string]any) (any, error) {
	r.record("execute_graphql", args)
	return r.execOut, nil
}

func newFailingExecProtocol(t *testing.T, execOut any) *protocolRuntime {
	t.Helper()
	base := &failingExecRuntime{fakeRuntime: &fakeRuntime{}, execOut: execOut}
	runtime := newProtocolRuntime(base, "roast plan coverage", "", 20, nil, nil, CatalogSearchFeatures{})
	if _, err := runtime.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.QueryCatalog(context.Background(), map[string]any{"id": "table:ops:public.green_lots"}); err != nil {
		t.Fatal(err)
	}
	return runtime
}

// Failed execution recovery must use evidence already discovered in the run;
// it must not perform hidden catalog calls or inject extra cards.
func TestFailedExecutionDoesNotPerformHiddenCatalogLookup(t *testing.T) {
	base := &failingExecRuntime{fakeRuntime: &fakeRuntime{}, execOut: executeResult{
		Errors: []ErrorInfo{{Message: "field 'available' is not a column or a function"}},
	}}
	runtime := newProtocolRuntime(base, "does the roast plan cover committed shipments", "", 20, nil, nil, CatalogSearchFeatures{})
	if _, err := runtime.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Table-only discovery: the model looked at the schema but chose to author
	// raw GraphQL anyway, which is the shape of the observed failure.
	if _, err := runtime.QueryCatalog(context.Background(), map[string]any{"id": "table:ops:public.green_lots"}); err != nil {
		t.Fatal(err)
	}
	out, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": "query { green_lots { available } }"})
	if err != nil {
		t.Fatal(err)
	}
	res, ok := out.(executeResult)
	if !ok {
		t.Fatalf("result type = %T", out)
	}
	recovery, ok := res.Recovery.(map[string]any)
	if !ok {
		t.Fatalf("recovery = %#v", res.Recovery)
	}
	next, ok := recovery["next"].(map[string]any)
	if !ok || next["recommended_tool"] != toolQueryCatalog {
		t.Fatalf("recovery.next = %#v, want structured query_catalog pointer", recovery["next"])
	}
	// The directive must reach errors[].message: a model that has decided the
	// run failed summarizes the message, not sibling guidance fields.
	if len(res.Errors) == 0 || !strings.Contains(res.Errors[0].Message, recoveryDirectivePrefix) {
		t.Fatalf("error message = %q, want embedded recovery directive", res.Errors[0].Message)
	}
	if !strings.Contains(res.Errors[0].Message, "field 'available' is not a column") {
		t.Fatalf("error message = %q, want the original compiler text preserved", res.Errors[0].Message)
	}
	recoveryActions := 0
	for _, action := range runtime.state.actions {
		if action.Source == "recovery" && action.Tool == "query_catalog" {
			recoveryActions++
		}
	}
	if recoveryActions != 0 {
		t.Fatalf("recovery-sourced catalog actions = %d, want none", recoveryActions)
	}
	// It must run at most once per run, even across repeated failures.
	before := len(base.calls)
	if _, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": "query { green_lots { alsoBad } }"}); err != nil {
		t.Fatal(err)
	}
	if got := len(base.calls) - before; got != 1 {
		t.Fatalf("second failure issued %d extra calls, want 1 (execute only)", got)
	}
}

// Dynamic authoring is the agent's primary path: a raw query whose root fields
// happen to overlap an approved saved query must still execute — the dynamic
// version can carry filters (today's orders, queued status) the fixed saved
// query cannot express. Saved queries are shortcuts, never gates.
func TestRawGraphQLCoveredBySavedQueryStillExecutes(t *testing.T) {
	base := &fakeRuntime{}
	runtime := newProtocolRuntime(base, "does the roast plan cover committed shipments", "", 20, nil, nil, CatalogSearchFeatures{})
	if _, err := runtime.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.QueryCatalog(context.Background(), map[string]any{"id": "table:ops:public.production_orders"}); err != nil {
		t.Fatal(err)
	}
	raw := "query { production_orders(where: { status: { eq: \"queued\" } }) { id } roast_schedule { id } }"
	if _, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": raw}); err != nil {
		t.Fatalf("dynamic query overlapping a saved query must execute: %v", err)
	}
	executed := false
	for _, call := range base.calls {
		if call == "execute_graphql" {
			executed = true
		}
	}
	if !executed {
		t.Fatal("dynamic query must reach the runtime")
	}
	for _, code := range ProtocolViolationCodes(runtime.state.finalize(Response{Status: StatusAnswered, Answer: "queued orders listed"})) {
		if code == "saved_query_preferred" {
			t.Fatal("overlap with a saved query must not record a violation")
		}
	}
}

// Authoring raw GraphQL straight off the seed is what produced invented field
// names and a silently wrong result set. It must fail while the run can still
// recover, not at finalize after the work is wasted.
func TestRawGraphQLRejectedBeforeModelDiscovery(t *testing.T) {
	base := &fakeRuntime{}
	runtime := newProtocolRuntime(base, "does the roast plan cover committed shipments", "", 20, nil, nil, CatalogSearchFeatures{})
	if _, err := runtime.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": "query { green_lots { available } }"})
	if err == nil {
		t.Fatal("raw GraphQL off the bare seed must be rejected")
	}
	if !strings.Contains(err.Error(), "not discovery") {
		t.Fatalf("error = %q, want the seed-is-not-discovery rejection", err)
	}
	if len(base.calls) != 1 {
		t.Fatalf("catalog calls = %v, want only the seed", base.calls)
	}
	for _, call := range base.calls {
		if call == "execute_graphql" {
			t.Fatal("rejected raw GraphQL must never reach the runtime")
		}
	}
	codes := ProtocolViolationCodes(runtime.state.finalize(Response{Status: StatusAnswered, Answer: "done"}))
	found := false
	for _, code := range codes {
		if code == "raw_graphql_discovery_required" {
			found = true
		}
	}
	if !found {
		t.Fatalf("violation codes = %v, want raw_graphql_discovery_required", codes)
	}
}

func TestRawGraphQLRejectedAfterBroadCatalogListing(t *testing.T) {
	base := &fakeRuntime{}
	runtime := newProtocolRuntime(base, "inspect roast quality", "", 20, nil, nil, CatalogSearchFeatures{})
	if _, err := runtime.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.QueryCatalog(context.Background(), map[string]any{
		"database": "roast_warehouse",
		"kind":     "column",
		"table":    "roast_batches",
	}); err != nil {
		t.Fatal(err)
	}
	_, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": "query { roast_batches { id } }"})
	if err == nil || !strings.Contains(err.Error(), "broad catalog results are not discovery detail") {
		t.Fatalf("error = %v, want exact-detail discovery rejection", err)
	}
	for _, call := range base.calls {
		if call == "execute_graphql" {
			t.Fatal("raw GraphQL after only a broad listing must never reach the runtime")
		}
	}
}

// The guard must not obstruct the normal governed path: discover, then query.
func TestRawGraphQLAllowedAfterModelDiscovery(t *testing.T) {
	base := &fakeRuntime{}
	runtime := newProtocolRuntime(base, "roast plan", "", 20, nil, nil, CatalogSearchFeatures{})
	if _, err := runtime.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.QueryCatalog(context.Background(), map[string]any{"id": "table:ops:public.green_lots"}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": "query { green_lots { remaining_kg } }"}); err != nil {
		t.Fatalf("raw GraphQL after discovery must be allowed: %v", err)
	}
}

// The seed stays one unexpanded catalog call with no hidden supplement.
func TestSeedSearchItselfIsASingleUnexpandedCall(t *testing.T) {
	base := &fakeRuntime{}
	runtime := newProtocolRuntime(base, "does the roast plan cover committed shipments", "", 20, nil, nil, CatalogSearchFeatures{})
	if _, err := runtime.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	seedArgs := runtime.state.actions[0].Args
	if stringArg(seedArgs, "search") != "does the roast plan cover committed shipments" {
		t.Fatalf("seed args = %+v, want the verbatim instruction search", seedArgs)
	}
	if seedArgs["searches"] != nil {
		t.Fatalf("seed must never auto-expand into a coverage batch: %+v", seedArgs)
	}
}

func TestExecuteGraphQLErrorAttachesCompactRecovery(t *testing.T) {
	runtime := newFailingExecProtocol(t, executeResult{
		Errors: []ErrorInfo{{
			Message:    "no db column found for field 'available' on table 'green_lots'",
			Extensions: map[string]any{"graphjin_repair": map[string]any{"kind": "field_not_on_table"}},
		}},
	})
	out, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": "query { green_lots { available } }"})
	if err != nil {
		t.Fatal(err)
	}
	res, ok := out.(executeResult)
	if !ok {
		t.Fatalf("result type = %T", out)
	}
	recovery, ok := res.Recovery.(map[string]any)
	if !ok {
		t.Fatalf("recovery = %#v", res.Recovery)
	}
	instruction, _ := recovery["instruction"].(string)
	if !strings.Contains(instruction, "do not report it as broken or propose schema changes") {
		t.Fatalf("recovery instruction = %q", instruction)
	}
	next, ok := recovery["next"].(map[string]any)
	if !ok || next["recommended_tool"] != toolQueryCatalog {
		t.Fatalf("recovery.next = %#v, want structured query_catalog pointer", recovery["next"])
	}
	if _, exists := recovery["approved_saved_queries"]; exists {
		t.Fatalf("recovery should not inject saved-query cards: %#v", recovery)
	}
}

func TestNonAggregateUnknownFieldKeepsGenericRecovery(t *testing.T) {
	base := &failingExecRuntime{fakeRuntime: &fakeRuntime{}, execOut: executeResult{
		Errors: []ErrorInfo{{
			Message:    "unknown field display_label",
			Extensions: map[string]any{"compiler_stage": "qcode"},
		}},
	}}
	runtime := newProtocolRuntime(base, "Show the account display label", "", 20, nil, nil, CatalogSearchFeatures{})
	runtime.state.seedOK = true
	runtime.state.modelDiscoveryAction = true
	runtime.state.catalogDetails = []string{"table:app:main.accounts"}
	out, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": "query { accounts { display_label } }"})
	if err != nil {
		t.Fatal(err)
	}
	res := out.(executeResult)
	if len(res.Errors) != 1 {
		t.Fatalf("errors = %+v", res.Errors)
	}
	if repair := mapValue(res.Errors[0].Extensions["graphjin_repair"]); repair != nil {
		t.Fatalf("non-aggregate failure received graphjin_repair: %+v", repair)
	}
	recovery := mapValue(res.Recovery)
	if next := mapValue(recovery["next"]); next["recommended_tool"] != toolQueryCatalog {
		t.Fatalf("recovery = %+v, want generic query_catalog path", recovery)
	}
}

func TestFailedQueryRequiresDistinctRepairAndRejectsDuplicate(t *testing.T) {
	base := &failingExecRuntime{fakeRuntime: &fakeRuntime{}, execOut: executeResult{
		Errors: []ErrorInfo{{Message: "field not found"}},
	}}
	runtime := newProtocolRuntime(base, "How many accounts are active?", "", 20, nil, nil, CatalogSearchFeatures{})
	if _, err := runtime.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.QueryCatalog(context.Background(), map[string]any{"id": "table:public.accounts"}); err != nil {
		t.Fatal(err)
	}
	failed := "query { accounts { wrong_count } }"
	if _, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": failed}); err != nil {
		t.Fatal(err)
	}
	if message := runtime.state.pendingRequiredFinalization(); !strings.HasPrefix(message, "execution_repair_required:") {
		t.Fatalf("pending final = %q", message)
	}
	before := len(base.calls)
	out, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": failed})
	if err != nil {
		t.Fatal(err)
	}
	if len(base.calls) != before {
		t.Fatal("identical failed query reached the database runtime")
	}
	duplicate := out.(executeResult)
	if got := stringFromMap(duplicate.Errors[0].Extensions, "code"); got != "duplicate_failed_query" {
		t.Fatalf("duplicate code = %q", got)
	}
	if _, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": "query { accounts { count_id } }"}); err != nil {
		t.Fatal(err)
	}
	if runtime.state.pendingFailedQueryKey != "" || len(runtime.state.failedQueryKeys) != 2 {
		t.Fatalf("repair state = pending:%q failed:%v", runtime.state.pendingFailedQueryKey, runtime.state.failedQueryKeys)
	}
	distinctFailed := "query { accounts { count_id } }"
	before = len(base.calls)
	out, err = runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": distinctFailed})
	if err != nil {
		t.Fatal(err)
	}
	if len(base.calls) != before || stringFromMap(out.(executeResult).Errors[0].Extensions, "code") != "duplicate_failed_query" {
		t.Fatalf("distinct failed query was re-executed: calls=%d result=%+v", len(base.calls), out)
	}
	newFailure := "query { accounts { max_id } }"
	if _, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": newFailure}); err != nil {
		t.Fatal(err)
	}
	if runtime.state.pendingFailedQueryKey != executionQueryKey(map[string]any{"query": newFailure}) || len(runtime.state.failedQueryKeys) != 3 {
		t.Fatalf("new failed identity did not re-arm repair: pending=%q failed=%v", runtime.state.pendingFailedQueryKey, runtime.state.failedQueryKeys)
	}
}

func TestDatabaseComputationFinalGuard(t *testing.T) {
	state := newDiscoveryState("How many accounts are active?")
	state.recordExecution(toolExecuteGraphQL, map[string]any{"query": "query { accounts { id status } }"}, executeResult{
		Data: map[string]any{"accounts": []any{map[string]any{"id": 1}}},
	})
	if message := state.pendingDatabaseComputation(); !strings.HasPrefix(message, "database_computation_required:") {
		t.Fatalf("row-list final guard = %q", message)
	}
	state.recordExecution(toolExecuteGraphQL, map[string]any{"query": "query { accounts { count_id } }"}, executeResult{
		Data: map[string]any{"accounts": []any{map[string]any{"count_id": 12}}},
	})
	if message := state.pendingDatabaseComputation(); message != "" {
		t.Fatalf("aggregate result remained blocked: %q", message)
	}

	compat := newDiscoveryState("How many accounts are active?")
	compat.recordExecution(toolExecuteGraphQL, map[string]any{"query": "query { accounts_aggregate { aggregate { count } } }"}, executeResult{
		Data: map[string]any{"accounts_aggregate": map[string]any{"aggregate": map[string]any{"count": 12}}},
	})
	if message := compat.pendingDatabaseComputation(); message != "" {
		t.Fatalf("Hasura-compatible aggregate result remained blocked: %q", message)
	}

	ranking := newDiscoveryState("Which plan contributes the most total revenue?")
	ranking.recordExecution(toolExecuteGraphQL, map[string]any{"query": "query { plans { name sum_revenue } }"}, executeResult{
		Data: map[string]any{"plans": []any{map[string]any{"name": "pro", "sum_revenue": 10}}},
	})
	if message := ranking.pendingDatabaseComputation(); !strings.Contains(message, "aggregate order_by") {
		t.Fatalf("ranking guard = %q", message)
	}
	ranking.recordExecution(toolExecuteGraphQL, map[string]any{"query": "query { plans(order_by: {sum_revenue: desc}, limit: 1) { name sum_revenue } }"}, executeResult{
		Data: map[string]any{"plans": []any{map[string]any{"name": "pro", "sum_revenue": 10}}},
	})
	if message := ranking.pendingDatabaseComputation(); message != "" {
		t.Fatalf("database-ranked result remained blocked: %q", message)
	}

	recordRanking := newDiscoveryState("Which record in invoices has the highest amount cents, and what is the value?")
	recordRanking.seedOK = true
	recordRanking.modelDiscoveryAction = true
	rankingArgs := map[string]any{"query": "query { invoices(order_by: { amount_cents: desc }, limit: 1) { id amount_cents } }"}
	rankingResult := executeResult{Data: map[string]any{"invoices": []any{map[string]any{"id": 7, "amount_cents": 9900}}}}
	action := recordRanking.startAction("model", toolExecuteGraphQL, rankingArgs)
	recordRanking.finishAction(action, toolExecuteGraphQL, rankingArgs, rankingResult, nil)
	recordRanking.recordExecution(toolExecuteGraphQL, rankingArgs, rankingResult)
	if message := recordRanking.pendingDatabaseComputation(); message != "" {
		t.Fatalf("order_by+limit record ranking remained blocked: %q", message)
	}
	if resp := recordRanking.finalize(Response{Status: StatusAnswered, Answer: "Invoice 7 has the highest amount cents: 9900."}); resp.Status != StatusAnswered {
		t.Fatalf("order_by+limit ranking did not finalize: %+v", resp)
	}

	wrongRankedColumn := newDiscoveryState("Which record in invoices has the highest amount cents, and what is the value?")
	wrongRankedColumn.recordExecution(toolExecuteGraphQL, map[string]any{"query": "query { invoices(order_by: { id: desc }, limit: 1) { id amount_cents } }"}, rankingResult)
	if message := wrongRankedColumn.pendingDatabaseComputation(); !strings.HasPrefix(message, "database_computation_required:") {
		t.Fatalf("unrelated order_by column bypassed ranking guard: %q", message)
	}

	countWithLimit := newDiscoveryState("How many invoices are there?")
	countWithLimit.recordExecution(toolExecuteGraphQL, rankingArgs, rankingResult)
	if message := countWithLimit.pendingDatabaseComputation(); !strings.HasPrefix(message, "database_computation_required:") {
		t.Fatalf("order_by+limit bypassed strict count guard: %q", message)
	}

	savedRows := newDiscoveryState("How many accounts are active?")
	savedRows.recordExecution(toolExecuteSavedQuery, map[string]any{"name": "account_rows"}, executeResult{
		Data: map[string]any{"accounts": []any{map[string]any{"id": 1}, map[string]any{"id": 2}}},
	})
	if message := savedRows.pendingDatabaseComputation(); !strings.HasPrefix(message, "database_computation_required:") {
		t.Fatalf("row-returning saved query bypassed computation guard: %q", message)
	}
	savedAggregate := newDiscoveryState("What is the total monthly recurring revenue?")
	savedAggregate.savedQueryGraphQL["mrr_summary"] = "query { accounts { total: sum_mrr } }"
	savedAggregate.recordExecution(toolExecuteSavedQuery, map[string]any{"name": "mrr_summary"}, executeResult{
		Data: map[string]any{"accounts": []any{map[string]any{"total": 42}}},
	})
	if message := savedAggregate.pendingDatabaseComputation(); message != "" {
		t.Fatalf("aggregate saved query remained blocked: %q", message)
	}
}

func TestResultContainsHasuraCompatibleAggregate(t *testing.T) {
	value := map[string]any{
		"accounts_aggregate": map[string]any{
			"aggregate": map[string]any{"count": 12, "max": map[string]any{"renewed_at": "2027-02-19"}},
		},
	}
	if !resultContainsAggregateField(value) {
		t.Fatal("Hasura-compatible aggregate result was not recognized")
	}
}

func TestCachedExecutionRecoveryKeepsPendingRequirement(t *testing.T) {
	base := &successfulExecutionRuntime{}
	runtime := newProtocolRuntime(base, "How many invoices are there?", "", 8, nil, nil, CatalogSearchFeatures{})
	runtime.state.seedOK = true
	runtime.state.modelDiscoveryAction = true
	runtime.state.catalogDetails = []string{"table:main:invoices"}
	args := map[string]any{"query": "query { invoices { id } }"}
	if _, err := runtime.ExecuteGraphQL(context.Background(), cloneAnyMap(args)); err != nil {
		t.Fatal(err)
	}
	duplicate, err := runtime.ExecuteGraphQL(context.Background(), cloneAnyMap(args))
	if err != nil || base.graphqlCalls != 1 {
		t.Fatalf("cached execution = %+v calls=%d err=%v", duplicate, base.graphqlCalls, err)
	}
	result, ok := duplicate.(executeResult)
	if !ok || len(result.Errors) != 1 {
		t.Fatalf("cached rejection = %#v, want one structured error", duplicate)
	}
	if result.Data != nil || mapValue(duplicate)["data"] != nil {
		t.Fatalf("cached rejection exposed data: %#v", duplicate)
	}
	errorInfo := result.Errors[0]
	if got := stringFromMap(errorInfo.Extensions, "code"); got != "database_computation_required" {
		t.Fatalf("cached error code = %q, want database_computation_required", got)
	}
	repair := mapValue(errorInfo.Extensions["graphjin_repair"])
	next := stringFromMap(repair, "next")
	if strings.Contains(next, "Call final now") ||
		!strings.Contains(next, "max_<column>") ||
		!strings.Contains(next, "do not calculate from fetched rows") {
		t.Fatalf("cached repair guidance = %q", next)
	}
	if got := stringFromMap(repair, "kind"); got != "distinct_aggregate_required" {
		t.Fatalf("cached repair kind = %q", got)
	}
	summary := runtime.state.actions[len(runtime.state.actions)-1].Summary
	if summary["has_data"] == true || !containsString(evidenceStringSlice(summary["error_codes"]), "database_computation_required") ||
		!containsString(evidenceStringSlice(summary["recovery_codes"]), "distinct_aggregate_required") {
		t.Fatalf("cached rejection summary = %#v", summary)
	}
	if runtime.state.completionLatchKey != "" || runtime.state.completionReady {
		t.Fatalf("pending requirement armed completion latch: %+v", runtime.state)
	}
}

func TestCachedSavedQueryWithPendingRequirementWithholdsRows(t *testing.T) {
	base := &successfulExecutionRuntime{}
	runtime := newProtocolRuntime(base, "How many invoices are there?", "", 8, nil, nil, CatalogSearchFeatures{})
	runtime.state.seedOK = true
	runtime.state.modelDiscoveryAction = true
	runtime.state.markSavedQueryDetailed("invoice_snapshot")
	args := map[string]any{"name": "invoice_snapshot"}
	if _, err := runtime.ExecuteSavedQuery(context.Background(), cloneAnyMap(args)); err != nil {
		t.Fatal(err)
	}
	duplicate, err := runtime.ExecuteSavedQuery(context.Background(), cloneAnyMap(args))
	if err != nil || base.savedCalls != 1 {
		t.Fatalf("cached saved query = %+v calls=%d err=%v", duplicate, base.savedCalls, err)
	}
	result, ok := duplicate.(executeResult)
	if !ok || result.Data != nil || len(result.Errors) != 1 {
		t.Fatalf("cached saved-query rejection = %#v", duplicate)
	}
	if got := stringFromMap(result.Errors[0].Extensions, "code"); got != "database_computation_required" {
		t.Fatalf("cached saved-query error code = %q", got)
	}
	repair := mapValue(result.Errors[0].Extensions["graphjin_repair"])
	if next := stringFromMap(repair, "next"); !strings.Contains(next, "count_") || !strings.Contains(next, "do not calculate from fetched rows") {
		t.Fatalf("cached saved-query repair guidance = %q", next)
	}
}

func TestCachedExecutionBecomesCurrentCompletionEvidence(t *testing.T) {
	base := &sequencedExecRuntime{
		fakeRuntime: &fakeRuntime{},
		outputs: []any{
			map[string]any{"data": map[string]any{"invoices": []any{map[string]any{"id": "A"}}}},
			map[string]any{"data": map[string]any{"invoices": []any{map[string]any{"id": "B"}}}},
		},
	}
	runtime := newProtocolRuntime(base, "Show invoice A", "", 8, nil, nil, CatalogSearchFeatures{})
	runtime.state.seedOK = true
	runtime.state.modelDiscoveryAction = true
	runtime.state.catalogDetails = []string{"table:main:invoices"}
	queryA := map[string]any{"query": `query { invoices(where: {id: {eq: "A"}}) { id } }`}
	queryB := map[string]any{"query": `query { invoices(where: {id: {eq: "B"}}) { id } }`}
	if _, err := runtime.ExecuteGraphQL(context.Background(), cloneAnyMap(queryA)); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.ExecuteGraphQL(context.Background(), cloneAnyMap(queryB)); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.ExecuteGraphQL(context.Background(), cloneAnyMap(queryA)); err != nil {
		t.Fatal(err)
	}
	if base.calls != 2 {
		t.Fatalf("cached query reached database: calls=%d", base.calls)
	}
	data, ok := runtime.state.lastExecutionData()
	rows := anySlice(mapValue(data)["invoices"])
	if !ok || len(rows) != 1 || stringFromMap(mapValue(rows[0]), "id") != "A" {
		t.Fatalf("cached completion evidence = %+v, want query A", data)
	}
}

func TestFinalizeClearsResolvedDiscoveryRefusalAfterTerminalExecution(t *testing.T) {
	base := &successfulExecutionRuntime{}
	runtime := newProtocolRuntime(base, "Show invoice INV-1", "", 8, nil, nil, CatalogSearchFeatures{})
	runtime.state.seedOK = true
	runtime.state.modelDiscoveryAction = true
	runtime.state.catalogDetails = []string{"table:main:invoices"}
	runtime.state.addViolation("raw_graphql_discovery_required", "inspect the relevant catalog detail first", toolExecuteGraphQL, true, nil)
	if _, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": "query { invoices(limit: 1) { id status } }"}); err != nil {
		t.Fatal(err)
	}
	resp := runtime.state.finalize(Response{
		Status: StatusBlocked,
		Answer: "Invoice INV-1 is paid.",
		Refusal: &Refusal{
			Code:          "raw_graphql_discovery_required",
			BlockedAction: toolExecuteGraphQL,
			Because:       []string{"inspect the relevant catalog detail first"},
			Retryable:     true,
		},
		Errors: []ErrorInfo{{Message: "inspect the relevant catalog detail first", Extensions: map[string]any{"code": "raw_graphql_discovery_required"}}},
	})
	if resp.Status != StatusAnswered || resp.Refusal != nil || len(resp.Errors) != 0 {
		t.Fatalf("resolved terminal execution remained blocked: %+v", resp)
	}
	violations := anySlice(mapValue(resp.Evidence)["violations"])
	if len(violations) != 1 || mapValue(violations[0])["blocking"] != false || mapValue(mapValue(violations[0])["details"])["resolved"] != true {
		t.Fatalf("resolved discovery violation evidence = %+v", violations)
	}
}

func TestExecuteGraphQLSuccessAttachesNoRecovery(t *testing.T) {
	runtime := newFailingExecProtocol(t, executeResult{
		Data: map[string]any{"green_lots": []any{map[string]any{"remaining_kg": 720}}},
	})
	out, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": "query { green_lots { remaining_kg } }"})
	if err != nil {
		t.Fatal(err)
	}
	res, ok := out.(executeResult)
	if !ok {
		t.Fatalf("result type = %T", out)
	}
	if res.Recovery != nil {
		t.Fatalf("recovery = %#v, want nil", res.Recovery)
	}
}

func TestExecuteGraphQLErrorAttachesRecoveryToMapResults(t *testing.T) {
	runtime := newFailingExecProtocol(t, map[string]any{
		"errors": []any{map[string]any{"message": "column not found"}},
	})
	out, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": "query { green_lots { available } }"})
	if err != nil {
		t.Fatal(err)
	}
	res, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T", out)
	}
	if _, ok := res["recovery"].(map[string]any); !ok {
		t.Fatalf("recovery = %#v", res["recovery"])
	}
}

func groundedAnswerState(t *testing.T) *discoveryState {
	t.Helper()
	state := newDiscoveryState("does the roast plan cover committed shipments")
	state.seedOK = true
	state.modelDiscoveryAction = true
	args := map[string]any{"name": "daily_roast_context"}
	index := state.startAction("model", "execute_saved_query", args)
	state.finishAction(index, "execute_saved_query", args, map[string]any{
		"data": map[string]any{
			"production_orders": []any{map[string]any{"quantity_bags": 240, "requested_ship_date": "2026-07-28"}},
			"roast_schedule":    []any{map[string]any{"target_output_kg": 132}},
		},
	}, nil)
	return state
}

func TestFinalizeBlocksUngroundedAnswerFields(t *testing.T) {
	state := groundedAnswerState(t)
	resp := state.finalize(Response{
		Status: StatusAnswered,
		Answer: "The evidence explicitly states coversCommittedShipments is false.",
	})
	if resp.Status != StatusBlocked {
		t.Fatalf("status = %s, want %s", resp.Status, StatusBlocked)
	}
	codes := ProtocolViolationCodes(resp)
	found := false
	for _, code := range codes {
		if code == "ungrounded_answer_fields" {
			found = true
		}
	}
	if !found {
		t.Fatalf("violation codes = %v, want ungrounded_answer_fields", codes)
	}
	if resp.Refusal == nil {
		t.Fatal("blocked ungrounded answer must carry a refusal")
	}
}

func TestFinalizeAllowsGroundedAnswerFields(t *testing.T) {
	state := groundedAnswerState(t)
	resp := state.finalize(Response{
		Status: StatusAnswered,
		Answer: "Orders total 240 quantity_bags with requested_ship_date 2026-07-28; scheduled target_output_kg is 132, so the roast plan covers committed shipments. Next: confirm packaging labor.",
	})
	if resp.Status != StatusAnswered {
		t.Fatalf("status = %s, want %s (violations: %v)", resp.Status, StatusAnswered, ProtocolViolationCodes(resp))
	}
}

func TestFinalizeAllowsProtocolVocabularyAndInstructionTokens(t *testing.T) {
	state := newDiscoveryState("audit the coversCommittedShipments flag semantics")
	state.seedOK = true
	state.modelDiscoveryAction = true
	resp := state.finalize(Response{
		Status: StatusAnswered,
		Answer: "The coversCommittedShipments wording comes from your request; per gj_catalog discovery, use an approved saved_query via execute_saved_query.",
	})
	if resp.Status != StatusAnswered {
		t.Fatalf("status = %s, want %s (violations: %v)", resp.Status, StatusAnswered, ProtocolViolationCodes(resp))
	}
}

func TestFinalizeSkipsGroundingForNonAnsweredStatus(t *testing.T) {
	state := groundedAnswerState(t)
	resp := state.finalize(Response{
		Status: StatusBlocked,
		Answer: "Blocked: madeUpFieldName is unavailable to this role.",
	})
	for _, code := range ProtocolViolationCodes(resp) {
		if code == "ungrounded_answer_fields" {
			t.Fatal("grounding must not judge non-answered responses")
		}
	}
}

func TestUngroundedAnswerTokenExtraction(t *testing.T) {
	state := newDiscoveryState("plan question")
	state.addGrounding(map[string]any{"data": map[string]any{"remaining_kg": 720}})
	tokens := state.ungroundedAnswerTokens("remaining_kg is observed, planStatus is invented, a_b is short, and gj_catalog is vocabulary.")
	if len(tokens) != 1 || tokens[0] != "planStatus" {
		t.Fatalf("tokens = %v, want [planStatus]", tokens)
	}
}

func TestGroundingCorpusOverflowDisablesCheck(t *testing.T) {
	state := newDiscoveryState("plan question")
	state.addGrounding(strings.Repeat("x", maxGroundingCorpusBytes+1))
	if !state.groundingOverflow {
		t.Fatal("oversized grounding write must trip the overflow guard")
	}
	if tokens := state.ungroundedAnswerTokens("inventedFieldName everywhere"); tokens != nil {
		t.Fatalf("tokens = %v, want nil after overflow", tokens)
	}
}

func TestResultSummaryCarriesRecoveryCodesWithoutMessages(t *testing.T) {
	summary := resultSummary(toolExecuteGraphQL, nil, map[string]any{
		"errors": []any{map[string]any{
			"message": "private compiler detail",
			"extensions": map[string]any{
				"code": "field_not_on_table",
				"graphjin_repair": map[string]any{
					"kind": "field_not_on_table",
					"next": map[string]any{"tool": toolGraphQLHelp},
				},
			},
		}},
		"recovery": map[string]any{
			"next": map[string]any{"tool": toolGraphQLHelp},
		},
	})
	if got := summary["error_codes"]; stringify(got) != `["field_not_on_table"]` {
		t.Fatalf("error codes = %v", got)
	}
	if got := summary["recovery_codes"]; stringify(got) != `["field_not_on_table"]` {
		t.Fatalf("recovery codes = %v", got)
	}
	if got := summary["recovery_tool"]; got != toolGraphQLHelp {
		t.Fatalf("recovery tool = %v, want %s", got, toolGraphQLHelp)
	}
	if strings.Contains(stringify(summary), "private compiler detail") {
		t.Fatalf("action summary leaked error prose: %v", summary)
	}
}
