package agent

import (
	"context"
	"fmt"
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
	queries   []string
}

func (r *remoteJoinRuntime) ExecuteGraphQL(_ context.Context, args map[string]any) (any, error) {
	query, _ := args["query"].(string)
	r.queries = append(r.queries, query)
	// The fake used to count calls without reading the query. That is exactly
	// how a repair with the braces dropped from its where argument — `where:
	// name: {eq: "x"}` — passed every test in this file and then reached seven
	// benchmark episodes labelled "execute exactly as given". A stand-in for a
	// GraphQL endpoint has to reject what a GraphQL endpoint rejects.
	if err := checkGraphQLParses(query); err != nil {
		return nil, err
	}
	r.execCalls++
	return map[string]any{"data": map[string]any{"accounts": []any{map[string]any{
		"name": "Meridian Robotics", "account_health": map[string]any{"health": "red", "open_risk_count": 4},
	}}}}, nil
}

// checkGraphQLParses applies the two things a parser would reject on to any
// string this package hands a model as ready to run: delimiters must balance,
// and every argument value must actually be a value. A bare name where a value
// belongs, followed by its own colon, is an object that lost its braces.
func checkGraphQLParses(query string) error {
	if strings.TrimSpace(query) == "" {
		return fmt.Errorf("empty query")
	}
	if strings.ContainsAny(query, "<>") {
		return fmt.Errorf("placeholder text is not executable: %s", query)
	}
	closers := map[byte]byte{'}': '{', ')': '(', ']': '['}
	var open []byte
	for i := 0; i < len(query); i++ {
		switch c := query[i]; c {
		case '"':
			for i++; i < len(query); i++ {
				if query[i] == '\\' {
					i++
					continue
				}
				if query[i] == '"' {
					break
				}
			}
			if i >= len(query) {
				return fmt.Errorf("unterminated string: %s", query)
			}
		case '{', '(', '[':
			open = append(open, c)
		case '}', ')', ']':
			if len(open) == 0 || open[len(open)-1] != closers[c] {
				return fmt.Errorf("unbalanced %q at %d: %s", string(c), i, query)
			}
			open = open[:len(open)-1]
		case ':':
			value := i + 1
			for value < len(query) && (query[value] == ' ' || query[value] == '\t' || query[value] == '\n') {
				value++
			}
			end := value
			for end < len(query) && isGraphQLNameContinue(query[end]) {
				end++
			}
			if end == value {
				continue
			}
			next := end
			for next < len(query) && (query[next] == ' ' || query[next] == '\t' || query[next] == '\n') {
				next++
			}
			if next < len(query) && query[next] == ':' {
				return fmt.Errorf("argument value at %d lost its braces (%q is a key, not a value): %s", value, query[value:end], query)
			}
		}
	}
	if len(open) != 0 {
		return fmt.Errorf("unclosed %q: %s", string(open[len(open)-1]), query)
	}
	return nil
}

// TestCheckGraphQLParsesCatchesTheDroppedBraces pins the checker itself: a test
// helper that silently accepts everything is how the original bug survived.
func TestCheckGraphQLParsesCatchesTheDroppedBraces(t *testing.T) {
	broken := `query { accounts(where: name: {eq: "Meridian Robotics"}, limit: 20) { id name } }`
	if err := checkGraphQLParses(broken); err == nil {
		t.Fatal("the shipped round-2 shape must not be accepted")
	}
	for _, good := range []string{
		`query { accounts(where: {name: {eq: "Meridian Robotics"}}, limit: 20) { id name account_health { health } } }`,
		`query { accounts(where: {and: [{plan: {eq: "pro"}}, {status: {eq: "active"}}]}) { id } }`,
		`query { accounts { id alias: name other: plan } }`,
		`query { accounts(where: {name: {eq: "a: b"}}) { id } }`,
	} {
		if err := checkGraphQLParses(good); err != nil {
			t.Fatalf("valid query rejected: %v", err)
		}
	}
}

