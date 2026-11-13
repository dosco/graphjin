package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// hopRuntime answers the two reads of a look-up through a relationship: the
// ticket, and its account. A query that names a column the account does not
// have fails the way the engine fails it, with no data.
type hopRuntime struct {
	fakeRuntime
	queries []string
}

func (r *hopRuntime) ExecuteGraphQL(_ context.Context, args map[string]any) (any, error) {
	query, _ := args["query"].(string)
	r.queries = append(r.queries, query)
	switch {
	case strings.Contains(query, "churn_risk"), strings.Contains(query, "risk_level"):
		return map[string]any{"errors": []any{map[string]any{"message": "field is not a column or a function; available columns: id, name, status"}}}, nil
	case strings.Contains(query, "accounts"):
		return map[string]any{"data": map[string]any{"accounts": []any{map[string]any{"id": 1, "name": "Meridian Robotics", "status": "churn_risk"}}}}, nil
	}
	return map[string]any{"data": map[string]any{"support_tickets": []any{map[string]any{"id": 25, "subject": "Cannot update billing contact", "account_id": 1}}}}, nil
}

func hopProtocol(t *testing.T) (*protocolRuntime, *hopRuntime) {
	t.Helper()
	base := &hopRuntime{}
	profile := &CapabilityProfile{RoleClass: "user", AllowedActions: []string{}}
	runtime := newProtocolRuntime(base, "Is the account of support ticket 25 at churn risk?", "", 8, profile, nil, CatalogSearchFeatures{})
	runtime.state.seedOK = true
	runtime.state.modelDiscoveryAction = true
	runtime.state.tablesDetailed = map[string]bool{"support_tickets": true, "accounts": true}
	runtime.state.catalogDetails = []string{"table:app:main.support_tickets", "table:app:main.accounts"}
	return runtime, base
}

const (
	ticketHop     = `query { support_tickets(where: { id: { eq: 25 } }) { id subject account_id } }`
	accountWrong  = `query { accounts(where: { id: { eq: 1 } }) { id name churn_risk } }`
	accountWrong2 = `query { accounts(where: { id: { eq: 1 } }) { id name risk_level } }`
	accountRight  = `query { accounts(where: { id: { eq: 1 } }) { id name status } }`
)

func execute(t *testing.T, runtime *protocolRuntime, query string) (any, error) {
	t.Helper()
	return runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": query})
}

// A look-up through a relationship reads the ticket, then fails on the
// account. The model's next program is the whole look-up again: the ticket
// read it already ran, then the repaired account query. Refusing the ticket
// read ended that step before the repair in it could run, and the run could
// neither repair nor finish. The read is answered from its earlier result, so
// the repair runs.
func TestARepeatedReadIsAnsweredWhileARepairIsOwed(t *testing.T) {
	runtime, base := hopProtocol(t)
	if _, err := execute(t, runtime, ticketHop); err != nil {
		t.Fatalf("the first hop: %v", err)
	}
	if _, err := execute(t, runtime, accountWrong); err == nil {
		t.Fatal("a query naming a column the table does not have must fail")
	}
	if runtime.state.pendingRequiredFinalization() == "" {
		t.Fatal("a failed query must leave its repair owed")
	}

	out, err := execute(t, runtime, ticketHop)
	if err != nil {
		t.Fatalf("the read before the repair was refused: %v", err)
	}
	if len(base.queries) != 2 {
		t.Fatalf("the repeated read ran again: %q", base.queries)
	}
	result := mapValue(normalizeValue(out))
	if row := mapValue(anySlice(mapValue(result["data"])["support_tickets"])[0]); row["account_id"] == nil {
		t.Fatalf("the repeated read must return its rows: %v", result)
	}
	// It discharges nothing: the repair is still owed, and the result says so.
	if runtime.state.pendingRequiredFinalization() == "" {
		t.Fatal("a repeated read must not discharge the repair")
	}
	if recovery := mapValue(result["recovery"]); !strings.Contains(fmt.Sprint(recovery["next"]), "still blocks finalization") {
		t.Fatalf("the repeated read must say what is still owed: %v", recovery)
	}
	if last := runtime.state.actions[len(runtime.state.actions)-1]; last.Source != "replay" || last.Status != "ok" {
		t.Fatalf("last action = %+v, want the read recorded as answered from this run's result", last)
	}

	// The repair runs, and the run may finish.
	if _, err := execute(t, runtime, accountRight); err != nil {
		t.Fatalf("the repaired query: %v", err)
	}
	if owed := runtime.state.pendingRequiredFinalization(); owed != "" {
		t.Fatalf("after the repair nothing may be owed: %s", owed)
	}
}

