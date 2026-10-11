package agent

import "sync"

// maxIdenticalFailures is how many times one exact tool call may fail before
// the harness refuses to run it again.
const maxIdenticalFailures = 2

type failedCalls struct {
	mu     sync.Mutex
	counts map[string]int
	last   map[string]string
}

func newFailedCalls() *failedCalls {
	return &failedCalls{counts: map[string]int{}, last: map[string]string{}}
}

func (f *failedCalls) record(name, input, message string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := name + "\x00" + input
	f.counts[key]++
	f.last[key] = message
}

func (f *failedCalls) repeated(name, input string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := name + "\x00" + input
	return f.last[key], f.counts[key] >= maxIdenticalFailures
}
