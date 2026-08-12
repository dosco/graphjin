package agent

import "testing"

// The multi-turn family scored 1 of 21 in benchmark generation 2028.1, and every
// failure was the same: the subject carried in prior turns never reached the where
// clause. These cases come from that run, plus the shapes the guard must leave
// alone — a guard that fires on a legitimately unscoped follow-up costs a step and
// teaches the wrong lesson.

func turns(contents ...string) []Turn {
	out := make([]Turn, 0, len(contents))
	for i, content := range contents {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		out = append(out, Turn{Role: role, Content: content})
	}
	return out
}

func TestUnresolvedHistoryReferentFiresOnMeasuredFailures(t *testing.T) {
	for _, tc := range []struct {
		name, instruction, query string
		history                  []Turn
		wantSubject              string
	}{
		{
			name:        "account scoped invoice count",
			instruction: "How many failed invoices does that account have?",
			query:       `{ invoices_aggregate(where: { status: { eq: "failed" } }) { count } }`,
			history:     turns("Focus on Meridian Robotics, account 1.", "I will use account 1 as the subject."),
			wantSubject: "account 1",
		},
		{
			name:        "users belonging to it",
			instruction: "How many users belong to it?",
			query:       `{ users_aggregate { count } }`,
			history:     turns("Use Harborlight Systems, account 3.", "The retained account id is 3."),
			wantSubject: "account 3",
		},
		{
			name:        "its amount",
			instruction: "What is its amount in cents?",
			query:       `{ invoices { id amount_cents } payments { id amount_cents } }`,
			history:     turns("Use invoice 10 for the next question.", "Invoice 10 is selected."),
			wantSubject: "invoice 10",
		},
		{
			name:        "that ticket severity",
			instruction: "What severity is that ticket?",
			query:       `{ support_tickets { id severity } }`,
			history:     turns("We are reviewing support ticket 1.", "Ticket 1 is the current subject."),
			wantSubject: "ticket 1",
		},
	} {
		refs := unresolvedHistoryReferent(tc.instruction, tc.query, nil, tc.history)
		if len(refs) == 0 {
			t.Errorf("%s: expected the retained subject to be reported", tc.name)
			continue
		}
		var found bool
		for _, ref := range refs {
			if ref.String() == tc.wantSubject {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: retained subjects %v do not include %q", tc.name, refs, tc.wantSubject)
		}
	}
}

func TestUnresolvedHistoryReferentStaysQuiet(t *testing.T) {
	for _, tc := range []struct {
		name, instruction, query string
		history                  []Turn
		args                     map[string]any
	}{
		{
			name:        "query already scoped to the retained subject",
			instruction: "How many users belong to it?",
			query:       `{ users_aggregate(where: { account_id: { eq: 3 } }) { count } }`,
			history:     turns("Use Harborlight Systems, account 3.", "The retained account id is 3."),
		},
		{
			name:        "subject bound through variables",
			instruction: "What is its amount in cents?",
			query:       `query Amount($id: Int!) { invoices(where: { id: { eq: $id } }) { amount_cents } }`,
			history:     turns("Use invoice 10 for the next question.", "Invoice 10 is selected."),
			args:        map[string]any{"variables": map[string]any{"id": 10}},
		},
		{
			name:        "instruction names its own subject",
			instruction: "What is account 5's current MRR in cents?",
			query:       `{ accounts(where: { id: { eq: 5 } }) { mrr_cents } }`,
			history:     turns("Focus on Meridian Robotics, account 1.", "I will use account 1 as the subject."),
		},
		{
			name:        "deliberately unscoped follow-up",
			instruction: "Now give me the total across every account.",
			query:       `{ accounts { sum_mrr_cents } }`,
			history:     turns("Focus on Meridian Robotics, account 1.", "I will use account 1 as the subject."),
		},
		{
			name:        "no history at all",
			instruction: "How many users are there?",
			query:       `{ users_aggregate { count } }`,
			history:     nil,
		},
		{
			name:        "history holds quantities, not subjects",
			instruction: "How many of those are open?",
			query:       `{ support_tickets_aggregate { count } }`,
			history:     turns("Show me the top 5 tickets from the last 30 days.", "Here are the top 5."),
		},
	} {
		if refs := unresolvedHistoryReferent(tc.instruction, tc.query, tc.args, tc.history); len(refs) != 0 {
			t.Errorf("%s: guard must stay quiet, reported %v", tc.name, refs)
		}
	}
}

// TestUnresolvedHistoryReferentCoversSavedQueries pins the majority path: 10 of
// the 21 measured multi-turn episodes answered from a saved query and never
// touched execute_graphql, so a guard watching only raw GraphQL would miss them.
// A saved query cannot be narrowed at call time, which is why its repair points at
// authoring rather than at editing the query.
func TestUnresolvedHistoryReferentCoversSavedQueries(t *testing.T) {
	history := turns("Focus on Meridian Robotics, account 1.", "I will use account 1 as the subject.")

	unscoped := `query { accounts { max_mrr_cents } }`
	if refs := unresolvedHistoryReferent("What is that account's current MRR in cents?", unscoped, nil, history); len(refs) == 0 {
		t.Fatal("an unscoped saved metric must report the retained subject")
	}

	// A saved query that already filters on the subject is a governed shortcut and
	// must run untouched.
	scoped := `query { accounts(where: { id: { eq: 1 } }) { max_mrr_cents } }`
	if refs := unresolvedHistoryReferent("What is that account's current MRR in cents?", scoped, nil, history); len(refs) != 0 {
		t.Fatalf("a scoped saved query must not be intercepted, got %v", refs)
	}

	// So must one the caller narrows through variables.
	parameterized := `query MRR($account: Int!) { accounts(where: { id: { eq: $account } }) { max_mrr_cents } }`
	args := map[string]any{"variables": map[string]any{"account": 1}}
	if refs := unresolvedHistoryReferent("What is that account's current MRR in cents?", parameterized, args, history); len(refs) != 0 {
		t.Fatalf("a variable-bound saved query must not be intercepted, got %v", refs)
	}
}

// TestHistoryEntityReferencesPrefersRecentTurns pins the ordering the repair
// message depends on: a subject re-bound later in the conversation wins.
func TestHistoryEntityReferencesPrefersRecentTurns(t *testing.T) {
	refs := historyEntityReferences(turns(
		"Start with account 1.", "Using account 1.",
		"Actually switch to account 7.", "Using account 7.",
	))
	if len(refs) == 0 || refs[0].String() != "account 7" {
		t.Fatalf("most recent binding must lead, got %v", refs)
	}
}

// TestRecoveredReferentGuardStillAnswers is the property whose absence cost a whole
// family. A blocking violation forces the final response to blocked, so a guard
// that intercepts one call and is then satisfied has to be discharged. Without
// that, multi-turn went from 1/21 to 0/21 between two runs of the same suite: the
// guard correctly named the missing filter, the model added it, and the answer was
// thrown away anyway.
func TestRecoveredReferentGuardStillAnswers(t *testing.T) {
	state := newDiscoveryState("How many users belong to it?")
	state.addViolation("history_referent_unresolved", "subject not bound", "execute_graphql", true,
		map[string]any{"retained_subject": []entityReference{{Entity: "account", ID: "3"}}})
	if !state.hasBlockingViolation() {
		t.Fatal("the guard must block the call that triggered it")
	}

	state.resolveSuccessfulExecutionViolations()
	if state.hasBlockingViolation() {
		t.Fatal("a satisfied referent guard must not keep blocking the run")
	}
	for _, violation := range state.violations {
		if violation.Code == "history_referent_unresolved" && violation.Details["resolved"] != true {
			t.Fatal("the discharged guard must be recorded as resolved")
		}
	}

	// The same holds for the watch quoting guard, which also intercepts one call.
	watch := newDiscoveryState("Create a watch for failed invoices.")
	watch.addViolation("watch_query_invalid", "unescaped subscription string", "execute_graphql", true, nil)
	watch.resolveSuccessfulExecutionViolations()
	if watch.hasBlockingViolation() {
		t.Fatal("a repaired watch mutation must not keep blocking the run")
	}

	// A genuine policy refusal must stay terminal.
	refusal := newDiscoveryState("Delete every invoice.")
	refusal.addViolation("access_blocked", "policy refusal", "execute_graphql", true, nil)
	refusal.resolveSuccessfulExecutionViolations()
	if !refusal.hasBlockingViolation() {
		t.Fatal("a policy refusal must survive a later successful execution")
	}
}

// TestReferentGuardDischargesOnSavedQueryPath covers the asymmetry that kept a
// third of the intercepted episodes blocked: the guard fires on the saved-query
// path as well as on raw GraphQL, but discharge was wired only into the GraphQL
// path. A follow-up correctly rescoped through a saved query stayed blocked on a
// requirement it had already satisfied.
func TestReferentGuardDischargesOnSavedQueryPath(t *testing.T) {
	state := newDiscoveryState("What is that account's current MRR in cents?")
	state.addViolation("history_referent_unresolved", "subject not bound", "execute_saved_query", true, nil)

	state.resolveSuccessfulSavedQueryViolations()
	if state.hasBlockingViolation() {
		t.Fatal("a scoped saved-query execution must discharge the referent guard")
	}

	// The narrower list must not hand a saved query the power to justify raw
	// GraphQL authored without discovery.
	discovery := newDiscoveryState("Show me everything.")
	discovery.addViolation("raw_graphql_discovery_required", "no discovery", "execute_graphql", true, nil)
	discovery.resolveSuccessfulSavedQueryViolations()
	if !discovery.hasBlockingViolation() {
		t.Fatal("a saved query must not discharge the raw-GraphQL discovery prerequisite")
	}
}

// TestIdenticalUnscopedRetryIsRefusedAgain pins the flaw a one-shot flag created.
// The guard refused the unscoped query, the model re-sent it byte-identical, the
// flag was already spent, and the unscoped answer went out — the escape hatch was
// the default path. Measured on multi-turn-same-account-failed, which ran
// invoices_aggregate with a status filter and no account filter twice in a row.
func TestIdenticalUnscopedRetryIsRefusedAgain(t *testing.T) {
	state := newDiscoveryState("How many failed invoices does that account have?")
	unscoped := normalizeGraphQLIdentity(`{ invoices_aggregate(where: { status: { eq: "failed" } }) { count } }`)

	if !state.refuseUnscopedReferent(unscoped) {
		t.Fatal("the first unscoped attempt must be refused")
	}
	state.recordReferentRejection(unscoped)

	if !state.refuseUnscopedReferent(unscoped) {
		t.Fatal("an identical retry must be refused again, not waved through")
	}
	state.recordReferentRejection(unscoped)

	// But not forever. Refusing indefinitely turned a wrong answer into a guaranteed
	// timeout: measured over one run the guard fired 31 times across 7 episodes, the
	// model resending the same query until its step budget was gone.
	if state.refuseUnscopedReferent(unscoped) {
		t.Fatal("a third identical attempt must proceed; further refusals only burn the step budget")
	}

	// A genuinely different operation proceeds: re-authoring shows the message was
	// considered, and the retained subject could be wrong, so the run must not dead-end.
	other := normalizeGraphQLIdentity(`{ invoices { id status account_id } }`)
	if state.refuseUnscopedReferent(other) {
		t.Fatal("a different operation must be allowed after the first refusal")
	}
}
