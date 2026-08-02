package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	gjeval "github.com/dosco/graphjin/agent/v3/eval"
	"github.com/spf13/cobra"
)

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
	Demo   bool
	Remote bool
	Yes    bool
	JSON   bool
	Debug  bool
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

	cmd.AddCommand(evalCreateCmd(opts))
	cmd.AddCommand(evalAddCmd(opts))
	cmd.AddCommand(evalRemoveCmd(opts))
	cmd.AddCommand(evalRunCmd(opts, false))
	cmd.AddCommand(evalBaselineCmd(opts))
	cmd.AddCommand(evalBenchCmd(opts))
	cmd.AddCommand(evalImportCmd())
	return cmd
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
	return &cobra.Command{
		Use:   "create",
		Short: "Generate a verified catalog-derived evaluation suite",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			projectPath, target, err := resolveEvalTarget(cmd, opts)
			if err != nil {
				return err
			}
			instance, err := (evalEnvironment{StatusOut: os.Stderr}).Start(cmd.Context(), gjeval.EnvSpec{Target: target, ConfigPath: projectPath, Seed: 23})
			if err != nil {
				return evalEnvironmentError(err)
			}
			defer instance.Close() //nolint:errcheck
			client := &http.Client{Timeout: 120 * time.Second}
			source := gjeval.HTTPCatalogSource{Client: client, BaseURL: instance.BaseURL(), Headers: instance.Headers()}
			verifier := &gjeval.Verifier{Client: client, BaseURL: instance.BaseURL(), Headers: instance.Headers()}
			suite, err := (gjeval.Generator{Source: source, Verifier: verifier}).Generate(cmd.Context(), gjeval.GeneratorOptions{Seed: 23, Scale: gjeval.DefaultSuiteSize})
			if err != nil {
				return &evalExitError{Code: 2, Err: err}
			}
			path := evalSuitePath(projectPath)
			if err := gjeval.SaveSuite(path, *suite); err != nil {
				return err
			}
			if opts.JSON {
				return writeEvalJSON(cmd.OutOrStdout(), map[string]any{"status": "created", "suite": path, "tasks": len(suite.Tasks), "seed": suite.Generator.Seed, "catalog_fingerprint": suite.CatalogFingerprint})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created %s with %d verified tasks (seed %d).\n", path, len(suite.Tasks), suite.Generator.Seed)
			return nil
		},
	}
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
			client := &http.Client{Timeout: 180 * time.Second}
			if err := ensureEvalAgentReady(cmd.Context(), client, instance); err != nil {
				return evalEnvironmentError(err)
			}
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
	return &cobra.Command{
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
			expected := fmt.Sprintf("up to %d provider-backed agent runs (%d tasks x %d repeats, excluding confirmation reruns)", len(suite.Tasks)*gjeval.DefaultRepeats, len(suite.Tasks), gjeval.DefaultRepeats)
			if err := approveProviderTraffic(cmd, opts.Yes, expected); err != nil {
				return err
			}
			report, store, err := executeEvalSuite(cmd.Context(), cmd, opts, projectPath, target, *suite, gjeval.RunModeRun, 23, !promote)
			if err != nil {
				return err
			}
			if promote {
				if !report.Acceptance.HardPass {
					return &evalExitError{Code: 1, Err: errors.New("cannot promote a failing evaluation")}
				}
				if err := store.PromoteBaseline(*report); err != nil {
					return err
				}
				if !opts.JSON {
					fmt.Fprintln(cmd.OutOrStdout(), "Promoted this run as the baseline.")
				}
			}
			return evalReportExit(report)
		},
	}
}

func evalBaselineCmd(opts *evalCLIOptions) *cobra.Command { return evalRunCmd(opts, true) }

func evalBenchCmd(opts *evalCLIOptions) *cobra.Command {
	var scale int
	var seed int64
	cmd := &cobra.Command{
		Use:   "bench",
		Short: "Generate and run the extended stratified benchmark distribution",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if scale <= 0 {
				return errors.New("--scale must be positive")
			}
			if err := approveProviderTraffic(cmd, opts.Yes, fmt.Sprintf("up to %d provider-backed agent runs (%d generated tasks x %d repeats)", scale*gjeval.DefaultRepeats, scale, gjeval.DefaultRepeats)); err != nil {
				return err
			}
			projectPath, target, err := resolveEvalTarget(cmd, opts)
			if err != nil {
				return err
			}
			instance, err := (evalEnvironment{StatusOut: os.Stderr}).Start(cmd.Context(), gjeval.EnvSpec{Target: target, ConfigPath: projectPath, Seed: seed})
			if err != nil {
				return evalEnvironmentError(err)
			}
			defer instance.Close() //nolint:errcheck
			client := &http.Client{Timeout: 180 * time.Second}
			if err := ensureEvalAgentReady(cmd.Context(), client, instance); err != nil {
				return evalEnvironmentError(err)
			}
			suite, err := (gjeval.Generator{
				Source:   gjeval.HTTPCatalogSource{Client: client, BaseURL: instance.BaseURL(), Headers: instance.Headers()},
				Verifier: &gjeval.Verifier{Client: client, BaseURL: instance.BaseURL(), Headers: instance.Headers()},
			}).Generate(cmd.Context(), gjeval.GeneratorOptions{Seed: seed, Scale: scale, Name: "GraphJin Frontier Benchmark"})
			if err != nil {
				return &evalExitError{Code: 2, Err: err}
			}
			store := gjeval.NewStore(filepath.Join(projectPath, gjeval.DefaultStateDir))
			baseline, err := store.LoadBaseline()
			if err != nil {
				return err
			}
			runner := gjeval.Runner{Client: client}
			report, err := runner.Run(cmd.Context(), *suite, instance, gjeval.RunOptions{Mode: gjeval.RunModeBenchmark, Repeats: gjeval.DefaultRepeats, Seed: seed, Provenance: evalProvenance(instance, seed), Baseline: baseline, Store: store})
			if err != nil {
				return err
			}
			printEvalReport(cmd, opts, report)
			return evalReportExit(report)
		},
	}
	cmd.Flags().IntVar(&scale, "scale", 100, "number of verified generated benchmark tasks")
	cmd.Flags().Int64Var(&seed, "seed", 23, "deterministic generator and rollout seed")
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

