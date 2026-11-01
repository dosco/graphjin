package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/open-neko/openneko/apps/ax-harness/internal/agent"
)

const request = `{"version":1,"run_id":"run-1","input_id":"input-1","prompt":"Find reference"}`

func configure(t *testing.T, url string) {
	t.Helper()
	t.Setenv("HARNESS_MODEL_URL", url)
	t.Setenv("HARNESS_MODEL", "fixture")
	t.Setenv("HARNESS_MODEL_API_KEY", "synthetic-test-key")
}
func events(t *testing.T, b *bytes.Buffer) []agent.Event {
	t.Helper()
	var out []agent.Event
	dec := json.NewDecoder(b)
	for {
		var e agent.Event
		err := dec.Decode(&e)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, e)
	}
	terminal := 0
	for i, e := range out {
		if e.Sequence != uint64(i+1) || e.RunID != "run-1" || e.InputID != "input-1" {
			t.Fatalf("bad event: %+v", e)
		}
		if e.Type == "run.finished" {
			terminal++
		}
	}
	if len(out) < 2 || out[0].Type != "run.started" || out[len(out)-1].Type != "run.finished" || terminal != 1 {
		t.Fatalf("invalid lifecycle: %+v", out)
	}
	return out
}
func TestRunHTTP(t *testing.T) {
	calls := 0
	responses := []string{`{"javascriptCode":"final('Find reference', {})"}`, `{"javascriptCode":"final('Report reference', {reference:'REF-42'});"}`, `Answer: REF-42`}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		if r.Header.Get("Authorization") != "Bearer synthetic-test-key" {
			t.Error("missing auth")
		}
		if calls >= len(responses) {
			http.Error(w, "unexpected request", 400)
			return
		}
		content := responses[calls]
		calls++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": content}, "finish_reason": "stop"}}})
	}))
	defer server.Close()
	configure(t, server.URL)
	var out bytes.Buffer
	code, err := run(context.Background(), strings.NewReader(request), &out)
	if code != 0 || err != nil {
		t.Fatalf("code=%d err=%v output=%s", code, err, out.String())
	}
	raw := out.String()
	es := events(t, &out)
	for _, e := range es {
		if (e.Type == "model.request.started" || e.Type == "model.request.finished") && e.Name != "fixture" {
			t.Fatalf("model receipt lost host-pinned identity: %+v", e)
		}
	}
	if calls != 3 || es[len(es)-1].Result.Answer != "REF-42" {
		t.Fatalf("calls=%d events=%+v", calls, es)
	}
	if strings.Contains(raw, "synthetic-test-key") || strings.Contains(raw, "Find reference") {
		t.Fatal("content leaked into lifecycle metadata")
	}
	if len(es) < 4 {
		t.Fatal("missing Ax lifecycle spans")
	}
}

func TestRunCancellationHTTP(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	closed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		cancel()
		<-r.Context().Done()
		close(closed)
	}))
	defer server.Close()
	configure(t, server.URL)
	var out bytes.Buffer
	code, err := run(ctx, strings.NewReader(request), &out)
	if code != 1 || err != nil {
		t.Fatalf("code=%d err=%v", code, err)
	}
	es := events(t, &out)
	if es[len(es)-1].Result.Status != "cancelled" {
		t.Fatal("cancellation reported incorrectly")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("HTTP request remained open")
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("sink closed") }
func TestRejectInputAndFailedSink(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("unadmitted run contacted model")
		http.Error(w, "unexpected", 400)
	}))
	defer server.Close()
	configure(t, server.URL)
	for _, input := range []string{request + request, strings.Replace(request, `"version":1`, `"version":2`, 1), strings.Replace(request, `"version":1`, `"endpoint":"http://other","version":1`, 1), request + strings.Repeat(" ", agent.MaxInputBytes)} {
		var out bytes.Buffer
		code, err := run(context.Background(), strings.NewReader(input), &out)
		if code == 0 || err == nil || out.Len() != 0 {
			t.Fatal(fmt.Sprintf("invalid input admitted code=%d", code))
		}
	}
	code, err := run(context.Background(), strings.NewReader(request), brokenWriter{})
	if code != 1 || err == nil {
		t.Fatal("sink failure ignored")
	}
}

func TestModelFailureHasOneRedactedTerminal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "synthetic-private-upstream-detail", http.StatusUnauthorized)
	}))
	defer server.Close()
	configure(t, server.URL)
	var out bytes.Buffer
	code, err := run(context.Background(), strings.NewReader(request), &out)
	if code != 1 || err != nil {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if strings.Contains(out.String(), "synthetic-private") {
		t.Fatal("upstream error leaked")
	}
	es := events(t, &out)
	result := es[len(es)-1].Result
	if result.Status != "failed" || result.Code != "model_http_401" {
		t.Fatalf("unexpected result %+v", result)
	}
}
