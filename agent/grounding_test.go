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
