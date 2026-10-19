package serv

import (
	"strings"
	"testing"
)

func TestArtifactStorePostgresSQL(t *testing.T) {
	base := strings.Join(ArtifactStorePostgresSQL("", false), "\n")
	withWatches := strings.Join(ArtifactStorePostgresSQL("_graphjin", true), "\n")
	for _, want := range []string{`CREATE SCHEMA IF NOT EXISTS "_graphjin"`, `"_graphjin"."artifacts"`, `"_graphjin"."revisions"`, `ADD COLUMN IF NOT EXISTS approved_at`} {
		if !strings.Contains(base, want) {
			t.Fatalf("store SQL lacks %s", want)
		}
	}
	if strings.Contains(base, `"watches"`) {
		t.Fatal("store SQL without watches creates the watch tables")
	}
	for _, want := range []string{`"_graphjin"."watches"`, `"_graphjin"."watch_events"`, `ADD COLUMN IF NOT EXISTS absence_json`} {
		if !strings.Contains(withWatches, want) {
			t.Fatalf("watch store SQL lacks %s", want)
		}
	}
	for _, stmt := range ArtifactStorePostgresSQL("_graphjin", true) {
		if !strings.Contains(stmt, "IF NOT EXISTS") {
			t.Fatalf("statement is not idempotent: %s", stmt)
		}
	}
}
