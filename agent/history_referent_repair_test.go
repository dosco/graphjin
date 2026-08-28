package agent

import (
	"context"
	"strings"
	"testing"
)

// Four of the six stable multi-turn losses in run c3-r2 were the referent
// guard's own escape hatch: refuse the unscoped count, watch the model resend
// it, let it through, and let the whole table's number be reported as the
// retained subject's. Asked how many failed invoices account 1 has, the user
// was told 3 — the count for every account in the database. These tests pin
// the replacement: a mechanical binding executes the scoped query instead.

func referentTestRuntime(t *testing.T, instruction string, history ...Turn) (*protocolRuntime, *strictWriteRuntime) {
	t.Helper()
	base := &strictWriteRuntime{columns: map[string][]string{
		"users":           {"id", "account_id", "name", "email", "role"},
		"invoices":        {"id", "account_id", "subscription_id", "amount_cents", "status"},
		"support_tickets": {"id", "account_id", "user_id", "subject", "severity", "status"},
		"accounts":        {"id", "name", "plan", "status", "mrr_cents"},
	}}
	runtime := newProtocolRuntime(base, instruction, "", 8, nil, nil, CatalogSearchFeatures{})
	runtime.state.seedOK = true
	runtime.state.modelDiscoveryAction = true
	runtime.state.history = history
	runtime.state.tableColumnNames = map[string][]string{
		"users":           {"id", "account_id", "name", "email", "role"},
		"invoices":        {"id", "account_id", "subscription_id", "amount_cents", "status"},
		"support_tickets": {"id", "account_id", "user_id", "subject", "severity", "status"},
		"accounts":        {"id", "name", "plan", "status", "mrr_cents"},
	}
	runtime.state.catalogDetails = []string{"table:app:main.users"}
	return runtime, base
}

func TestReferentRewriteScopesThroughTheForeignKey(t *testing.T) {
	runtime, base := referentTestRuntime(t, "How many users belong to it?",
		Turn{Role: "user", Content: "Use Harborlight Systems, account 3."},
		Turn{Role: "assistant", Content: "The retained account id is 3."},
	)
	out, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": `query { users { id name } }`})
	if err != nil {
		t.Fatal(err)
	}
	if base.execCalls != 1 {
		t.Fatalf("the scoped rewrite should execute, calls=%d", base.execCalls)
	}
	ran := base.queries[len(base.queries)-1]
	if !strings.Contains(ran, `users(where: {account_id: {eq: 3}})`) {
		t.Fatalf("the rewrite should bind account 3 through users.account_id: %q", ran)
	}
	if err := checkGraphQLParses(ran); err != nil {
		t.Fatalf("the executed rewrite must parse: %v", err)
	}
	recovery := mapValue(mapValue(out)["recovery"])
	if stringFromMap(recovery, "kind") != "history_referent_bound" {
		t.Fatalf("an executed rewrite must announce itself: %+v", recovery)
	}
	if !strings.Contains(stringFromMap(recovery, "instruction"), "account 3") {
		t.Fatalf("the notice should name the subject: %+v", recovery)
	}
	if runtime.state.hasBlockingViolation() {
		t.Fatal("a successful scoped execution should discharge the guard")
	}
}

func TestReferentRewriteScopesTheSubjectsOwnTable(t *testing.T) {
	runtime, base := referentTestRuntime(t, "What severity is that ticket?",
		Turn{Role: "user", Content: "We are reviewing support ticket 1."},
		Turn{Role: "assistant", Content: "Ticket 1 is the current subject."},
	)
	if _, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": `query { support_tickets(limit: 5) { id subject severity status } }`}); err != nil {
		t.Fatal(err)
	}
	ran := base.queries[len(base.queries)-1]
	if !strings.Contains(ran, "id: {eq: 1}") || !strings.Contains(ran, "limit: 5") {
		t.Fatalf("the rewrite should add the id filter and keep existing arguments: %q", ran)
	}
	if err := checkGraphQLParses(ran); err != nil {
		t.Fatalf("the executed rewrite must parse: %v", err)
	}
}

