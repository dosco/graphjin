package agent

import (
	"strings"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
)

// The RLM contract is write code -> it runs -> you see output. The model
// observes a tool result only by printing it, so if console output never
// returns, the model is blind and re-fetches what it already holds. Measured
// before ax #602 on 339 recorded episodes: 34 intermediate steps called
// console.log and every one received empty output.
//
// This pins the loop through GraphJin's own runtime wrapper, not just ax's, so
// an ax downgrade or a wrapper change that swallows logs fails here.
func TestGraphJinRuntimeReturnsConsoleOutputToTheModel(t *testing.T) {
	runtime := newGraphJinCodeRuntime(func() any { return nil }, nil, nil, nil)
	session, err := runtime.CreateSession(map[string]ax.Value{
		"inputs": map[string]any{"instruction": "inspect the catalog"},
	}, map[string]ax.Value{"reservedNames": []any{"inputs"}})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	defer session.Close()

	step := mapValue(session.Execute(`const rows = [{id: 1}, {id: 2}, {id: 3}];
console.log("rows:", rows.length);`, nil))

	logs, ok := step["logs"].([]ax.Value)
	if !ok || len(logs) == 0 {
		t.Fatalf("an intermediate step returned no logs, so the model cannot observe: %#v", step)
	}
	if line := strings.Join(valuesToStrings(logs), " "); !strings.Contains(line, "rows:") || !strings.Contains(line, "3") {
		t.Fatalf("logs = %q, want the printed row count", line)
	}
}

// Logs belong to the turn that produced them; replaying a prior turn's output
// would have the model act on stale observations.
func TestGraphJinRuntimeScopesConsoleOutputToItsTurn(t *testing.T) {
	runtime := newGraphJinCodeRuntime(func() any { return nil }, nil, nil, nil)
	session, err := runtime.CreateSession(map[string]ax.Value{
		"inputs": map[string]any{"instruction": "inspect the catalog"},
	}, map[string]ax.Value{"reservedNames": []any{"inputs"}})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	defer session.Close()

	first := mapValue(session.Execute(`console.log("first turn");`, nil))
	if logs, _ := first["logs"].([]ax.Value); len(logs) != 1 {
		t.Fatalf("first turn logs = %#v", first["logs"])
	}
	// State persists across turns — that is the REPL promise — but the *output*
	// does not.
	second := mapValue(session.Execute(`const carried = 42;`, nil))
	if _, present := second["logs"]; present {
		t.Fatalf("a silent turn replayed earlier output: %#v", second["logs"])
	}
	third := mapValue(session.Execute(`console.log("carried is", carried);`, nil))
	line := strings.Join(valuesToStrings(third["logs"].([]ax.Value)), " ")
	if !strings.Contains(line, "42") {
		t.Fatalf("logs = %q, want the value carried from the earlier turn", line)
	}
}

func valuesToStrings(values []ax.Value) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
