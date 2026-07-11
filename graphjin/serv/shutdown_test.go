package serv_test

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/dosco/graphjin/serv/v3"
)

// freePort grabs an ephemeral port from the kernel and releases it so the
// service can bind it moments later. Good enough for a single test.
func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	defer l.Close() //nolint:errcheck
	return l.Addr().String()
}

// waitListening blocks until addr accepts a TCP connection or the deadline
// passes.
func waitListening(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			c.Close() //nolint:errcheck
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("server never started listening on %s", addr)
}

// TestShutdownUnblocksStart is the regression test for the demo-mode hang:
// Start() blocks on the HTTP listener, and Shutdown() must stop it so Start()
// returns and the process can exit. Before the fix, Start() only returned via
// an internal SIGINT-only handler, so a SIGTERM (the signal the demo path and
// most process managers send) left the server serving forever.
func TestShutdownUnblocksStart(t *testing.T) {
	addr := freePort(t)

	conf, err := serv.NewConfig(fmt.Sprintf("host_port: %s\n", addr), "yaml")
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}

	gj, err := serv.NewGraphJinService(conf)
	if err != nil {
		t.Fatalf("NewGraphJinService: %v", err)
	}

	startErr := make(chan error, 1)
	go func() { startErr <- gj.Start() }()

	waitListening(t, addr)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := gj.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	select {
	case err := <-startErr:
		if err != nil {
			t.Fatalf("Start returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after Shutdown — HTTP server never stopped")
	}

	// The listener must be gone now.
	if c, err := net.DialTimeout("tcp", addr, 300*time.Millisecond); err == nil {
		c.Close() //nolint:errcheck
		t.Fatal("port still accepting connections after Shutdown")
	}
}

// TestShutdownBeforeStart verifies Shutdown is a safe no-op when the HTTP
// server was never started (mirrors MCP stdio mode, where srv is nil).
func TestShutdownBeforeStart(t *testing.T) {
	conf, err := serv.NewConfig("host_port: 127.0.0.1:0\n", "yaml")
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	gj, err := serv.NewGraphJinService(conf)
	if err != nil {
		t.Fatalf("NewGraphJinService: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := gj.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown before Start should be a no-op, got: %v", err)
	}
}