// offeredQueryFromError extracts the HOLE-form offered query from a thrown
// join refusal: the query follows the last "then execute it: " and runs to the
// end of the message.
func offeredQueryFromError(t *testing.T, err error) string {
	t.Helper()
	marker := "then execute it: "
	index := strings.LastIndex(err.Error(), marker)
	if index < 0 {
		t.Fatalf("exception does not carry the offered query: %v", err)
	}
	return strings.TrimSpace(err.Error()[index+len(marker):])
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

	_, err := runtime.ExecuteGraphQL(context.Background(),
		map[string]any{"query": `query { account_health(where: {account_id: {eq: 1}}) { health open_risk_count } }`})
	if err == nil {
		t.Fatal("the doomed top-level query must throw the offer")
	}
	if base.execCalls != 0 {
		t.Fatalf("the doomed top-level query must not execute, calls=%d", base.execCalls)
	}
	for _, want := range []string{"did NOT execute", "nested under accounts", "accounts(where:"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the exception must carry %q: %v", want, err)
		}
	}
	found := false
	for _, violation := range runtime.state.violations {
		if violation.Code == "remote_join_path_required" && violation.Blocking {
			found = true
		}
	}
	if !found {
		t.Fatalf("the interception should record its violation: %+v", runtime.state.violations)
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

	_, err := runtime.ExecuteGraphQL(context.Background(),
		map[string]any{"query": `query { account_health { health } }`})
	if err == nil || !strings.Contains(err.Error(), "nested under accounts") {
		t.Fatalf("the interception must throw and name the parent: %v", err)
	}
	if base.execCalls != 0 {
		t.Fatalf("the doomed query must not execute, calls=%d", base.execCalls)
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

// A filter whose every column is legal on the parent makes the nested query the
// unique reading of the model's own question, so it runs rather than being
// handed back. Round 2 handed it back seven times and got zero executions.
func TestRemoteJoinRepairExecutesACleanGraft(t *testing.T) {
	runtime, base := remoteJoinTestRuntime(t)
	seedRemoteJoinColumns(runtime)

	out, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{
		"query": `query { account_health(where: {name: {eq: "Meridian Robotics"}}) { health open_risk_count } }`,
	})
	if err != nil {
		t.Fatalf("the rewritten query should execute: %v", err)
	}
	if base.execCalls != 1 {
		t.Fatalf("the rewrite should reach the runtime exactly once, calls=%d queries=%q", base.execCalls, base.queries)
	}
	// What reached the endpoint is the nested route carrying the model's filter,
	// and it parses — the assertion the substring checks here used to miss.
	ran := base.queries[len(base.queries)-1]
	if err := checkGraphQLParses(ran); err != nil {
		t.Fatalf("executed query does not parse: %v", err)
	}
	if !strings.Contains(ran, `accounts(where: {name: {eq: "Meridian Robotics"}}`) {
		t.Fatalf("the model's own filter should ride the parent, braces and all: %q", ran)
	}
	if strings.Index(ran, "accounts(") > strings.Index(ran, "account_health {") {
		t.Fatalf("account_health must be nested under accounts, not the other way around: %q", ran)
	}
	// Data came back under a field name the model did not ask for, so the result
	// says so; an unexplained reshaping is how a model reports an empty answer.
	recovery := mapValue(mapValue(out)["recovery"])
	if stringFromMap(recovery, "kind") != "remote_join_route_rewritten" {
		t.Fatalf("an executed rewrite must announce itself: %+v", recovery)
	}
	if !strings.Contains(stringFromMap(recovery, "instruction"), "nested inside") {
		t.Fatalf("the notice must say where to read the join from: %+v", recovery)
	}
	if runtime.state.hasBlockingViolation() {
		t.Fatal("a successful rewrite should discharge the interception")
	}
}

// A filter naming the child's own join column cannot be carried across to the
// parent, and carries no value to salvage either. The shape still arrives, with
// the columns that would work — offered, because the row is the model's choice.
func TestRemoteJoinRepairReplacesAnUnusableFilterWithAHole(t *testing.T) {
	runtime, base := remoteJoinTestRuntime(t)
	seedRemoteJoinColumns(runtime)

	_, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{
		"query": `query { account_health(where: {account_id: {eq: 1}}) { health } }`,
	})
	if err == nil {
		t.Fatal("the unusable filter must throw the holed offer")
	}
	if base.execCalls != 0 {
		t.Fatalf("a repair the model still has to complete must not run, calls=%d", base.execCalls)
	}
	offered := offeredQueryFromError(t, err)
	if strings.Contains(offered, "account_id: {eq: 1}") {
		t.Fatalf("a child-column filter must not be grafted onto the parent: %q", offered)
	}
	if !strings.Contains(offered, "<filter on accounts") || !strings.Contains(offered, "name") {
		t.Fatalf("the hole should name real parent columns: %q", offered)
	}
	// The instruction has to be the one the query can actually satisfy. Round 2
	// said "execute exactly as given" AND "fill the placeholder" in the same
	// breath, about a string that parses as neither.
	if !strings.Contains(err.Error(), "Replace the <filter on accounts> placeholder") {
		t.Fatalf("a holed repair must ask for the placeholder to be filled: %v", err)
	}
	if strings.Contains(err.Error(), "exactly as given") {
		t.Fatalf("a holed repair must not also claim to be ready to run: %v", err)
	}
	// And once filled, it is a real query.
	filled := remoteJoinFillHole(offered, `{name: {eq: "Meridian Robotics"}}`)
	if err := checkGraphQLParses(filled); err != nil {
		t.Fatalf("the filled repair should parse: %v", err)
	}
	if _, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": filled}); err != nil {
		t.Fatalf("the filled repair should execute: %v", err)
	}
	if runtime.state.hasBlockingViolation() {
		t.Fatal("executing the filled repair should clear the block")
	}
}

