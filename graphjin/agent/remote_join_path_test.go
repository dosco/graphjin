package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// Run 969337b6 counted 32 top-level account_health queries, every one an error:
// the table is an API join with no rows of its own, GraphJin knew the only route in
// was through accounts, and the SQL error said neither. Several episodes burned
// their whole step budget on it. These tests pin the redirection.

type remoteJoinRuntime struct {
	fakeRuntime
	execCalls int
}

func (r *remoteJoinRuntime) ExecuteGraphQL(_ context.Context, args map[string]any) (any, error) {
	r.execCalls++
	return map[string]any{"data": map[string]any{"accounts": []any{map[string]any{
		"name": "Meridian Robotics", "account_health": map[string]any{"health": "red", "open_risk_count": 4},
	}}}}, nil
}

func remoteJoinTestRuntime(t *testing.T) (*protocolRuntime, *remoteJoinRuntime) {
	t.Helper()
	base := &remoteJoinRuntime{}
	runtime := newProtocolRuntime(base, "How healthy is Meridian Robotics right now?", "", 8, nil, nil, CatalogSearchFeatures{})
	runtime.state.seedOK = true
	runtime.state.modelDiscoveryAction = true
	runtime.state.catalogDetails = []string{"table:app:main.account_health"}
	// Register the join the way a live run does: through the relationship card.
	runtime.state.recordCatalogRows(map[string]any{"cards": []any{map[string]any{
		"id":            "relationship:app:main.account_health.__account_health_id->app:main.accounts.id",
		"kind":          "relationship",
		"source":        "remote_join",
		"evidence_json": `{"FromTableName":"account_health","ToTableName":"accounts","Source":"remote_join"}`,
	}}}, false)
	return runtime, base
}

func TestTopLevelRemoteJoinIsRedirected(t *testing.T) {
	runtime, base := remoteJoinTestRuntime(t)

	out, err := runtime.ExecuteGraphQL(context.Background(),
		map[string]any{"query": `query { account_health(where: {account_id: {eq: 1}}) { health open_risk_count } }`})
	if err != nil {
		t.Fatalf("interception should return a repair, not an error: %v", err)
	}
	if base.execCalls != 0 {
		t.Fatalf("the doomed top-level query must not execute, calls=%d", base.execCalls)
	}
	payload, _ := json.Marshal(out)
	for _, want := range []string{"remote_join_path_required", "nested under accounts", "accounts(where:"} {
		if !strings.Contains(string(payload), want) {
			t.Fatalf("repair must carry %q: %s", want, payload)
		}
	}

	// The nested route executes untouched, and its success discharges the guard.
	if _, err := runtime.ExecuteGraphQL(context.Background(),
		map[string]any{"query": `query { accounts(where: {id: {eq: 1}}) { name account_health { health open_risk_count } } }`}); err != nil {
		t.Fatalf("nested join should execute: %v", err)
	}
	if base.execCalls != 1 {
		t.Fatalf("nested join should reach the runtime once, calls=%d", base.execCalls)
	}
	if runtime.state.hasBlockingViolation() {
		t.Fatal("a successful nested execution must discharge the interception")
	}
}

// TestRemoteJoinInterceptionStaysScoped keeps ordinary tables and unknown roots
// out of its reach: only a table a remote_join relationship names is join-only.
func TestRemoteJoinInterceptionStaysScoped(t *testing.T) {
	runtime, base := remoteJoinTestRuntime(t)

	if _, err := runtime.ExecuteGraphQL(context.Background(),
		map[string]any{"query": `query { accounts(where: {id: {eq: 1}}) { name } }`}); err != nil {
		t.Fatalf("an ordinary table must not be intercepted: %v", err)
	}
	if base.execCalls != 1 {
		t.Fatalf("ordinary query should execute, calls=%d", base.execCalls)
	}

	// A run that never saw the relationship card has nothing to intercept with.
	bare := &remoteJoinRuntime{}
	plain := newProtocolRuntime(bare, "How healthy is Meridian?", "", 8, nil, nil, CatalogSearchFeatures{})
	plain.state.seedOK = true
	plain.state.modelDiscoveryAction = true
	plain.state.catalogDetails = []string{"table:app:main.account_health"}
	if _, err := plain.ExecuteGraphQL(context.Background(),
		map[string]any{"query": `query { account_health { health } }`}); err != nil {
		t.Fatalf("without the relationship card the query passes through: %v", err)
	}
	if bare.execCalls != 1 {
		t.Fatalf("unregistered root should execute, calls=%d", bare.execCalls)
	}
}

