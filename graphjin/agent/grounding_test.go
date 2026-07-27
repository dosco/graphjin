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
	if _, err := runtime.QueryCatalog(context.Background(), map[string]any{"kind": "saved_query"}); err != nil {
		t.Fatal(err)
	}
	return runtime
}

// The failure that motivated this guard: a seed that surfaces no saved query,
// so the model authors raw GraphQL against an invented field. GraphJin must
// perform the skipped approved-path discovery itself and name real candidates.
func TestFailedExecutionDiscoversApprovedSavedQueriesNotSeeded(t *testing.T) {
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
	names, _ := recovery["approved_saved_queries"].([]string)
	if len(names) == 0 {
		t.Fatal("failed execution must name approved saved queries discovered on the agent's behalf")
	}
	// The directive must reach errors[].message: a model that has decided the
	// run failed summarizes the message, not sibling guidance fields.
	if len(res.Errors) == 0 || !strings.Contains(res.Errors[0].Message, recoveryDirectivePrefix) {
		t.Fatalf("error message = %q, want embedded recovery directive", res.Errors[0].Message)
	}
	if !strings.Contains(res.Errors[0].Message, "field 'available' is not a column") {
		t.Fatalf("error message = %q, want the original compiler text preserved", res.Errors[0].Message)
	}
	if !strings.Contains(res.Errors[0].Message, names[0]) {
		t.Fatalf("error message = %q, want approved saved query %q named", res.Errors[0].Message, names[0])
	}
	// The lookup is attributed evidence, not a model action, and must not
	// satisfy the saved-query detail guard.
	recoveryActions := 0
	for _, action := range runtime.state.actions {
		if action.Source == "recovery" && action.Tool == "query_catalog" {
			recoveryActions++
		}
	}
	// One saved-query list at seed, one definitions lookup on the raw attempt.
	if recoveryActions != 2 {
		t.Fatalf("recovery-sourced catalog actions = %d, want 2 (list + definitions)", recoveryActions)
	}
	if runtime.state.savedQueryDetailed(names[0]) {
		t.Fatalf("saved-query lookup must not satisfy the detail guard for %q", names[0])
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

// The seed itself must surface the approved path when the ranked search buries
// it under tables and columns — the model cannot prefer what it never sees.
func TestSeedSurfacesApprovedSavedQueriesWhenSearchRanksThemOut(t *testing.T) {
	base := &fakeRuntime{}
	runtime := newProtocolRuntime(base, "does the roast plan cover committed shipments", "", 20, nil, nil, CatalogSearchFeatures{})
	seed, err := runtime.Seed(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result := mapValue(seed)
	if result == nil {
		t.Fatalf("seed = %#v", seed)
	}
	cards := catalogCards(result)
	found := false
	for _, card := range cards {
		if stringFromMap(card, "id") == "saved_query:daily_roast_context" {
			found = true
		}
	}
	if !found {
		t.Fatalf("seed cards must include the approved saved query: %#v", result["cards"])
	}
	if int(floatFromAny(result["count"])) != len(cards) {
		t.Fatalf("count = %v, want %d", result["count"], len(cards))
	}
	// Supplied discovery is not the model's own, and does not pre-satisfy the
	// detail guard that still gates execution.
	if runtime.state.modelDiscoveryAction {
		t.Fatal("saved-query supplement must not count as the model's discovery action")
	}
	if runtime.state.savedQueryDetailed("daily_roast_context") {
		t.Fatal("saved-query supplement must not satisfy the detail guard")
	}
	if runtime.state.actions[1].Source != "recovery" {
		t.Fatalf("supplement action source = %q, want recovery", runtime.state.actions[1].Source)
	}
}

// A hand-rolled equivalent of an approved saved query adds filters nobody
// approved: observed live, it silently returned an empty result set and the
// model concluded there was no work to do. Redirect it to the governed query.
func TestRawGraphQLRedirectedToCoveringSavedQuery(t *testing.T) {
	base := &savedQueryDefinitionRuntime{fakeRuntime: &fakeRuntime{}}
	runtime := newProtocolRuntime(base, "does the roast plan cover committed shipments", "", 20, nil, nil, CatalogSearchFeatures{})
	if _, err := runtime.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.QueryCatalog(context.Background(), map[string]any{"id": "table:ops:public.production_orders"}); err != nil {
		t.Fatal(err)
	}
	raw := "query { production_orders(where: { requested_ship_date: { eq: \"2026-07-27\" } }) { id } roast_schedule { id } }"
	_, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": raw})
	if err == nil {
		t.Fatal("a raw query covered by an approved saved query must be redirected")
	}
	if !strings.Contains(err.Error(), "daily_roast_context") {
		t.Fatalf("error = %q, want the covering saved query named", err)
	}
	for _, call := range base.calls {
		if call == "execute_graphql" {
			t.Fatal("redirected query must not reach the runtime")
		}
	}
	// The redirect is advisory, not a dead end: a model that insists proceeds.
	if _, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": raw}); err != nil {
		t.Fatalf("second attempt must proceed under the remaining guards: %v", err)
	}
}

// A raw query reaching beyond what any approved saved query returns is the
// legitimate use of raw GraphQL and must not be redirected.
func TestRawGraphQLNotRedirectedWhenNotCovered(t *testing.T) {
	base := &savedQueryDefinitionRuntime{fakeRuntime: &fakeRuntime{}}
	runtime := newProtocolRuntime(base, "sensor drift", "", 20, nil, nil, CatalogSearchFeatures{})
	if _, err := runtime.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.QueryCatalog(context.Background(), map[string]any{"id": "table:ops:public.sensor_samples"}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{
		"query": "query { sensor_samples { id reading } }",
	}); err != nil {
		t.Fatalf("uncovered raw query must be allowed: %v", err)
	}
}

// savedQueryDefinitionRuntime serves a saved-query definition detail row, the
// shape the live catalog returns for saved_query ids.
type savedQueryDefinitionRuntime struct {
	*fakeRuntime
}

func (r *savedQueryDefinitionRuntime) QueryCatalog(ctx context.Context, args map[string]any) (any, error) {
	ids := detailIDsFromArgs(args)
	if len(ids) == 1 && strings.HasPrefix(ids[0], "saved_query:") {
		r.record("query_catalog", args)
		return map[string]any{
			"count": 1,
			"cards": []any{map[string]any{
				"id": ids[0], "kind": "saved_query", "name": savedQueryNameFromID(ids[0]),
			}},
			"details": []any{map[string]any{
				"card_id":   ids[0],
				"section":   "saved_query_definition",
				"data_json": `{"name":"daily_roast_context","operation":"query","query":"query daily_roast_context {\n  production_orders { id }\n  roast_schedule { id }\n  green_lots { id }\n  subscriptions { id }\n}"}`,
			}},
		}, nil
	}
	return r.fakeRuntime.QueryCatalog(ctx, args)
}

// A seed that already surfaced saved queries needs no supplement.
func TestSeedSkipsSupplementWhenSavedQueriesAlreadyPresent(t *testing.T) {
	base := &fakeRuntime{}
	base.catalogOverride = func(map[string]any) any {
		return map[string]any{
			"count": 1,
			"cards": []any{map[string]any{
				"id": "saved_query:daily_roast_context", "kind": "saved_query", "name": "daily_roast_context",
			}},
		}
	}
	runtime := newProtocolRuntime(base, "roast plan", "", 20, nil, nil, CatalogSearchFeatures{})
	if _, err := runtime.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(base.calls) != 1 {
		t.Fatalf("catalog calls = %v, want only the seed", base.calls)
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
	if !strings.Contains(err.Error(), "daily_roast_context") {
		t.Fatalf("error = %q, want the approved saved query named", err)
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

// The seed's own search stays a single unexpanded call; the saved-query
// supplement is a separate lookup, never a coverage expansion of the seed.
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

func TestExecuteGraphQLErrorAttachesSavedQueryRecovery(t *testing.T) {
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
	if !strings.Contains(instruction, "never advise schema or data changes") {
		t.Fatalf("recovery instruction = %q", instruction)
	}
	names, _ := recovery["approved_saved_queries"].([]string)
	found := false
	for _, name := range names {
		if name == "daily_roast_context" {
			found = true
		}
	}
	if !found {
		t.Fatalf("approved_saved_queries = %v, want daily_roast_context", names)
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
