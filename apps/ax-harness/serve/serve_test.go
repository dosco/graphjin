package serve

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/open-neko/openneko/apps/ax-harness/harness"
)

const spec = `{"version":1,"run_id":"run-1","input_id":"input-1","prompt":"Find reference"}`

func startServer(t *testing.T, server *Server) string {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "serve.sock")
	listener, err := Listen(socket)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, listener) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	return socket
}

// client is the worker's end of one turn.
type client struct {
	conn   net.Conn
	enc    *json.Encoder
	mu     sync.Mutex
	frames chan Frame
	mcp    *io.PipeWriter
	onMCP  func(json.RawMessage)
}

func dial(t *testing.T, socket string, start Frame, setup ...func(*client)) *client {
	t.Helper()
	conn, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	c := &client{conn: conn, enc: json.NewEncoder(conn), frames: make(chan Frame, 256)}
	for _, fn := range setup {
		fn(c)
	}
	if err := c.send(start); err != nil {
		t.Fatal(err)
	}
	go func() {
		defer close(c.frames)
		scanner := bufio.NewScanner(conn)
		scanner.Buffer(make([]byte, 1<<20), maxFrameBytes)
		for scanner.Scan() {
			var frame Frame
			if json.Unmarshal(scanner.Bytes(), &frame) == nil {
				if frame.Type == "mcp" && c.mcp != nil {
					if c.onMCP != nil {
						c.onMCP(frame.Message)
					}
					_, _ = c.mcp.Write(append(append([]byte{}, frame.Message...), '\n'))
					continue
				}
				c.frames <- frame
			}
		}
	}()
	return c
}

func (c *client) send(frame Frame) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.enc.Encode(frame)
}

// serveTools answers the harness's tool client with an MCP server, as the
// worker does, carrying its traffic in mcp frames.
func (c *client) serveTools(t *testing.T, listed chan<- struct{}) {
	t.Helper()
	server := protocol.NewServer(&protocol.Implementation{Name: "worker", Version: "1"}, nil)
	protocol.AddTool(server, &protocol.Tool{Name: "memory_search", Description: "Search memory"},
		func(context.Context, *protocol.CallToolRequest, struct{}) (*protocol.CallToolResult, any, error) {
			return &protocol.CallToolResult{}, nil, nil
		})
	fromServe, toServer := io.Pipe()
	c.mcp = toServer
	c.onMCP = func(message json.RawMessage) {
		if strings.Contains(string(message), `"tools/list"`) {
			select {
			case listed <- struct{}{}:
			default:
			}
		}
	}
	writer := &lineWriter{emit: func(line []byte) error { return c.send(Frame{Type: "mcp", Message: line}) }}
	go func() {
		_, _ = server.Connect(context.Background(), &protocol.IOTransport{Reader: fromServe, Writer: writer}, nil)
	}()
}

func fixtureModel(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var keys []string
	var mu sync.Mutex
	responses := []string{`{"javascriptCode":"final('Find reference', {})"}`, `{"javascriptCode":"final('Report', {reference:'REF-42'});"}`, `Answer: REF-42`}
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	t.Cleanup(model.Close)
	return model, &keys
}

func TestServeRunsATurnWithServeHeldKeyAndSocketTools(t *testing.T) {
	model, keys := fixtureModel(t)
	env := map[string]string{"OPENAI_API_KEY": "serve-held-key", "PATH": "/usr/bin:/bin"}
	socket := startServer(t, &Server{Getenv: func(name string) string { return env[name] }})
	listed := make(chan struct{}, 1)

	c := dial(t, socket, Frame{
		Type: "start",
		Environ: []string{
			"OPENNEKO_MCP_ORG_ID=org-1", "OPENNEKO_MCP_THREAD_ID=thread-1",
			"HARNESS_MODEL_URL=" + model.URL, "HARNESS_MODEL=fixture",
			"HARNESS_MODEL_API_KEY=worker-supplied-key", "OPENAI_API_KEY=worker-supplied-key",
		},
		Spec:        json.RawMessage(spec),
		Bridge:      true,
		ModelKeyEnv: "OPENAI_API_KEY",
	}, func(c *client) { c.serveTools(t, listed) })

	var events []json.RawMessage
	var exit *Frame
	for frame := range c.frames {
		switch frame.Type {
		case "event":
			events = append(events, frame.Event)
		case "exit":
			f := frame
			exit = &f
		}
		if exit != nil {
			break
		}
	}
	if exit == nil || exit.Code == nil || *exit.Code != 0 {
		t.Fatalf("exit = %+v, events = %d", exit, len(events))
	}
	select {
	case <-listed:
	default:
		t.Fatal("the harness never listed the worker's tools over the socket")
	}
	last := string(events[len(events)-1])
	if !strings.Contains(last, `"run.finished"`) || !strings.Contains(last, "REF-42") {
		t.Fatalf("last event = %s", last)
	}
	for _, key := range *keys {
		if key != "Bearer serve-held-key" {
			t.Fatalf("model called with %q, want the key serve holds", key)
		}
	}
}

func TestServeCancelsOnHangupAndBoundsTurns(t *testing.T) {
	started := make(chan struct{}, 2)
	cancelled := make(chan struct{}, 2)
	var seen harness.Turn
	server := &Server{MaxTurns: 1, Run: func(ctx context.Context, turn harness.Turn) (int, error) {
		seen = turn
		started <- struct{}{}
		<-ctx.Done()
		cancelled <- struct{}{}
		return 1, ctx.Err()
	}}
	socket := startServer(t, server)
	first := dial(t, socket, Frame{Type: "start", Spec: json.RawMessage(spec), Environ: []string{"OPENNEKO_MCP_BRIDGE=/x", "ANTHROPIC_API_KEY=leak", "A=1"},
		ModelKeys: map[string]string{"MODEL_API_KEY": "turn-key", "DATABASE_PASSWORD": "x"}})
	<-started
	for _, entry := range seen.Environ {
		if strings.HasPrefix(entry, "OPENNEKO_MCP_BRIDGE=") || strings.HasPrefix(entry, "ANTHROPIC_API_KEY=") {
			t.Fatalf("serve passed %q through from the worker", entry)
		}
	}
	if got := seen.Secrets("DATABASE_URL"); got != "" {
		t.Fatalf("a non-key variable was readable as a secret: %q", got)
	}
	if got := seen.Secrets("HARNESS_MODEL_API_KEY"); got != "turn-key" {
		t.Fatalf("the turn's key = %q, want the one in model_keys", got)
	}
	if got := seen.Secrets("DATABASE_PASSWORD"); got != "" {
		t.Fatalf("a non-key variable was readable as a secret: %q", got)
	}

	second := dial(t, socket, Frame{Type: "start", Spec: json.RawMessage(spec)})
	frame := <-second.frames
	if frame.Type != "exit" || !strings.Contains(frame.Error, "turn limit") {
		t.Fatalf("second turn = %+v, want refused at the limit", frame)
	}

	_ = first.conn.Close()
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("hanging up did not cancel the turn")
	}
}
