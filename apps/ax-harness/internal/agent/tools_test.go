package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
)

func TestCapabilityOrderIsStable(t *testing.T) {
	call := func(context.Context, json.RawMessage) (json.RawMessage, error) { return json.RawMessage(`{}`), nil }
	capability := func(name string) Capability {
		return Capability{Name: name, Version: "1", Origin: "host", Effect: "read", Description: "Read a fixture.", InputSchema: json.RawMessage(`{"type":"object"}`), Call: call}
	}
	first := Tools{Capabilities: []Capability{capability("zeta"), capability("alpha")}}
	second := Tools{Capabilities: []Capability{capability("alpha"), capability("zeta")}}
	a, err := first.admitted()
	if err != nil {
		t.Fatal(err)
	}
	b, err := second.admitted()
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"alpha", "zeta"} {
		if a[i].Name != want || b[i].Name != want {
			t.Fatalf("tool order changed: %q, %q", a[i].Name, b[i].Name)
		}
	}
}

func TestInvalidToolSelectionEmitsMetadataWithoutDispatch(t *testing.T) {
	answers := []string{
		`{"javascriptCode":"final('Check the catalog',{})"}`,
		`{"javascriptCode":"try { catalog({id:'wrong'}) } catch (error) {} final('No verified row',{})"}`,
		`{"answer":"No verified row."}`,
	}
	var modelCalls, toolCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		index := int(modelCalls.Add(1)) - 1
		if index >= len(answers) {
			http.Error(w, "unexpected model call", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ax.Object("choices", ax.Array(ax.Object("message",
			ax.Object("role", "assistant", "content", answers[index]), "finish_reason", "stop"))))
	}))
	defer server.Close()
	client := ax.NewOpenAICompatibleClient(ax.Object("base_url", server.URL, "api_key", "synthetic", "model", "fixture"))
	tool := Capability{Name: "catalog", Version: "1", Origin: "fixture", Effect: "read",
		Description: "Read a row.", InputSchema: json.RawMessage(`{"type":"object","required":["id"],"properties":{"id":{"type":"integer"}},"additionalProperties":false}`),
		Call: func(context.Context, json.RawMessage) (json.RawMessage, error) {
			toolCalls.Add(1)
			return json.RawMessage(`{"id":42}`), nil
		}}
	var observed []Event
	result, err := RunWithTools(context.Background(), Spec{Version: 1, RunID: "invalid-tool", InputID: "input", Prompt: "Check the catalog"},
		client, Tools{Capabilities: []Capability{tool}}, func(e Event) error { observed = append(observed, e); return nil })
	if err != nil || result.Status != "completed" || modelCalls.Load() != 3 || toolCalls.Load() != 0 {
		t.Fatalf("result=%+v err=%v model=%d tool=%d", result, err, modelCalls.Load(), toolCalls.Load())
	}
	var rejected, started int
	for _, event := range observed {
		switch event.Type {
		case "tool.input.rejected":
			rejected++
			if event.Name != "catalog" || event.Error != "invalid_input" || len(event.Data) != 0 {
				t.Fatalf("rejection exposed input: %+v", event)
			}
		case "tool.started":
			started++
		}
	}
	if rejected != 1 || started != 0 {
		t.Fatalf("rejected=%d started=%d", rejected, started)
	}
}

