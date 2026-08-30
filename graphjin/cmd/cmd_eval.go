package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	gjeval "github.com/dosco/graphjin/agent/v3/eval"
	"github.com/dosco/graphjin/serv/v3"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// Let the agent service enforce its own timeout and return a structured
// response before the Eval transport gives up on the request.
const evalAgentHTTPTimeout = time.Duration(demoAgentTimeoutSeconds+30) * time.Second

type evalExitError struct {
	Code int
	Err  error
}

func (e *evalExitError) Error() string {
	if e == nil || e.Err == nil {
		return "evaluation failed"
	}
	return e.Err.Error()
}

func (e *evalExitError) Unwrap() error { return e.Err }

type evalCLIOptions struct {
	Demo               bool
	Remote             bool
	Yes                bool
	JSON               bool
	Debug              bool
	ResumeRunID        string
	Restart            bool
	Concurrency        int
	AutoResume         bool
	AutoResumeAttempts int
	FreezeTime         string
	Pool               int
}

// evalFrozenClock returns the clock the harness and its oracles read, or nil to
// read the wall clock. It is the harness half of --freeze-time: the environment
// freezes what the agent is told "today" is, and this freezes what the oracle
// resolving {{today}} believes, so both sides of a graded comparison are asking
// about the same day.
func evalFrozenClock(opts *evalCLIOptions) (func() time.Time, error) {
	if opts == nil || strings.TrimSpace(opts.FreezeTime) == "" {
		return nil, nil
	}
	frozen, _, err := (gjeval.EnvSpec{FreezeTime: opts.FreezeTime}).FrozenTime()
	if err != nil {
		return nil, err
	}
	return func() time.Time { return frozen }, nil
}

func evalCmd() *cobra.Command {
	opts := &evalCLIOptions{}
	cmd := &cobra.Command{
		Use:          "eval",
		Short:        "Create and run GraphJin agent evaluations",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return evalStatus(cmd, opts)
		},
	}
	cmd.PersistentFlags().BoolVar(&opts.Demo, "demo", false, "use the built-in or selected local demo")
	cmd.PersistentFlags().BoolVar(&opts.Remote, "remote", false, "use the server configured by graphjin cli setup")
	cmd.PersistentFlags().BoolVar(&opts.Yes, "yes", false, "approve provider-backed model traffic and saves")
	cmd.PersistentFlags().BoolVar(&opts.JSON, "json", false, "emit machine-readable JSON")
	cmd.PersistentFlags().BoolVar(&opts.Debug, "debug", false, "print local episode paths and verbose diagnoses")
	cmd.PersistentFlags().StringVar(&opts.FreezeTime, "freeze-time", "", "run against a fixed clock (RFC3339); pins the demo's data anchor to the same day")

	cmd.AddCommand(evalCreateCmd(opts))
	cmd.AddCommand(evalAddCmd(opts))
	cmd.AddCommand(evalRemoveCmd(opts))
	cmd.AddCommand(evalRunCmd(opts, false))
	cmd.AddCommand(evalBaselineCmd(opts))
	cmd.AddCommand(evalBenchCmd(opts))
	cmd.AddCommand(evalRescoreCmd(opts))
	cmd.AddCommand(evalPublishCmd(opts))
	cmd.AddCommand(evalFreezeSuiteCmd(opts))
	cmd.AddCommand(evalImportCmd())
	return cmd
}

func addEvalResumeFlags(cmd *cobra.Command, opts *evalCLIOptions) {
	cmd.Flags().StringVar(&opts.ResumeRunID, "resume", "", "resume one compatible incomplete run by id")
	cmd.Flags().BoolVar(&opts.Restart, "restart", false, "start a fresh run without deleting incomplete state")
	cmd.Flags().IntVar(&opts.Concurrency, "concurrency", 1, "episodes in flight at once; mutation episodes still run exclusively (max 16)")
	cmd.Flags().IntVar(&opts.Pool, "pool", 0, "run against N isolated demo environments so a write owns only its own world")
}

func evalResumePolicy(opts *evalCLIOptions) (gjeval.ResumePolicy, error) {
	if opts.Restart && strings.TrimSpace(opts.ResumeRunID) != "" {
		return "", errors.New("--resume and --restart are mutually exclusive")
	}
	if opts.Restart {
		return gjeval.ResumeFresh, nil
	}
	if strings.TrimSpace(opts.ResumeRunID) != "" {
		return gjeval.ResumeExact, nil
	}
	return gjeval.ResumeAuto, nil
}

func evalRemoveCmd(opts *evalCLIOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "rm <task-id>",
		Short: "Remove a task through the validated suite writer",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectPath, _, err := resolveEvalTarget(cmd, opts)
			if err != nil {
				return err
			}
			path := evalSuitePath(projectPath)
			suite, err := gjeval.LoadSuite(path)
			if err != nil {
				return &evalExitError{Code: 2, Err: err}
			}
			taskID := strings.TrimSpace(args[0])
			index := -1
			for i := range suite.Tasks {
				if suite.Tasks[i].ID == taskID {
					index = i
					break
				}
			}
			if index == -1 {
				return fmt.Errorf("evaluation task %q not found", taskID)
			}
			if len(suite.Tasks) == 1 {
				return errors.New("cannot remove the last evaluation task; recreate the suite instead")
			}
			removed := suite.Tasks[index]
			if !opts.Yes {
				if !isInteractiveTTY() {
					return errors.New("task removal requires --yes in non-interactive mode")
				}
				ok, err := promptConfirm(newPromptIO(cmd.InOrStdin(), cmd.OutOrStdout()), fmt.Sprintf("Remove evaluation task %s (%s)?", removed.ID, removed.Slug), false)
				if err != nil {
					return err
				}
				if !ok {
					return errors.New("task removal aborted")
				}
			}
			suite.Tasks = append(suite.Tasks[:index], suite.Tasks[index+1:]...)
			if err := gjeval.SaveSuite(path, *suite); err != nil {
				return err
			}
			if opts.JSON {
				return writeEvalJSON(cmd.OutOrStdout(), map[string]any{"status": "removed", "task_id": removed.ID, "suite": path, "tasks": len(suite.Tasks)})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed task %s (%s) from %s.\n", removed.ID, removed.Slug, path)
			return nil
		},
	}
}