func TestReferentRewriteMergesIntoAnExistingWhere(t *testing.T) {
	runtime, base := referentTestRuntime(t, "How many failed invoices does that account have?",
		Turn{Role: "user", Content: "Focus on Meridian Robotics, account 1."},
		Turn{Role: "assistant", Content: "I will use account 1 as the subject."},
	)
	if _, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{
		"query": `query { invoices(where: { status: { eq: "failed" } }) { id } }`,
	}); err != nil {
		t.Fatal(err)
	}
	ran := base.queries[len(base.queries)-1]
	if !strings.Contains(ran, "account_id: {eq: 1}") || !strings.Contains(ran, `status: { eq: "failed" }`) {
		t.Fatalf("the rewrite should AND the subject into the existing where: %q", ran)
	}
	if err := checkGraphQLParses(ran); err != nil {
		t.Fatalf("the merged where must parse: %v", err)
	}
}

func TestUnbindableReferentKeepsTheRefusal(t *testing.T) {
	// payments carries no account_id-free binding for a "widget" subject; the
	// refusal, escape hatch and all, is the correct behavior when the schema
	// offers no certain column.
	runtime, base := referentTestRuntime(t, "How many rows does that widget have?",
		Turn{Role: "user", Content: "Look at widget 9."},
	)
	_, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": `query { invoices { id } }`})
	if err == nil {
		t.Fatal("an unbindable subject must throw the refusal")
	}
	if base.execCalls != 0 {
		t.Fatalf("an unbindable subject must not be guessed at, calls=%d", base.execCalls)
	}
	for _, want := range []string{"did NOT execute", "widget 9", "Add the where clause"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal must carry %q: %v", want, err)
		}
	}
	found := false
	for _, violation := range runtime.state.violations {
		if violation.Code == "history_referent_unresolved" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the refusal should record its violation: %+v", runtime.state.violations)
	}
}

func TestQueryAlreadyBindingTheSubjectIsUntouched(t *testing.T) {
	runtime, base := referentTestRuntime(t, "What is that account's current MRR in cents?",
		Turn{Role: "user", Content: "Which account is Meridian Robotics?"},
		Turn{Role: "assistant", Content: "Meridian Robotics is account 1."},
	)
	query := `query { accounts(where: { id: { eq: 1 } }) { id name mrr_cents } }`
	if _, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": query}); err != nil {
		t.Fatal(err)
	}
	if base.queries[len(base.queries)-1] != query {
		t.Fatalf("a query that already binds the subject must pass through unchanged: %q", base.queries[len(base.queries)-1])
	}
}

// The saved-query door was the other half of the wrong-number path: refuse the
// unscopable saved query twice, let the third attempt run, and the whole
// table's total is reported as invoice 10's amount. With a subject the schema
// can bind, the refusal now stands — the scoped raw query the refusal asks for
// actually exists.
func TestSavedQueryReferentRefusalStandsWhenBindable(t *testing.T) {
	runtime, base := referentTestRuntime(t, "What is its amount in cents?",
		Turn{Role: "user", Content: "Use invoice 10 for the next question."},
		Turn{Role: "assistant", Content: "Invoice 10 is selected."},
	)
	runtime.state.markSavedQueryDiscovered("failed_invoice_amount")
	runtime.state.markSavedQueryDetailed("failed_invoice_amount")
	runtime.state.savedQueryGraphQL = map[string]string{
		"failed_invoice_amount": `query { invoices(where: { status: { eq: "failed" } }) { sum_amount_cents } }`,
	}
	for attempt := 1; attempt <= 4; attempt++ {
		_, err := runtime.ExecuteSavedQuery(context.Background(), map[string]any{"name": "failed_invoice_amount"})
		if err == nil {
			t.Fatalf("attempt %d: the unscopable saved query must stay refused", attempt)
		}
		for _, want := range []string{"does not filter on it", "Author a query scoped to invoice 10 with execute_graphql"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("attempt %d: the refusal must carry %q: %v", attempt, want, err)
			}
		}
	}
	found := false
	for _, violation := range runtime.state.violations {
		if violation.Code == "history_referent_unresolved" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the refusal should record its violation: %+v", runtime.state.violations)
	}
	if got := len(base.calls); got != 0 {
		t.Fatalf("the unscoped saved query must never reach the runtime, calls=%v", base.calls)
	}
}