// remoteJoinFillHole substitutes a real filter for the placeholder, the way the
// recovery instruction asks the model to.
func remoteJoinFillHole(offered, filter string) string {
	start := strings.Index(offered, "{<filter on")
	if start < 0 {
		return offered
	}
	end := strings.Index(offered[start:], ">}")
	if end < 0 {
		return offered
	}
	return offered[:start] + filter + offered[start+end+2:]
}

// Every cross-source miss on record put the account's name under a column
// accounts does not have — client, account_name, executive_owner — so the one
// literal in the filter is a value the model chose and the column is the only
// thing it got wrong. Move the value; offer the result rather than running it.
func TestRemoteJoinRepairSalvagesTheModelsOwnValue(t *testing.T) {
	runtime, base := remoteJoinTestRuntime(t)
	seedRemoteJoinColumns(runtime)

	_, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{
		"query": `query { account_health(where: {client: {eq: "Meridian Robotics"}}) { health } }`,
	})
	if err == nil {
		t.Fatal("a guessed column must throw the salvaged offer")
	}
	if base.execCalls != 0 {
		t.Fatalf("a guessed column must not execute on the model's behalf, calls=%d", base.execCalls)
	}
	offered := correctedMutationFromError(t, err)
	if parseErr := checkGraphQLParses(offered); parseErr != nil {
		t.Fatalf("the salvaged repair must be executable: %v", parseErr)
	}
	if !strings.Contains(offered, `accounts(where: {name: {eq: "Meridian Robotics"}}`) {
		t.Fatalf("the salvage should move the literal to accounts.name: %q", offered)
	}
	// The value is the model's and the column is ours, so the exception says so
	// rather than presenting the filter as settled.
	if !strings.Contains(err.Error(), "moved to the accounts column") {
		t.Fatalf("the salvage must disclose the inference: %v", err)
	}
	if _, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": offered}); err != nil {
		t.Fatalf("the salvaged repair should execute: %v", err)
	}
}

