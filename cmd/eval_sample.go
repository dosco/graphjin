package main

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	gjeval "github.com/dosco/graphjin/agent/v3/eval"
	"github.com/spf13/cobra"
)

// Collecting attempts rather than measuring them.
//
// A benchmark asks each question a fixed number of times and reaches a verdict.
// Training wants the opposite shape: many attempts at each question, drawn at a
// temperature high enough that they differ, kept or discarded by whether they
// passed. The scoring is identical — that is the point, since a policy trained
// against one contract and measured against another is optimizing a number
// nobody else can reproduce — but the run has no baseline, promotes nothing,
// and reaches no verdict.
//
// It is a separate command rather than flags on `eval run` because those are
// different questions. A run that quietly took `--repeats 8 --temperature 0.9`
// would still print an acceptance verdict, and that verdict would be red for a
// reason that has nothing to do with the model: raising the temperature loses
// against a greedy baseline every time.

const (
	sampleDefaultRepeats = 4
	sampleMaxRepeats     = 100
)

func evalSampleCmd(opts *evalCLIOptions) *cobra.Command {
	var (
		repeats     int
		temperature float64
		topP        float64
		splitPath   string
		side        string
	)
	command := &cobra.Command{
		Use:   "sample",
		Short: "Collect several graded attempts at each task, for building a training corpus",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if repeats < 1 || repeats > sampleMaxRepeats {
				return fmt.Errorf("--repeats must be between 1 and %d, got %d", sampleMaxRepeats, repeats)
			}
			projectPath, target, err := resolveEvalTarget(cmd, opts)
			if err != nil {
				return err
			}
			sampling := gjeval.EnvSpec{}
			if cmd.Flags().Changed("temperature") {
				sampling.Temperature = &temperature
			}
			if cmd.Flags().Changed("top-p") {
				sampling.TopP = &topP
			}
			// A remote server samples the way its own operator configured it.
			// Accepting the flag and silently not applying it would record a
			// temperature in provenance that the run never used.
			if target == gjeval.TargetRemote && (sampling.Temperature != nil || sampling.TopP != nil) {
				return fmt.Errorf(
					"--temperature and --top-p configure a locally booted environment; a remote server samples as its own operator set it")
			}

			suite, err := gjeval.LoadSuite(evalSuitePath(projectPath))
			if err != nil {
				return &evalExitError{Code: 2, Err: err}
			}
			selected, provenanceSide, fingerprint, err := sampleSuiteSide(*suite, splitPath, side)
			if err != nil {
				return &evalExitError{Code: 2, Err: err}
			}
			if sampling.Temperature == nil && os.Getenv("GJ_AGENT_TEMPERATURE") == "" {
				fmt.Fprintln(cmd.ErrOrStderr(),
					"  note: no temperature configured, so every repeat of a task will draw the same program; "+
						"pass --temperature (or set GJ_AGENT_TEMPERATURE) to collect attempts that differ.")
			}
			return runEvalSample(cmd.Context(), cmd, opts, projectPath, target, selected,
				repeats, sampling, provenanceSide, fingerprint)
		},
	}
	command.Flags().IntVar(&repeats, "repeats", sampleDefaultRepeats, "attempts to collect for each task")
	command.Flags().Float64Var(&temperature, "temperature", 0, "sampling temperature for the model under evaluation")
	command.Flags().Float64Var(&topP, "top-p", 0, "nucleus sampling cutoff for the model under evaluation")
	command.Flags().StringVar(&splitPath, "split", "", "split manifest naming which tasks are held out")
	command.Flags().StringVar(&side, "side", "train", "which side of the split to sample: train or eval")
	addEvalResumeFlags(command, opts)
	return command
}

// sampleSuiteSide narrows the suite to one side of a split.
//
// The split has to be the one this suite was cut from: a manifest generated
// against a different suite names task ids that no longer exist, so filtering
// by it would silently produce an empty or arbitrary set rather than the
// holdout somebody intended.
func sampleSuiteSide(suite gjeval.Suite, splitPath, side string) (gjeval.Suite, string, string, error) {
	if strings.TrimSpace(splitPath) == "" {
		return suite, "", "", nil
	}
	side = strings.ToLower(strings.TrimSpace(side))
	if side != "train" && side != "eval" {
		return gjeval.Suite{}, "", "", fmt.Errorf("--side must be train or eval, got %q", side)
	}
	split, err := gjeval.LoadSplit(splitPath)
	if err != nil {
		return gjeval.Suite{}, "", "", err
	}
	if want := gjeval.SuiteFingerprint(suite); split.SuiteFingerprint != "" && split.SuiteFingerprint != want {
		return gjeval.Suite{}, "", "", fmt.Errorf(
			"%s was cut from a different suite (%s) than the one being sampled (%s)",
			splitPath, split.SuiteFingerprint, want)
	}
	filtered := suite
	filtered.Tasks = nil
	for _, task := range suite.Tasks {
		if split.SideOf(task.ID) == side {
			filtered.Tasks = append(filtered.Tasks, task)
		}
	}
	if len(filtered.Tasks) == 0 {
		return gjeval.Suite{}, "", "", fmt.Errorf("the %s side of %s contains none of this suite's tasks", side, splitPath)
	}
	filtered.Generator.Scale = len(filtered.Tasks)
	return filtered, side, split.Fingerprint(), nil
}