// TestRemoteJoinRegistersFromEdges pins the wider registration path. Run baa86d61
// armed the redirect in 3 episodes via relationship cards while 19 doomed
// top-level queries ran in episodes that had inspected the table detail — which
// carries the same join in edges_json — but never the relationship card itself.
func TestRemoteJoinRegistersFromEdges(t *testing.T) {
	got := relationshipIDsInText(`{"edges":[{"id":"relationship:app:main.account_health.__account_health_id->app:main.accounts.id","kind":"relationship"},{"id":"relationship:app:main.invoices.account_id->app:main.accounts.id"}]}`)
	if len(got) != 2 || !strings.Contains(got[0], "account_health") {
		t.Fatalf("edge scan = %v", got)
	}
	// The live payload spells the arrow -\u003e: Go's JSON encoder escapes > inside
	// string content, and the first scanner version cut the id at the backslash.
	escaped := relationshipIDsInText(`[{"id":"edge:relationship:app:main.account_health.__account_health_id-\u003eapp:main.accounts.id","kind":"served_under"}]`)
	if len(escaped) != 1 {
		t.Fatalf("escaped-arrow scan = %v", escaped)
	}
	if from, parent, ok := parseRemoteJoinRelationshipID(escaped[0]); !ok || from != "account_health" || parent != "accounts" {
		t.Fatalf("escaped-arrow parse = %q %q %v", from, parent, ok)
	}

	base := &remoteJoinRuntime{}
	runtime := newProtocolRuntime(base, "How healthy is Meridian Robotics right now?", "", 8, nil, nil, CatalogSearchFeatures{})
	runtime.state.seedOK = true
	runtime.state.modelDiscoveryAction = true
	runtime.state.catalogDetails = []string{"table:app:main.account_health"}
	// Only the table card, as a detail response: no relationship card anywhere.
	runtime.state.recordCatalogRows(map[string]any{"cards": []any{map[string]any{
		"id":         "table:app:main.account_health",
		"kind":       "table",
		"table_name": "account_health",
		"edges_json": `[{"id":"relationship:app:main.account_health.__account_health_id->app:main.accounts.id","kind":"relationship"}]`,
	}}}, false)

	out, err := runtime.ExecuteGraphQL(context.Background(),
		map[string]any{"query": `query { account_health { health } }`})
	if err != nil {
		t.Fatalf("interception should return a repair: %v", err)
	}
	if base.execCalls != 0 {
		t.Fatalf("the doomed query must not execute, calls=%d", base.execCalls)
	}
	payload, _ := json.Marshal(out)
	if !strings.Contains(string(payload), "nested under accounts") {
		t.Fatalf("repair must name the parent: %s", payload)
	}
	// The ordinary foreign-key edge in the same payload must register nothing:
	// invoices is a real table, freely queryable at the top level.
	if _, joined := runtime.state.remoteJoinParents["invoices"]; joined {
		t.Fatal("an ordinary foreign-key edge must not mark its table join-only")
	}
}

// seedRemoteJoinColumns pre-populates the column cache the repair reads, which
// is what a live run fills from the catalog on its first lookup.
func seedRemoteJoinColumns(runtime *protocolRuntime) {
	runtime.state.tableColumnNames = map[string][]string{
		"accounts":       {"id", "name", "plan", "status", "mrr_cents", "renewal_date"},
		"account_health": {"account_id", "health", "executive_owner", "open_risk_count"},
	}
}

