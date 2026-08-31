package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	gjeval "github.com/dosco/graphjin/agent/v3/eval"
)

// A trajectory built from a real episode has to carry what the model was
// asked, or every corpus made from it is empty.
//
// This is the probe that found the export broken, kept as a test. The exporter
// read chat-log keys that no trace ax produces, so a run that worked perfectly
// exported steps with no prompt and an unknown author — and the two fields the
// SFT script filters on are exactly those. Unit fixtures could not catch it:
// the fixture was written in the same imagined shape as the reader.
func TestTrajectoryFromALiveEpisodeCarriesItsPrompts(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded service integration")
	}
	server, stop := startEnvTestServer(t, `
		const detail = await query_catalog({id: "table:app:main.accounts"});
		const res = await execute_graphql({query: "query { accounts { count_id } }"});
		await final({status: "answered", answer: "There are " + res.data.accounts[0].count_id + " accounts.", data: res.data, evidence: [detail]});
	`)
	defer stop()

	body, err := json.Marshal(envEpisodeRequest{Slug: "count-accounts", IncludeTrajectory: true})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	server.handleEpisode(rec, httptest.NewRequest(http.MethodPost, "/episodes", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var episode envEpisodeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &episode); err != nil {
		t.Fatal(err)
	}
	trajectory := episode.Trajectory
	if trajectory == nil || len(trajectory.Steps) == 0 {
		t.Fatalf("a trajectory was requested and must come back with steps: %+v", trajectory)
	}
	if !trajectory.PromptsRecorded {
		t.Fatal("a live trace records rendered prompts; the export failed to find them")
	}
	if !trajectory.AuthorshipResolved {
		t.Fatal("a live trace records completions; the export failed to tell the model's programs from the runtime's")
	}
	for index, step := range trajectory.Steps {
		if len(step.Prompt) == 0 {
			t.Fatalf("step %d carries no prompt, so nothing can be trained on it", index)
		}
		if step.Author != gjeval.AuthorModel {
			t.Fatalf("step %d author = %q, want %q", index, step.Author, gjeval.AuthorModel)
		}
		if step.Program == "" {
			t.Fatalf("step %d carries no program", index)
		}
	}
	if len(trajectory.TraceNotes) != 0 {
		t.Fatalf("a complete live trace must raise no doubts: %v", trajectory.TraceNotes)
	}
}
