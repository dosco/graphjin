package agent

import "testing"

// Drawn from benchmark generation 2028.1: asked for open_ticket_count, whose value
// is 4, the agent reported 0 and attributed it to the named metric. The 0 belonged
// to open_critical_ticket_count, executed earlier in the same run. The number was
// real and present in the run's evidence, so grounding passed; only the identity of
// the metric was wrong.

func stateWithSavedMetrics(instruction string, executed string, known ...string) *discoveryState {
	state := newDiscoveryState(instruction)
	for _, name := range known {
		state.savedQueryGraphQL[name] = "query " + name + " { support_tickets { count_id } }"
	}
	if executed != "" {
		state.lastExecution = map[string]any{
			"tool":   toolExecuteSavedQuery,
			"args":   map[string]any{"name": executed},
			"result": map[string]any{"data": map[string]any{"support_tickets": []any{map[string]any{"count_id": 0}}}},
		}
	}
	return state
}

func TestMismatchedSavedMetricCatchesTheMeasuredFailure(t *testing.T) {
	state := stateWithSavedMetrics(
		"Use the approved open ticket count saved metric and explain its current result.",
		"open_critical_ticket_count",
		"open_ticket_count", "open_critical_ticket_count",
	)
	executed, requested := state.mismatchedSavedMetric()
	if executed != "open_critical_ticket_count" || requested != "open_ticket_count" {
		t.Fatalf("mismatch = (%q, %q), want the critical count executed and the plain count requested", executed, requested)
	}
}

func TestMismatchedSavedMetricStaysQuiet(t *testing.T) {
	for _, tc := range []struct {
		name, instruction, executed string
		known                       []string
	}{
		{
			name:        "the requested metric is the one that ran",
			instruction: "Use the approved open ticket count saved metric and explain its current result.",
			executed:    "open_ticket_count",
			known:       []string{"open_ticket_count", "open_critical_ticket_count"},
		},
		{
			// The underscored name in the request must count as naming it.
			name:        "request uses the stored name",
			instruction: "Run open_critical_ticket_count and explain the result.",
			executed:    "open_critical_ticket_count",
			known:       []string{"open_ticket_count", "open_critical_ticket_count"},
		},
		{
			name:        "request names no metric at all",
			instruction: "How many support tickets are open right now?",
			executed:    "open_ticket_count",
			known:       []string{"open_ticket_count", "open_critical_ticket_count"},
		},
		{
			// Ambiguity is not a finding: with two candidates named there is no single
			// correction to offer.
			name:        "request names more than one other metric",
			instruction: "Compare the open ticket count and the open critical ticket count metrics.",
			executed:    "ticket_sla_context",
			known:       []string{"open_ticket_count", "open_critical_ticket_count", "ticket_sla_context"},
		},
		{
			name:        "answer does not rest on a saved query",
			instruction: "Use the approved open ticket count saved metric and explain its current result.",
			executed:    "",
			known:       []string{"open_ticket_count"},
		},
		{
			// A metric the run never inspected cannot be what the request meant.
			name:        "candidate was never inspected in this run",
			instruction: "Use the approved open ticket count saved metric and explain its current result.",
			executed:    "ticket_sla_context",
			known:       []string{"ticket_sla_context"},
		},
	} {
		state := stateWithSavedMetrics(tc.instruction, tc.executed, tc.known...)
		if executed, requested := state.mismatchedSavedMetric(); executed != "" {
			t.Errorf("%s: must stay quiet, reported executed=%q requested=%q", tc.name, executed, requested)
		}
	}
}

func TestExecutedSavedQueryNameIgnoresRawGraphQL(t *testing.T) {
	state := newDiscoveryState("Use the approved open ticket count saved metric.")
	state.savedQueryGraphQL["open_ticket_count"] = "query open_ticket_count { support_tickets { count_id } }"
	state.lastExecution = map[string]any{
		"tool":   toolExecuteGraphQL,
		"args":   map[string]any{"query": "{ support_tickets { count_id } }"},
		"result": map[string]any{"data": map[string]any{}},
	}
	if got := state.executedSavedQueryName(); got != "" {
		t.Fatalf("raw GraphQL evidence must not be attributed to a saved query, got %q", got)
	}
	if executed, _ := state.mismatchedSavedMetric(); executed != "" {
		t.Fatalf("an answer authored directly is not a metric mismatch, got %q", executed)
	}
}

func TestHumanizedMetricName(t *testing.T) {
	for name, want := range map[string]string{
		"open_ticket_count":          "open ticket count",
		"open-critical-ticket-count": "open critical ticket count",
		"finance.mrr_total":          "finance mrr total",
		"  Spaced_Name  ":            "spaced name",
	} {
		if got := humanizedMetricName(name); got != want {
			t.Errorf("humanizedMetricName(%q) = %q, want %q", name, got, want)
		}
	}
}
