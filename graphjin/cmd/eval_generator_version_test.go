package main

import (
	"testing"

	gjeval "github.com/dosco/graphjin/agent/v3/eval"
)

// A published suite is a frozen artifact: the leaderboard's cohorts were
// measured against exactly these tasks. Adding a generator version must
// therefore leave the previous one runnable, and must not disturb the identity
// of a single published task.
//
// Task IDs are content hashes, so this is enforceable rather than asserted:
// loading the embedded suite revalidates every task against its own content,
// and a task whose content had shifted would no longer match the ID it was
// published under.
func TestFrozenPublicSuiteSurvivesTheGeneratorVersionBump(t *testing.T) {
	suite, err := loadPublicEvalSuite()
	if err != nil {
		t.Fatal(err)
	}
	if suite.Generator.Version == gjeval.GeneratorVersion {
		t.Skip("the frozen suite was regenerated at the current version; this guard covers the cross-version case")
	}
	if !gjeval.IsSupportedGeneratorVersion(suite.Generator.Version) {
		t.Fatalf("the frozen public suite (%s) is no longer runnable by this binary, which supports %v",
			suite.Generator.Version, gjeval.SupportedGeneratorVersions)
	}
	for _, task := range suite.Tasks {
		want, err := task.ContentID()
		if err != nil {
			t.Fatalf("task %q: %v", task.Slug, err)
		}
		if task.ID != want {
			t.Fatalf("published task %q changed identity: id %s, content hash %s", task.Slug, task.ID, want)
		}
	}
}

// The current version must be in the supported set, or a suite this binary
// generates could not be run by the binary that generated it.
func TestGeneratorVersionIsSupported(t *testing.T) {
	if !gjeval.IsSupportedGeneratorVersion(gjeval.GeneratorVersion) {
		t.Fatalf("%s is not in the supported set %v", gjeval.GeneratorVersion, gjeval.SupportedGeneratorVersions)
	}
}
