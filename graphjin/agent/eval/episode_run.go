package eval

import (
	"context"
	"fmt"
	"net/http"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
)

// EpisodeOptions configures a single graded episode.
type EpisodeOptions struct {
	Profile RewardProfile
	// Repeat labels the episode when several are run against one task.
	Repeat int
	// Provenance describes the policy and binary this episode ran against.
	Provenance RunProvenance
}

// EpisodePrep is what an episode learns about its environment before anything
// is asked of the agent: the ground truth, whether the instance has to be put
// back afterwards, and what the rest of the database looked like beforehand.
//
// It exists because an episode is not only a question. A write task resets the
// instance, runs setup steps, waits for the environment to reach a state, and
// records everything the task is not allowed to disturb — and all of that has
// to happen for a caller driving the agent itself just as much as for one
// letting the runner drive it. Two implementations of that choreography would
// eventually disagree, and the disagreement would show up as a score.
type EpisodePrep struct {
	// Oracle is the resolved ground truth, or nil for a task that has none.
	Oracle *OracleResult
	// Resettable is set when the task writes, and is what puts the world back.
	Resettable ResettableInstance
	// CollateralBefore is every row the task must leave alone, as it was.
	CollateralBefore []OracleResult
	client           HTTPDoer
}

func (r Runner) httpClient() HTTPDoer {
	if r.Client != nil {
		return r.Client
	}
	return &http.Client{Timeout: 5 * time.Minute}
}

// BeginEpisodeEnvironment puts the environment into the state a task expects
// and records what the task will be graded against.
//
// On any failure the instance is put back before the error is returned: a task
// that could not be prepared must not leave its half-finished setup behind for
// whatever runs next.
func (r Runner) BeginEpisodeEnvironment(ctx context.Context, instance Instance, task Task) (EpisodePrep, error) {
	if instance == nil {
		return EpisodePrep{}, fmt.Errorf("nil evaluation instance")
	}
	if err := task.Validate(); err != nil {
		return EpisodePrep{}, err
	}
	prep := EpisodePrep{client: r.httpClient()}

	if task.Oracle != nil {
		verifier := Verifier{Client: prep.client, Now: r.Now, BaseURL: instance.BaseURL(), Headers: instance.Headers()}
		resolved, err := verifier.Resolve(ctx, *task.Oracle)
		if err != nil {
			return EpisodePrep{}, fmt.Errorf("resolve oracle: %w", err)
		}
		prep.Oracle = &resolved
	}

	if task.Mutation != nil {
		resettable, ok := instance.(ResettableInstance)
		if !ok {
			return EpisodePrep{}, fmt.Errorf("task %s writes, but this environment cannot reset", task.Slug)
		}
		if err := resettable.Reset(ctx); err != nil {
			return EpisodePrep{}, fmt.Errorf("reset before write: %w", err)
		}
		prep.Resettable = resettable
		if err := prepareMutationEpisode(ctx, r, prep.client, instance, task.Mutation); err != nil {
			_ = resettable.Reset(ctx)
			return EpisodePrep{}, fmt.Errorf("prepare write: %w", err)
		}
		collateral, err := resolveMutationCollateral(ctx, r, prep.client, instance, task.Mutation.Collateral)
		if err != nil {
			_ = resettable.Reset(ctx)
			return EpisodePrep{}, fmt.Errorf("resolve collateral: %w", err)
		}
		prep.CollateralBefore = collateral
	}
	return prep, nil
}

// FinishEpisodeScoring grades a response and puts the environment back.
//
// This is the half a caller driving the agent itself cannot reimplement without
// eventually scoring differently from the benchmark: what the database ended up
// holding, what it must not have touched, and the profile the reward is priced
// under all live here.
func (r Runner) FinishEpisodeScoring(ctx context.Context, instance Instance, task Task, prep EpisodePrep,
	response gjagent.Response, latencyMS int64, profile RewardProfile) (ScoreDetail, *MutationEvidence, error) {
	normalized, err := profile.normalize()
	if err != nil {
		return ScoreDetail{}, nil, err
	}
	client := prep.client
	if client == nil {
		client = r.httpClient()
	}

	detail := Score(task, prep.Oracle, response, latencyMS)
	var evidence *MutationEvidence
	// A provider failure means the agent never ran. Resolving the post-state
	// anyway would find it unchanged and relabel an environment failure as the
	// model declining to do the work.
	if task.Mutation != nil && detail.FailureCategory != "environment_failure" {
		var outcome MutationOutcome
		evidence, outcome = resolveMutationEvidence(ctx, r, client, instance, task, prep.CollateralBefore)
		detail = ScoreMutation(detail, outcome, response)
	}
	if prep.Resettable != nil {
		if err := prep.Resettable.Reset(ctx); err != nil {
			return detail, evidence, fmt.Errorf("reset after write: %w", err)
		}
	}
	// The scorer grades under the published contract. Another profile is repriced
	// from the same observed components rather than re-graded, so the two can
	// never disagree about what happened.
	if normalized != RewardProfileBenchmark {
		detail.Vector.Reward = rewardForProfile(normalized, detail.Vector)
	}
	return detail, evidence, nil
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
	profile, err := opts.Profile.normalize()
	if err != nil {
		return Episode{}, err
	}
	prep, err := r.BeginEpisodeEnvironment(ctx, instance, task)
	if err != nil {
		return Episode{}, err
	}

	oracles := map[string]OracleResult{}
	if prep.Oracle != nil {
		oracles[task.ID] = *prep.Oracle
	}
	runOptions := RunOptions{Repeats: 1, Provenance: opts.Provenance}
	episode := r.runEpisode(ctx, prep.client, instance, runOptions, task, opts.Repeat, false, oracles, prep.CollateralBefore)
	if prep.Resettable != nil {
		if err := prep.Resettable.Reset(ctx); err != nil {
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
