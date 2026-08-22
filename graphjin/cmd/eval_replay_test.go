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

// TestReplayReactiveCreationSuppliesEvidence pins the fix that took DeepORG watch
// creation off zero. The recovery already named the exact catalog id and the model
// still did not fetch it, so every creation episode looped on
// mutation_evidence_required until its step budget ran out. GraphJin now fetches
// the detail itself, the same way history_read_required supplies bounded history
// instead of naming a global for the model to read.
//
// The fixture's embedded subscription targets a real application table, which is
// the case GraphJin can discharge. Trajectories that watch a system root or an
// invented table must still be rejected — see the sibling test.
func TestReplayReactiveCreationSuppliesEvidence(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded replay integration")
	}
	fixture := loadReplayFixture(t, "reactive-reactive-create-deeporg-failed-invoices-rep1.json")
	observed := replayFixture(t, fixture)
	t.Logf("baseline: %s/%s turns=%d | replay: status=%s turns=%d recovery=%v errors=%v",
		fixture.Observed.Status, fixture.Observed.FailureCategory, fixture.Observed.ActorTurns,
		observed.Status, observed.ActorTurns, observed.RecoveryCodes, observed.ErrorCodes)

	// A resolvable target must be discharged, not rejected: the guard's own
	// requirement is satisfied by GraphJin supplying the column evidence.
	if observed.HasCode("mutation_evidence_required") {
		t.Errorf("resolvable watch target was still rejected for missing evidence instead of being supplied it: %+v", observed)
	}
}

// TestReplayReactiveCreationRejectsSystemRootTarget keeps the guard honest. This
// trajectory's subscription watches gj_watch_event, a system root rather than an
// application table, so GraphJin cannot resolve a table detail for it and must
// fail loudly rather than silently discharge the prerequisite.
func TestReplayReactiveCreationRejectsSystemRootTarget(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded replay integration")
	}
	fixture := loadReplayFixture(t, "reactive-reactive-create-deeporg-churn-accounts-rep2.json")
	observed := replayFixture(t, fixture)
	t.Logf("replay: status=%s turns=%d recovery=%v errors=%v",
		observed.Status, observed.ActorTurns, observed.RecoveryCodes, observed.ErrorCodes)

	if !observed.HasCode("mutation_evidence_required") {
		t.Errorf("an unresolvable watch target must still be rejected: %+v", observed)
	}
}

// TestReplayUnknownColumnWriteGetsColumnNamedRecovery replays the run-5d12a35b
// payments episode whose insert used created_at (real column: recorded_at) and
// retried the identical query until duplicate_failed_query locked it out. With
// strict mutation columns the failure now surfaces at compile naming the
// column, and the enriched recovery names the table's real columns and carries
// a machine-readable kind. Mechanism only, never score: canned programs cannot
// adapt to the better error.
func TestReplayUnknownColumnWriteGetsColumnNamedRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded replay integration")
	}
	fixture := loadReplayFixture(t, "action-record-payment-deeporg-pay-002-rep1.json")
	observed := replayFixture(t, fixture)
	t.Logf("baseline: status=%s turns=%d | replay: status=%s turns=%d recovery=%v errors=%v",
		fixture.Observed.Status, fixture.Observed.ActorTurns,
		observed.Status, observed.ActorTurns, observed.RecoveryCodes, observed.ErrorCodes)

	if !observed.HasCode("execution_error") {
		t.Errorf("failed write recovery must carry its kind: %+v", observed)
	}
}

// TestReplayValueGuardLetsThirdAttemptLand replays the run-dcea36f8 episode
// whose value-vocabulary guard re-fired seven times on the same rejected
// status "closed" and never let the caller's decision land: a query-text veto
// short-circuited the (table, column, value) strike budget, and models never
// resubmit byte-identically. With strikes as the sole authority the third
// attempt must reach dispatch. Mechanism only, never score: the canned
// programs still write "closed", so the task's post-state may well fail — the
// defect was the unreachable let-through, not the vocabulary.
// dcea36f8's lesson — two corrections are friction, not schema, so the third
// attempt lands — is superseded exactly when a clear-winner repaired_query
// exists: the Muse-Glimmer run measured the override's only users as 8
// episodes persisting "closed" into open|pending|resolved after ignoring the
// exact repair twice. This fixture is such a repair-carrying loop, so the
// refusal now stands for as long as the model resends the invalid value, and
// the write never lands. The list-only let-through is pinned separately in
// agent/mutation_value_check_test.go.
func TestReplayValueGuardKeepsRefusingWhileRepairExists(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded replay integration")
	}
	fixture := loadReplayFixture(t, "action-need-ticket-3-rep1.json")
	baselineFires := 0
	for _, code := range fixture.Observed.ViolationCodes {
		if code == "observed_value_mismatch" {
			baselineFires++
		}
	}
	if baselineFires < 3 {
		t.Fatalf("fixture is not a value-guard loop baseline: %+v", fixture.Observed)
	}
	observed := replayFixture(t, fixture)
	t.Logf("baseline: status=%s turns=%d fires=%d | replay: status=%s turns=%d violations=%v",
		fixture.Observed.Status, fixture.Observed.ActorTurns, baselineFires,
		observed.Status, observed.ActorTurns, observed.ViolationCodes)

	fires := 0
	for _, code := range observed.ViolationCodes {
		if code == "observed_value_mismatch" {
			fires++
		}
	}
	if fires == 0 {
		t.Errorf("the vocabulary guard must fire on this fixture: %+v", observed)
	}
	if observed.Status != "blocked" {
		t.Errorf("an out-of-vocabulary write with a known repair must stay blocked, got %q", observed.Status)
	}
}