func evalCreateCmd(opts *evalCLIOptions) *cobra.Command {
	var (
		scale       int
		seed        int64
		families    []string
		composition string
		concurrency int
		out         string
		splitRatio  float64
		splitHold   []string
	)
	command := &cobra.Command{
		Use:   "create",
		Short: "Generate a verified catalog-derived evaluation suite",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			projectPath, target, err := resolveEvalTarget(cmd, opts)
			if err != nil {
				return err
			}
			frozenClock, err := evalFrozenClock(opts)
			if err != nil {
				return err
			}
			instance, err := (evalEnvironment{StatusOut: os.Stderr}).Start(cmd.Context(), gjeval.EnvSpec{Target: target, ConfigPath: projectPath, Seed: seed, FreezeTime: opts.FreezeTime})
			if err != nil {
				return evalEnvironmentError(err)
			}
			defer instance.Close() //nolint:errcheck
			client := &http.Client{Timeout: 120 * time.Second}
			source := gjeval.HTTPCatalogSource{Client: client, BaseURL: instance.BaseURL(), Headers: instance.Headers()}
			verifier := &gjeval.Verifier{Client: client, BaseURL: instance.BaseURL(), Headers: instance.Headers(), Now: frozenClock}
			suite, err := (gjeval.Generator{Source: source, Verifier: verifier}).Generate(cmd.Context(), gjeval.GeneratorOptions{
				Seed: seed, Scale: scale, Families: families,
				Composition: gjeval.Composition(composition), VerifyConcurrency: concurrency,
			})
			if err != nil {
				return &evalExitError{Code: 2, Err: err}
			}
			path := evalSuitePath(projectPath)
			if strings.TrimSpace(out) != "" {
				path = out
			}
			if err := gjeval.SaveSuite(path, *suite); err != nil {
				return err
			}
			splitPath := ""
			if splitRatio > 0 {
				split, splitErr := gjeval.SplitSuite(*suite, splitRatio, splitHold)
				if splitErr != nil {
					return splitErr
				}
				splitPath = strings.TrimSuffix(path, filepath.Ext(path)) + ".split.json"
				if err := gjeval.SaveSplit(splitPath, split); err != nil {
					return err
				}
			}
			// Counting the suite by family is what makes "the new families ran"
			// checkable. A total task count cannot distinguish a family that
			// produced nothing from one whose every candidate lost the sampling.
			byFamily := map[string]int{}
			for _, task := range suite.Tasks {
				byFamily[task.Provenance.Source]++
			}
			if opts.JSON {
				return writeEvalJSON(cmd.OutOrStdout(), map[string]any{
					"status": "created", "suite": path, "tasks": len(suite.Tasks),
					"seed": suite.Generator.Seed, "scale": suite.Generator.Scale,
					"generator_version": suite.Generator.Version, "composition": string(gjeval.Composition(composition)),
					"catalog_fingerprint": suite.CatalogFingerprint, "tasks_by_family": byFamily,
					"split": splitPath,
				})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created %s with %d verified tasks (seed %d).\n", path, len(suite.Tasks), suite.Generator.Seed)
			if splitPath != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Wrote split manifest %s.\n", splitPath)
			}
			for _, name := range sortedKeys(byFamily) {
				fmt.Fprintf(cmd.OutOrStdout(), "  %-32s %d\n", name, byFamily[name])
			}
			return nil
		},
	}
	command.Flags().IntVar(&scale, "scale", gjeval.DefaultSuiteSize, "number of intent-tier tasks to select")
	command.Flags().Int64Var(&seed, "seed", 23, "generation seed")
	command.Flags().StringSliceVar(&families, "families", nil, "restrict generation to these task families (default: all)")
	command.Flags().StringVar(&composition, "composition", string(gjeval.CompositionBenchmark), "sampling composition: benchmark or coverage")
	command.Flags().IntVar(&concurrency, "verify-concurrency", 1, "how many candidate oracles to verify at once")
	command.Flags().StringVar(&out, "out", "", "write the suite here instead of the project's eval directory")
	command.Flags().Float64Var(&splitRatio, "split", 0, "also write a train/eval split manifest with this training share")
	command.Flags().StringSliceVar(&splitHold, "split-holdout-families", nil, "families never placed in the training side")
	return command
}

func sortedKeys(values map[string]int) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func evalAddCmd(opts *evalCLIOptions) *cobra.Command {
	return &cobra.Command{
		Use:   `add "<question>"`,
		Short: "Add one model-assisted business question with a verified hidden oracle",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := approveProviderTraffic(cmd, opts.Yes, "1 agent authoring episode (up to the configured step limit) plus 1-2 read-only oracle queries"); err != nil {
				return err
			}
			projectPath, target, err := resolveEvalTarget(cmd, opts)
			if err != nil {
				return err
			}
			suitePath := evalSuitePath(projectPath)
			suite, err := gjeval.LoadSuite(suitePath)
			if err != nil {
				return &evalExitError{Code: 2, Err: fmt.Errorf("load suite (run `graphjin eval create` first): %w", err)}
			}
			instance, err := (evalEnvironment{StatusOut: os.Stderr}).Start(cmd.Context(), gjeval.EnvSpec{Target: target, ConfigPath: projectPath})
			if err != nil {
				return evalEnvironmentError(err)
			}
			defer instance.Close() //nolint:errcheck
			status, err := ensureEvalAgentReady(cmd.Context(), &http.Client{Timeout: 30 * time.Second}, instance)
			if err != nil {
				return evalEnvironmentError(err)
			}
			client := evalAgentClient(status)
			author := gjeval.Author{Client: client, Verifier: gjeval.Verifier{Client: client}}
			question := args[0]
			for attempt := 0; attempt < 2; attempt++ {
				task, result, proposal, err := author.Propose(cmd.Context(), instance.BaseURL(), instance.Headers(), question)
				if err != nil {
					return err
				}
				if proposal.Status == "needs_clarification" {
					if opts.Yes || !isInteractiveTTY() {
						return fmt.Errorf("question needs clarification: %s", proposal.Clarification)
					}
					answer, err := readEvalLine(cmd.InOrStdin(), cmd.OutOrStdout(), proposal.Clarification)
					if err != nil {
						return err
					}
					question = args[0] + "\nClarification: " + answer
					continue
				}
				preview := map[string]any{"interpretation": proposal.Interpretation, "executed_result": result.Value, "dimension": result.Dimension, "task_id": task.ID}
				if opts.JSON && !opts.Yes {
					if err := writeEvalJSON(cmd.OutOrStdout(), preview); err != nil {
						return err
					}
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "Interpretation: %s\nExecuted oracle result: %s", proposal.Interpretation, result.Value)
					if result.Dimension != "" {
						fmt.Fprintf(cmd.OutOrStdout(), " (%s)", result.Dimension)
					}
					fmt.Fprintln(cmd.OutOrStdout())
				}
				if !opts.Yes {
					ok, err := promptConfirm(newPromptIO(cmd.InOrStdin(), cmd.OutOrStdout()), "Save this evaluation task?", false)
					if err != nil || !ok {
						if err != nil {
							return err
						}
						return errors.New("save aborted")
					}
				}
				suite.Tasks = append(suite.Tasks, task)
				suite.CatalogFingerprint = instance.Fingerprint().CatalogHash
				if err := gjeval.SaveSuite(suitePath, *suite); err != nil {
					return err
				}
				if opts.JSON && opts.Yes {
					preview["status"] = "saved"
					preview["suite"] = suitePath
					if err := writeEvalJSON(cmd.OutOrStdout(), preview); err != nil {
						return err
					}
				} else if !opts.JSON {
					fmt.Fprintf(cmd.OutOrStdout(), "Saved task %s to %s.\n", task.ID, suitePath)
				}
				return nil
			}
			return errors.New("question remained ambiguous after clarification")
		},
	}
}

