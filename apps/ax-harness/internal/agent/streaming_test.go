package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ax "github.com/ax-llm/ax/packages/go"
)

func TestResponderDeltaArrivesBeforeProviderFinishes(t *testing.T) {
	var calls atomic.Int32
	firstObserved := make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		call := calls.Add(1)
		if call < 3 {
			w.Header().Set("Content-Type", "application/json")
			answer := `{"javascriptCode":"final('Answer',{})"}`
			_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":%q},"finish_reason":"stop"}]}`, answer)
			return
		}
		if call != 3 || !strings.Contains(string(body), `"stream":true`) {
			http.Error(w, "expected one streaming responder", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Answer: Hello \"}}]}\n\n")
		flusher.Flush()
		select {
		case <-firstObserved:
		case <-time.After(5 * time.Second):
			return
		}
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"world\"},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()
	client := ax.NewOpenAICompatibleClient(ax.Object("base_url", server.URL, "api_key", "synthetic", "model", "fixture"))
	var deltas []string
	result, err := RunWithTools(context.Background(), Spec{Version: 1, RunID: "stream", InputID: "input", Prompt: "Say hello", StreamResponses: true}, client, Tools{}, func(event Event) error {
		if event.Type != "answer.delta" {
			return nil
		}
		if event.Sequence != 0 {
			t.Errorf("provisional event advanced durable sequence: %+v", event)
		}
		var data struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(event.Data, &data); err != nil {
			t.Errorf("invalid delta: %v", err)
		}
		deltas = append(deltas, data.Text)
		once.Do(func() { close(firstObserved) })
		return nil
	})
	if err != nil || result.Status != "completed" || result.Answer != "Hello world" || len(deltas) < 2 || calls.Load() != 3 {
		t.Fatalf("result=%+v err=%v deltas=%q calls=%d", result, err, deltas, calls.Load())
	}
}

type malformedOnceClient struct {
	ax.AIClient
	calls int
}

func (c *malformedOnceClient) Chat(context.Context, map[string]ax.Value, map[string]ax.Value) (ax.Value, error) {
	c.calls++
	if c.calls == 1 {
		return nil, fmt.Errorf("Gemini finish reason was blocked: MALFORMED_FUNCTION_CALL")
	}
	return ax.Object("results", ax.Array()), nil
}

func TestMalformedFunctionCallIsSampledAgain(t *testing.T) {
	inner := &malformedOnceClient{}
	client := &streamingModeClient{AIClient: inner}
	if _, err := client.Chat(context.Background(), ax.Object(), ax.Object()); err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if inner.calls != 2 {
		t.Fatalf("calls = %d, want 2", inner.calls)
	}
}

type failedStream struct{ err error }

func (s failedStream) Next() bool      { return false }
func (s failedStream) Value() ax.Value { return nil }
func (s failedStream) Err() error      { return s.err }
func (s failedStream) Close() error    { return nil }

type malformedStreamClient struct {
	ax.AIClient
	opens    int
	failures int
}

func (c *malformedStreamClient) StreamEvents(context.Context, map[string]ax.Value, map[string]ax.Value) (ax.AxChatStream, error) {
	c.opens++
	if c.opens <= max(c.failures, 1) {
		return failedStream{err: fmt.Errorf("Gemini finish reason was blocked: MALFORMED_FUNCTION_CALL")}, nil
	}
	return &valueStream{values: []ax.Value{ax.Object("results", ax.Array())}}, nil
}

func TestMalformedStreamIsResampledUpToTheLimit(t *testing.T) {
	for _, tc := range []struct {
		failures  int
		recovered bool
	}{{maxResamples, true}, {maxResamples + 1, false}} {
		inner := &malformedStreamClient{failures: tc.failures}
		client := &streamingModeClient{AIClient: inner, enabled: true}
		stream, err := client.StreamEvents(context.Background(), ax.Object(), ax.Object())
		if err != nil {
			t.Fatalf("StreamEvents() error = %v", err)
		}
		if stream.Next() != tc.recovered {
			t.Fatalf("failures=%d: recovered = %v, want %v", tc.failures, !tc.recovered, tc.recovered)
		}
		if inner.opens != maxResamples+1 && !tc.recovered || tc.recovered && inner.opens != tc.failures+1 {
			t.Fatalf("failures=%d: opens = %d", tc.failures, inner.opens)
		}
	}
}

func TestMalformedStreamIsOpenedAgainBeforeAnyChunk(t *testing.T) {
	inner := &malformedStreamClient{}
	client := &streamingModeClient{AIClient: inner, enabled: true}
	stream, err := client.StreamEvents(context.Background(), ax.Object(), ax.Object())
	if err != nil {
		t.Fatalf("StreamEvents() error = %v", err)
	}
	if !stream.Next() || stream.Err() != nil {
		t.Fatalf("stream did not recover: %v", stream.Err())
	}
	if inner.opens != 2 {
		t.Fatalf("opens = %d, want 2", inner.opens)
	}
}

type thoughtThenMalformedClient struct {
	ax.AIClient
	opens int
}

func (c *thoughtThenMalformedClient) StreamEvents(context.Context, map[string]ax.Value, map[string]ax.Value) (ax.AxChatStream, error) {
	c.opens++
	thought := ax.Object("results", ax.MutableArray(ax.Object("thought", "planning")))
	if c.opens == 1 {
		return &thenFail{values: []ax.Value{thought}, err: fmt.Errorf("Gemini finish reason was blocked: MALFORMED_FUNCTION_CALL")}, nil
	}
	return &valueStream{values: []ax.Value{ax.Object("results", []ax.Value{ax.Object("content", "done")})}}, nil
}

type thenFail struct {
	values []ax.Value
	index  int
	err    error
}

func (s *thenFail) Next() bool {
	if s.index >= len(s.values) {
		return false
	}
	s.index++
	return true
}
func (s *thenFail) Value() ax.Value { return s.values[s.index-1] }
func (s *thenFail) Err() error {
	if s.index >= len(s.values) {
		return s.err
	}
	return nil
}
func (s *thenFail) Close() error { return nil }

func TestMalformedStreamAfterThoughtsIsOpenedAgain(t *testing.T) {
	inner := &thoughtThenMalformedClient{}
	client := &streamingModeClient{AIClient: inner, enabled: true}
	stream, _ := client.StreamEvents(context.Background(), ax.Object(), ax.Object())
	for stream.Next() {
	}
	if stream.Err() != nil || inner.opens != 2 {
		t.Fatalf("err = %v, opens = %d; want recovery on the second open", stream.Err(), inner.opens)
	}
}

func TestVisibleOutputReadsAxArrays(t *testing.T) {
	if !visibleOutput(ax.Object("results", ax.MutableArray(ax.Object("content", "answer")))) {
		t.Fatal("content in an Ax array is visible output")
	}
	if visibleOutput(ax.Object("results", ax.MutableArray(ax.Object("thought", "planning")))) {
		t.Fatal("a thought alone is not visible output")
	}
}

func TestDroppedConnectionIsResampled(t *testing.T) {
	if !resampleable(fmt.Errorf(`Network Error: Post "https://generativelanguage.googleapis.com/v1beta/models/x:streamGenerateContent": EOF`)) {
		t.Fatal("a dropped connection is safe to resample")
	}
	if resampleable(fmt.Errorf("invalid argument")) {
		t.Fatal("a request error is not resampled")
	}
}

type contentThenMalformedClient struct {
	ax.AIClient
	opens int
}

func (c *contentThenMalformedClient) StreamEvents(context.Context, map[string]ax.Value, map[string]ax.Value) (ax.AxChatStream, error) {
	c.opens++
	chunk := ax.Object("results", ax.MutableArray(ax.Object("content", `{"javascriptCode":`)))
	if c.opens == 1 {
		return &thenFail{values: []ax.Value{chunk}, err: fmt.Errorf("Gemini finish reason was blocked: MALFORMED_FUNCTION_CALL")}, nil
	}
	return &valueStream{values: []ax.Value{chunk, ax.Object("results", ax.MutableArray(ax.Object("content", `"final()"}`)))}}, nil
}

func TestStructuredStreamIsSampledAgainAfterPartialContent(t *testing.T) {
	inner := &contentThenMalformedClient{}
	client := &streamingModeClient{AIClient: inner, enabled: true}
	stream, err := client.StreamEvents(context.Background(), ax.Object("response_format", ax.Object()), ax.Object())
	if err != nil {
		t.Fatalf("StreamEvents() error = %v", err)
	}
	count := 0
	for stream.Next() {
		count++
	}
	if stream.Err() != nil || inner.opens != 2 || count != 2 {
		t.Fatalf("err = %v, opens = %d, chunks = %d; want the second sample's 2 chunks only", stream.Err(), inner.opens, count)
	}
}
