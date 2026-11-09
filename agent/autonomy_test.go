package agent

import (
	"context"
	"strings"
	"testing"
)

func TestResolveAutonomy(t *testing.T) {
	rules := []AutonomyRule{
		{Action: CapabilityActionDataUpdate, Table: "invoices", Level: AutonomyNotify, MaxRows: 10},
		{Action: CapabilityActionDataUpdate, Table: "*", Level: AutonomyAsk},
		{Action: CapabilityActionDataInsert, Table: "*", Level: AutonomyAuto},
	}
	for _, test := range []struct {
		name    string
		actions []string
		table   string
		rows    int
		want    string
	}{
		{"named table within the cap", []string{"data.update"}, "invoices", 4, AutonomyNotify},
		{"over the cap", []string{"data.update"}, "invoices", 11, AutonomyAsk},
		{"a table only the ask wildcard covers", []string{"data.update"}, "accounts", 1, AutonomyAsk},
		{"wildcard auto", []string{"data.insert"}, "notes", 50, AutonomyAuto},
		{"the least autonomous rule decides", []string{"data.insert", "data.update"}, "invoices", 2, AutonomyNotify},
		{"an action with no rule asks", []string{"data.delete"}, "invoices", 1, AutonomyAsk},
		{"no table asks", []string{"data.insert"}, "", 1, AutonomyAsk},
	} {
		got := ResolveAutonomy(rules, test.actions, test.table, test.rows)
		if got.Level != test.want {
			t.Errorf("%s: level = %s, want %s", test.name, got.Level, test.want)
		}
		if got.Level != AutonomyAsk && !strings.HasPrefix(got.Rule, "autonomy:") {
			t.Errorf("%s: rule = %q", test.name, got.Rule)
		}
	}
	if got := ResolveAutonomy(nil, []string{"data.update"}, "invoices", 1); got.Level != AutonomyAsk {
		t.Fatalf("no rules must ask: %+v", got)
	}
}

func TestValidateAutonomy(t *testing.T) {
	if err := ValidateAutonomy([]AutonomyRule{{Action: "data.update", Table: "*", Level: "notify", MaxRows: 5}}); err != nil {
		t.Fatal(err)
	}
	for _, rule := range []AutonomyRule{
		{Action: "api.write", Table: "*", Level: "auto"},
		{Action: "gj_watch.insert", Table: "*", Level: "auto"},
		{Action: "data.update", Level: "auto"},
		{Action: "data.update", Table: "*", Level: "always"},
		{Action: "data.update", Table: "*", Level: "auto", MaxRows: -1},
	} {
		if err := ValidateAutonomy([]AutonomyRule{rule}); err == nil {
			t.Errorf("rule %+v must be refused", rule)
		}
	}
}

// decidingRuntime is a proposer whose autonomy rule ran the write at once.
type decidingRuntime struct{ proposingRuntime }

func (r *decidingRuntime) ProposeWrite(_ context.Context, args map[string]any) (any, *Approval, error) {
	r.proposed = append(r.proposed, stringArg(args, "query"))
	return map[string]any{"data": map[string]any{"support_tickets": []any{map[string]any{"id": 7, "status": "closed"}}}},
		&Approval{ID: "apr_2", Summary: "update 1 row in support_tickets", Status: "executed", DecidedBy: "autonomy:data.update:support_tickets:auto"}, nil
}

// A write an autonomy rule ran is a done write: nothing waits for the user.
func TestDecidedApprovalIsAnExecutedWrite(t *testing.T) {
	base := &decidingRuntime{}
	rt := writeReadyRuntime(base, true)
	out, err := rt.ExecuteGraphQL(context.Background(), map[string]any{"query": closeTicket})
	if err != nil || executionFailed(out) {
		t.Fatalf("out = %v err = %v", out, err)
	}
	if rt.state.pendingApproval != nil || !rt.state.mutationSucceeded {
		t.Fatalf("pending=%v succeeded=%v", rt.state.pendingApproval, rt.state.mutationSucceeded)
	}
}

func TestAPIRootsClassifyAPIWrites(t *testing.T) {
	roots := map[string]string{"create_refund": CapabilityActionAPIWrite, "cancel_order": CapabilityActionAPIDelete}
	got := MutationActionsWith(`mutation { create_refund(call: { amount: 5 }) { id } }`, roots)
	if len(got) != 1 || got[0] != CapabilityActionAPIWrite {
		t.Fatalf("api write actions = %v", got)
	}
	state := &discoveryState{capabilities: &CapabilityProfile{AllowedActions: []string{CapabilityActionAPIWrite}, APIRoots: roots}}
	if missing := state.missingCapabilityActions(`mutation { create_refund(call: { amount: 5 }) { id } }`); len(missing) != 0 {
		t.Fatalf("an api.write caller was refused: %v", missing)
	}
	if missing := state.missingCapabilityActions(`mutation { cancel_order(call: { id: 5 }) { id } }`); len(missing) != 1 || missing[0] != CapabilityActionAPIDelete {
		t.Fatalf("api.delete missing = %v", missing)
	}
}

func TestMutationTargetCountQuery(t *testing.T) {
	for query, want := range map[string]string{
		`mutation { orders(where: { status: { eq: "open" } }, update: { status: "closed" }) { id } }`: `query { orders(where: { status: { eq: "open" } }) { count_id } }`,
		`mutation close($s: String!) { orders(where: { status: { eq: $s } }, delete: true) { id } }`: `query($s: String!) { orders(where: { status: { eq: $s } }) { count_id } }`,
	} {
		got, ok := MutationTargetCountQuery(query)
		if !ok || got != want {
			t.Errorf("count query for %q = %q, %v; want %q", query, got, ok, want)
		}
	}
	for _, query := range []string{
		`mutation { orders(insert: { status: "open" }) { id } }`,
		`mutation { orders(update: { status: "closed" }) { id } }`,
		`mutation { a: orders(where: { id: { eq: 1 } }, delete: true) { id } b: orders(where: { id: { eq: 2 } }, delete: true) { id } }`,
		`query { orders { id } }`,
	} {
		if got, ok := MutationTargetCountQuery(query); ok {
			t.Errorf("%q must not be counted, got %q", query, got)
		}
	}
}
