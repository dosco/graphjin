package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/open-neko/openneko/apps/ax-harness/internal/agent"
)

// These tests run OpenNeko's real stdio bridge against a fixture broker.
// Set OPENNEKO_BRIDGE_TEST=1 after installing apps/worker dependencies.
func startBridge(t *testing.T, broker string) ([]agent.Capability, *exec.Cmd, func() error) {
	t.Helper()
	if os.Getenv("OPENNEKO_BRIDGE_TEST") != "1" {
		t.Skip("set OPENNEKO_BRIDGE_TEST=1 to run the OpenNeko bridge")
	}
	worker, err := filepath.Abs("../../../worker")
	if err != nil {
		t.Fatal(err)
	}
	bridge := filepath.Join(worker, "src/agent-sandbox/mcp-bridge.ts")
	if _, err := os.Stat(bridge); err != nil {
		t.Skipf("OpenNeko bridge unavailable: %v", err)
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node unavailable")
	}
	cmd := exec.Command("node", "--import", "tsx", bridge, "neko_memory")
	cmd.Dir = worker
	cmd.Env = append(os.Environ(),
		"OPENNEKO_BROKER_URL="+broker,
		"OPENNEKO_BROKER_TOKEN=fixture-broker-token",
		"OPENNEKO_MCP_MEMORY_READ_ONLY=1",
		"OPENNEKO_MCP_MODE=work",
		"OPENNEKO_MCP_ORG_ID=org-fixture",
		"OPENNEKO_MCP_THREAD_ID=thread-fixture",
		"OPENNEKO_MCP_RUN_ID=run-fixture",
		"OPENNEKO_MCP_SKILLS_ROOT="+t.TempDir(),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	caps, closeBridge, err := Connect(ctx, cmd, "mcp_neko_", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = closeBridge() })
	if len(caps) != 1 || caps[0].Name != "mcp_neko_memory_search" {
		t.Fatalf("bridge exposed unexpected tools: %+v", caps)
	}
	return caps, cmd, closeBridge
}

func TestOpenNekoBridgeRead(t *testing.T) {
	broker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-broker-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil || body["orgId"] != "org-fixture" || body["query"] != "find policy" {
			http.Error(w, "wrong scope or query", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"memory-1","text":"Policy"}]`))
	}))
	defer broker.Close()
	caps, cmd, closeBridge := startBridge(t, broker.URL)
	result, err := caps[0].Call(context.Background(), json.RawMessage(`{"query":"find policy"}`))
	if err != nil || !strings.Contains(string(result), "memory-1") {
		t.Fatalf("bridge read failed: result=%s err=%v", result, err)
	}
	if err := closeBridge(); err != nil || cmd.ProcessState == nil {
		t.Fatalf("bridge did not exit on close: err=%v state=%v", err, cmd.ProcessState)
	}
}

func TestOpenNekoBridgeDeathFailsCall(t *testing.T) {
	broker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer broker.Close()
	caps, cmd, _ := startBridge(t, broker.URL)
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	if _, err := caps[0].Call(ctx, json.RawMessage(`{"query":"find policy"}`)); err == nil {
		t.Fatal("dead bridge returned a successful tool result")
	}
}

func TestOpenNekoBridgeStalledCallEndsOnTeardown(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	broker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["query"] == "stall policy" {
			close(started)
			<-r.Context().Done()
			close(stopped)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer broker.Close()
	caps, cmd, closeBridge := startBridge(t, broker.URL)
	ctx, stop := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer stop()
	done := make(chan error, 1)
	go func() {
		_, err := caps[0].Call(ctx, json.RawMessage(`{"query":"stall policy"}`))
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("stalled broker request never started")
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled MCP call succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("MCP call did not observe its deadline")
	}
	_ = closeBridge()
	if cmd.ProcessState == nil {
		t.Fatal("bridge teardown left the child process running")
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("bridge teardown left the broker request open")
	}
}
