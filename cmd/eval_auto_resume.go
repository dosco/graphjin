package main

import (
	"context"
	"fmt"
	"io"
	"time"

	gjeval "github.com/dosco/graphjin/agent/v3/eval"
)

// A public benchmark run is hours of provider traffic, and providers have bad
// afternoons: one Together window degraded far enough that a 339-slot run
// halted four times on provider_timeout alone. Every halt is individually
// correct — scoring provider weather as model failure is the thing the
// environment classifier exists to prevent — but it leaves the run parked until
// a human retypes the resume command.
//
// Resuming costs nothing: completed episodes are reused, so the only price of
// waiting is time. That makes the retry loop pure operations, and operations
// belong in the tool rather than in whatever shell script the operator
// improvises at 3am.
const (
	// autoResumeDelay is the pause after a halt that still made progress.
	// Providers recover on the order of minutes, and each attempt re-resolves
	// every oracle against the local demo, so a tight cadence buys nothing.
	autoResumeDelay = 20 * time.Minute
	// autoResumeZeroProgressDelay backs off further when an attempt completed
	// no new slots. Nothing was learned, so the provider is still down and the
	// next attempt should cost less.
	autoResumeZeroProgressDelay = 60 * time.Minute
	// defaultAutoResumeAttempts bounds the loop. Twelve attempts spans a bad
	// afternoon at these delays without becoming an unattended forever-loop.
	defaultAutoResumeAttempts = 12
)

// benchAttemptFunc runs one complete Prepare/approve/Execute/Close cycle. It is
// a function so the loop can be tested without a demo instance, and so each
// attempt scopes its own PreparedRun: the run lock is released only by Close,
// which means the next attempt cannot start until this one has finished.
type benchAttemptFunc func(ctx context.Context, policy gjeval.ResumePolicy, resumeRunID string) (*gjeval.Report, error)

type autoResumeConfig struct {
	Enabled     bool
	MaxAttempts int
	Store       *gjeval.Store
	Provider    string
	Stderr      io.Writer
	// Sleep and Now are injected so tests can drive the schedule without
	// waiting out real backoff.
	Sleep func(context.Context, time.Duration) error
	Now   func() time.Time
}

func (c autoResumeConfig) sleep(ctx context.Context, d time.Duration) error {
	if c.Sleep != nil {
		return c.Sleep(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// runAutoResumeBench runs attempt until the run completes, hits a halt not
// worth resuming, or exhausts its attempts. The returned report is always the
// last one produced, so the caller prints and exits on it exactly as it would
// without the loop.
func runAutoResumeBench(
	ctx context.Context,
	cfg autoResumeConfig,
	policy gjeval.ResumePolicy,
	resumeRunID string,
	attempt benchAttemptFunc,
) (*gjeval.Report, error) {
	maxAttempts := cfg.MaxAttempts
	if !cfg.Enabled || maxAttempts <= 0 {
		maxAttempts = 1
	}
	previousProgress := -1
	for number := 1; ; number++ {
		report, err := attempt(ctx, policy, resumeRunID)
		// A transport or preparation failure is not provider weather. This also
		// covers a deliberate interrupt and an incompatible resume, both of
		// which must surface rather than be retried into.
		if err != nil || report == nil {
			return report, err
		}
		if !cfg.Enabled || report.RunStatus != gjeval.RunStatusEnvironmentFailed {
			return report, nil
		}
		// The halting code lives only on the manifest: finishIncomplete returns
		// a nil error and the report carries the fact of an environment failure
		// without naming it.
		code := ""
		if cfg.Store != nil {
			if manifest, mErr := cfg.Store.LoadManifest(report.RunID); mErr == nil && manifest != nil {
				code = manifest.LastEnvironmentCode
			}
		}
		reason := gjeval.FriendlyStopReason(code, cfg.Provider)
		if !gjeval.TransientEnvironmentCode(code) {
			fmt.Fprintf(cfg.Stderr, "auto-resume stopping: %s; this needs attention rather than another attempt\n", reason)
			return report, nil
		}
		if number >= maxAttempts {
			fmt.Fprintf(cfg.Stderr, "auto-resume giving up after %d attempt(s): %s\n", number, reason)
			return report, nil
		}

		progress := report.Progress.CompletedInitialSlots + report.Progress.CompletedConfirmation
		delay := autoResumeDelay
		if previousProgress >= 0 && progress <= previousProgress {
			delay = autoResumeZeroProgressDelay
		}
		previousProgress = progress

		fmt.Fprintf(cfg.Stderr, "auto-resume attempt %d/%d: %s; resuming run %s in %s (%d of %d attempts complete)\n",
			number+1, maxAttempts, reason, report.RunID, delay,
			progress, report.Progress.PlannedInitialSlots)
		if err := cfg.sleep(ctx, delay); err != nil {
			return report, nil
		}
		// Pin the run being resumed. Auto-picking a manifest mid-loop could
		// silently switch runs after an unrelated run appeared in the store.
		policy, resumeRunID = gjeval.ResumeExact, report.RunID
	}
}
