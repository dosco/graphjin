package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
	axgoja "github.com/ax-llm/ax/packages/go/runtime/goja"
)

func TestDistillerToolEvidenceReachesExecutorWithoutAnotherRead(t *testing.T) {
	var modelCalls, reads, checks atomic.Int32
	bulk := strings.Repeat("BULK-OBSERVATION-", 7000)
	var executorRequest string
	answers := []string{
		`{"javascriptCode":"const ref=seed({}); if (!ref.reference || ref.result_bytes < 110000) throw new Error('missing run reference'); const row=harnessSavedOperation(1); final('Use the row', {token: row.result.token});"}`,
		`{"javascriptCode":"const check=verify({token: harnessEvidence.token}); final('Report the row', {check});"}`,
		`{"answer":"REF-42"}`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		index := int(modelCalls.Add(1)) - 1
		body, _ := io.ReadAll(r.Body)
		if index == 1 {
			executorRequest = string(body)
		}
		if index >= len(answers) {
			http.Error(w, "unexpected model call", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ax.Object("choices", ax.Array(ax.Object("message", ax.Object("role", "assistant", "content", answers[index]), "finish_reason", "stop"))))
	}))
	defer server.Close()
	client := ax.NewOpenAICompatibleClient(ax.Object("base_url", server.URL, "api_key", "synthetic", "model", "fixture"))
	read := Capability{Name: "seed", Version: "1", Origin: "fixture", Effect: "read", Description: "Read one row.",
		InputSchema: json.RawMessage(`{"type":"object"}`), Call: func(context.Context, json.RawMessage) (json.RawMessage, error) {
			reads.Add(1)
			result, _ := json.Marshal(ax.Object("token", "REF-42", "bulk", bulk))
			return result, nil
		}}
	verify := Capability{Name: "verify", Version: "1", Origin: "fixture", Effect: "read", Description: "Check the row.",
		InputSchema: json.RawMessage(`{"type":"object","required":["token"],"properties":{"token":{"type":"string"}}}`),
		Call: func(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
			checks.Add(1)
			if string(raw) != `{"token":"REF-42"}` {
				t.Errorf("executor lost distilled evidence: %s", raw)
			}
			return json.RawMessage(`{"ok":true}`), nil
		}}
	var retrieved []Event
	result, err := RunWithTools(context.Background(), Spec{Version: 1, RunID: "handoff", InputID: "input", Prompt: "Use and verify the row"},
		client, Tools{Capabilities: []Capability{read, verify}}, func(e Event) error {
			if e.Type == "observation.retrieved" {
				retrieved = append(retrieved, e)
			}
			return nil
		})
	if err != nil || result.Status != "completed" || reads.Load() != 1 || checks.Load() != 1 || modelCalls.Load() != 3 {
		t.Fatalf("result=%+v err=%v reads=%d checks=%d model=%d", result, err, reads.Load(), checks.Load(), modelCalls.Load())
	}
	if strings.Contains(executorRequest, "BULK-OBSERVATION-") {
		t.Fatal("bulk observation leaked into the executor model request")
	}
	if len(retrieved) != 1 || retrieved[0].OperationID != 1 || len(retrieved[0].Data) != 0 {
		t.Fatalf("saved observation read receipt = %+v", retrieved)
	}
}

func TestHandoffEvidenceStaysOutOfRuntimeSnapshots(t *testing.T) {
	runtime := &handoffRuntime{Runtime: axgoja.NewRuntime()}
	session, err := runtime.CreateSession(map[string]ax.Value{"inputs": map[string]ax.Value{"question": "read"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	session.Execute(`final("continue", {token:"SECRET-REF-42"});`, nil)
	session.PatchGlobals(map[string]ax.Value{"version": 1, "bindings": map[string]ax.Value{}}, nil)
	used := session.Execute(`final("done", {token:harnessEvidence.token});`, nil)
	if !strings.Contains(toJSON(used), "SECRET-REF-42") {
		t.Fatalf("executor could not read run-local evidence: %v", used)
	}
	for _, visible := range []ax.Value{session.Inspect(nil), session.SnapshotGlobals(nil)} {
		if strings.Contains(toJSON(visible), "SECRET-REF-42") {
			t.Fatalf("runtime evidence leaked into snapshot: %v", visible)
		}
	}
}

func TestLargeSavedValueStaysOutOfRuntimeProjection(t *testing.T) {
	runtime := &handoffRuntime{Runtime: axgoja.NewRuntime()}
	session, err := runtime.CreateSession(map[string]ax.Value{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	large := strings.Repeat("PRIVATE-RESULT-", 5000)
	encoded, _ := json.Marshal(large)
	session.Execute("const saved = {content:"+string(encoded)+"};", nil)
	for _, projection := range []ax.Value{session.Inspect(nil), session.SnapshotGlobals(nil)} {
		visible := toJSON(projection)
		if strings.Contains(visible, "PRIVATE-RESULT-") || !strings.Contains(visible, "runtime value omitted") || len(visible) > 16_384 {
			t.Fatalf("large local value escaped bounded projection: bytes=%d", len(visible))
		}
	}
	used := session.Execute(`final("verified", {length:saved.content.length});`, nil)
	if !strings.Contains(toJSON(used), `"length":75000`) {
		t.Fatalf("bounded projection changed live JS value: %v", used)
	}
}

func TestRuntimeProjectionBoundsAccumulatedSmallValues(t *testing.T) {
	runtime := &handoffRuntime{Runtime: axgoja.NewRuntime()}
	session, err := runtime.CreateSession(map[string]ax.Value{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	var code strings.Builder
	for i := 0; i < 40; i++ {
		code.WriteString(fmt.Sprintf("const item%d = %q;\n", i, strings.Repeat("X", 1000)))
	}
	code.WriteString(`console.log("CUMULATIVE-LOG-MARKER");`)
	session.Execute(code.String(), nil)
	for _, projection := range []ax.Value{session.Inspect(nil), session.SnapshotGlobals(nil)} {
		visible := toJSON(projection)
		if len(visible) > maxVisibleRuntimeProjectionBytes || !strings.Contains(visible, "runtime binding omitted") ||
			strings.Contains(visible, "CUMULATIVE-LOG-MARKER") {
			t.Fatalf("accumulated runtime values escaped total projection bound: bytes=%d", len(visible))
		}
	}
}

func toJSON(value ax.Value) string {
	data, _ := json.Marshal(value)
	return string(data)
}