// The guard used to describe the nested shape in prose. One episode answered
// that description with sixteen syntactic permutations of the closed route, so
// the repair now carries the query itself.
func TestRemoteJoinRepairOffersTheNestedQuery(t *testing.T) {
	runtime, _ := remoteJoinTestRuntime(t)
	seedRemoteJoinColumns(runtime)

	out, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{
		"query": `query { account_health(where: {name: {eq: "Meridian Robotics"}}) { health open_risk_count } }`,
	})
	if err != nil {
		t.Fatal(err)
	}
	offered := stringFromMap(mapValue(mapValue(mapValue(mapValue(out)["recovery"])["next"])["args"]), "query")
	if offered == "" {
		t.Fatalf("repair must carry an executable query: %+v", out)
	}
	// accounts is the outer field and account_health is nested inside it — the
	// one route the join actually has.
	if !strings.Contains(offered, "accounts(where:") || !strings.Contains(offered, "account_health {") {
		t.Fatalf("offered query has the wrong shape: %q", offered)
	}
	if strings.Index(offered, "accounts(") > strings.Index(offered, "account_health {") {
		t.Fatalf("account_health must be nested under accounts, not the other way around: %q", offered)
	}
	// The model filtered on a real accounts column, so its own filter survives
	// rather than being replaced by a placeholder.
	if !strings.Contains(offered, `name: {eq: "Meridian Robotics"}`) {
		t.Fatalf("a legal parent filter should be grafted, got %q", offered)
	}
	if !strings.Contains(stringFromMap(mapValue(mapValue(out)["recovery"]), "instruction"), "exactly as given") {
		t.Fatalf("recovery should state the fix as fact: %+v", mapValue(out)["recovery"])
	}
}

// A filter naming the child's own join column cannot be carried across to the
// parent. The shape still arrives, with the columns that would work.
func TestRemoteJoinRepairReplacesAnUnusableFilterWithAHole(t *testing.T) {
	runtime, _ := remoteJoinTestRuntime(t)
	seedRemoteJoinColumns(runtime)

	out, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{
		"query": `query { account_health(where: {account_id: {eq: 1}}) { health } }`,
	})
	if err != nil {
		t.Fatal(err)
	}
	offered := stringFromMap(mapValue(mapValue(mapValue(mapValue(out)["recovery"])["next"])["args"]), "query")
	if strings.Contains(offered, "account_id: {eq: 1}") {
		t.Fatalf("a child-column filter must not be grafted onto the parent: %q", offered)
	}
	if !strings.Contains(offered, "<filter on accounts") || !strings.Contains(offered, "name") {
		t.Fatalf("the hole should name real parent columns: %q", offered)
	}
	if !strings.Contains(stringFromMap(mapValue(mapValue(out)["recovery"]), "instruction"), "placeholder") {
		t.Fatalf("a holed repair should say the placeholder needs filling: %+v", mapValue(out)["recovery"])
	}
}

// The offered query is the whole point, so it has to actually run.
func TestRemoteJoinRepairedQueryExecutes(t *testing.T) {
	runtime, base := remoteJoinTestRuntime(t)
	seedRemoteJoinColumns(runtime)

	out, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{
		"query": `query { account_health(where: {name: {eq: "Meridian Robotics"}}) { health } }`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if base.execCalls != 0 {
		t.Fatalf("the closed route must never reach the runtime, calls=%d", base.execCalls)
	}
	offered := stringFromMap(mapValue(mapValue(mapValue(mapValue(out)["recovery"])["next"])["args"]), "query")
	if _, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": offered}); err != nil {
		t.Fatalf("the offered query should execute: %v", err)
	}
	if base.execCalls != 1 {
		t.Fatalf("the offered query should reach the runtime exactly once, calls=%d", base.execCalls)
	}
	// A successful execution discharges the guard, so the run is free to finish.
	if runtime.state.hasBlockingViolation() {
		t.Fatal("executing the offered query should clear the block")
	}
}

