package serve

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestServeLoad runs many turns at once through one serve, each against the
// model as its own conversation, and checks every turn answers and nothing
// leaks once they end.
func TestServeLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("load test")
	}
	const turns = 64
	var calls atomic.Int64
	var mu sync.Mutex
	depth := map[string]int{}
	responses := []string{`{"javascriptCode":"final('Find reference', {})"}`, `{"javascriptCode":"final('Report', {reference:'REF-42'});"}`, `Answer: REF-42`}
	// Each turn has its own URL path, so its calls form one conversation.
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		calls.Add(1)
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
	before := runtime.NumGoroutine()
	env := map[string]string{"MODEL_API_KEY": "k"}
	socket := startServer(t, &Server{Getenv: func(name string) string { return env[name] }, MaxTurns: turns})

	started := time.Now()
	var wg sync.WaitGroup
	var failed atomic.Int64
	var lastFinished atomic.Value
	for i := 0; i < turns; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c := dial(t, socket, Frame{Type: "start", Spec: json.RawMessage(spec), Environ: []string{
				"OPENNEKO_MCP_ORG_ID=org-1", "OPENNEKO_MCP_THREAD_ID=thread-load",
				"HARNESS_MODEL_URL=" + model.URL + "/t" + strconv.Itoa(i), "HARNESS_MODEL=fixture",
			}})
			answered := false
			for frame := range c.frames {
				if frame.Type == "event" && strings.Contains(string(frame.Event), `"run.finished"`) {
					lastFinished.Store(string(frame.Event))
				}
				if frame.Type == "event" && strings.Contains(string(frame.Event), `"run.finished"`) && strings.Contains(string(frame.Event), "REF-42") {
					answered = true
				}
				if frame.Type == "exit" {
					if frame.Code == nil || *frame.Code != 0 || !answered {
						failed.Add(1)
						t.Errorf("turn failed: code=%v answered=%v", *frame.Code, answered)
					}
					break
				}
			}
			_ = c.conn.Close()
		}(i)
	}
	wg.Wait()
	elapsed := time.Since(started)
	t.Logf("%d turns, %d model calls, %s (%s a turn at %d at once)", turns, calls.Load(), elapsed, elapsed/turns, turns)
	if failed.Load() > 0 {
		t.Logf("last finish: %v", lastFinished.Load())
		t.Fatalf("%d of %d turns failed", failed.Load(), turns)
	}
	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > before+10 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > before+10 {
		t.Fatalf("goroutines grew from %d to %d after every turn ended", before, n)
	}
}
