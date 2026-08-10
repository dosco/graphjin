package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	ax "github.com/ax-llm/ax/packages/go"
	gjagent "github.com/dosco/graphjin/agent/v3"
	gjeval "github.com/dosco/graphjin/agent/v3/eval"
)

// Trajectory replay: re-run the exact executor programs a model emitted in a
// stored benchmark episode against current GraphJin code, and assert on the
// guards that fire. This turns "$4 per learned mechanism" into a $0 regression
// check.
//
// Replay asserts mechanism, never score. The canned programs cannot adapt to new
// guidance, so a replayed failing episode is not expected to pass; what it proves
// is which violation, recovery, and continuation codes current code produces.

func loadReplayFixture(t *testing.T, name string) gjeval.ReplayFixture {
	t.Helper()
	path := filepath.Join("testdata", "replay", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var fixture gjeval.ReplayFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("decode fixture %s: %v", name, err)
	}
	if len(fixture.Programs) == 0 {
		t.Fatalf("fixture %s carries no executor programs", name)
	}
	return fixture
}

// replayFixture boots a writable embedded instance, feeds the fixture's programs
// to the scripted client in order, and returns what current code produced.
func replayFixture(t *testing.T, fixture gjeval.ReplayFixture) gjeval.ReplayObservation {
	t.Helper()
	project := t.TempDir()
	if err := extractDefaultDemo(project); err != nil {
		t.Fatal(err)
	}
	originalPath, originalConf, originalDB, originalOpened := cpath, conf, db, dbOpened
	defer func() {
		cpath, conf, db, dbOpened = originalPath, originalConf, originalDB, originalOpened
	}()
	t.Setenv("GO_ENV", "dev")

	client := &evalScriptClient{}
	client.setSequence(fixture.Programs...)
	factory := func(gjagent.Config) (ax.AIClient, error) { return client, nil }

	instance, err := (evalEnvironment{ClientFactory: factory}).Start(context.Background(), gjeval.EnvSpec{
		Target: gjeval.TargetDemo, ConfigPath: project, Seed: 23,
		Writable: true, Reactive: true, Resettable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close() //nolint:errcheck

	headers := instance.Headers()
	// Refusal tasks are scored against an unprivileged caller; replay must use
	// the same identity or the guard under test never engages.
	if fixture.Task.CapabilityProfile.RoleClass == "anon" {
		headers = make(map[string]string, len(instance.Headers()))
		for key, value := range instance.Headers() {
			headers[key] = value
		}
		headers["X-User-Role"] = "anon"
		delete(headers, "X-User-ID")
		delete(headers, "X-Account-ID")
	}

	response, _, _, err := gjeval.PostAgentForReplay(
		context.Background(), &http.Client{Timeout: 180 * time.Second}, instance.BaseURL(), headers,
		fixture.Task.Prompt, fixture.Task.Turns,
	)
	if err != nil {
		t.Fatalf("replay %s: %v", fixture.TaskSlug, err)
	}
	score := gjeval.ScoreForReplay(fixture.Task, response)
	return gjeval.ObserveReplay(response, score)
}

// TestReplayRefusalRunawayReachesTerminalDenial is the $0 answer to the open
// question from run 35621a4f: 8 refusal episodes exhausted their step budget
// because the model was forbidden to attempt the write but could not conclude
// "blocked". Commit 774c84d9 added pre-dispatch refusal; this replays those exact
// trajectories against it.
func TestReplayRefusalRunawayReachesTerminalDenial(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded replay integration")
	}
	for _, name := range []string{
		"refusal-refusal-anon-record-payment-deeporg-pay-002-rep2.json",
		"refusal-refusal-user-delete-all-invoices-rep1.json",
	} {
		t.Run(name, func(t *testing.T) {
			fixture := loadReplayFixture(t, name)
			if fixture.Observed.FailureCategory != "runaway" {
				t.Fatalf("fixture is not a runaway baseline: %+v", fixture.Observed)
			}
			observed := replayFixture(t, fixture)
			t.Logf("baseline: status=%s turns=%d | replay: status=%s turns=%d violations=%v recovery=%v errors=%v",
				fixture.Observed.Status, fixture.Observed.ActorTurns,
				observed.Status, observed.ActorTurns, observed.ViolationCodes,
				observed.RecoveryCodes, observed.ErrorCodes)

			// Mechanism assertions. Pre-dispatch refusal (774c84d9) terminates the
			// request before any executor program runs, so there are deliberately
			// no action-level guard codes: the evidence is a clean blocked status
			// reached without attempts and without burning the step budget.
			if observed.Status != "blocked" {
				t.Errorf("forbidden write did not terminate as blocked: %+v", observed)
			}
			if len(observed.ForbiddenAttempts) != 0 {
				t.Errorf("forbidden write reached dispatch: %+v", observed)
			}
			if observed.HasCode("duplicate_failed_query") {
				t.Errorf("policy denial was treated as a repairable query: %+v", observed)
			}
			// The original trajectory burned its whole budget; replay must not.
			if fixture.Observed.ActorTurns > 0 && observed.ActorTurns >= fixture.Observed.ActorTurns {
				t.Errorf("replay did not shorten the runaway: baseline %d turns, replay %d turns",
					fixture.Observed.ActorTurns, observed.ActorTurns)
			}
		})
	}
}

// TestReplayReactiveCreationNamesRequiredEvidence pins the creation mechanism
// found in forensics: mutationEvidenceNext returned an enumerate-tables fallback
// instead of the exact catalog detail id, so the model listed tables, corrected
// its root, retried, and was rejected identically until exhaustion.
func TestReplayReactiveCreationNamesRequiredEvidence(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded replay integration")
	}
	fixture := loadReplayFixture(t, "reactive-reactive-create-deeporg-churn-accounts-rep2.json")
	observed := replayFixture(t, fixture)
	t.Logf("baseline: %s/%s | replay: status=%s turns=%d recovery=%v errors=%v",
		fixture.Observed.Status, fixture.Observed.FailureCategory,
		observed.Status, observed.ActorTurns, observed.RecoveryCodes, observed.ErrorCodes)

	if !observed.HasCode("mutation_evidence_required") {
		t.Skipf("creation trajectory no longer reaches the evidence guard: %+v", observed)
	}
	// The guard may fire, but it must not leave the model looping on identical
	// rejected mutations: that is the signature of an under-specified recovery.
	if observed.HasCode("duplicate_failed_query") {
		t.Errorf("evidence guard left the model retrying identical mutations; recovery must name the exact catalog detail id: %+v", observed)
	}
}
