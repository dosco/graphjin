package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func withIsolatedConfigDir(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("HOME", tmp)
	if runtime.GOOS == "windows" {
		t.Setenv("AppData", tmp)
	}
	return tmp
}

func TestSaveAndLoadRoundtrip(t *testing.T) {
	withIsolatedConfigDir(t)

	in := &ClientConfig{
		Server:    "https://graphjin.test",
		Token:     "eyJhbGciOi.foo.bar",
		ExpiresAt: time.Unix(1_800_000_000, 0).UTC(),
		Email:     "alice@example.com",
		Issuer:    "https://accounts.google.com",
	}
	if err := SaveClientConfig(in); err != nil {
		t.Fatal(err)
	}

	got, err := LoadClientConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("loaded nil")
	}
	if got.Server != in.Server || got.Token != in.Token || got.Email != in.Email || got.Issuer != in.Issuer {
		t.Errorf("roundtrip mismatch: got %+v", got)
	}
	if !got.ExpiresAt.Equal(in.ExpiresAt) {
		t.Errorf("expires_at = %v, want %v", got.ExpiresAt, in.ExpiresAt)
	}
}

func TestLoadMissingReturnsNil(t *testing.T) {
	withIsolatedConfigDir(t)
	got, err := LoadClientConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestSaveUsesRestrictivePerms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix perms")
	}
	withIsolatedConfigDir(t)

	if err := SaveClientConfig(&ClientConfig{Server: "http://x", Token: "t"}); err != nil {
		t.Fatal(err)
	}
	p, _ := ClientConfigPath()
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("perm = %o, want 0600", perm)
	}
}

func TestSaveOverwritesAtomically(t *testing.T) {
	withIsolatedConfigDir(t)
	if err := SaveClientConfig(&ClientConfig{Server: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveClientConfig(&ClientConfig{Server: "b"}); err != nil {
		t.Fatal(err)
	}
	got, err := LoadClientConfig()
	if err != nil || got == nil || got.Server != "b" {
		t.Fatalf("overwrite failed: err=%v got=%+v", err, got)
	}
	p, _ := ClientConfigPath()
	entries, err := os.ReadDir(filepath.Dir(p))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != filepath.Base(p) {
			t.Errorf("unexpected leftover file: %s", e.Name())
		}
	}
}

func TestDeleteIsIdempotent(t *testing.T) {
	withIsolatedConfigDir(t)
	if err := DeleteClientConfig(); err != nil {
		t.Errorf("delete on empty should be nil, got %v", err)
	}
	if err := SaveClientConfig(&ClientConfig{Server: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := DeleteClientConfig(); err != nil {
		t.Fatal(err)
	}
	got, _ := LoadClientConfig()
	if got != nil {
		t.Error("expected nil after delete")
	}
}

func TestSavedContentIsValidJSON(t *testing.T) {
	withIsolatedConfigDir(t)
	_ = SaveClientConfig(&ClientConfig{Server: "s", Token: "t"})
	p, _ := ClientConfigPath()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if m["server"] != "s" {
		t.Errorf("server = %v", m["server"])
	}
}