// Episode same-invoice-amount rep3 authored the correct scoped query and was
// blocked five times by the saved-route demand for a saved query the referent
// guard refuses. The demand now yields to a retained subject the saved query
// cannot bind.
func TestSavedRouteDemandYieldsToRetainedSubject(t *testing.T) {
	runtime, _ := referentTestRuntime(t, "What is its amount in cents?",
		Turn{Role: "user", Content: "Use invoice 10 for the next question."},
	)
	runtime.state.markSavedQueryDiscovered("failed_invoice_amount")
	runtime.state.savedQueryGraphQL = map[string]string{
		"failed_invoice_amount": `query { invoices(where: { status: { eq: "failed" } }) { sum_amount_cents } }`,
	}
	if name, _ := runtime.state.requiredSavedQueryExecution(); name != "" {
		t.Fatalf("the saved route must yield to the retained subject, demanded %q", name)
	}
	// A saved query that DOES bind the subject remains the demanded route.
	runtime.state.savedQueryGraphQL["failed_invoice_amount"] = `query { invoices(where: { id: { eq: 10 } }) { amount_cents } }`
	if name, _ := runtime.state.requiredSavedQueryExecution(); name != "failed_invoice_amount" {
		t.Fatalf("a saved query binding the subject stays demanded, got %q", name)
	}
}

// The read half of the rename repair: same-account-mrr asked accounts for mrr,
// was told the real columns four times, and resent mrr into the duplicate
// guard until the run died.
func TestFailedReadCarriesTheCorrectedQuery(t *testing.T) {
	runtime, base := referentTestRuntime(t, "What is account 1's MRR in cents?")
	_, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{
		"query": `query { accounts(where: { id: { eq: 1 } }) { id name mrr } }`,
	})
	if err == nil {
		t.Fatal("the dataless read failure must throw")
	}
	repaired := correctedMutationFromError(t, err)
	if !strings.Contains(repaired, "mrr_cents") || strings.Contains(repaired, "mrr ") {
		t.Fatalf("the read repair should rename mrr to mrr_cents: %q", repaired)
	}
	if _, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": repaired}); err != nil {
		t.Fatalf("the corrected read should execute: %v", err)
	}
	if base.execCalls < 2 {
		t.Fatalf("the corrected read should reach the runtime, calls=%d", base.execCalls)
	}
}

// The watch half: a subscription naming amount for amount_cents failed with an
// honest report and no repair, because the unknown column lives inside a
// quoted string no other rename path can reach.
func TestWatchSubscriptionColumnRepair(t *testing.T) {
	query := `mutation { gj_watch(insert: { name: "deeporg_new_payments", query: "subscription { payments(where: { amount: { gt: 0 } }, first: 25, after: $cursor) { id amount } payments_cursor }", delivery_json: { kind: "inbox", digest: { window: "1h" } } }) { id name status } }`
	repaired, ok := repairWatchSubscriptionColumn(query, "amount", "amount_cents")
	if !ok {
		t.Fatal("the subscription rename should apply")
	}
	if !strings.Contains(repaired, "amount_cents: { gt: 0 }") || !strings.Contains(repaired, "id amount_cents }") {
		t.Fatalf("both subscription occurrences should rename: %q", repaired)
	}
	// The watch's own name is a string value and must never be touched.
	if !strings.Contains(repaired, `name: "deeporg_new_payments"`) {
		t.Fatalf("values outside the subscription must survive: %q", repaired)
	}
	if _, ok := repairWatchSubscriptionColumn(query, "missing_col", "other"); ok {
		t.Fatal("a column absent from the subscription has nothing to rename")
	}
}

// A query touching only gj_* system roots targets a fixed, documented
// contract, so the discovery prerequisite is deterministic and gets supplied —
// the same treatment security and mutation evidence already receive. The
// recorded reactive episode gave up after two bare refusals of the canonical
// inbox read and answered "no unseen events found" over an inbox it never read.
func TestSystemRootDiscoveryIsSuppliedOnce(t *testing.T) {
	base := &fakeRuntime{catalogOverride: func(args map[string]any) any {
		ids, _ := args["ids"].([]any)
		cards := make([]any, 0, len(ids))
		for _, id := range ids {
			cards = append(cards, map[string]any{"id": id, "kind": "help", "summary": "watch contract"})
		}
		return map[string]any{"cards": cards}
	}}
	runtime := newProtocolRuntime(base, "Review the unseen event from the payments watch", "", 8, nil, nil, CatalogSearchFeatures{})
	runtime.state.seedOK = true
	runtime.state.modelDiscoveryAction = true

	inbox := `query { gj_watch_event(where: { seen: { eq: false } }, order_by: { created_at: desc }, limit: 1) { id watch_id data_json seen } }`
	_, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": inbox})
	if err == nil || !strings.Contains(err.Error(), "did NOT execute") || !strings.Contains(err.Error(), "Re-execute the exact same call") {
		t.Fatalf("the first system-root read should be consumed by the supply and thrown: %v", err)
	}
	if !containsString(runtime.state.catalogDetails, "help:watches") {
		t.Fatalf("the supplied contract must count as detail evidence: %v", runtime.state.catalogDetails)
	}
	if runtime.state.hasBlockingViolation() {
		t.Fatal("a supplied prerequisite records no violation")
	}
	// The control-plane read also owes the one-shot security/runtime supply,
	// so the ladder is two supplied throws; the attempt after them executes.
	_, err = runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": inbox})
	if err == nil || !strings.Contains(err.Error(), "security and runtime guidance") {
		t.Fatalf("the second attempt should be consumed by the security supply: %v", err)
	}
	if _, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": inbox}); err != nil {
		t.Fatalf("the attempt after both supplies should execute: %v", err)
	}
	found := false
	for _, call := range base.calls {
		if call == toolExecuteGraphQL {
			found = true
		}
	}
	if !found {
		t.Fatalf("the retry should reach the runtime, calls=%v", base.calls)
	}
}

