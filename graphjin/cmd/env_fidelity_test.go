package main

import (
	"strings"
	"testing"
	"time"

	gjeval "github.com/dosco/graphjin/agent/v3/eval"
)

// The side flag exists to keep held-out tasks out of a training loop. Compared
// against a literal, any typo fell through to serving exactly the tasks it was
// meant to protect — silently, and in the direction that does damage.
func TestEnvServerValidatesTheSide(t *testing.T) {
	suite := envTestSuite(t)
	for _, side := range []string{"trian", "TRAIN-ish", "", "holdout"} {
		if _, err := newEnvServer(suite, gjeval.RewardProfileRL, side, nil, ""); err == nil {
			t.Fatalf("side %q must be refused rather than treated as eval", side)
		}
	}
	for _, side := range []string{"train", "eval", " Train "} {
		if _, err := newEnvServer(suite, gjeval.RewardProfileRL, side, nil, ""); err != nil {
			t.Fatalf("side %q must be accepted: %v", side, err)
		}
	}
}

// Freezing the data without freezing what the oracle calls "today" leaves
// date-relative questions drifting against fixed rows. The served runner used
// to be a zero value, so --freeze-time reached the environment and not the
// grading — the half that decides what the answer should be.
func TestEnvServerGivesTheRunnerTheFrozenClock(t *testing.T) {
	suite := envTestSuite(t)
	const frozen = "2026-08-01T12:00:00Z"

	server, err := newEnvServer(suite, gjeval.RewardProfileRL, "train", nil, frozen)
	if err != nil {
		t.Fatal(err)
	}
	if server.runner.Now == nil {
		t.Fatal("the runner resolves oracles against the wall clock, so a frozen environment is graded against a moving today")
	}
	want, err := time.Parse(time.RFC3339, frozen)
	if err != nil {
		t.Fatal(err)
	}
	if got := server.runner.Now(); !got.Equal(want) {
		t.Fatalf("the runner's clock = %s, want %s", got, want)
	}

	// Without the flag it stays unset, which is what makes "now" mean now.
	plain, err := newEnvServer(suite, gjeval.RewardProfileRL, "train", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if plain.runner.Now != nil {
		t.Fatal("an unfrozen environment must not pin the clock")
	}
	// A malformed instant is a startup error, not a silently ignored flag.
	if _, err := newEnvServer(suite, gjeval.RewardProfileRL, "train", nil, "yesterday"); err == nil {
		t.Fatal("an unparseable freeze time must be refused")
	}
}

// Three stages are three policies. Mixing them into one corpus teaches none of
// them, so which one a trajectory covers is a choice the caller makes.
func TestEpisodeTrajectoryStageSelection(t *testing.T) {
	cases := map[string]string{
		"":           "executor",
		"executor":   "executor",
		"distiller":  "distiller",
		"responder":  "responder",
		"all":        "",
		" Executor ": "executor",
	}
	for requested, want := range cases {
		got, err := episodeTrajectoryStage(requested)
		if err != nil {
			t.Fatalf("%q: %v", requested, err)
		}
		if got != want {
			t.Fatalf("%q resolved to %q, want %q", requested, got, want)
		}
	}
	if _, err := episodeTrajectoryStage("finalize"); err == nil {
		t.Fatal("a stage with no trajectory of its own must be refused")
	}
	if _, err := episodeTrajectoryStage("nonsense"); err == nil {
		t.Fatal("an unknown stage must be refused rather than silently serving the executor")
	}
}

// A split naming tasks this suite does not contain leaves a side empty, which
// indexTasks refuses — better a startup error than a server that answers every
// request with "unknown task".
func TestEnvServerRefusesASplitThatSelectsNothing(t *testing.T) {
	suite := envTestSuite(t)
	split := gjeval.SuiteSplit{
		SchemaVersion: gjeval.SplitSchemaVersion, SuiteFingerprint: gjeval.SuiteFingerprint(suite),
		Train: []string{"a-task-from-another-suite"},
	}
	path := t.TempDir() + "/split.json"
	if err := gjeval.SaveSplit(path, split); err != nil {
		t.Fatal(err)
	}
	resolved, _, err := resolveEnvSplit(path, suite)
	if err != nil {
		t.Fatal(err)
	}
	_, err = newEnvServer(suite, gjeval.RewardProfileRL, "train", resolved, "")
	if err == nil {
		t.Fatal("a split that selects none of this suite's tasks must be refused at startup")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "task") {
		t.Fatalf("the refusal should say what is empty: %v", err)
	}
}
