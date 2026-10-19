package serv

import (
	"bufio"
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsSSERequest(t *testing.T) {
	cases := []struct {
		accept string
		want   bool
	}{
		{"text/event-stream", true},
		{"application/json, text/event-stream", true},
		{"application/json", false},
		{"", false},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		if c.accept != "" {
			r.Header.Set("Accept", c.accept)
		}
		if got := isSSERequest(r); got != c.want {
			t.Fatalf("Accept=%q: got %v want %v", c.accept, got, c.want)
		}
	}
}

func TestSSEErrorAndCompleteEnvelope(t *testing.T) {
	// Drive the SSE error+complete writers directly to confirm framing without
	// requiring a live subscription. Real subscription smoke testing happens via
	// the graphjin engine in the secured-routes harness.
	rr := httptest.NewRecorder()
	flusher := &recordingFlusher{ResponseRecorder: rr}

	writeSSEError(flusher, flusher, errFakeSSE{})
	writeSSEComplete(flusher, flusher)

	body := rr.Body.String()
	if !strings.Contains(body, "event: next") {
		t.Fatalf("missing next event:\n%s", body)
	}
	if !strings.Contains(body, "event: complete") {
		t.Fatalf("missing complete event:\n%s", body)
	}

	scanner := bufio.NewScanner(bytes.NewReader(rr.Body.Bytes()))
	scanner.Buffer(make([]byte, 0, 4096), 4096)
	scanner.Split(bufio.ScanLines)
	var sawNext, sawData, sawComplete bool
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "event: next":
			sawNext = true
		case strings.HasPrefix(line, "data: ") && sawNext && !sawData:
			sawData = true
			if !strings.Contains(line, "fake sse error") {
				t.Fatalf("data line missing error message: %q", line)
			}
		case line == "event: complete":
			sawComplete = true
		}
	}
	if !sawNext || !sawData || !sawComplete {
		t.Fatalf("framing incomplete: next=%v data=%v complete=%v", sawNext, sawData, sawComplete)
	}
}

type errFakeSSE struct{}

func (errFakeSSE) Error() string { return "fake sse error" }

type recordingFlusher struct {
	*httptest.ResponseRecorder
	flushed int
}

func (r *recordingFlusher) Flush() { r.flushed++ }
