package eval

import "testing"

func TestStoreRejectsUnsafeRunID(t *testing.T) {
	store := NewStore(t.TempDir())
	if _, err := store.WriteReport(Report{RunID: "../outside"}); err == nil {
		t.Fatal("unsafe report run_id was accepted")
	}
	if _, err := store.WriteEpisode(Episode{RunID: `..\outside`}); err == nil {
		t.Fatal("unsafe episode run_id was accepted")
	}
}