func executeEvalSuite(ctx context.Context, cmd *cobra.Command, opts *evalCLIOptions, projectPath string, target gjeval.Target, suite gjeval.Suite, mode gjeval.RunMode, seed int64, autoBaseline bool) (*gjeval.Report, *gjeval.Store, error) {
	instance, err := (evalEnvironment{StatusOut: os.Stderr}).Start(ctx, gjeval.EnvSpec{Target: target, ConfigPath: projectPath, Seed: seed})
	if err != nil {
		return nil, nil, evalEnvironmentError(err)
	}
	defer instance.Close() //nolint:errcheck
	client := &http.Client{Timeout: 180 * time.Second}
	if err := ensureEvalAgentReady(ctx, client, instance); err != nil {
		return nil, nil, evalEnvironmentError(err)
	}
	store := gjeval.NewStore(filepath.Join(projectPath, gjeval.DefaultStateDir))
	baseline, err := store.LoadBaseline()
	if err != nil {
		return nil, nil, err
	}
	report, err := (gjeval.Runner{Client: client}).Run(ctx, suite, instance, gjeval.RunOptions{Mode: mode, Repeats: gjeval.DefaultRepeats, Seed: seed, Provenance: evalProvenance(instance, seed), Baseline: baseline, Store: store, AutoBaseline: autoBaseline})
	if err != nil {
		return nil, nil, err
	}
	printEvalReport(cmd, opts, report)
	return report, store, nil
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
	status := map[string]any{"suite_path": suitePath, "state_dir": store.Root, "suite_exists": suiteErr == nil, "baseline_exists": baselineErr == nil && baseline != nil}
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

func printEvalReport(cmd *cobra.Command, opts *evalCLIOptions, report *gjeval.Report) {
	if opts.JSON {
		_ = writeEvalJSON(cmd.OutOrStdout(), report)
		return
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Run %s: recall %.3f, ground truth %.3f, method %.3f, safety %.3f\n", report.RunID, report.Metrics.Recall, report.Metrics.GroundTruthRecall, report.Metrics.MethodRecall, report.Metrics.SafetyPrecision)
	fmt.Fprintf(cmd.OutOrStdout(), "pass@%d %.3f, pass^%d %.3f; accepted=%t\n", report.Provenance.Repeats, report.Metrics.PassAtK, report.Provenance.Repeats, report.Metrics.PassPowerK, report.Acceptance.HardPass)
	for _, notice := range report.Acceptance.Notices {
		fmt.Fprintf(cmd.OutOrStdout(), "Notice: %s\n", notice)
	}
	if opts.Debug {
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
}

func evalReportExit(report *gjeval.Report) error {
	if !report.Acceptance.SuiteValid {
		return &evalExitError{Code: 2, Err: errors.New("evaluation suite is invalid")}
	}
	if !report.Acceptance.HardPass {
		return &evalExitError{Code: 1, Err: errors.New("evaluation gate failed")}
	}
	return nil
}

func evalEnvironmentError(err error) error { return &evalExitError{Code: 3, Err: err} }

func ensureEvalAgentReady(ctx context.Context, client *http.Client, instance gjeval.Instance) error {
	baseURL := strings.TrimRight(strings.TrimSpace(instance.BaseURL()), "/")
	for _, suffix := range []string{"/api/v1/agent/status", "/api/v1/agent", "/api/v1/graphql"} {
		baseURL = strings.TrimSuffix(baseURL, suffix)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/v1/agent/status", nil)
	if err != nil {
		return err
	}
	for key, value := range instance.Headers() {
		request.Header.Set(key, value)
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("read agent status: %w", err)
	}
	defer response.Body.Close()
	var status gjeval.AgentStatus
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("agent status returned HTTP %d", response.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&status); err != nil {
		return fmt.Errorf("decode agent status: %w", err)
	}
	if !status.Enabled || !status.Ready {
		message := strings.TrimSpace(status.Message)
		if message == "" {
			message = "agent is disabled or its model credentials are unavailable"
		}
		return errors.New(message)
	}
	return nil
}

func evalSuitePath(projectPath string) string {
	return filepath.Join(projectPath, gjeval.DefaultEvaluationDir, gjeval.DefaultSuiteFilename)
}

func evalProvenance(instance gjeval.Instance, seed int64) gjeval.RunProvenance {
	return gjeval.RunProvenance{
		Model:              os.Getenv("GJ_AGENT_MODEL"),
		AxVersion:          evalAxVersion(),
		GraphJinCommit:     commit,
		PromptRegistryHash: evalPromptRegistryHash(),
		Temperature:        0,
		Seed:               seed,
		Repeats:            gjeval.DefaultRepeats,
		Target:             instance.Label(),
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
