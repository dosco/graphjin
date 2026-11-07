package agent

import (
	"fmt"
	"strings"
	"testing"
	"time"

	ax "github.com/ax-llm/ax/packages/go"
)

// A host call that outlasts goja's default 5s code budget, such as a query
// against a slow warehouse, still returns its rows to the data agent's code.
func TestDataAgentSlowCallReturnsItsResult(t *testing.T) {
	if testing.Short() {
		t.Skip("waits out goja's default code budget")
	}
	runtime := newGraphJinCodeRuntime(func() any { return nil }, nil, nil, nil)
	runtime.RegisterCallable("execute_graphql", func(ax.Value) (ax.Value, error) {
		time.Sleep(6 * time.Second)
		return map[string]ax.Value{"data": map[string]ax.Value{"accounts": []ax.Value{map[string]ax.Value{"name": "Meridian Robotics"}}}}, nil
	})
	session, err := runtime.CreateSession(map[string]ax.Value{
		"inputs": map[string]any{"instruction": "which account renews soonest"},
	}, map[string]ax.Value{"reservedNames": []any{"inputs"}})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	defer session.Close()

	step := fmt.Sprint(session.Execute(`const r = await execute_graphql({ query: "{ accounts { name } }" }); console.log(JSON.stringify(r));`, nil))
	if strings.Contains(step, "timed out") || !strings.Contains(step, "Meridian Robotics") {
		t.Fatalf("a slow call's result did not reach the code: %s", step)
	}
}

func TestCodeBlockBudgetFloorsAtAMinute(t *testing.T) {
	for _, tc := range []struct{ turn, wantMs int }{{0, 60000}, {50, 60000}, {120, 120000}} {
		r := newGraphJinCodeRuntime(nil, nil, nil, nil).WithCodeBlockBudget(tc.turn)
		if got := fmt.Sprint(r.base.RuntimePolicy()["timeoutMs"]); got != fmt.Sprint(tc.wantMs) {
			t.Fatalf("turn %ds: timeoutMs = %s, want %d", tc.turn, got, tc.wantMs)
		}
	}
}
