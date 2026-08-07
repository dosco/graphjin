package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEvalSQLiteSnapshotRestoresApplicationAndControlPlaneFiles(t *testing.T) {
	project := t.TempDir()
	appDB := filepath.Join(project, "demo", "databases", "app", "graphjin_demo.db")
	controlDB := filepath.Join(project, ".graphjin", "artifacts.sqlite3")
	for path, content := range map[string]string{appDB: "app-baseline", controlDB: "control-baseline"} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := newEvalSQLiteSnapshot(project)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close() //nolint:errcheck
	for _, path := range []string{appDB, controlDB} {
		if err := os.WriteFile(path, []byte("episode-mutation"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path+"-wal", []byte("stale"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := snapshot.Restore(); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{appDB: "app-baseline", controlDB: "control-baseline"} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("restored %s = %q err=%v", path, got, err)
		}
		if _, err := os.Stat(path + "-wal"); !os.IsNotExist(err) {
			t.Fatalf("stale sidecar remains for %s: %v", path, err)
		}
	}
}
