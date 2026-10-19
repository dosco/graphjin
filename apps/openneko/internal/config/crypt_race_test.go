package config

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// Two overlapping CLI invocations (a deploy `upgrade` racing a cron
// `status`) must never mint different secret keys: every enc:v1 value hangs
// off this one file.
func TestLoadOrCreateKeyConcurrent(t *testing.T) {
	dir := t.TempDir()

	const workers = 16
	keys := make([][]byte, workers)
	errs := make([]error, workers)
	var start, done sync.WaitGroup
	start.Add(1)
	done.Add(workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			defer done.Done()
			start.Wait()
			keys[i], errs[i] = loadOrCreateKey(dir)
		}(i)
	}
	start.Done()
	done.Wait()

	for i := 0; i < workers; i++ {
		if errs[i] != nil {
			t.Fatalf("worker %d: %v", i, errs[i])
		}
		if string(keys[i]) != string(keys[0]) {
			t.Fatalf("worker %d derived a different key than worker 0", i)
		}
	}

	info, err := os.Stat(filepath.Join(dir, "secret-key"))
	if err != nil {
		t.Fatalf("stat key file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("key file mode = %o, want 600", perm)
	}
}

func TestLoadOrCreateKeyEmptyFileFails(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "secret-key"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadOrCreateKey(dir); err == nil {
		t.Fatal("expected an error for an empty pre-existing key file")
	}
}
