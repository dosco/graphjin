// Package serve runs agent turns for the worker over a local socket. The
// openneko binary's serve role calls Main.
//
// One connection is one turn. Both sides write newline-delimited JSON frames:
//
//	worker → serve  {"type":"start","environ":[...],"spec":{...},"bridge":true,"model_key_env":"MODEL_API_KEY","model_keys":{...}}
//	worker → serve  {"type":"mcp","message":{...}}    the worker's tool server
//	serve → worker  {"type":"event","event":{...}}    a harness lifecycle event
//	serve → worker  {"type":"mcp","message":{...}}    the harness's tool client
//	serve → worker  {"type":"exit","code":0,"error":""}
//
// A connection that opens with a fetch frame is one model call instead: see
// gateway.go. One that opens with a config frame checks or applies an edited
// GraphJin config: see config.go.
//
// Closing the connection cancels the turn. Serve holds the model keys for
// the turn: the worker names the variable and sends the org's key in
// model_keys, or serve reads it from its own environment. A key never goes
// into the run's environment, so the run's tools never see it.
package serve

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"

	"github.com/open-neko/openneko/apps/ax-harness/harness"
)

// DefaultSocket is where serve listens unless told otherwise.
const DefaultSocket = "/run/openneko/serve.sock"

const (
	maxFrameBytes = 32 << 20
	defaultTurns  = 32
)

