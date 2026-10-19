package secrets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetUnset(t *testing.T) {
	s := Store{}
	s, err := Set(s, "foo", "API_KEY", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if s["foo"]["API_KEY"] != "secret" {
		t.Fatalf("expected secret, got %v", s)
	}
	s, removed := Unset(s, "foo", "API_KEY")
	if !removed {
		t.Fatal("expected removal")
	}
	if _, ok := s["foo"]; ok {
		t.Fatal("plugin entry should be gone when last key removed")
	}
}

func TestSetInvalidKey(t *testing.T) {
	if _, err := Set(Store{}, "foo", "lowercase", "x"); err == nil {
		t.Fatal("expected error for non-UPPER_SNAKE_CASE key")
	}
	if _, err := Set(Store{}, "foo", "1LEADS_DIGIT", "x"); err == nil {
		t.Fatal("expected error for leading digit")
	}
}

func TestReadWriteRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := Store{}
	s, _ = Set(s, "plugin-b", "Z_KEY", "z")
	s, _ = Set(s, "plugin-a", "B_KEY", "b")
	s, _ = Set(s, "plugin-a", "A_KEY", "a")
	if err := Write(s, dir); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, StoreFilename))
	if err != nil {
		t.Fatal(err)
	}
	// Plugins sorted alphabetically; keys within a plugin sorted alphabetically.
	got := string(raw)
	if !strings.Contains(got, `"plugin-a"`) || !strings.Contains(got, `"plugin-b"`) {
		t.Fatalf("missing plugins in output: %s", got)
	}
	if idxA, idxB := strings.Index(got, `"plugin-a"`), strings.Index(got, `"plugin-b"`); idxA > idxB {
		t.Fatalf("plugin-a should come before plugin-b, got: %s", got)
	}
	if idxA, idxB := strings.Index(got, `"A_KEY"`), strings.Index(got, `"B_KEY"`); idxA > idxB {
		t.Fatalf("A_KEY should come before B_KEY, got: %s", got)
	}
	if !strings.HasSuffix(got, "}\n") {
		t.Fatalf("expected trailing newline, got %q", got)
	}

	back, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if back["plugin-a"]["A_KEY"] != "a" || back["plugin-b"]["Z_KEY"] != "z" {
		t.Fatalf("round-trip lost values: %v", back)
	}
}

func TestReadMissing(t *testing.T) {
	dir := t.TempDir()
	s, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 0 {
		t.Fatalf("expected empty store, got %v", s)
	}
}

func TestWritePerms(t *testing.T) {
	dir := t.TempDir()
	s := Store{"x": {"K": "v"}}
	if err := Write(s, dir); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, StoreFilename))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600, got %o", info.Mode().Perm())
	}
}

func TestListKeysSorted(t *testing.T) {
	s := Store{"p": {"Z": "1", "A": "2", "M": "3"}}
	keys := ListKeysForSection(s, "p")
	want := []string{"A", "M", "Z"}
	for i, k := range want {
		if keys[i] != k {
			t.Fatalf("want %v, got %v", want, keys)
		}
	}
}

func TestAllValues(t *testing.T) {
	s := Store{
		"p1": {"A": "v1", "B": "v2"},
		"p2": {"C": "v1", "D": ""},
	}
	vals := AllValues(s)
	// v1 deduped; empty skipped.
	if len(vals) != 2 {
		t.Fatalf("expected 2 unique non-empty values, got %v", vals)
	}
}