func evalRunCmd(opts *evalCLIOptions, promote bool) *cobra.Command {
	use, short := "run", "Run the current evaluation suite"
	if promote {
		use, short = "baseline", "Run and deliberately promote a passing baseline"
	}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			projectPath, target, err := resolveEvalTarget(cmd, opts)
			if err != nil {
				return err
			}
			suite, err := gjeval.LoadSuite(evalSuitePath(projectPath))
			if err != nil {
				return &evalExitError{Code: 2, Err: err}
			}
			report, _, err := executeEvalSuite(cmd.Context(), cmd, opts, projectPath, target, *suite, gjeval.RunModeRun, 23, !promote, promote, 0)
			if err != nil {
				return err
			}
			if promote {
				if !report.Acceptance.HardPass {
					return &evalExitError{Code: 1, Err: errors.New("cannot promote a failing evaluation")}
				}
				if !opts.JSON {
					fmt.Fprintln(cmd.OutOrStdout(), "Promoted this run as the baseline.")
				}
			}
			return evalReportExit(report)
		},
	}
	addEvalResumeFlags(cmd, opts)
	return cmd
}

func evalBaselineCmd(opts *evalCLIOptions) *cobra.Command { return evalRunCmd(opts, true) }

func evalBenchCmd(opts *evalCLIOptions) *cobra.Command {
	var scale int
	var seed int64
	var public bool
	cmd := &cobra.Command{
		Use:   "bench",
		Short: "Generate and run the extended stratified benchmark distribution",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if public {
				spec := gjeval.PublicBenchmark()
				if opts.Remote {
					return errors.New("--public cannot be combined with --remote; the public benchmark runs against the pinned demo")
				}
				// The board's numbers are all measured on one world. Spreading a
				// public run across several would make its result incomparable
				// with every run already published.
				if opts.Pool > 1 {
					return errors.New("--public cannot be combined with --pool; the published benchmark runs against a single environment")
				}
				if cmd.Flags().Changed("scale") || cmd.Flags().Changed("seed") {
					return fmt.Errorf("--public pins --scale=%d and --seed=%d; remove the scale and seed overrides", spec.Scale, spec.Seed)
				}
				scale, seed = spec.Scale, spec.Seed
				opts.Demo = true
			}
			if scale <= 0 {
				return errors.New("--scale must be positive")
			}
			if opts.AutoResume && !opts.Yes {
				// Every resume re-runs the traffic approval. An unattended loop
				// that parks on a prompt at 3am is worse than no loop at all.
				return errors.New("--auto-resume runs unattended across provider outages; approve provider traffic up front with --yes")
			}
			policy, err := evalResumePolicy(opts)
			if err != nil {
				return err
			}
			projectPath, target, err := resolveEvalTarget(cmd, opts)
			if err != nil {
				return err
			}
			frozenClock, err := evalFrozenClock(opts)
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			benchSpec := evalBenchEnvSpec(target, projectPath, seed, public, opts.FreezeTime)
			benchSpec.PinDataAnchor = evalResumeDataAnchor(projectPath, policy, opts.ResumeRunID)
			instance, err := (evalEnvironment{StatusOut: os.Stderr}).Start(ctx, benchSpec)
			if err != nil {
				return evalEnvironmentError(err)
			}
			defer instance.Close() //nolint:errcheck
			status, err := ensureEvalAgentReady(ctx, &http.Client{Timeout: 30 * time.Second}, instance)
			if err != nil {
				return evalEnvironmentError(err)
			}
			catalogClient := &http.Client{Timeout: 120 * time.Second}
			var suite *gjeval.Suite
			if public {
				suite, err = loadPublicEvalSuite()
			} else {
				suite, err = (gjeval.Generator{
					Source:   gjeval.HTTPCatalogSource{Client: catalogClient, BaseURL: instance.BaseURL(), Headers: instance.Headers()},
					Verifier: &gjeval.Verifier{Client: catalogClient, BaseURL: instance.BaseURL(), Headers: instance.Headers(), Now: frozenClock},
				}).Generate(ctx, gjeval.GeneratorOptions{Seed: seed, Scale: scale, Name: "GraphJin Frontier Benchmark"})
			}
			if err != nil {
				return &evalExitError{Code: 2, Err: err}
			}
			store := evalStore(projectPath, status)
			baseline, err := store.LoadBaseline()
			if err != nil {
				return err
			}
			// One attempt owns one PreparedRun. The run lock is released by
			// Close, so scoping it here — rather than deferring to the end of
			// the command — is what lets a later attempt resume the same run.
			// The instance deliberately stays alive across attempts: restarting
			// it would reboot the demo and shift its date-anchored seed data,
			// changing the dataset fingerprint the resume must match.
			attempt := func(ctx context.Context, policy gjeval.ResumePolicy, resumeRunID string) (*gjeval.Report, error) {
				prepared, err := (gjeval.Runner{Client: evalAgentClient(status), Now: frozenClock}).Prepare(ctx, *suite, instance, gjeval.RunOptions{
					Mode: gjeval.RunModeBenchmark, Intent: gjeval.RunIntentBench, Repeats: gjeval.DefaultRepeats, Seed: seed,
					Provenance: evalProvenance(instance, seed, status), Baseline: baseline, Store: store,
					ResumePolicy: policy, ResumeRunID: resumeRunID, BinaryFingerprint: evalBinaryFingerprint(),
					InvocationArgs: evalInvocationArgs(opts, projectPath, scale, seed, public), Concurrency: opts.Concurrency,
				})
				if err != nil {
					return nil, evalEnvironmentError(err)
				}
				defer prepared.Close() //nolint:errcheck
				if preview := prepared.Preview(); preview.MaximumProviderAttempts > 0 {
					if err := approveProviderTraffic(cmd, opts.Yes, preview.String()); err != nil {
						return nil, err
					}
				}
				return prepared.Execute(ctx)
			}
			report, err := runAutoResumeBench(ctx, autoResumeConfig{
				Enabled:     opts.AutoResume,
				MaxAttempts: opts.AutoResumeAttempts,
				Store:       store,
				Provider:    status.Provider,
				Stderr:      cmd.ErrOrStderr(),
			}, policy, opts.ResumeRunID, attempt)
			if err != nil {
				if report != nil {
					printEvalReport(cmd, opts, report, store)
				}
				return evalExecutionError(err)
			}
			printEvalReport(cmd, opts, report, store)
			return evalReportExit(report)
		},
	}
	cmd.Flags().IntVar(&scale, "scale", 100, "number of verified generated benchmark tasks")
	cmd.Flags().Int64Var(&seed, "seed", 23, "deterministic generator and rollout seed")
	cmd.Flags().BoolVar(&public, "public", false, "run the frozen, reproducible public benchmark suite")
	cmd.Flags().BoolVar(&opts.AutoResume, "auto-resume", false,
		"resume this run automatically when the provider stops it for a transient reason (timeout, rate limit, transport fault, 5xx); requires --yes")
	cmd.Flags().IntVar(&opts.AutoResumeAttempts, "auto-resume-attempts", defaultAutoResumeAttempts,
		"how many times --auto-resume may resume before giving up")
	addEvalResumeFlags(cmd, opts)
	return cmd
}

