package eval

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// EpisodeOptions configures a single graded episode.
type EpisodeOptions struct {
	Profile RewardProfile
	// Repeat labels the episode when several are run against one task.
	Repeat int
	// Provenance describes the policy and binary this episode ran against.
	Provenance RunProvenance
}

// RunEpisode runs one task against one instance and grades it.
//
// The suite runner exists to measure a fixed set of tasks and record a report;
// a rollout wants one episode at a time and wants the result back rather than
// on disk. Both go through the same execution and scoring, so an episode served
// to a trainer and an episode in a published run are graded identically — a
// rollout loop that scored differently from the benchmark would be optimizing
// against a number nobody else can reproduce.
//
// A write resets the instance before and after, exactly as the suite runner
// does. The instance must be resettable for such a task, because a write left
// in place would silently change every episode that follows it.
func (r Runner) RunEpisode(ctx context.Context, instance Instance, task Task, opts EpisodeOptions) (Episode, error) {
	if instance == nil {
		return Episode{}, fmt.Errorf("nil evaluation instance")
	}
	if err := task.Validate(); err != nil {
		return Episode{}, err
	}
	profile, err := opts.Profile.normalize()
	if err != nil {
		return Episode{}, err
	}
	client := r.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	runOptions := RunOptions{Repeats: 1, Provenance: opts.Provenance}

	oracles := map[string]OracleResult{}
	if task.Oracle != nil {
		verifier := Verifier{Client: client, Now: r.Now, BaseURL: instance.BaseURL(), Headers: instance.Headers()}
		resolved, err := verifier.Resolve(ctx, *task.Oracle)
		if err != nil {
			return Episode{}, fmt.Errorf("resolve oracle: %w", err)
		}
		oracles[task.ID] = resolved
	}

	var resettable ResettableInstance
	var collateralBefore []OracleResult
	if task.Mutation != nil {
		ok := false
		if resettable, ok = instance.(ResettableInstance); !ok {
			return Episode{}, fmt.Errorf("task %s writes, but this environment cannot reset", task.Slug)
		}
		if err := resettable.Reset(ctx); err != nil {
			return Episode{}, fmt.Errorf("reset before write: %w", err)
		}
		if err := prepareMutationEpisode(ctx, r, client, instance, task.Mutation); err != nil {
			_ = resettable.Reset(ctx)
			return Episode{}, fmt.Errorf("prepare write: %w", err)
		}
		collateralBefore, err = resolveMutationCollateral(ctx, r, client, instance, task.Mutation.Collateral)
		if err != nil {
			_ = resettable.Reset(ctx)
			return Episode{}, fmt.Errorf("resolve collateral: %w", err)
		}
	}

	episode := r.runEpisode(ctx, client, instance, runOptions, task, opts.Repeat, false, oracles, collateralBefore)
	if resettable != nil {
		if err := resettable.Reset(ctx); err != nil {
			return episode, fmt.Errorf("reset after write: %w", err)
		}
	}
	// The runner grades under the published contract. A rollout asking for the
	// training profile is repriced from the same observed components rather than
	// re-graded, so the two can never disagree about what happened.
	if profile != RewardProfileBenchmark {
		episode.Score.Vector.Reward = rewardForProfile(profile, episode.Score.Vector)
	}
	return episode, nil
}
