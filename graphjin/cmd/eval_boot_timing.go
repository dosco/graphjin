package main

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// How long it takes to bring a world up, and where the time goes.
//
// Bringing up one world runs DDL, seeds a database, builds a query generation
// and starts a service — twice over when the suite writes, since a resettable
// world is booted, snapshotted and booted again. A pool does all of that once
// per worker, in sequence. Nobody could previously say which part cost what:
// the boot printed provisioning notices and nothing else, so "it takes about a
// minute" was the whole of what was known. Anyone deciding whether to make this
// faster needs the breakdown first.

type bootPhase struct {
	Name string `json:"name"`
	MS   int64  `json:"ms"`
}

// bootTimer records the phases of one world's boot. A nil timer is usable and
// does nothing, so the instrumented paths need no guards.
type bootTimer struct {
	out    io.Writer
	worker int
	start  time.Time
	last   time.Time
	mu     sync.Mutex
	phases []bootPhase
}

func newBootTimer(out io.Writer, worker int) *bootTimer {
	now := time.Now()
	return &bootTimer{out: out, worker: worker, start: now, last: now}
}

// mark closes the phase that just finished.
func (t *bootTimer) mark(name string) {
	if t == nil {
		return
	}
	now := time.Now()
	t.mu.Lock()
	elapsed := now.Sub(t.last)
	t.last = now
	t.phases = append(t.phases, bootPhase{Name: name, MS: elapsed.Milliseconds()})
	out, worker := t.out, t.worker
	t.mu.Unlock()
	if out != nil {
		fmt.Fprintf(out, "boot worker=%d phase=%-20s %s\n", worker, name, elapsed.Round(time.Millisecond))
	}
}

func (t *bootTimer) totalMS() int64 {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.last.Sub(t.start).Milliseconds()
}

// summary renders the phases worth naming, longest first, for one line of
// output that says where a slow boot went.
func (t *bootTimer) summary() string {
	if t == nil {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	parts := make([]string, 0, len(t.phases))
	for _, phase := range t.phases {
		if phase.MS < 50 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %dms", phase.Name, phase.MS))
	}
	return strings.Join(parts, " · ")
}