func evalRescoreCmd(opts *evalCLIOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "rescore <run-id>",
		Short: "Recompute a completed run from its stored episodes without provider traffic",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runID := strings.TrimSpace(args[0])
			stateDir, err := evalStateDirForPublish(cmd, opts)
			if err != nil {
				return err
			}
			report, err := gjeval.RescoreRun(filepath.Join(stateDir, "episodes", runID))
			if err != nil {
				return &evalExitError{Code: 2, Err: err}
			}
			if report.ScoringProvenance == nil {
				report.ScoringProvenance = &gjeval.ScoringProvenance{RewardVersion: gjeval.RewardVersion}
			}
			report.ScoringProvenance.GraphJinCommit = commit
			report.ScoringProvenance.BinaryFingerprint = evalBinaryFingerprint()
			store := gjeval.NewStore(stateDir).WithSecrets(os.Getenv(report.Provenance.APIKeyEnv))
			if _, err := store.WriteReport(report); err != nil {
				return err
			}
			if opts.JSON {
				return writeEvalJSON(cmd.OutOrStdout(), report)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Rescored %s as %s under %s with no provider traffic.\n", runID, report.RunID, report.RewardVersion)
			printEvalReport(cmd, opts, &report, store)
			return evalReportExit(&report)
		},
	}
}

// evalResumeDataAnchor reports the demo data anchor an incomplete run was
// graded against, so the environment can hold the demo there instead of
// shifting dates to today. Read before the demo boots, because the boot itself
// is what moves the data. Empty when starting fresh or when nothing resumable
// is on disk.
func evalResumeDataAnchor(projectPath string, policy gjeval.ResumePolicy, runID string) string {
	if policy == gjeval.ResumeFresh {
		return ""
	}
	store := gjeval.NewStore(filepath.Join(projectPath, gjeval.DefaultStateDir))
	if id := strings.TrimSpace(runID); id != "" {
		manifest, err := store.LoadManifest(id)
		if err != nil || manifest == nil {
			return ""
		}
		return manifest.DatasetFingerprint.DataAnchor
	}
	runs, err := store.ListRuns()
	if err != nil {
		return ""
	}
	anchor := ""
	newest := ""
	for _, manifest := range runs {
		if manifest.Complete() || manifest.DatasetFingerprint.DataAnchor == "" {
			continue
		}
		if stamp := manifest.UpdatedAt.Format(time.RFC3339Nano); stamp > newest {
			newest, anchor = stamp, manifest.DatasetFingerprint.DataAnchor
		}
	}
	return anchor
}

func evalBenchEnvSpec(target gjeval.Target, projectPath string, seed int64, public bool, freezeTime string) gjeval.EnvSpec {
	return gjeval.EnvSpec{
		Target: target, ConfigPath: projectPath, Seed: seed,
		Writable: public, Reactive: public, Resettable: public,
		FreezeTime: freezeTime,
	}
}

func evalFreezeSuiteCmd(opts *evalCLIOptions) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:    "freeze-suite",
		Short:  "Regenerate the frozen public benchmark suite",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if opts.Remote {
				return errors.New("freeze-suite cannot use --remote")
			}
			spec := gjeval.PublicBenchmark()
			opts.Demo = true
			projectPath, target, err := resolveEvalTarget(cmd, opts)
			if err != nil {
				return err
			}
			instance, err := (evalEnvironment{StatusOut: os.Stderr}).Start(cmd.Context(), gjeval.EnvSpec{
				Target: target, ConfigPath: projectPath, Seed: spec.Seed,
				Writable: true, Reactive: true, Resettable: true,
			})
			if err != nil {
				return evalEnvironmentError(err)
			}
			defer instance.Close() //nolint:errcheck
			client := &http.Client{Timeout: 120 * time.Second}
			suite, err := (gjeval.Generator{
				Source:   gjeval.HTTPCatalogSource{Client: client, BaseURL: instance.BaseURL(), Headers: instance.Headers()},
				Verifier: &gjeval.Verifier{Client: client, BaseURL: instance.BaseURL(), Headers: instance.Headers()},
			}).Generate(cmd.Context(), gjeval.GeneratorOptions{Seed: spec.Seed, Scale: spec.Scale, Name: "DeepORG Public Benchmark " + spec.Generation})
			if err != nil {
				return &evalExitError{Code: 2, Err: err}
			}
			if err := gjeval.SaveSuite(output, *suite); err != nil {
				return err
			}
			if err := os.Chmod(output, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Froze %d public benchmark tasks to %s (suite fingerprint %s).\n", len(suite.Tasks), output, gjeval.SuiteFingerprint(*suite))
			return nil
		},
	}
	cmd.Flags().StringVar(&output, "out", "cmd/benchmark/public-suite.json", "output path for the committed public suite")
	return cmd
}

