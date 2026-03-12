package core

import (
	"testing"
	"time"
)

func TestGraphJinCloseStopsWatcherPromptly(t *testing.T) {
	g := &GraphJin{done: make(chan bool)}

	stopped := make(chan struct{})
	go func() {
		g.startDBWatcher(10 * time.Second)
		close(stopped)
	}()

	g.Close()
	g.Close()

	select {
	case <-stopped:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("expected watcher to stop promptly after Close")
	}
}
