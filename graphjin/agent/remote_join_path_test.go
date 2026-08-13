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