// One answer for each repair owed. A program that repeats the read again
// without having run the repair is refused and told what is owed, as before;
// a later repair gives the read its answer again.
func TestARepeatedReadIsAnsweredOnceForEachRepairOwed(t *testing.T) {
	runtime, base := hopProtocol(t)
	for _, query := range []string{ticketHop, accountWrong, ticketHop} {
		execute(t, runtime, query) //nolint:errcheck
	}
	_, err := execute(t, runtime, ticketHop)
	if err == nil {
		t.Fatal("a second repeat with the same repair owed must be refused")
	}
	for _, want := range []string{"did NOT execute", "identical successful query already ran", "execution_repair_required"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal must carry %q: %v", want, err)
		}
	}
	if len(base.queries) != 2 {
		t.Fatalf("a refused or repeated read must not run: %q", base.queries)
	}

	// A different repair is owed: the read is answered once more.
	runtime, _ = hopProtocol(t)
	for _, query := range []string{ticketHop, accountWrong, ticketHop, accountRight, accountWrong2} {
		execute(t, runtime, query) //nolint:errcheck
	}
	if runtime.state.owedOperation() == "" {
		t.Fatal("the second failed query must leave its own repair owed")
	}
	if _, err := execute(t, runtime, ticketHop); err != nil {
		t.Fatalf("the read before a new repair was refused: %v", err)
	}
}

// What is owed decides. With nothing owed, or with only the database-side
// computation of the answer owed, a repeat is not answered here: the first is
// the ordinary replay that ends the run, and the second is a wrong attempt at
// what is owed (TestCachedExecutionRecoveryKeepsPendingRequirement).
func TestOwedOperationNamesARepairOrAWriteAndNothingElse(t *testing.T) {
	s := newDiscoveryState("How many invoices are there?")
	if s.owedOperation() != "" || s.replayWhileOwed("q") {
		t.Fatal("with nothing owed no read is answered as a repeat")
	}
	s.pendingFailedQueryKey = "failed"
	if s.owedOperation() != "repair:failed" {
		t.Fatalf("owed = %q, want the failed query's repair", s.owedOperation())
	}
	if !s.replayWhileOwed("q") || s.replayWhileOwed("q") || !s.replayWhileOwed("other") {
		t.Fatal("each read is answered once for one repair owed")
	}
	s.pendingFailedQueryKey = ""
	s.addViolation("mutation_evidence_required", "this write did NOT execute: gather mutation-shape evidence for gj_watch_event first",
		"execute_graphql", true, map[string]any{"tables": []any{"gj_watch_event"}})
	if owed := s.owedOperation(); !strings.HasPrefix(owed, "retry:mutation_evidence_required:") {
		t.Fatalf("owed = %q, want the refused write", owed)
	}
	if !s.replayWhileOwed("q") {
		t.Fatal("a read is answered while a refused write is owed")
	}
}

// Only a read is answered as a repeat. A write that already succeeded and is
// sent again while something is owed is refused as before: it is never run
// twice and never reported from an earlier result.
func TestARepeatedWriteIsNeverAnsweredAsARepeat(t *testing.T) {
	base := &hopRuntime{}
	profile := &CapabilityProfile{RoleClass: "user", AllowedActions: []string{"data.update"}}
	runtime := newProtocolRuntime(base, "Resolve support ticket 25", "", 8, profile, nil, CatalogSearchFeatures{})
	runtime.state.seedOK = true
	runtime.state.modelDiscoveryAction = true
	runtime.state.tablesDetailed = map[string]bool{"support_tickets": true}
	runtime.state.catalogDetails = []string{"table:app:main.support_tickets"}
	args := map[string]any{"query": `mutation { support_tickets(where: { id: { eq: 25 } }, update: { status: "resolved" }) { id } }`}
	runtime.state.cacheSuccessfulExecution(executionQueryKey(args), map[string]any{"data": map[string]any{"support_tickets": []any{map[string]any{"id": 25}}}})
	runtime.state.pendingFailedQueryKey = "a read that failed"

	// The first write of a run is held back once while GraphJin loads its
	// security guidance; sent again, it reaches the repeat check.
	_, err := runtime.ExecuteGraphQL(context.Background(), cloneAnyMap(args))
	if err != nil && strings.Contains(err.Error(), "Re-execute the exact same operation now") {
		_, err = runtime.ExecuteGraphQL(context.Background(), cloneAnyMap(args))
	}
	if err == nil || !strings.Contains(err.Error(), "identical successful query already ran") {
		t.Fatalf("a repeated write with something owed must be refused at the repeat check: %v", err)
	}
	if len(base.queries) != 0 {
		t.Fatalf("the write ran again: %q", base.queries)
	}
	for _, action := range runtime.state.actions {
		if action.Source == "replay" {
			t.Fatalf("a write was answered as a repeat: %+v", action)
		}
	}
}