// A model looping on the same closed route should not be re-taught the same
// paragraph, but must still receive the corrected query every time.
func TestRemoteJoinRepairStopsRepeatingItselfButKeepsOffering(t *testing.T) {
	runtime, _ := remoteJoinTestRuntime(t)
	seedRemoteJoinColumns(runtime)
	query := map[string]any{"query": `query { account_health(where: {name: {eq: "Meridian Robotics"}}) { health } }`}

	first, err := runtime.ExecuteGraphQL(context.Background(), cloneAnyMap(query))
	if err != nil {
		t.Fatal(err)
	}
	second, err := runtime.ExecuteGraphQL(context.Background(), cloneAnyMap(query))
	if err != nil {
		t.Fatal(err)
	}
	firstReason := stringFromMap(mapValue(mapValue(mapValue(first)["recovery"])["next"]), "reason")
	secondReason := stringFromMap(mapValue(mapValue(mapValue(second)["recovery"])["next"]), "reason")
	if len(secondReason) >= len(firstReason) {
		t.Fatalf("the repeat should be terser: %q then %q", firstReason, secondReason)
	}
	for _, out := range []any{first, second} {
		offered := stringFromMap(mapValue(mapValue(mapValue(mapValue(out)["recovery"])["next"])["args"]), "query")
		if !strings.Contains(offered, "accounts(where:") {
			t.Fatalf("every fire must carry the corrected query: %q", offered)
		}
	}
	// The violation record stays single — the interception repeats, the record
	// does not.
	fires := 0
	for _, v := range runtime.state.violations {
		if v.Code == "remote_join_path_required" {
			fires++
		}
	}
	if fires != 1 {
		t.Fatalf("violation records = %d, want 1", fires)
	}
}

// The graft decides whether the model's filter is trustworthy on the parent.
func TestRemoteJoinFilterGraftRules(t *testing.T) {
	for name, tc := range map[string]struct {
		query      string
		wantGraft  bool
		wantInside string
	}{
		"parent column grafts":       {query: `query { account_health(where: {name: {eq: "x"}}) { health } }`, wantGraft: true, wantInside: `name: {eq: "x"}`},
		"combinator of parent cols":  {query: `query { account_health(where: {and: [{plan: {eq: "pro"}}, {status: {eq: "active"}}]}) { health } }`, wantGraft: true, wantInside: "plan"},
		"child join column rejected": {query: `query { account_health(where: {account_id: {eq: 1}}) { health } }`, wantGraft: false},
		"invented column rejected":   {query: `query { account_health(where: {health_score: {gt: 3}}) { health } }`, wantGraft: false},
		"variables rejected":         {query: `query($id:Int!){ account_health(where: {id: {eq: $id}}) { health } }`, wantGraft: false},
		"no filter at all":           {query: `query { account_health { health } }`, wantGraft: false},
	} {
		t.Run(name, func(t *testing.T) {
			runtime, _ := remoteJoinTestRuntime(t)
			seedRemoteJoinColumns(runtime)
			offered, grafted := runtime.remoteJoinRepairedQuery(context.Background(), tc.query, "account_health", "accounts")
			if grafted != tc.wantGraft {
				t.Fatalf("grafted = %v, want %v (offer %q)", grafted, tc.wantGraft, offered)
			}
			if tc.wantGraft && !strings.Contains(offered, tc.wantInside) {
				t.Fatalf("grafted offer should carry the filter: %q", offered)
			}
			if !tc.wantGraft && !strings.Contains(offered, "<filter on accounts") {
				t.Fatalf("rejected filter should leave a hole: %q", offered)
			}
			// The route is correct either way — that is the part that is never
			// the model's to guess.
			if !strings.Contains(offered, "accounts(where:") || !strings.Contains(offered, "account_health {") {
				t.Fatalf("offer must always carry the nested route: %q", offered)
			}
		})
	}
}