func TestChildToolsSkipPauseAndMissingTools(t *testing.T) {
	read := Capability{Name: "catalog", Version: "1", Origin: "host", Effect: "read", Description: "Read catalog.", InputSchema: json.RawMessage(`{"type":"object"}`), Call: func(context.Context, json.RawMessage) (json.RawMessage, error) { return json.RawMessage(`{}`), nil }}
	write := read
	write.Name, write.Effect = "write", "durable"
	for _, names := range [][]string{{"catalog", "catalog"}} {
		tools := Tools{Capabilities: []Capability{read, write}, ChildTools: names}
		admitted, err := tools.admitted()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tools.childTools(admitted); err == nil {
			t.Fatalf("accepted child authority %v", names)
		}
	}
	ask := read
	ask.Name, ask.Effect = "ask", "pause"
	for _, tc := range []struct {
		names []string
		want  string
	}{{[]string{"missing", "write", "ask", "catalog"}, "catalog,write"}, {[]string{"*"}, "catalog,write"}} {
		tools := Tools{Capabilities: []Capability{read, write, ask}, ChildTools: tc.names}
		admitted, err := tools.admitted()
		if err != nil {
			t.Fatal(err)
		}
		child, err := tools.childTools(admitted)
		var names []string
		for _, capability := range child {
			names = append(names, capability.Name)
		}
		sort.Strings(names)
		if err != nil || strings.Join(names, ",") != tc.want {
			t.Fatalf("%v: child=%v err=%v", tc.names, names, err)
		}
	}
}

func TestModelAdmissionEventFailurePreventsDispatch(t *testing.T) {
	var calls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "must not dispatch", 500)
	}))
	defer model.Close()
	client := ax.NewOpenAICompatibleClient(ax.Object("base_url", model.URL, "api_key", "synthetic", "model", "fixture"))
	spec := Spec{Version: 1, RunID: "model-admission", InputID: "input", Prompt: "Answer"}
	_, err := RunWithTools(context.Background(), spec, client, Tools{}, func(e Event) error {
		if e.Type == "model.request.started" {
			return errors.New("event sink unavailable")
		}
		return nil
	})
	if err == nil || calls.Load() != 0 {
		t.Fatalf("dispatch after failed event: err=%v calls=%d", err, calls.Load())
	}
}

