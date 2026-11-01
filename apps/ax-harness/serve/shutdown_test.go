package serve

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if socket := os.Getenv("SERVE_SHUTDOWN_CHILD"); socket != "" {
		MainWith([]string{"--socket", socket}, Options{})
		return
	}
	os.Exit(m.Run())
}

func TestStopSignalEndsServeAtOnce(t *testing.T) {
	socket := filepath.Join(shortTempDir(t), "serve.sock")
	child := exec.Command(os.Args[0], "-test.run=^$")
	child.Env = append(os.Environ(), "SERVE_SHUTDOWN_CHILD="+socket)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill() }()

	var conn net.Conn
	for deadline := time.Now().Add(10 * time.Second); ; {
		var err error
		if conn, err = net.Dial("unix", socket); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("serve did not listen: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	defer conn.Close()
	// An idle connection holds a handler open, as a turn waiting on a model
	// call does.
	time.Sleep(100 * time.Millisecond)

	if err := child.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- child.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve exited with %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("serve still running a second after SIGTERM")
	}
}

func shortTempDir(t *testing.T) string {
	dir, err := os.MkdirTemp("/tmp", "serve-stop-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}
