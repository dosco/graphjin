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
