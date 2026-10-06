package serv

import (
	"testing"
	"time"
)

func TestScopedCopySharesTheConfigLock(t *testing.T) {
	parent := &graphjinService{conf: &Config{}}
	lock := parent.configLocker()
	lock.Lock()
	scoped := parent.scopedCopy(&Config{}, "", "", nil)
	lock.Unlock()

	if scoped.configLocker() != parent.configLocker() {
		t.Fatal("a scoped copy must share its parent's config lock")
	}
	done := make(chan struct{})
	go func() {
		scoped.configLocker().Lock()
		scoped.configLocker().Unlock()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the scoped copy's config lock stayed locked after the parent released it")
	}
}

func TestScopedCopyHandlersBindToTheLiveService(t *testing.T) {
	parent := &graphjinService{conf: &Config{}}
	scoped := parent.scopedCopy(&Config{}, "", "", nil)
	if scoped.liveService() != parent {
		t.Fatal("a scoped copy must report its parent as the live service")
	}
	again := scoped.scopedCopy(&Config{}, "", "", nil)
	if again.liveService() != parent {
		t.Fatal("a copy of a copy must still report the original live service")
	}
}