func TestToolErrorReachesTheModel(t *testing.T) {
	answers := []string{
		`{"javascriptCode":"final('Save the file',{})"}`,
		`{"javascriptCode":"const receipt=file_write({}); final('Report the outcome',{receipt});"}`,
		`Answer: The file was not saved: write_denied.`,
	}
	var calls atomic.Int32
	var mu sync.Mutex
	var bodies []string
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		mu.Unlock()
		index := int(calls.Add(1)) - 1
		if index >= len(answers) {
			http.Error(w, "unexpected model call", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ax.Object("choices", ax.Array(ax.Object("message", ax.Object("role", "assistant", "content", answers[index]), "finish_reason", "stop"))))
	}))
	defer model.Close()
	client := ax.NewOpenAICompatibleClient(ax.Object("base_url", model.URL, "api_key", "synthetic", "model", "fixture"))
	tool := Capability{Name: "file_write", Version: "1", Origin: "fixture", Effect: "durable", Description: "Save a file.", InputSchema: json.RawMessage(`{"type":"object"}`), Call: func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"is_error":true,"error":"write_denied"}`), nil
	}}
	result, err := RunWithTools(context.Background(), Spec{Version: 1, RunID: "failed-write", InputID: "input", Prompt: "Save the file"},
		client, Tools{Capabilities: []Capability{tool}}, func(Event) error { return nil })
	if err != nil || result.Status != "completed" || result.Kind != "answer" || calls.Load() != 3 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, calls.Load())
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(bodies[2], "write_denied") {
		t.Fatal("the responder did not see the tool error")
	}
}

// Tools are JavaScript functions in the actor, so five calls in one step cost
// no more model calls than one.
func TestManyToolCallsShareOneActorStep(t *testing.T) {
	answers := []string{
		`{"javascriptCode":"final('Total five regions',{})"}`,
		`{"javascriptCode":"let total=0; for (const r of ['n','s','e','w','c']) total+=sales({region:r}).total; final('Report the total',{total});"}`,
		`{"answer":"Total 15."}`,
	}
	var modelCalls, toolCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		index := int(modelCalls.Add(1)) - 1
		if index >= len(answers) {
			http.Error(w, "unexpected model call", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ax.Object("choices", ax.Array(ax.Object("message",
			ax.Object("role", "assistant", "content", answers[index]), "finish_reason", "stop"))))
	}))
	defer server.Close()
	client := ax.NewOpenAICompatibleClient(ax.Object("base_url", server.URL, "api_key", "synthetic", "model", "fixture"))
	tool := Capability{Name: "sales", Version: "1", Origin: "fixture", Effect: "read", Description: "Sales total for a region.",
		InputSchema: json.RawMessage(`{"type":"object","required":["region"],"properties":{"region":{"type":"string"}},"additionalProperties":false}`),
		Call: func(context.Context, json.RawMessage) (json.RawMessage, error) {
			toolCalls.Add(1)
			return json.RawMessage(`{"total":3}`), nil
		}}
	result, err := RunWithTools(context.Background(), Spec{Version: 1, RunID: "many-tools", InputID: "input", Prompt: "Total sales"},
		client, Tools{Capabilities: []Capability{tool}}, func(Event) error { return nil })
	if err != nil || result.Status != "completed" || toolCalls.Load() != 5 || modelCalls.Load() != 3 {
		t.Fatalf("result=%+v err=%v tools=%d model=%d", result, err, toolCalls.Load(), modelCalls.Load())
	}
}

func TestSkillDescriptionsFollowAgentSkillsLimit(t *testing.T) {
	ok := Tools{Skills: []Skill{{Name: "docx", Description: strings.Repeat("é", 1024)}}}
	if _, err := ok.skills(); err != nil {
		t.Fatalf("1,024-character description refused: %v", err)
	}
	long := Tools{Skills: []Skill{{Name: "docx", Description: strings.Repeat("é", 1025)}}}
	if _, err := long.skills(); err == nil {
		t.Fatal("1,025-character description admitted")
	}
}

func TestGraphQLErrorsReachTheModelAndRepeatsStop(t *testing.T) {
	answers := []string{
		`{"javascriptCode":"final('Read products',{})"}`,
		`{"javascriptCode":"const r = await query({q:'bad'}); console.log('done');"}`,
		`{"javascriptCode":"await query({q:'bad'}); await query({q:'bad'}); console.log('again');"}`,
		`{"javascriptCode":"final('Report',{})"}`,
		`Answer: The products could not be read.`,
	}
	var calls, tools atomic.Int32
	var mu sync.Mutex
	var bodies []string
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		mu.Unlock()
		index := int(calls.Add(1)) - 1
		if index >= len(answers) {
			http.Error(w, "unexpected model call", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ax.Object("choices", ax.Array(ax.Object("message", ax.Object("role", "assistant", "content", answers[index]), "finish_reason", "stop"))))
	}))
	defer model.Close()
	client := ax.NewOpenAICompatibleClient(ax.Object("base_url", model.URL, "api_key", "synthetic", "model", "fixture"))
	tool := Capability{Name: "query", Version: "1", Origin: "fixture", Effect: "read", Description: "Query data.", InputSchema: json.RawMessage(`{"type":"object"}`), Call: func(context.Context, json.RawMessage) (json.RawMessage, error) {
		tools.Add(1)
		return json.RawMessage(`{"content":["failed"],"structured_content":{"data":null,"errors":[{"message":"field 'productnumbe' is not a column; did you mean productnumber?"}]},"is_error":false}`), nil
	}}
	var failed int
	result, err := RunWithTools(context.Background(), Spec{Version: 1, RunID: "graphql-errors", InputID: "input", Prompt: "Read products"},
		client, Tools{Capabilities: []Capability{tool}}, func(event Event) error {
			if event.Type == "tool.finished" && event.Error == "query_failed" && len(event.Data) > 0 {
				failed++
			}
			return nil
		})
	if err != nil || result.Status != "completed" || tools.Load() != 2 || failed != 2 {
		t.Fatalf("result=%+v err=%v tools=%d failed=%d", result, err, tools.Load(), failed)
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(bodies[2], "did you mean productnumber") || !strings.Contains(bodies[3], "already failed 2 times") {
		t.Fatalf("failures did not reach the model")
	}
}
