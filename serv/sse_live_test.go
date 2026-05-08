package serv

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dosco/graphjin/auth/v3"
	core "github.com/dosco/graphjin/core/v3"
	"github.com/spf13/afero"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap"
	_ "modernc.org/sqlite"
)

// newLiveSSEHandler boots a real GraphJin core against a fresh SQLite file,
// wraps it in the same HTTP routing the production server uses, and returns
// the handler plus the open *sql.DB so tests can drive subscription updates.
//
// The auth config matches newSecuredTestHandler (X-Test-Auth: secret) so the
// test can talk to the protected /api/v1/graphql endpoint without bypassing
// auth in the server.
func newLiveSSEHandler(t *testing.T) (http.Handler, *sql.DB) {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "live.sqlite3")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	for _, stmt := range []string{
		`CREATE TABLE chats (id INTEGER PRIMARY KEY, body TEXT)`,
		`INSERT INTO chats (id, body) VALUES (1, 'first')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("exec %q: %v", stmt, err)
		}
	}

	logger := zap.NewNop()
	fs := newAferoFS(afero.NewMemMapFs(), "/")

	coreConf := core.Config{
		DBType:           "sqlite",
		Production:       false,
		DisableAllowList: true,
		SubsPollDuration: 200 * time.Millisecond,
	}
	gj, err := core.NewGraphJin(&coreConf, db,
		core.OptionSetFS(fs),
		core.OptionSetDatabases(map[string]*sql.DB{core.DefaultDBName: db}))
	if err != nil {
		t.Fatalf("create test GraphJin: %v", err)
	}

	svc := &graphjinService{
		gj:     gj,
		log:    logger.Sugar(),
		zlog:   logger,
		tracer: otel.Tracer("graphjin.com/serv/test"),
		conf: &Config{
			Serv: Serv{
				Auth: auth.Auth{
					Type: "header",
					Header: struct {
						Name   string
						Value  string
						Exists bool
					}{
						Name:  "X-Test-Auth",
						Value: "secret",
					},
				},
			},
		},
	}

	hs := &HttpService{}
	hs.Store(svc)

	router := http.NewServeMux()
	handler, err := routesHandler(hs, router, nil)
	if err != nil {
		t.Fatalf("routes handler: %v", err)
	}
	return handler, db
}

// TestSSESubscription_LivePath exercises the full subscription path end-to-end
// against SQLite: client opens an SSE POST to /api/v1/graphql, server runs a
// real core.Subscribe, polls, and pushes every change as `event: next` frames.
// The test inserts rows in a goroutine and asserts the wire frames arrive.
func TestSSESubscription_LivePath(t *testing.T) {
	handler, db := newLiveSSEHandler(t)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	// Insert rows ~100ms apart so the subscription poll (200ms) sees changes.
	insertDone := make(chan struct{})
	go func() {
		defer close(insertDone)
		for i := 2; i <= 5; i++ {
			time.Sleep(120 * time.Millisecond)
			if _, err := db.Exec(fmt.Sprintf("INSERT INTO chats (id, body) VALUES (%d, 'msg %d')", i, i)); err != nil {
				return
			}
		}
	}()

	body := []byte(`{"query":"subscription { chats(order_by: { id: desc }, limit: 1) { id body } }"}`)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/api/v1/graphql", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("X-Test-Auth", "secret")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		got, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body=%s", resp.StatusCode, string(got))
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)

	type frame struct {
		event string
		data  string
	}
	var (
		frames    []frame
		event     string
		dataLines []string
	)
	flush := func() {
		if event == "" && len(dataLines) == 0 {
			return
		}
		frames = append(frames, frame{event: event, data: strings.Join(dataLines, "\n")})
		event = ""
		dataLines = dataLines[:0]
	}

	// Read until we have at least 3 next-frames or context expires.
	gotNext := 0
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			flush()
			if gotNext >= 3 {
				break
			}
			if len(frames) > 0 && frames[len(frames)-1].event == "next" {
				gotNext++
				if gotNext >= 3 {
					break
				}
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		switch {
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			d := strings.TrimPrefix(line, "data:")
			d = strings.TrimPrefix(d, " ")
			dataLines = append(dataLines, d)
		}
	}
	cancel()
	<-insertDone

	if gotNext < 2 {
		t.Fatalf("expected at least 2 `next` frames over the wire, got %d (frames=%+v)", gotNext, frames)
	}

	// Verify the first frame's payload parses and carries chat data.
	var first frame
	for _, f := range frames {
		if f.event == "next" {
			first = f
			break
		}
	}
	var payload struct {
		Data struct {
			Chats []struct {
				ID   int    `json:"id"`
				Body string `json:"body"`
			} `json:"chats"`
		} `json:"data"`
		Errors json.RawMessage `json:"errors,omitempty"`
	}
	if err := json.Unmarshal([]byte(first.data), &payload); err != nil {
		t.Fatalf("decode first frame %q: %v", first.data, err)
	}
	if len(payload.Errors) > 0 {
		t.Fatalf("frame carried errors: %s", string(payload.Errors))
	}
	if len(payload.Data.Chats) == 0 {
		t.Fatalf("frame had no chats: %s", first.data)
	}
}

// TestSSESubscription_Unauthorized verifies that the SSE endpoint enforces the
// same auth as the JSON endpoint — a request without the auth header gets 401
// instead of an open stream.
func TestSSESubscription_Unauthorized(t *testing.T) {
	handler, _ := newLiveSSEHandler(t)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/graphql",
		strings.NewReader(`{"query":"subscription { chats { id } }"}`))
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}
