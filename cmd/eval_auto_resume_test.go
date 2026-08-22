package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	gjeval "github.com/dosco/graphjin/agent/v3/eval"
)

func autoResumeTestStore(t *testing.T, runID, code string) *gjeval.Store {
	t.Helper()
	store := gjeval.NewStore(t.TempDir())
	manifest := gjeval.RunManifest{RunID: runID, LastEnvironmentCode: code}
	if _, err := store.WriteManifest(manifest); err != nil {
		t.Fatal(err)
	}
	return store
}

func haltedReport(runID string, completed, planned int) *gjeval.Report {
	return &gjeval.Report{
		RunID:     runID,
		RunStatus: gjeval.RunStatusEnvironmentFailed,
		Progress:  gjeval.RunProgress{CompletedInitialSlots: completed, PlannedInitialSlots: planned},
	}
}

// A benchmark run that stops because the provider is having a bad afternoon has
// lost nothing: every completed episode is reused on resume, so the only cost of
// waiting is time. The loop spends that time so an operator does not have to.
func TestAutoResumeRetriesTransientHaltsUntilComplete(t *testing.T) {
	runID := "20260822T090543.000000000Z-abcdef01"
	store := autoResumeTestStore(t, runID, gjagent.ErrorCodeProviderTimeout)
	var stderr bytes.Buffer
	var slept []time.Duration

	calls := 0
	var sawPolicy []gjeval.ResumePolicy
	var sawRunID []string
	attempt := func(_ context.Context, policy gjeval.ResumePolicy, resumeRunID string) (*gjeval.Report, error) {
		calls++
		sawPolicy = append(sawPolicy, policy)
		sawRunID = append(sawRunID, resumeRunID)
		switch calls {
		case 1:
			return haltedReport(runID, 44, 339), nil
		case 2:
			return haltedReport(runID, 150, 339), nil
		default:
			return &gjeval.Report{RunID: runID, RunStatus: gjeval.RunStatusComplete}, nil
		}
	}

	report, err := runAutoResumeBench(context.Background(), autoResumeConfig{
		Enabled: true, MaxAttempts: 12, Store: store, Provider: "together", Stderr: &stderr,
		Sleep: func(_ context.Context, d time.Duration) error { slept = append(slept, d); return nil },
	}, gjeval.ResumeFresh, "", attempt)

	if err != nil {
		t.Fatalf("loop error = %v", err)
	}
	if report == nil || report.RunStatus != gjeval.RunStatusComplete {
		t.Fatalf("final report = %+v, want a completed run", report)
	}
	if calls != 3 {
		t.Fatalf("attempts = %d, want 3", calls)
	}
	// The first attempt honours the caller's policy; every resume pins the same
	// run, so an unrelated manifest appearing in the store cannot capture it.
	if sawPolicy[0] != gjeval.ResumeFresh || sawRunID[0] != "" {
		t.Fatalf("first attempt = %v/%q, want the caller's policy", sawPolicy[0], sawRunID[0])
	}
	for i := 1; i < len(sawPolicy); i++ {
		if sawPolicy[i] != gjeval.ResumeExact || sawRunID[i] != runID {
			t.Fatalf("attempt %d resumed %v/%q, want exact %s", i+1, sawPolicy[i], sawRunID[i], runID)
		}
	}
	// Both attempts made progress, so neither escalated to the stalled delay.
	if len(slept) != 2 || slept[0] != autoResumeDelay || slept[1] != autoResumeDelay {
		t.Fatalf("delays = %v, want two %s waits", slept, autoResumeDelay)
	}
	if !strings.Contains(stderr.String(), "auto-resume attempt 2/12") ||
		!strings.Contains(stderr.String(), "did not respond before the configured timeout") {
		t.Fatalf("progress lines should name the attempt and the reason: %q", stderr.String())
	}
}

// An attempt that completes no new slots learned nothing, so the provider is
// still down and the next attempt should cost less.
func TestAutoResumeBacksOffFurtherWhenAnAttemptMakesNoProgress(t *testing.T) {
	runID := "20260822T090543.000000000Z-abcdef02"
	store := autoResumeTestStore(t, runID, gjagent.ErrorCodeProviderRateLimit)
	var slept []time.Duration
	calls := 0
	attempt := func(context.Context, gjeval.ResumePolicy, string) (*gjeval.Report, error) {
		calls++
		if calls >= 3 {
			return &gjeval.Report{RunID: runID, RunStatus: gjeval.RunStatusComplete}, nil
		}
		return haltedReport(runID, 44, 339), nil // identical progress twice
	}
	if _, err := runAutoResumeBench(context.Background(), autoResumeConfig{
		Enabled: true, MaxAttempts: 12, Store: store, Stderr: &bytes.Buffer{},
		Sleep: func(_ context.Context, d time.Duration) error { slept = append(slept, d); return nil },
	}, gjeval.ResumeFresh, "", attempt); err != nil {
		t.Fatal(err)
	}
	if len(slept) != 2 || slept[0] != autoResumeDelay || slept[1] != autoResumeZeroProgressDelay {
		t.Fatalf("delays = %v, want %s then the stalled %s", slept, autoResumeDelay, autoResumeZeroProgressDelay)
	}
}

