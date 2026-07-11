package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap/zapcore"

	"github.com/dosco/graphjin/auth/v3"
	"github.com/dosco/graphjin/serv/v3"
	_ "modernc.org/sqlite"
)

// newLiveCLIServer boots a real serv.HttpService against an in-memory SQLite
// database (the same wire stack production uses) and returns:
//   - a httptest.Server URL that serves /api/v1/graphql,
//   - the *sql.DB so the test can drive subscription updates,
//   - a cleanup function.
//
// Auth is disabled for the test (no_auth) so the CLI doesn't need a token; the
// auth-enforcement path is covered by serv/sse_live_test.go.
func newLiveCLIServer(t *testing.T) (string, *sql.DB, func()) {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "live.sqlite3")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	for _, stmt := range []string{
		`CREATE TABLE chats (id INTEGER PRIMARY KEY, body TEXT)`,
		`INSERT INTO chats (id, body) VALUES (1, 'first')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			t.Fatalf("exec %q: %v", stmt, err)
		}
	}

	conf := &serv.Config{}
	conf.Core.DBType = "sqlite"
	conf.Core.DisableAllowList = true
	conf.Core.SubsPollDuration = 200 * time.Millisecond
	conf.Serv.Production = false
	conf.Serv.MCP.Disable = true
	conf.Serv.ConfigPath = dir // valid base for initFS

	hs, err := serv.NewGraphJinService(conf,
		serv.OptionSetDB(db),
		serv.OptionSetLogOutput(zapcore.AddSync(io.Discard)),
	)
	if err != nil {
		db.Close()
		t.Fatalf("NewGraphJinService: %v", err)
	}

	noAuth := func(w http.ResponseWriter, r *http.Request) (context.Context, error) {
		return r.Context(), nil
	}

	mux := http.NewServeMux()
	mux.Handle("/api/v1/graphql", hs.GraphQL(auth.HandlerFunc(noAuth)))
	srv := httptest.NewServer(mux)

	cleanup := func() {
		srv.Close()
		db.Close()
	}
	return srv.URL, db, cleanup
}

// TestRunSubscribeStream_LivePath wires the CLI's runSubscribeStream into a
// real serv.HttpService backed by SQLite. Inserts rows over time and asserts
// that the CLI sees streamed payloads, then cancellation drains cleanly.
func TestRunSubscribeStream_LivePath(t *testing.T) {
	url, db, cleanup := newLiveCLIServer(t)
	defer cleanup()

	gqlURL, err := resolveGraphQLURL(url + "/api/v1/mcp/message")
	if err != nil {
		t.Fatalf("resolveGraphQLURL: %v", err)
	}

	insertDone := make(chan struct{})
	go func() {
		defer close(insertDone)
		for i := 2; i <= 6; i++ {
			time.Sleep(150 * time.Millisecond)
			if _, err := db.Exec(fmt.Sprintf("INSERT INTO chats (id, body) VALUES (%d, 'msg %d')", i, i)); err != nil {
				return
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var (
		mu       sync.Mutex
		emitted  []string
		emitChan = make(chan struct{}, 16)
	)
	emit := func(p json.RawMessage) error {
		mu.Lock()
		emitted = append(emitted, string(p))
		mu.Unlock()
		select {
		case emitChan <- struct{}{}:
		default:
		}
		return nil
	}

	streamErr := make(chan error, 1)
	go func() {
		streamErr <- runSubscribeStream(
			ctx,
			gqlURL,
			"",
			nil,
			"subscription { chats(order_by: { id: desc }, limit: 1) { id body } }",
			nil,
			"",
			emit,
			io.Discard,
		)
	}()

	wantFrames := 2
	for i := 0; i < wantFrames; i++ {
		select {
		case <-emitChan:
		case <-time.After(6 * time.Second):
			cancel()
			<-streamErr
			t.Fatalf("only %d frames received within timeout (want %d): %v", len(emitted), wantFrames, emitted)
		}
	}

	cancel()
	if err := <-streamErr; err != nil {
		t.Fatalf("runSubscribeStream returned error: %v", err)
	}
	<-insertDone

	mu.Lock()
	defer mu.Unlock()
	if len(emitted) < wantFrames {
		t.Fatalf("expected >=%d frames, got %d: %v", wantFrames, len(emitted), emitted)
	}

	// First frame should parse as a Payload with a real chats array.
	var payload struct {
		Data struct {
			Chats []struct {
				ID   int    `json:"id"`
				Body string `json:"body"`
			} `json:"chats"`
		} `json:"data"`
		Errors json.RawMessage `json:"errors,omitempty"`
	}
	if err := json.Unmarshal([]byte(emitted[0]), &payload); err != nil {
		t.Fatalf("decode frame %q: %v", emitted[0], err)
	}
	if len(payload.Errors) > 0 {
		t.Fatalf("frame had errors: %s", string(payload.Errors))
	}
	if len(payload.Data.Chats) == 0 {
		t.Fatalf("frame had no chats: %s", emitted[0])
	}
	if !strings.Contains(emitted[0], "body") {
		t.Errorf("frame missing 'body' field: %s", emitted[0])
	}
}