// A mixed or app-table query keeps the normal refusal: the supply is for
// contracts that are fixed, not a bypass of app-schema discovery.
func TestAppTableQueriesKeepTheDiscoveryRefusal(t *testing.T) {
	base := &fakeRuntime{}
	runtime := newProtocolRuntime(base, "count invoices", "", 8, nil, nil, CatalogSearchFeatures{})
	runtime.state.seedOK = true
	runtime.state.modelDiscoveryAction = true

	_, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": `query { invoices { id } }`})
	if err == nil || !strings.Contains(err.Error(), "did NOT execute") || !strings.Contains(err.Error(), "not discovery detail") {
		t.Fatalf("an app table still requires model-driven discovery: %v", err)
	}
	found := false
	for _, violation := range runtime.state.violations {
		if violation.Code == "raw_graphql_discovery_required" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the refusal should record its violation: %+v", runtime.state.violations)
	}
}

// TestConfirmationReadCarriesTheUnperformedActionNotice covers the failure the
// 2028.4 run isolated: on "Yes, go ahead and set that up." the model takes the
// saved-query shortcut the instructions advertise, gets rows back, and
// finalizes a count as proof of a watch that was never created. Across the
// run's confirmation turns, every episode that reached for a saved query
// failed and every one that authored the mutation passed. The read still
// returns its rows — a confirmation can legitimately confirm a proposed read,
// so this must not block — but it now says the action is unperformed.
func TestConfirmationReadCarriesTheUnperformedActionNotice(t *testing.T) {
	base := &successfulExecutionRuntime{}
	runtime := newProtocolRuntime(base, "Yes, go ahead and set that up.", "", 8, nil, nil, CatalogSearchFeatures{})
	runtime.state.seedOK = true
	runtime.state.modelDiscoveryAction = true
	runtime.state.markSavedQueryDetailed("open_critical_ticket_count")

	out, err := runtime.ExecuteSavedQuery(context.Background(), map[string]any{"name": "open_critical_ticket_count"})
	if err != nil {
		t.Fatalf("the read must still execute and return: %v", err)
	}
	mapped := mapValue(normalizeValue(out))
	guidance, _ := mapped["guidance"].(string)
	if !strings.Contains(guidance, "has not performed it") {
		t.Fatalf("confirmation read carries no unperformed-action notice: %#v", mapped)
	}

	// A write in hand means the confirmation was honoured; the notice must not
	// second-guess a run that already performed its action.
	runtime.state.mutationSucceeded = true
	after, err := runtime.ExecuteSavedQuery(context.Background(), map[string]any{"name": "open_critical_ticket_count", "variables": map[string]any{"n": 2}})
	if err != nil {
		t.Fatal(err)
	}
	if g, _ := mapValue(normalizeValue(after))["guidance"].(string); strings.Contains(g, "has not performed it") {
		t.Fatalf("notice still riding after the mutation succeeded: %q", g)
	}

	// A turn that names its own subject is not a confirmation; no notice.
	plain := newProtocolRuntime(base, "Show the invoice snapshot", "", 8, nil, nil, CatalogSearchFeatures{})
	plain.state.seedOK = true
	plain.state.modelDiscoveryAction = true
	plain.state.markSavedQueryDetailed("invoice_snapshot")
	res, err := plain.ExecuteSavedQuery(context.Background(), map[string]any{"name": "invoice_snapshot"})
	if err != nil {
		t.Fatal(err)
	}
	if g, _ := mapValue(normalizeValue(res))["guidance"].(string); g != "" {
		t.Fatalf("ordinary question acquired a confirmation notice: %q", g)
	}
}