// Quota, credentials, and a deliberate interrupt are not weather. Retrying them
// wastes an afternoon and hides the thing that actually needs a human.
func TestAutoResumeStopsOnTerminalConditions(t *testing.T) {
	for name, tc := range map[string]struct {
		code      string
		attemptFn func(context.Context, gjeval.ResumePolicy, string) (*gjeval.Report, error)
		wantErr   bool
	}{
		"provider quota":  {code: gjagent.ErrorCodeProviderQuota},
		"bad credentials": {code: gjagent.ErrorCodeProviderAuth},
		"interrupt": {
			code: gjagent.ErrorCodeProviderTimeout,
			attemptFn: func(context.Context, gjeval.ResumePolicy, string) (*gjeval.Report, error) {
				return nil, errors.New("evaluation run interrupted")
			},
			wantErr: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			runID := "20260822T090543.000000000Z-abcdef03"
			store := autoResumeTestStore(t, runID, tc.code)
			calls := 0
			attempt := func(ctx context.Context, p gjeval.ResumePolicy, id string) (*gjeval.Report, error) {
				calls++
				if tc.attemptFn != nil {
					return tc.attemptFn(ctx, p, id)
				}
				return haltedReport(runID, 44, 339), nil
			}
			slept := 0
			_, err := runAutoResumeBench(context.Background(), autoResumeConfig{
				Enabled: true, MaxAttempts: 12, Store: store, Stderr: &bytes.Buffer{},
				Sleep: func(context.Context, time.Duration) error { slept++; return nil },
			}, gjeval.ResumeFresh, "", attempt)
			if tc.wantErr && err == nil {
				t.Fatal("an interrupt must surface rather than be retried")
			}
			if calls != 1 {
				t.Fatalf("attempts = %d, want the loop to stop after the first", calls)
			}
			if slept != 0 {
				t.Fatal("a terminal halt must not wait")
			}
		})
	}
}

// The cap is what keeps an unattended loop from becoming a forever-loop.
func TestAutoResumeHonoursTheAttemptCap(t *testing.T) {
	runID := "20260822T090543.000000000Z-abcdef04"
	store := autoResumeTestStore(t, runID, gjagent.ErrorCodeProviderTimeout)
	calls := 0
	var stderr bytes.Buffer
	report, err := runAutoResumeBench(context.Background(), autoResumeConfig{
		Enabled: true, MaxAttempts: 3, Store: store, Stderr: &stderr,
		Sleep: func(context.Context, time.Duration) error { return nil },
	}, gjeval.ResumeFresh, "", func(context.Context, gjeval.ResumePolicy, string) (*gjeval.Report, error) {
		calls++
		return haltedReport(runID, 44, 339), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("attempts = %d, want the cap of 3", calls)
	}
	// The caller still receives the halted report and exits on it as usual.
	if report == nil || report.RunStatus != gjeval.RunStatusEnvironmentFailed {
		t.Fatalf("report = %+v, want the last halted run", report)
	}
	if !strings.Contains(stderr.String(), "giving up after 3") {
		t.Fatalf("giving up should say so: %q", stderr.String())
	}
}

// Without the flag the command behaves exactly as it did before: one attempt,
// no waiting, no manifest reads.
func TestAutoResumeDisabledRunsExactlyOnce(t *testing.T) {
	runID := "20260822T090543.000000000Z-abcdef05"
	calls := 0
	report, err := runAutoResumeBench(context.Background(), autoResumeConfig{
		Enabled: false, MaxAttempts: 12, Stderr: &bytes.Buffer{},
		Sleep: func(context.Context, time.Duration) error { t.Fatal("disabled loop must not wait"); return nil },
	}, gjeval.ResumeFresh, "", func(context.Context, gjeval.ResumePolicy, string) (*gjeval.Report, error) {
		calls++
		return haltedReport(runID, 44, 339), nil
	})
	if err != nil || calls != 1 {
		t.Fatalf("attempts = %d, err = %v, want a single attempt", calls, err)
	}
	if report.RunStatus != gjeval.RunStatusEnvironmentFailed {
		t.Fatalf("report = %+v", report)
	}
}