// A model looping on the same closed route should not be re-taught the same
// paragraph, but must still receive the corrected query every time.
func TestRemoteJoinRepairStopsRepeatingItselfButKeepsOffering(t *testing.T) {
	// The repeat re-throws the full offer each time — the thrown text is the
	// one channel the next turn reads, so there is no terser second form; the
	// non-repetition lives in the single violation record.
	runtime, _ := remoteJoinTestRuntime(t)
	seedRemoteJoinColumns(runtime)
	// A hole-path filter, so the repair keeps being offered rather than run.
	query := map[string]any{"query": `query { account_health(where: {account_id: {eq: 1}}) { health } }`}

	_, firstErr := runtime.ExecuteGraphQL(context.Background(), cloneAnyMap(query))
	if firstErr == nil {
		t.Fatal("the first fire must throw the offer")
	}
	_, secondErr := runtime.ExecuteGraphQL(context.Background(), cloneAnyMap(query))
	if secondErr == nil {
		t.Fatal("the repeat must throw the offer again")
	}
	for _, err := range []error{firstErr, secondErr} {
		if !strings.Contains(err.Error(), "placeholder") {
			t.Fatalf("every fire must name the placeholder: %v", err)
		}
		if !strings.Contains(offeredQueryFromError(t, err), "accounts(where:") {
			t.Fatalf("every fire must carry the corrected query: %v", err)
		}
	}
	if !runtime.state.remoteJoinRepairOffered["account_health"] {
		t.Fatal("the repeat state should mark the root as already taught")
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

// The graft decides whether the model's filter is trustworthy on the parent, and
// therefore whether the repair may run itself.
func TestRemoteJoinFilterGraftRules(t *testing.T) {
	for name, tc := range map[string]struct {
		query      string
		wantKind   remoteJoinFilterKind
		wantInside string
	}{
		"parent column grafts":       {query: `query { account_health(where: {name: {eq: "x"}}) { health } }`, wantKind: remoteJoinFilterGrafted, wantInside: `where: {name: {eq: "x"}}`},
		"combinator of parent cols":  {query: `query { account_health(where: {and: [{plan: {eq: "pro"}}, {status: {eq: "active"}}]}) { health } }`, wantKind: remoteJoinFilterGrafted, wantInside: `where: {and: [{plan:`},
		"child join column salvages": {query: `query { account_health(where: {account_id: {eq: "1"}}) { health } }`, wantKind: remoteJoinFilterSalvaged, wantInside: `where: {name: {eq: "1"}}`},
		"invented column salvages":   {query: `query { account_health(where: {client: {eq: "Meridian"}}) { health } }`, wantKind: remoteJoinFilterSalvaged, wantInside: `where: {name: {eq: "Meridian"}}`},
		"numeric child column holes": {query: `query { account_health(where: {account_id: {eq: 1}}) { health } }`, wantKind: remoteJoinFilterHole},
		"two literals are ambiguous": {query: `query { account_health(where: {and: [{client: {eq: "a"}}, {owner: {eq: "b"}}]}) { health } }`, wantKind: remoteJoinFilterHole},
		"variables rejected":         {query: `query($id:Int!){ account_health(where: {id: {eq: $id}}) { health } }`, wantKind: remoteJoinFilterHole},
		"no filter at all":           {query: `query { account_health { health } }`, wantKind: remoteJoinFilterHole},
	} {
		t.Run(name, func(t *testing.T) {
			runtime, _ := remoteJoinTestRuntime(t)
			seedRemoteJoinColumns(runtime)
			offered, kind := runtime.remoteJoinRepairedQuery(context.Background(), tc.query, "account_health", "accounts")
			if kind != tc.wantKind {
				t.Fatalf("kind = %v, want %v (offer %q)", kind, tc.wantKind, offered)
			}
			if tc.wantInside != "" && !strings.Contains(offered, tc.wantInside) {
				t.Fatalf("offer should carry %q: %q", tc.wantInside, offered)
			}
			if kind == remoteJoinFilterHole && !strings.Contains(offered, "<filter on accounts") {
				t.Fatalf("an unsalvageable filter should leave a hole: %q", offered)
			}
			// Anything not holed is claimed to be runnable, so it has to parse.
			if kind != remoteJoinFilterHole {
				if err := checkGraphQLParses(offered); err != nil {
					t.Fatalf("a repair offered as runnable must parse: %v", err)
				}
			}
			// The route is correct either way — that is the part that is never
			// the model's to guess.
			if !strings.Contains(offered, "accounts(where:") || !strings.Contains(offered, "account_health {") {
				t.Fatalf("offer must always carry the nested route: %q", offered)
			}
		})
	}
}
