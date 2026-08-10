package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadReplayFixtureExtractsExecutorPrograms(t *testing.T) {
	episode := Episode{
		SchemaVersion: EpisodeSchemaVersion,
		RunID:         "run-1",
		TaskID:        "gjv1_test",
		TaskSlug:      "refusal-anon-record-payment",
		Repeat:        3,
		Task:          Task{Slug: "refusal-anon-record-payment", Prompt: "Record a payment."},
		Score: ScoreDetail{
			FailureCategory:   "runaway",
			ActorTurns:        8,
			ViolationCodes:    []string{"capability_disabled"},
			ForbiddenAttempts: []string{"execute_graphql:mutation"},
		},
		Response: map[string]any{
			"status": "error",
			"trace": map[string]any{
				"chat_log": []any{
					map[string]any{"name": "distiller", "item1": map[string]any{
						"content": `{"javascriptCode":"await query_catalog({search:\"x\"});"}`,
					}},
					map[string]any{"name": "executor", "item1": map[string]any{
						"content": `{"javascriptCode":"await execute_graphql({query:\"mutation { payments(insert: {}) { id } }\"});"}`,
					}},
					map[string]any{"name": "executor", "item1": map[string]any{
						"content": `{"javascriptCode":"await final({status:\"blocked\"});"}`,
					}},
					map[string]any{"name": "responder", "item1": map[string]any{"content": "prose"}},
				},
			},
		},
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "episode.json")
	data, err := json.Marshal(episode)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	fixture, err := LoadReplayFixture(path)
	if err != nil {
		t.Fatalf("LoadReplayFixture: %v", err)
	}
	if len(fixture.Programs) != 2 {
		t.Fatalf("expected 2 executor programs, got %d: %#v", len(fixture.Programs), fixture.Programs)
	}
	if !strings.Contains(fixture.Programs[0], "execute_graphql") {
		t.Fatalf("first executor program not preserved: %q", fixture.Programs[0])
	}
	if !strings.Contains(fixture.Programs[1], "final(") {
		t.Fatalf("second executor program not preserved: %q", fixture.Programs[1])
	}
	if fixture.Observed.Status != "error" || fixture.Observed.FailureCategory != "runaway" {
		t.Fatalf("observed baseline not captured: %+v", fixture.Observed)
	}
	if len(fixture.Observed.ForbiddenAttempts) != 1 {
		t.Fatalf("observed forbidden attempts not captured: %+v", fixture.Observed)
	}
	// Fixtures must stay program-only: no prompts beyond the published task, no
	// rows, no model answers.
	encoded, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"chat_log", "responder", "prose"} {
		if strings.Contains(string(encoded), leak) {
			t.Fatalf("fixture leaked %q into committed shape: %s", leak, encoded)
		}
	}
}

func TestLoadReplayFixtureRejectsEpisodeWithoutExecutorPrograms(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "episode.json")
	data, err := json.Marshal(Episode{
		SchemaVersion: EpisodeSchemaVersion,
		Response:      map[string]any{"status": "blocked"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadReplayFixture(path); err == nil {
		t.Fatal("expected an error when no executor programs are present")
	}
}

func TestObserveReplayCollectsGuardCodes(t *testing.T) {
	response := map[string]any{
		"status": "error",
		"actions": []any{
			map[string]any{"tool": "execute_graphql", "summary": map[string]any{
				"error_codes":    []any{"mutation_evidence_required"},
				"recovery_codes": []any{"mutation_evidence_required", "mutation_shape_detail_required"},
			}},
			map[string]any{"tool": "execute_graphql", "summary": map[string]any{
				"error_codes": []any{"duplicate_failed_query"},
			}},
		},
	}
	observation := ObserveReplay(response, ScoreDetail{
		ActorTurns:     8,
		ViolationCodes: []string{"mutation_evidence_required"},
	})
	for _, code := range []string{
		"mutation_evidence_required",
		"mutation_shape_detail_required",
		"duplicate_failed_query",
	} {
		if !observation.HasCode(code) {
			t.Fatalf("expected observation to surface %q: %+v", code, observation)
		}
	}
	if observation.HasCode("history_read_required") {
		t.Fatalf("unexpected code reported: %+v", observation)
	}
}
