package version

import (
	"strings"
	"testing"
)

func TestVersionDefault(t *testing.T) {
	if Version == "" {
		t.Fatal("Version must not be empty")
	}
}

func TestDetailedVersionOutput(t *testing.T) {
	oldVersion, oldCommit, oldTimestamp := Version, Commit, CommitTimestamp
	Version = "1.2.3"
	Commit = "abcdef123456"
	CommitTimestamp = "2026-06-17T08:48:42Z"
	t.Cleanup(func() {
		Version = oldVersion
		Commit = oldCommit
		CommitTimestamp = oldTimestamp
	})

	out := Detailed()
	for _, want := range []string{
		"OpenNeko 1.2.3",
		"For documentation, visit https://openneko.app",
		"Commit SHA-1          : abcdef123456",
		"Commit timestamp      : 2026-06-17T08:48:42Z",
		"Go version            : go",
		"Licensed under the Apache License 2.0",
		"Copyright 2026 Amit Deshmukh and OpenNeko contributors",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in output:\n%s", want, out)
		}
	}
}