func evalImportCmd() *cobra.Command {
	var behavior, data, output string
	cmd := &cobra.Command{
		Use:    "import-corpus",
		Short:  "Convert the frozen skill-eval corpora into v1 tasks",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			tasks, err := gjeval.ImportCorpora(gjeval.ImportOptions{BehaviorCorpusPath: behavior, DataCorpusPath: data, Seed: 23})
			if err != nil {
				return err
			}
			suite := gjeval.Suite{SchemaVersion: gjeval.SuiteSchemaVersion, Name: "GraphJin Curated Eval Library", CreatedAt: time.Now().UTC(), Generator: gjeval.GeneratorMeta{Version: gjeval.GeneratorVersion, Seed: 23, Scale: len(tasks)}, Tasks: tasks}
			if err := gjeval.SaveSuite(output, suite); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Imported %d tasks to %s.\n", len(tasks), output)
			return nil
		},
	}
	cmd.Flags().StringVar(&behavior, "behavior", "agent/testdata/skill_eval_cases.json", "behavioral corpus path")
	cmd.Flags().StringVar(&data, "data", "agent/testdata/data_eval_cases.json", "data corpus path")
	cmd.Flags().StringVar(&output, "out", "eval/imported-suite.yml", "output suite path")
	return cmd
}

func executeEvalSuite(ctx context.Context, cmd *cobra.Command, opts *evalCLIOptions, projectPath string, target gjeval.Target, suite gjeval.Suite, mode gjeval.RunMode, seed int64, autoBaseline, deliberatePromotion bool, scale int) (*gjeval.Report, *gjeval.Store, error) {
	policy, err := evalResumePolicy(opts)
	if err != nil {
		return nil, nil, err
	}
	frozenClock, err := evalFrozenClock(opts)
	if err != nil {
		return nil, nil, err
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	writable, reactive, resettable := evalSuiteEnvironmentRequirements(suite)
	spec := gjeval.EnvSpec{
		Target: target, ConfigPath: projectPath, Seed: seed,
		Writable: writable, Reactive: reactive, Resettable: resettable,
		PinDataAnchor: evalResumeDataAnchor(projectPath, policy, opts.ResumeRunID),
		FreezeTime:    opts.FreezeTime,
	}
	environment := evalEnvironment{StatusOut: os.Stderr}
	var pool *evalInstancePool
	var instance gjeval.Instance
	if opts.Pool > 1 {
		// Every worker must serve the same rows; the pool refuses to form
		// otherwise. Worker zero also answers the run's own setup queries, which
		// happen before any episode leases anything.
		pool, err = newEvalInstancePool(ctx, environment, spec, opts.Pool)
		if err != nil {
			return nil, nil, evalEnvironmentError(err)
		}
		defer pool.Close() //nolint:errcheck
		instance = pool.instances[0]
	} else {
		instance, err = environment.Start(ctx, spec)
		if err != nil {
			return nil, nil, evalEnvironmentError(err)
		}
		defer instance.Close() //nolint:errcheck
	}
	status, err := ensureEvalAgentReady(ctx, &http.Client{Timeout: 30 * time.Second}, instance)
	if err != nil {
		return nil, nil, evalEnvironmentError(err)
	}
	store := evalStore(projectPath, status)
	baseline, err := store.LoadBaseline()
	if err != nil {
		return nil, nil, err
	}
	intent := gjeval.RunIntentRun
	if deliberatePromotion {
		intent = gjeval.RunIntentBaseline
	} else if mode == gjeval.RunModeBenchmark {
		intent = gjeval.RunIntentBench
	}
	prepared, err := (gjeval.Runner{Client: evalAgentClient(status), Now: frozenClock}).Prepare(ctx, suite, instance, gjeval.RunOptions{
		Mode: mode, Intent: intent, Repeats: gjeval.DefaultRepeats, Seed: seed,
		Provenance: evalProvenance(instance, seed, status), Baseline: baseline, Store: store,
		AutoBaseline: autoBaseline, DeliberatePromotion: deliberatePromotion,
		ResumePolicy: policy, ResumeRunID: opts.ResumeRunID, BinaryFingerprint: evalBinaryFingerprint(),
		InvocationArgs: evalInvocationArgs(opts, projectPath, scale, seed, false), Concurrency: opts.Concurrency,
		Pool: poolForRun(pool),
	})
	if err != nil {
		return nil, nil, evalEnvironmentError(err)
	}
	defer prepared.Close() //nolint:errcheck
	if preview := prepared.Preview(); preview.MaximumProviderAttempts > 0 {
		if err := approveProviderTraffic(cmd, opts.Yes, preview.String()); err != nil {
			return nil, nil, err
		}
	}
	report, err := prepared.Execute(ctx)
	if err != nil {
		if report != nil {
			printEvalReport(cmd, opts, report, store)
		}
		return report, store, evalExecutionError(err)
	}
	printEvalReport(cmd, opts, report, store)
	return report, store, nil
}

func evalSuiteEnvironmentRequirements(suite gjeval.Suite) (writable, reactive, resettable bool) {
	for _, task := range suite.Tasks {
		if task.Mutation != nil {
			writable, resettable = true, true
		}
		if task.Category == gjeval.CategoryReactive {
			reactive = true
		}
	}
	return writable, reactive, resettable
}

func evalStatus(cmd *cobra.Command, opts *evalCLIOptions) error {
	projectPath, _, err := resolveEvalTarget(cmd, opts)
	if err != nil {
		return err
	}
	suitePath := evalSuitePath(projectPath)
	store := gjeval.NewStore(filepath.Join(projectPath, gjeval.DefaultStateDir))
	suite, suiteErr := gjeval.LoadSuite(suitePath)
	baseline, baselineErr := store.LoadBaseline()
	runs, runsErr := store.ListRuns()
	incomplete := make([]gjeval.RunManifest, 0)
	for _, run := range runs {
		if !run.Complete() {
			incomplete = append(incomplete, run)
		}
	}
	status := map[string]any{"suite_path": suitePath, "state_dir": store.Root, "suite_exists": suiteErr == nil, "baseline_exists": baselineErr == nil && baseline != nil}
	status["incomplete_runs"] = incomplete
	if suite != nil {
		status["task_count"] = len(suite.Tasks)
		status["catalog_fingerprint"] = suite.CatalogFingerprint
	}
	if baseline != nil {
		status["baseline_run_id"] = baseline.RunID
		status["baseline_recall"] = baseline.Metrics.Recall
	}
	if suiteErr != nil && !os.IsNotExist(unwrapPathError(suiteErr)) {
		status["suite_error"] = suiteErr.Error()
	}
	if baselineErr != nil {
		status["baseline_error"] = baselineErr.Error()
	}
	if runsErr != nil {
		status["runs_error"] = runsErr.Error()
	}
	if opts.JSON {
		return writeEvalJSON(cmd.OutOrStdout(), status)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Suite: %s\n", suitePath)
	if suite == nil {
		fmt.Fprintln(cmd.OutOrStdout(), "Status: not created (run `graphjin eval create`)")
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Status: %d tasks, catalog %s\n", len(suite.Tasks), suite.CatalogFingerprint)
	}
	if baseline == nil {
		fmt.Fprintln(cmd.OutOrStdout(), "Baseline: none")
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Baseline: %s (recall %.3f)\n", baseline.RunID, baseline.Metrics.Recall)
	}
	for _, run := range incomplete {
		fmt.Fprintf(cmd.OutOrStdout(), "Incomplete: %s (%s, %d/%d initial slots, model %s, updated %s)\n", run.RunID, run.Status, run.Progress.CompletedInitialSlots, run.Progress.PlannedInitialSlots, run.Provenance.Model, run.UpdatedAt.Format(time.RFC3339))
		fmt.Fprintf(cmd.OutOrStdout(), "Resume: %s\n", run.ResumeCommand())
		fmt.Fprintf(cmd.OutOrStdout(), "Restart: %s\n", run.RestartCommand())
	}
	return nil
}

func resolveEvalTarget(cmd *cobra.Command, opts *evalCLIOptions) (string, gjeval.Target, error) {
	if opts.Demo && opts.Remote {
		return "", "", errors.New("--demo and --remote are mutually exclusive")
	}
	target := gjeval.TargetLocal
	projectPath := cpath
	if opts.Remote {
		target = gjeval.TargetRemote
	} else if opts.Demo {
		target = gjeval.TargetDemo
		pathSet := flagChanged(cmd, "path") || flagChanged(cmd, "config")
		resolved, err := resolveDemoPath(pathSet, os.Stderr)
		if err != nil {
			return "", "", err
		}
		projectPath = resolved
		cpath = resolved
		if err := loadDemoEnv(projectPath, os.Stderr); err != nil {
			return "", "", err
		}
	}
	abs, err := filepath.Abs(projectPath)
	return abs, target, err
}

func flagChanged(cmd *cobra.Command, name string) bool {
	flag := cmd.Flags().Lookup(name)
	return flag != nil && flag.Changed
}

func approveProviderTraffic(cmd *cobra.Command, yes bool, expected string) error {
	out := cmd.ErrOrStderr()
	fmt.Fprintf(out, "Provider traffic preview: %s. Actual token cost depends on the configured model.\n", expected)
	if yes {
		return nil
	}
	if !isInteractiveTTY() {
		return errors.New("provider-backed evaluation requires --yes in non-interactive mode")
	}
	ok, err := promptConfirm(newPromptIO(cmd.InOrStdin(), cmd.OutOrStdout()), "Proceed with provider traffic?", false)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("evaluation aborted")
	}
	return nil
}

func printEvalReport(cmd *cobra.Command, opts *evalCLIOptions, report *gjeval.Report, store *gjeval.Store) {
	if report.RunStatus != "" && report.RunStatus != gjeval.RunStatusComplete {
		environmentCode := ""
		if store != nil {
			if manifest, err := store.LoadManifest(report.RunID); err == nil {
				environmentCode = manifest.LastEnvironmentCode
			}
		}
		partial := gjeval.PartialReport{
			SchemaVersion: report.SchemaVersion, UsageAccountingVersion: report.UsageAccountingVersion, RewardVersion: report.RewardVersion,
			RunID: report.RunID, RunStatus: report.RunStatus, Mode: report.Mode,
			GeneratedAt: time.Now().UTC(), SuiteFingerprint: report.SuiteFingerprint,
			CatalogFingerprint: report.CatalogFingerprint, DatasetFingerprint: report.DatasetFingerprint,
			OracleValueHash: report.OracleValueHash, Provenance: report.Provenance,
			Progress: report.Progress, ProviderUsage: report.ProviderUsage, EnvironmentCode: environmentCode,
			Notice: "evaluation is incomplete; finalized quality metrics are unavailable",
		}
		if opts.JSON {
			_ = writeEvalJSON(cmd.OutOrStdout(), partial)
			printEvalReportLocations(cmd, opts, report, store)
			return
		}
		summary := gjeval.SummarizePartialReport(partial)
		fmt.Fprintf(cmd.OutOrStdout(), "%s\n%s\n", summary.Title, summary.Message)
		fmt.Fprintf(cmd.OutOrStdout(), "Test attempts: %d of %d complete.\n", summary.CompletedTestAttempts, summary.PlannedTestAttempts)
		if opts.Debug {
			fmt.Fprintf(cmd.OutOrStdout(), "Technical: status=%s, provider attempts=%d, retries=%d.\n", report.RunStatus, report.Progress.ProviderAttempts, report.Progress.RetryCount)
			fmt.Fprintf(cmd.OutOrStdout(), "Provider usage so far: %d tokens (%d prompt, %d completion) across %d model calls; failed attempts and retries are included.\n",
				report.ProviderUsage.TotalTokens, report.ProviderUsage.PromptTokens, report.ProviderUsage.CompletionTokens, report.ProviderUsage.LLMCalls)
			printProviderUsageCompleteness(cmd, report.ProviderUsage)
		}
		printEvalReportLocations(cmd, opts, report, store)
		return
	}
	if opts.JSON {
		_ = writeEvalJSON(cmd.OutOrStdout(), report)
		printEvalReportLocations(cmd, opts, report, store)
		return
	}
	summary := gjeval.SummarizeReport(*report)
	if report.Acceptance.ScoringSuspect || gjeval.IsScoringDivergenceSuspect(report.Metrics) {
		fmt.Fprintf(cmd.OutOrStdout(), "SCORING INTEGRITY WARNING: correct-answer recall exceeds required-method recall by %.1f percentage points. Investigate the scoring contract before publishing.\n",
			100*gjeval.ScoringDivergence(report.Metrics))
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s\n%s\n", summary.Title, summary.Message)
	fmt.Fprintf(cmd.OutOrStdout(), "Correct answer: %d of %d data questions. Required database method: %d of %d. Full pass: %d of %d. Safety: %d of %d. Passed every attempt: %d of %d.\n",
		summary.CorrectAnswerQuestions, summary.DataQuestionCount,
		summary.RequiredMethodQuestions, summary.MethodQuestionCount,
		summary.FullPassQuestions, summary.QuestionCount,
		summary.SafetyRulesFollowed, summary.QuestionCount,
		summary.PassedEveryAttempt, summary.QuestionCount)
	fmt.Fprintf(cmd.OutOrStdout(), "Governance interventions: %d. Forbidden attempts: %d (all refused). Unsafe effects: %d.\n", summary.GuardInterventions, summary.ForbiddenAttempts, summary.UnsafeEffects)
	// Per-family publication gates, printed for every complete run. These decide
	// whether a public row may be published and were previously remembered rather
	// than recorded, which cost one run's verdict to a noise-level window swing.
	if gatesMet, unmet := gjeval.PublicationGatesMet(*report); gatesMet {
		fmt.Fprintf(cmd.OutOrStdout(), "Publication gates: all met.\n%s", gjeval.FormatPublicationGates(*report))
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Publication gates: NOT met (%s).\n%s", strings.Join(unmet, ", "), gjeval.FormatPublicationGates(*report))
	}
	if opts.Debug {
		fmt.Fprintf(cmd.OutOrStdout(), "Technical: recall %.3f, ground truth %.3f, method %.3f, safety %.3f.\n", report.Metrics.Recall, report.Metrics.GroundTruthRecall, report.Metrics.MethodRecall, report.Metrics.SafetyPrecision)
		fmt.Fprintf(cmd.OutOrStdout(), "Technical: pass@%d %.3f, pass^%d %.3f; accepted=%t.\n", report.Provenance.Repeats, report.Metrics.PassAtK, report.Provenance.Repeats, report.Metrics.PassPowerK, report.Acceptance.HardPass)
		perEpisode := 0.0
		if report.Metrics.EpisodeCount != 0 {
			perEpisode = float64(report.Metrics.TotalTokens) / float64(report.Metrics.EpisodeCount)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Finalized usage: %d tokens (%d prompt, %d completion) across %d model calls; %.1f tokens per episode.\n",
			report.Metrics.TotalTokens, report.Metrics.PromptTokens, report.Metrics.CompletionTokens, report.Metrics.LLMCalls, perEpisode)
		fmt.Fprintf(cmd.OutOrStdout(), "Actual provider usage: %d tokens across %d model calls and %d provider attempts; failed attempts and retries are included.\n",
			report.ProviderUsage.TotalTokens, report.ProviderUsage.LLMCalls, report.Progress.ProviderAttempts)
		printProviderUsageCompleteness(cmd, report.ProviderUsage)
		if comparison := report.UsageComparison; comparison != nil {
			if comparison.Comparable {
				fmt.Fprintf(cmd.OutOrStdout(), "Token change vs baseline %s: finalized %+.1f%% (%+d), per episode %+.1f%% (%+.1f), actual provider %+.1f%% (%+d).\n",
					comparison.BaselineRunID,
					evalPercent(comparison.FinalizedTokensChangePercent), comparison.FinalizedTokensDelta,
					evalPercent(comparison.TokensPerEpisodeChangePercent), comparison.TokensPerEpisodeDelta,
					evalPercent(comparison.ProviderTokensChangePercent), comparison.ProviderTokensDelta,
				)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "Token comparison vs baseline %s is advisory only: %s.\n", comparison.BaselineRunID, comparison.Reason)
			}
		}
		for _, notice := range report.Acceptance.Notices {
			fmt.Fprintf(cmd.OutOrStdout(), "Notice: %s\n", notice)
		}
		for taskID, detail := range report.InvalidOracleDetails {
			fmt.Fprintf(cmd.OutOrStdout(), "Invalid oracle: %s (%s)\n", taskID, detail)
		}
		for _, path := range report.EpisodePaths {
			fmt.Fprintf(cmd.OutOrStdout(), "Episode: %s\n", path)
		}
		for _, task := range report.Tasks {
			if !task.Pass {
				fmt.Fprintf(cmd.OutOrStdout(), "Failure: %s (%s)\n", task.TaskID, task.FailureCategory)
			}
		}
	}
	printEvalReportLocations(cmd, opts, report, store)
}

func printEvalReportLocations(cmd *cobra.Command, opts *evalCLIOptions, report *gjeval.Report, store *gjeval.Store) {
	if store == nil || report == nil || strings.TrimSpace(report.RunID) == "" {
		return
	}
	out := cmd.OutOrStdout()
	if opts.JSON {
		// Keep stdout as one machine-readable JSON document.
		out = cmd.ErrOrStderr()
	}
	projectPath := filepath.Dir(store.Root)
	fmt.Fprintf(out, "\nFriendly report:  %s\n", store.ReportMarkdownPath(report.RunID))
	fmt.Fprintf(out, "Technical report: %s\n", store.ReportTechnicalMarkdownPath(report.RunID))
	fmt.Fprintf(out, "JSON report:      %s\n", store.ReportPath(report.RunID))
	serveCommand := fmt.Sprintf("graphjin --path %s serve", strconv.Quote(projectPath))
	if opts.Demo {
		serveCommand += " --demo"
	}
	if consoleURL := evalConsoleReportURL(projectPath, opts.Demo, report.RunID); consoleURL != "" {
		fmt.Fprintf(out, "Console:         %s (start with `%s`)\n", consoleURL, serveCommand)
	} else {
		fmt.Fprintf(out, "Console:         start `%s`, then open Trainer -> Reports\n", serveCommand)
	}
	if report.RunStatus != "" && report.RunStatus != gjeval.RunStatusComplete {
		if manifest, err := store.LoadManifest(report.RunID); err == nil {
			fmt.Fprintf(out, "Resume:          %s\n", manifest.ResumeCommand())
		}
		return
	}
	public := gjeval.PublicBenchmark()
	if report.SuiteFingerprint == public.SuiteFingerprint {
		fmt.Fprintf(out, "Publish:         graphjin --path %s eval publish %s --benchmark %s --yes\n", strconv.Quote(projectPath), report.RunID, defaultBenchmarkSlug)
	} else {
		fmt.Fprintln(out, "Publish:         not a ranked public-suite run")
	}
}

func evalConsoleReportURL(projectPath string, demo bool, runID string) string {
	configName := serv.GetConfigName()
	if demo {
		configName = "dev"
	}
	data, err := os.ReadFile(filepath.Join(projectPath, configName+".yml"))
	if err != nil {
		return ""
	}
	var config struct {
		HostPort string `yaml:"host_port"`
	}
	if err := yaml.Unmarshal(data, &config); err != nil {
		return ""
	}
	hostPort := strings.TrimSpace(config.HostPort)
	if hostPort == "" {
		hostPort = "0.0.0.0:8080"
	}
	host, port, err := net.SplitHostPort(hostPort)
	if err != nil || port == "" {
		return ""
	}
	switch host {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/trainer/reports?run=" + url.QueryEscape(runID)
}

func printProviderUsageCompleteness(cmd *cobra.Command, usage gjeval.ProviderUsage) {
	if usage.Complete {
		fmt.Fprintln(cmd.OutOrStdout(), "Provider usage accounting is complete for every attempt.")
		return
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Provider usage accounting is incomplete: %d timeout or transport attempt(s) returned no provider usage. Recorded tokens are a lower bound.\n", usage.UnknownAttempts)
}

func evalPercent(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

func evalReportExit(report *gjeval.Report) error {
	if !report.Acceptance.SuiteValid {
		return &evalExitError{Code: 2, Err: errors.New("evaluation suite is invalid")}
	}
	if report.Acceptance.EnvironmentFailure {
		return evalEnvironmentError(errors.New("evaluation environment failed during provider-backed execution"))
	}
	if !report.Acceptance.HardPass {
		return &evalExitError{Code: 1, Err: errors.New("evaluation gate failed")}
	}
	return nil
}

func evalEnvironmentError(err error) error { return &evalExitError{Code: 3, Err: err} }

func evalExecutionError(err error) error {
	if errors.Is(err, gjeval.ErrRunInterrupted) {
		return &evalExitError{Code: 130, Err: err}
	}
	return err
}

func ensureEvalAgentReady(ctx context.Context, client *http.Client, instance gjeval.Instance) (gjeval.AgentStatus, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(instance.BaseURL()), "/")
	for _, suffix := range []string{"/api/v1/agent/status", "/api/v1/agent", "/api/v1/graphql"} {
		baseURL = strings.TrimSuffix(baseURL, suffix)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/v1/agent/status", nil)
	if err != nil {
		return gjeval.AgentStatus{}, err
	}
	for key, value := range instance.Headers() {
		request.Header.Set(key, value)
	}
	response, err := client.Do(request)
	if err != nil {
		return gjeval.AgentStatus{}, fmt.Errorf("read agent status: %w", err)
	}
	defer response.Body.Close()
	var status gjeval.AgentStatus
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return gjeval.AgentStatus{}, fmt.Errorf("agent status returned HTTP %d", response.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&status); err != nil {
		return gjeval.AgentStatus{}, fmt.Errorf("decode agent status: %w", err)
	}
	if !status.Enabled || !status.Ready {
		message := strings.TrimSpace(status.Message)
		if message == "" {
			message = "agent is disabled or its model credentials are unavailable"
		}
		return status, errors.New(message)
	}
	return status, nil
}

func evalAgentClient(status gjeval.AgentStatus) *http.Client {
	timeout := evalAgentHTTPTimeout
	if status.TimeoutSeconds > 0 {
		timeout = time.Duration(status.TimeoutSeconds+30) * time.Second
	}
	return &http.Client{Timeout: timeout}
}

func evalStore(projectPath string, status gjeval.AgentStatus) *gjeval.Store {
	apiKeyEnv := strings.TrimSpace(status.APIKeyEnv)
	if apiKeyEnv == "" {
		apiKeyEnv = strings.TrimSpace(os.Getenv("GJ_AGENT_API_KEY_ENV"))
	}
	return gjeval.NewStore(filepath.Join(projectPath, gjeval.DefaultStateDir)).WithSecrets(os.Getenv(apiKeyEnv))
}

func evalBinaryFingerprint() string {
	path, err := os.Executable()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func evalInvocationArgs(opts *evalCLIOptions, projectPath string, scale int, seed int64, public bool) []string {
	args := make([]string, 0, 10)
	if public {
		// --public pins the frozen suite along with its scale, seed and demo
		// target. Reprinting those pinned values would make the resume command
		// fail the very validation --public enforces, so the flag stands alone
		// and the values it pins are left off below. Dropping --public instead
		// would be worse: the resume would load the generated suite and be
		// rejected as incompatible with the frozen run it means to continue.
		args = append(args, "--public")
	}
	if opts.Demo && !public {
		args = append(args, "--demo")
	}
	if opts.Remote {
		args = append(args, "--remote")
	}
	args = append(args, "--path", strconv.Quote(projectPath))
	if scale > 0 && !public {
		args = append(args, "--scale", strconv.Itoa(scale), "--seed", strconv.FormatInt(seed, 10))
	}
	if opts.JSON {
		args = append(args, "--json")
	}
	if opts.Debug {
		args = append(args, "--debug")
	}
	if opts.Concurrency > 1 {
		args = append(args, "--concurrency", strconv.Itoa(opts.Concurrency))
	}
	if opts.AutoResume {
		// The printed resume command is what an operator retypes after a halt;
		// it should carry the same unattended behaviour the run started with.
		args = append(args, "--auto-resume")
		if opts.AutoResumeAttempts > 0 && opts.AutoResumeAttempts != defaultAutoResumeAttempts {
			args = append(args, "--auto-resume-attempts", strconv.Itoa(opts.AutoResumeAttempts))
		}
	}
	return args
}

func evalSuitePath(projectPath string) string {
	return filepath.Join(projectPath, gjeval.DefaultEvaluationDir, gjeval.DefaultSuiteFilename)
}

func evalProvenance(instance gjeval.Instance, seed int64, status gjeval.AgentStatus) gjeval.RunProvenance {
	return gjeval.RunProvenance{
		Provider:             status.Provider,
		Model:                status.Model,
		APIKeyEnv:            status.APIKeyEnv,
		ResponseFormat:       status.ResponseFormat,
		StructuredOutputMode: status.StructuredOutputMode,
		ServiceTier:          status.ServiceTier,
		ServerFingerprint:    status.EvalFingerprint,
		AxVersion:            evalAxVersion(),
		GraphJinCommit:       commit,
		PromptRegistryHash:   evalPromptRegistryHash(),
		Temperature:          0,
		Seed:                 seed,
		Repeats:              gjeval.DefaultRepeats,
		MaxSteps:             status.MaxSteps,
		Reasoning:            status.Reasoning,
		TimeoutSeconds:       status.TimeoutSeconds,
		Target:               instance.Label(),
	}
}

func evalAxVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, dependency := range info.Deps {
		if dependency.Path == "github.com/ax-llm/ax/packages/go" {
			return dependency.Version
		}
	}
	return ""
}

func evalPromptRegistryHash() string {
	return gjagent.PromptRegistryHash()
}

func writeEvalJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func readEvalLine(in io.Reader, out io.Writer, prompt string) (string, error) {
	fmt.Fprintf(out, "%s ", strings.TrimSpace(prompt))
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return "", errors.New("clarification cannot be empty")
	}
	return line, nil
}

func unwrapPathError(err error) error {
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return pathErr
	}
	return err
}
