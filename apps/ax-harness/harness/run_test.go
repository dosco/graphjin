package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fixtureModel answers one turn and records the key it was called with.
func fixtureModel(t *testing.T, answer string) (*httptest.Server, *[]string) {
	t.Helper()
	var keys []string
	var mu sync.Mutex
	responses := []string{`{"javascriptCode":"final('Find reference', {})"}`, `{"javascriptCode":"final('Report', {reference:'` + answer + `'});"}`, `Answer: ` + answer}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		keys = append(keys, r.Header.Get("Authorization"))
		call := len(keys) - 1
		mu.Unlock()
		if call >= len(responses) {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": responses[call]}, "finish_reason": "stop"}}})
	}))
	t.Cleanup(server.Close)
	return server, &keys
}

func TestRunIsolatesConcurrentTurnsByEnviron(t *testing.T) {
	t.Setenv("HARNESS_MODEL_API_KEY", "process-key-must-not-be-read")
	const spec = `{"version":1,"run_id":"run-1","input_id":"input-1","prompt":"Find reference"}`
	type turn struct {
		answer string
		keys   *[]string
		out    bytes.Buffer
		code   int
		err    error
	}
	turns := []*turn{{answer: "REF-A"}, {answer: "REF-B"}}
	var wg sync.WaitGroup
	for i, tr := range turns {
		server, keys := fixtureModel(t, tr.answer)
		tr.keys = keys
		environ := []string{
			"OPENNEKO_MCP_ORG_ID=org-1", "OPENNEKO_MCP_THREAD_ID=thread-" + tr.answer,
			"HARNESS_MODEL_URL=" + server.URL, "HARNESS_MODEL=fixture", "HARNESS_MODEL_API_KEY=key-" + string(rune('a'+i)),
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			tr.code, tr.err = Run(context.Background(), environ, strings.NewReader(spec), &tr.out)
		}()
	}
	wg.Wait()
	for i, tr := range turns {
		if tr.code != 0 || tr.err != nil {
			t.Fatalf("turn %d: code=%d err=%v out=%s", i, tr.code, tr.err, tr.out.String())
		}
		if !strings.Contains(tr.out.String(), tr.answer) {
			t.Fatalf("turn %d lost its answer: %s", i, tr.out.String())
		}
		want := "Bearer key-" + string(rune('a'+i))
		for _, key := range *tr.keys {
			if key != want {
				t.Fatalf("turn %d used key %q, want %q", i, key, want)
			}
		}
	}
}

func TestRunRefusesMissingScopeWithoutExiting(t *testing.T) {
	code, err := Run(context.Background(), nil, strings.NewReader("{}"), io.Discard)
	if code != 2 || err == nil {
		t.Fatalf("code=%d err=%v", code, err)
	}
}