func runEvalSample(ctx context.Context, cmd *cobra.Command, opts *evalCLIOptions, projectPath string,
	target gjeval.Target, suite gjeval.Suite, repeats int, sampling gjeval.EnvSpec,
	side, splitFingerprint string) error {
	policy, err := evalResumePolicy(opts)
	if err != nil {
		return err
	}
	frozenClock, err := evalFrozenClock(opts)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	const seed = 23
	writable, reactive, resettable := evalSuiteEnvironmentRequirements(suite)
	spec := gjeval.EnvSpec{
		Target: target, ConfigPath: projectPath, Seed: seed,
		Writable: writable, Reactive: reactive, Resettable: resettable,
		PinDataAnchor: evalResumeDataAnchor(projectPath, policy, opts.ResumeRunID),
		FreezeTime:    opts.FreezeTime,
		Temperature:   sampling.Temperature, TopP: sampling.TopP,
	}
	environment := evalEnvironment{StatusOut: os.Stderr}
	var pool *evalInstancePool
	var instance gjeval.Instance
	if opts.Pool > 1 {
		pool, err = newEvalInstancePool(ctx, func(int) evalEnvironment { return environment }, spec, opts.Pool)
		if err != nil {
			return evalEnvironmentError(err)
		}
		defer pool.Close() //nolint:errcheck
		instance = pool.instances[0]
	} else {
		instance, err = environment.Start(ctx, spec)
		if err != nil {
			return evalEnvironmentError(err)
		}
		defer instance.Close() //nolint:errcheck
	}
	status, err := ensureEvalAgentReady(ctx, &http.Client{Timeout: 30 * time.Second}, instance)
	if err != nil {
		return evalEnvironmentError(err)
	}
	store := evalStore(projectPath, status)

	provenance := evalProvenance(instance, seed, status)
	provenance.Repeats = repeats
	provenance.SplitSide = side
	provenance.SplitFingerprint = splitFingerprint

	prepared, err := (gjeval.Runner{Client: evalAgentClient(status), Now: frozenClock}).Prepare(ctx, suite, instance, gjeval.RunOptions{
		Mode: gjeval.RunModeSample, Intent: gjeval.RunIntentSample,
		Repeats: repeats, Seed: seed, Provenance: provenance, Store: store,
		ResumePolicy: policy, ResumeRunID: opts.ResumeRunID,
		BinaryFingerprint: evalBinaryFingerprint(),
		InvocationArgs:    evalSampleInvocationArgs(opts, projectPath, repeats, side),
		Concurrency:       opts.Concurrency, Pool: poolForRun(pool),
	})
	if err != nil {
		return evalEnvironmentError(err)
	}
	defer prepared.Close() //nolint:errcheck
	if preview := prepared.Preview(); preview.MaximumProviderAttempts > 0 {
		if err := approveProviderTraffic(cmd, opts.Yes, preview.String()); err != nil {
			return err
		}
	}
	report, err := prepared.Execute(ctx)
	if err != nil {
		if report != nil {
			printEvalSampleSummary(cmd, report, store, side)
		}
		return evalExecutionError(err)
	}
	printEvalSampleSummary(cmd, report, store, side)
	return nil
}

// printEvalSampleSummary reports what was collected.
//
// Deliberately not the run report: that one leads with an acceptance verdict,
// and a sampling run has none. What matters here is how many attempts at each
// task came back passing, because that is what there is to train on.
func printEvalSampleSummary(cmd *cobra.Command, report *gjeval.Report, store *gjeval.Store, side string) {
	if report == nil {
		return
	}
	out := cmd.OutOrStdout()
	type row struct {
		slug   string
		passed int
		count  int
		reward float64
	}
	rows := make([]row, 0, len(report.Tasks))
	passed, total := 0, 0
	for _, task := range report.Tasks {
		// Consistency is the fraction of this task's attempts that passed, which
		// is the number worth reporting here: it is exactly how much of what was
		// collected is usable.
		taskPassed := int(math.Round(task.Consistency * float64(task.EpisodeCount)))
		rows = append(rows, row{slug: task.Slug, passed: taskPassed, count: task.EpisodeCount, reward: task.MeanReward})
		passed += taskPassed
		total += task.EpisodeCount
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].slug < rows[j].slug })

	where := "the whole suite"
	if side != "" {
		where = "the " + side + " side"
	}
	fmt.Fprintf(out, "Collected %d attempt(s) across %d task(s) from %s; %d passed.\n",
		total, len(report.Tasks), where, passed)
	for _, item := range rows {
		fmt.Fprintf(out, "  %-44s %d/%d  mean reward %.3f\n", item.slug, item.passed, item.count, item.reward)
	}
	if total != 0 {
		fmt.Fprintf(out, "Passed at least once %.3f of tasks, every time %.3f.\n",
			report.Metrics.PassAtK, report.Metrics.PassPowerK)
	}
	if store != nil {
		fmt.Fprintf(out, "Episodes: %s\n", filepath.Join(store.Root, "episodes", report.RunID))
	}
	fmt.Fprintf(out, "Next: graphjin eval export %s", report.RunID)
	if side != "" {
		fmt.Fprintf(out, " --side %s", side)
	}
	fmt.Fprintln(out, " --stage executor")
}

func evalSampleInvocationArgs(opts *evalCLIOptions, projectPath string, repeats int, side string) []string {
	args := []string{"eval", "sample", "--path", projectPath, "--repeats", fmt.Sprint(repeats)}
	if side != "" {
		args = append(args, "--side", side)
	}
	if opts != nil && opts.FreezeTime != "" {
		args = append(args, "--freeze-time", opts.FreezeTime)
	}
	return args
}