var keyEnvName = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,126}_API_KEY$`)

// Frame is one line of the protocol.
type Frame struct {
	Type        string            `json:"type"`
	Environ     []string          `json:"environ,omitempty"`
	Spec        json.RawMessage   `json:"spec,omitempty"`
	Bridge      bool              `json:"bridge,omitempty"`
	ModelKeyEnv string            `json:"model_key_env,omitempty"`
	ModelKeys   map[string]string `json:"model_keys,omitempty"`
	Event       json.RawMessage   `json:"event,omitempty"`
	Message     json.RawMessage   `json:"message,omitempty"`
	Code        *int              `json:"code,omitempty"`
	Error       string            `json:"error,omitempty"`
	// GraphJin, on a start frame, lets the turn ask the GraphJin agent that
	// serve hosts, as the user the token names.
	GraphJin *GraphJinIdentity `json:"graphjin,omitempty"`
	// Request, Status, Headers and Data carry a model call through the
	// gateway.
	Request *FetchRequest     `json:"request,omitempty"`
	Status  int               `json:"status,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Data    []byte            `json:"data,omitempty"`
	// Config and Result carry an edited GraphJin config and GraphJin's
	// answer.
	Config *ConfigRequest  `json:"config,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
}

// GraphJinIdentity is who a turn asks GraphJin as: the JWT the host minted
// for the run, which GraphJin verifies, and the thread whose earlier data
// questions go along as history.
type GraphJinIdentity struct {
	Token    string `json:"token"`
	OrgID    string `json:"org_id"`
	ThreadID string `json:"thread_id,omitempty"`
}

// AskRequest is one data question for GraphJin's agent.
type AskRequest struct {
	Instruction string
	MaxSteps    int
	History     []HistoryTurn
	// OnStep receives each step the agent takes.
	OnStep func(text string)
}

// HistoryTurn is one earlier question or answer in a thread.
type HistoryTurn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Status  string `json:"status,omitempty"`
}

// AskResult is GraphJin's agent response and what it spent.
type AskResult struct {
	Response json.RawMessage
	Answer   string
	Status   string
	Tokens   int64
}

// Asker asks the GraphJin agent serve hosts, verifying token as GraphJin's
// /api/v1/agent route would.
type Asker func(ctx context.Context, token string, req AskRequest) (AskResult, error)

// Options configure Main.
type Options struct {
	// Ask, when set, is the GraphJin agent this serve hosts.
	Ask Asker
	// EditConfig, when set, checks and applies edits to the config of the
	// GraphJin this serve hosts.
	EditConfig ConfigEditor
}

// Server runs turns for connections on a listener.
type Server struct {
	// Getenv reads the host's environment for model keys and PATH.
	Getenv func(string) string
	// MaxTurns bounds the turns that run at once; more are refused.
	MaxTurns int
	// Run runs one turn; tests replace it.
	Run func(context.Context, harness.Turn) (int, error)
	// Ask is the GraphJin agent serve hosts; nil leaves turns on the bridge.
	Ask Asker
	// EditConfig edits the hosted GraphJin's config; nil refuses config
	// frames.
	EditConfig ConfigEditor
	// Client makes the gateway's model calls; nil uses a default client.
	Client *http.Client

	mu      sync.Mutex
	running int
	wg      sync.WaitGroup
	history historyStore
}

// Main parses args and serves until SIGINT or SIGTERM.
func Main(args []string) {
	MainWith(args, Options{})
}

// MainWith is Main for a host that gives serve more to offer turns.
func MainWith(args []string, opts Options) {
	flags := flag.NewFlagSet("serve", flag.ExitOnError)
	socket := flags.String("socket", envOr("OPENNEKO_SERVE_SOCKET", DefaultSocket), "Unix socket to listen on")
	_ = flags.Parse(args)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// Web and the worker share the run folders through a group, so files
	// serve writes stay group-writable.
	syscall.Umask(0o007)
	listener, err := Listen(*socket)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	fmt.Fprintf(os.Stderr, "openneko serve: listening on %s\n", *socket)
	// A stop signal ends the process at once. In-flight turns end with it;
	// their callers see the socket close and fail the run.
	go func() {
		<-ctx.Done()
		_ = listener.Close()
		fmt.Fprintln(os.Stderr, "openneko serve: stopped")
		os.Exit(0)
	}()
	server := &Server{Getenv: os.Getenv, MaxTurns: intEnv("OPENNEKO_SERVE_MAX_TURNS", defaultTurns), Ask: opts.Ask, EditConfig: opts.EditConfig}
	if err := server.Serve(ctx, listener); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// Listen replaces a stale socket at path and listens on it, readable and
// writable by the owner and group only.
func Listen(path string) (net.Listener, error) {
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSocket != 0 {
		_ = os.Remove(path)
	}
	old := syscall.Umask(0o117)
	listener, err := net.Listen("unix", path)
	syscall.Umask(old)
	if err != nil {
		return nil, fmt.Errorf("serve socket unavailable: %w", err)
	}
	return listener, nil
}

// Serve accepts turns until ctx ends, then cancels the running turns and
// waits for them.
func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	if s.Run == nil {
		s.Run = harness.RunTurn
	}
	if s.Getenv == nil {
		s.Getenv = func(string) string { return "" }
	}
	if s.MaxTurns <= 0 {
		s.MaxTurns = defaultTurns
	}
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			s.wg.Wait()
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handle(ctx, conn)
		}()
	}
}

func (s *Server) acquire() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running >= s.MaxTurns {
		return false
	}
	s.running++
	return true
}

func (s *Server) release() {
	s.mu.Lock()
	s.running--
	s.mu.Unlock()
}

func (s *Server) handle(parent context.Context, conn net.Conn) {
	defer conn.Close()
	out := &frameWriter{enc: json.NewEncoder(conn)}
	reader := bufio.NewReaderSize(conn, 64<<10)
	exit := func(code int, err error) {
		frame := Frame{Type: "exit", Code: &code}
		if err != nil {
			frame.Error = err.Error()
		}
		_ = out.write(frame)
	}
	start, err := readFrame(reader)
	if err == nil && start.Type == "fetch" {
		s.fetch(parent, conn, reader, out, start.Request)
		return
	}
	if err == nil && start.Type == "config" {
		s.editConfig(parent, out, start.Config)
		return
	}
	if err != nil || start.Type != "start" || len(start.Spec) == 0 {
		exit(2, errors.New("expected a start frame with a run specification"))
		return
	}
	if !s.acquire() {
		exit(2, errors.New("serve is at its turn limit"))
		return
	}
	defer s.release()
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	turn := harness.Turn{
		Environ: s.environ(start.Environ),
		Secrets: s.secrets(start.ModelKeyEnv, start.ModelKeys),
		In:      bytes.NewReader(start.Spec),
		Out:     &lineWriter{emit: func(line []byte) error { return out.write(Frame{Type: "event", Event: line}) }},
	}
	if s.Ask != nil && start.GraphJin != nil && start.GraphJin.Token != "" {
		turn.GraphJinAsk = s.graphJinAsk(*start.GraphJin)
	}
	var toHarness *io.PipeWriter
	if start.Bridge {
		var fromWorker *io.PipeReader
		fromWorker, toHarness = io.Pipe()
		turn.Bridge = &bridgeStream{
			PipeReader: fromWorker,
			out:        &lineWriter{emit: func(line []byte) error { return out.write(Frame{Type: "mcp", Message: line}) }},
		}
	}
	// The worker's frames after start are tool traffic; its hang-up cancels.
	go func() {
		defer cancel()
		for {
			frame, err := readFrame(reader)
			if err != nil {
				if toHarness != nil {
					_ = toHarness.CloseWithError(io.ErrClosedPipe)
				}
				return
			}
			if frame.Type == "mcp" && toHarness != nil && len(frame.Message) > 0 {
				if _, err := toHarness.Write(append(append([]byte{}, frame.Message...), '\n')); err != nil {
					return
				}
			}
		}
	}()
	code, runErr := s.Run(ctx, turn)
	exit(code, runErr)
}

// environ drops what the worker may not set: model credentials come from
// serve, and the tool bridge runs over the socket.
func (s *Server) environ(in []string) []string {
	out := make([]string, 0, len(in)+1)
	hasPath := false
	for _, entry := range in {
		name, _, ok := strings.Cut(entry, "=")
		if !ok || name == "" || name == "OPENNEKO_MCP_BRIDGE" || keyEnvName.MatchString(name) {
			continue
		}
		hasPath = hasPath || name == "PATH"
		out = append(out, entry)
	}
	if path := s.Getenv("PATH"); !hasPath && path != "" {
		out = append(out, "PATH="+path)
	}
	return out
}

// secrets reads model keys from the turn's keys, then serve's environment.
// HARNESS_MODEL_API_KEY reads the variable the worker named; route keys read
// their own names.
func (s *Server) secrets(modelKeyEnv string, keys map[string]string) func(string) string {
	return func(name string) string {
		if name == "HARNESS_MODEL_API_KEY" {
			if modelKeyEnv == "" {
				modelKeyEnv = "MODEL_API_KEY"
			}
			name = modelKeyEnv
		}
		if !keyEnvName.MatchString(name) {
			return ""
		}
		if value := keys[name]; value != "" {
			return value
		}
		return s.Getenv(name)
	}
}

func readFrame(r *bufio.Reader) (Frame, error) {
	var line []byte
	for {
		chunk, isPrefix, err := r.ReadLine()
		if err != nil {
			return Frame{}, err
		}
		line = append(line, chunk...)
		if len(line) > maxFrameBytes {
			return Frame{}, errors.New("frame too large")
		}
		if !isPrefix {
			break
		}
	}
	var frame Frame
	if err := json.Unmarshal(line, &frame); err != nil {
		return Frame{}, err
	}
	return frame, nil
}

// frameWriter serialises frames from the turn and its tool client.
type frameWriter struct {
	mu  sync.Mutex
	enc *json.Encoder
}

func (w *frameWriter) write(frame Frame) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.enc.Encode(frame)
}

// lineWriter passes each complete JSON line written to it to emit.
type lineWriter struct {
	mu   sync.Mutex
	buf  []byte
	emit func([]byte) error
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			return len(p), nil
		}
		line := bytes.TrimSpace(w.buf[:i])
		w.buf = w.buf[i+1:]
		if len(line) == 0 {
			continue
		}
		if err := w.emit(append([]byte{}, line...)); err != nil {
			return 0, err
		}
	}
}

func (w *lineWriter) Close() error { return nil }

// bridgeStream is the harness's end of the tool bridge.
type bridgeStream struct {
	*io.PipeReader
	out *lineWriter
}

func (b *bridgeStream) Write(p []byte) (int, error) { return b.out.Write(p) }
func (b *bridgeStream) Close() error                { return b.PipeReader.Close() }

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func intEnv(name string, fallback int) int {
	var n int
	if _, err := fmt.Sscanf(os.Getenv(name), "%d", &n); err != nil || n <= 0 {
		return fallback
	}
	return n
}
