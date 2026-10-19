package serve

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// A turn asks the GraphJin agent serve hosts through the native tool: the
// host's token goes to Ask, GraphJin's steps reach the turn's events, its
// tokens are reported, and the thread's next turn carries the history.
func TestServeTurnAsksHostedGraphJin(t *testing.T) {
	var mu sync.Mutex
	depth := map[string]int{}
	responses := []string{
		`{"javascriptCode":"final('Ask the data agent',{});"}`,
		`{"javascriptCode":"const r=mcp_neko_graphjin_agent_ask({instruction:'revenue'}); console.log(JSON.stringify(r));"}`,
		`{"javascriptCode":"final('Report',{});"}`,
		`Answer: 1200`,
	}
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		turn := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 2)[0]
		mu.Lock()
		n := depth[turn]
		depth[turn]++
		mu.Unlock()
		if n >= len(responses) {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": responses[n]}, "finish_reason": "stop"}}})
	}))
	defer model.Close()

	var asks []AskRequest
	var tokens []string
	ask := func(_ context.Context, token string, req AskRequest) (AskResult, error) {
		mu.Lock()
		asks = append(asks, req)
		tokens = append(tokens, token)
		mu.Unlock()
		req.OnStep("Data agent: query catalog")
		return AskResult{Response: json.RawMessage(`{"status":"answered","answer":"Revenue was 1200."}`), Answer: "Revenue was 1200.", Status: "answered", Tokens: 700}, nil
	}
	env := map[string]string{"MODEL_API_KEY": "k"}
	socket := startServer(t, &Server{Getenv: func(name string) string { return env[name] }, Ask: ask})

	run := func(path string) (events []string) {
		c := dial(t, socket, Frame{
			Type: "start", Spec: json.RawMessage(spec),
			Environ:  []string{"OPENNEKO_MCP_ORG_ID=org-1", "OPENNEKO_MCP_THREAD_ID=thread-1", "HARNESS_MODEL_URL=" + model.URL + "/" + path, "HARNESS_MODEL=fixture"},
			GraphJin: &GraphJinIdentity{Token: "run-jwt", OrgID: "org-1", ThreadID: "thread-1"},
		})
		for frame := range c.frames {
			if frame.Type == "event" {
				events = append(events, string(frame.Event))
			}
			if frame.Type == "exit" {
				if frame.Code == nil || *frame.Code != 0 {
					t.Fatalf("turn failed: %+v", frame)
				}
				break
			}
		}
		_ = c.conn.Close()
		return events
	}

	first := strings.Join(run("a"), "\n")
	if len(asks) != 1 || tokens[0] != "run-jwt" || asks[0].Instruction != "revenue" || asks[0].History != nil {
		t.Fatalf("asks=%+v tokens=%v", asks, tokens)
	}
	if !strings.Contains(first, `"actor.step"`) || !strings.Contains(first, "Data agent: query catalog") {
		t.Fatal("GraphJin's step never reached the turn's events")
	}
	if !strings.Contains(first, `"delegated_tokens":700`) {
		t.Fatal("the turn's usage does not count GraphJin's tokens")
	}

	run("b")
	if len(asks) != 2 || len(asks[1].History) != 2 || asks[1].History[1].Content != "Revenue was 1200." {
		t.Fatalf("the follow-up lost the thread's history: %+v", asks[1].History)
	}
}
