package agent

import (
	"encoding/json"
	"strings"
	"sync"

	ax "github.com/ax-llm/ax/packages/go"
)

// A code step shows the model what it printed and nothing else. A function
// called for its effect is where that loses something: the code awaits
// ask_graphjin to propose a change, prints nothing, and the step comes back
// as "(no output)", the same as a step in which the call never ran. On
// unattended turns 30 of 32 steps that called a function and went on to
// another step read that way (gpt-5.4-mini, 2026-11-11). Conversation turns
// ask for answers and print them: 0 of 15.
//
// So a step that called a function and printed nothing shows what the
// function returned. A step that printed anything, failed, or finished its
// stage is left as it is.
//
// What it changes, from the first run with it: the next step used the value
// in 2 of 3 turns (one reported the held change by its approval id, one
// re-planned after a question failed). It does not stop a model that sends
// its own program again: the third turn repeated the step five more times
// with the value in front of it, and steps after such a call were identical
// as often as before (4 of 7 against 16 of 27). The step limit bounds that
// and the reply is still built from what was returned.
type unprintedCalls struct {
	mu    sync.Mutex
	calls []returnedCall
}

type returnedCall struct {
	name   string
	result any
}

// record notes what a function returned to the step that called it.
func (u *unprintedCalls) record(name string, result ax.Value) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.calls = append(u.calls, returnedCall{name: name, result: normalizeValue(result)})
}

// take returns the calls recorded since the last take.
func (u *unprintedCalls) take() []returnedCall {
	u.mu.Lock()
	defer u.mu.Unlock()
	calls := u.calls
	u.calls = nil
	return calls
}

// showReturned puts what calls returned in a step result that would otherwise
// read "(no output)": no stage-ending call, no error, nothing printed.
func showReturned(result ax.Value, calls []returnedCall) {
	payload := mapValue(result)
	if len(calls) == 0 || payload == nil || payload["kind"] != "result" || payload["result"] != nil || payload["is_error"] == true {
		return
	}
	if len(anySlice(normalizeValue(payload["logs"]))) != 0 {
		return
	}
	lines := make([]ax.Value, 0, len(calls))
	for _, call := range calls {
		lines = append(lines, call.name+" returned (this step printed nothing):\n"+returnedText(call.result))
	}
	payload["logs"] = lines
}

// returnedTextLimit bounds one returned value in a step result. A value over
// it is shown without its row data, and cut if it is still too long.
const returnedTextLimit = 3000

func returnedText(value any) string {
	text := compactJSON(value)
	if len(text) <= returnedTextLimit {
		return text
	}
	text = compactJSON(withoutRowData(value))
	if len(text) <= returnedTextLimit {
		return text
	}
	return strings.ToValidUTF8(text[:returnedTextLimit], "") + "… (cut; assign the call to a variable and print the part you need)"
}

func compactJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(raw)
}

// withoutRowData replaces each result's data with a note saying it was left
// out, keeping the status and the answer that say what happened.
func withoutRowData(value any) any {
	switch typed := value.(type) {
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = withoutRowData(item)
		}
		return out
	case map[string]any:
		if _, ok := typed["data"]; !ok {
			return typed
		}
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = item
		}
		out["data"] = "(left out here; assign the call to a variable to read it)"
		return out
	}
	return value
}
