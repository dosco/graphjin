package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	gjeval "github.com/dosco/graphjin/agent/v3/eval"
	"github.com/spf13/cobra"
)

type evalRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn evalRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestEvalCommandSurfaceAndContextualFlags(t *testing.T) {
	command := evalCmd()
	want := map[string]bool{"create": false, "add": false, "rm": false, "run": false, "baseline": false, "bench": false, "publish": false}
	for _, child := range command.Commands() {
		if _, ok := want[child.Name()]; ok {
			want[child.Name()] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("missing eval command %s", name)
		}
	}
	bench, _, err := command.Find([]string{"bench"})
	if err != nil {
		t.Fatal(err)
	}
	if bench.Flags().Lookup("scale") == nil || bench.Flags().Lookup("seed") == nil || bench.Flags().Lookup("public") == nil || bench.Flags().Lookup("resume") == nil || bench.Flags().Lookup("restart") == nil {
		t.Fatal("bench is missing scale, seed, public, or resume flags")
	}
	if bench.Flags().Lookup("auto-resume") == nil || bench.Flags().Lookup("auto-resume-attempts") == nil {
		t.Fatal("bench is missing auto-resume flags")
	}
	for _, name := range []string{"run", "baseline"} {
		child, _, err := command.Find([]string{name})
		if err != nil || child.Flags().Lookup("resume") == nil || child.Flags().Lookup("restart") == nil {
			t.Fatalf("%s is missing resume flags", name)
		}
	}
	create, _, err := command.Find([]string{"create"})
	if err != nil {
		t.Fatal(err)
	}
	if create.Flags().Lookup("scale") != nil || create.Flags().Lookup("seed") != nil {
		t.Fatal("create exposed bench-only flags")
	}
}

func TestEvalProvenanceIncludesConfiguredAgentSettings(t *testing.T) {
	instance := &gjeval.StaticInstance{TargetLabel: "demo"}
	provenance := evalProvenance(instance, 23, gjeval.AgentStatus{
		Provider: "google-gemini", Model: "gemini-test", StructuredOutputMode: "json_object", ServiceTier: "flex", MaxSteps: 8,
	})
	if provenance.MaxSteps != 8 {
		t.Fatalf("max steps = %d, want 8", provenance.MaxSteps)
	}
	if provenance.StructuredOutputMode != "json_object" {
		t.Fatalf("structured output mode = %q, want json_object", provenance.StructuredOutputMode)
	}
	if provenance.ServiceTier != "flex" {
		t.Fatalf("service tier = %q, want flex", provenance.ServiceTier)
	}
}

func TestEmbeddedPublicBenchmarkSuiteMatchesPinnedSpec(t *testing.T) {
	suite, err := loadPublicEvalSuite()
	if err != nil {
		t.Fatal(err)
	}
	spec := gjeval.PublicBenchmark()
	// Scale bounds the intent tier, which is what the benchmark measures. Execution
	// twins ride along with their needs so a pair is never split, so the total task
	// count exceeds the scale by exactly the number of twins.
	intentTasks, executionTwins := 0, 0
	for _, task := range suite.Tasks {
		if task.Tier == gjeval.TierExecution {
			executionTwins++
			continue
		}
		intentTasks++
	}
	if intentTasks != spec.Scale || suite.Generator.Scale != spec.Scale || suite.Generator.Seed != spec.Seed {
		t.Fatalf("suite shape = intent:%d twins:%d generator:%+v spec:%+v",
			intentTasks, executionTwins, suite.Generator, spec)
	}
	if executionTwins == 0 {
		t.Fatal("public suite has no execution twins; the planning gap would be uncomputable")
	}
	// Every twin must have its intent counterpart, or the planning gap it exists to
	// compute is meaningless for that need.
	tiersByNeed := map[string]map[string]bool{}
	for _, task := range suite.Tasks {
		if task.NeedID == "" {
			continue
		}
		if tiersByNeed[task.NeedID] == nil {
			tiersByNeed[task.NeedID] = map[string]bool{}
		}
		tier := task.Tier
		if tier == "" {
			tier = gjeval.TierIntent
		}
		tiersByNeed[task.NeedID][tier] = true
	}
	for need, tiers := range tiersByNeed {
		if !tiers[gjeval.TierIntent] || !tiers[gjeval.TierExecution] {
			t.Fatalf("need %s is missing a tier: %v", need, tiers)
		}
	}
	if suite.Generator.Version != gjeval.GeneratorVersion {
		t.Fatalf("suite generator = %q, want %q", suite.Generator.Version, gjeval.GeneratorVersion)
	}
	compatAggregate := false
	categoryCounts := map[gjeval.Category]int{}
	for _, task := range suite.Tasks {
		categoryCounts[task.Category]++
		if task.Category != gjeval.CategoryAggregate {
			continue
		}
		for _, pattern := range task.Method.RequireQueryMatch {
			if strings.Contains(pattern, "_aggregate") {
				compatAggregate = true
			}
		}
	}
	if !compatAggregate {
		t.Fatal("public suite has no Hasura-compatible aggregate method rule")
	}
	for category, want := range map[gjeval.Category]int{
		// Generation 2028.1 counts include execution twins alongside their intent
		// tasks, which is why the write-side families grew.
		gjeval.CategoryAggregate:   15,
		gjeval.CategoryWindow:      15,
		gjeval.CategoryRanking:     12,
		gjeval.CategoryDiscovery:   10,
		gjeval.CategorySavedMetric: 9,
		gjeval.CategoryRefusal:     10,
		gjeval.CategoryAction:      15,
		gjeval.CategoryReactive:    12,
		gjeval.CategoryMultiTurn:   7,
		gjeval.CategoryCrossSource: 8,
	} {
		if categoryCounts[category] != want {
			t.Fatalf("public suite %s count = %d, want %d (all=%v)", category, categoryCounts[category], want, categoryCounts)
		}
	}
	if got := gjeval.SuiteFingerprint(*suite); got != spec.SuiteFingerprint {
		t.Fatalf("suite fingerprint = %s, want %s", got, spec.SuiteFingerprint)
	}
}

func TestPublicBenchmarkEnvironmentSupportsBehavioralTasks(t *testing.T) {
	public := evalBenchEnvSpec(gjeval.TargetDemo, "/tmp/deeporg", 23, true)
	if !public.Writable || !public.Reactive || !public.Resettable {
		t.Fatalf("public benchmark environment = %+v, want writable, reactive, and resettable", public)
	}
	private := evalBenchEnvSpec(gjeval.TargetLocal, "/tmp/private-eval", 23, false)
	if private.Writable || private.Reactive || private.Resettable {
		t.Fatalf("ordinary generated benchmark environment unexpectedly widened capabilities: %+v", private)
	}
}

func TestEvalRemoveTaskKeepsSuiteValid(t *testing.T) {
	original := cpath
	cpath = t.TempDir()
	defer func() { cpath = original }()
	suite := gjeval.Suite{
		Name: "test", Generator: gjeval.GeneratorMeta{Version: gjeval.GeneratorVersion, Scale: 2},
		Tasks: []gjeval.Task{
			{Slug: "one", Category: gjeval.CategoryDiscovery, Difficulty: gjeval.DifficultyT1, Prompt: "question one", Provenance: gjeval.Provenance{Source: "imported"}, ExpectedStatus: "answered"},
			{Slug: "two", Category: gjeval.CategoryDiscovery, Difficulty: gjeval.DifficultyT1, Prompt: "question two", Provenance: gjeval.Provenance{Source: "imported"}, ExpectedStatus: "answered"},
		},
	}
	if err := gjeval.SaveSuite(evalSuitePath(cpath), suite); err != nil {
		t.Fatal(err)
	}
	loaded, err := gjeval.LoadSuite(evalSuitePath(cpath))
	if err != nil {
		t.Fatal(err)
	}
	removedID := loaded.Tasks[0].ID
	command := evalCmd()
	output := new(bytes.Buffer)
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs([]string{"rm", removedID, "--yes", "--json"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"status": "removed"`) || !strings.Contains(output.String(), removedID) {
		t.Fatalf("unexpected removal output: %s", output.String())
	}
	remaining, err := gjeval.LoadSuite(evalSuitePath(cpath))
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining.Tasks) != 1 || remaining.Tasks[0].ID == removedID {
		t.Fatalf("task removal did not persist: %+v", remaining.Tasks)
	}
}

func TestEvalStatusJSONWithoutSuite(t *testing.T) {
	original := cpath
	cpath = t.TempDir()
	defer func() { cpath = original }()
	command := evalCmd()
	output := new(bytes.Buffer)
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs([]string{"--json"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"suite_exists": false`) || !strings.Contains(output.String(), `"baseline_exists": false`) {
		t.Fatalf("unexpected status: %s", output.String())
	}
}

func TestEvalReportExitCodes(t *testing.T) {
	tests := []struct {
		report *gjeval.Report
		code   int
	}{
		{report: &gjeval.Report{Acceptance: gjeval.Acceptance{SuiteValid: false}}, code: 2},
		{report: &gjeval.Report{Acceptance: gjeval.Acceptance{SuiteValid: true, EnvironmentFailure: true}}, code: 3},
		{report: &gjeval.Report{Acceptance: gjeval.Acceptance{SuiteValid: true, HardPass: false}}, code: 1},
	}
	for _, test := range tests {
		err := evalReportExit(test.report)
		var exitErr *evalExitError
		if !errors.As(err, &exitErr) || exitErr.Code != test.code {
			t.Fatalf("error=%v, want exit %d", err, test.code)
		}
	}
	if err := evalReportExit(&gjeval.Report{Acceptance: gjeval.Acceptance{SuiteValid: true, HardPass: true}}); err != nil {
		t.Fatalf("passing report returned error: %v", err)
	}
	err := evalExecutionError(gjeval.ErrRunInterrupted)
	var interrupted *evalExitError
	if !errors.As(err, &interrupted) || interrupted.Code != 130 {
		t.Fatalf("interruption error=%v, want exit 130", err)
	}
}

func TestPrintEvalReportJSONOmitsIncompleteMetrics(t *testing.T) {
	command := &cobra.Command{}
	output := new(bytes.Buffer)
	command.SetOut(output)
	printEvalReport(command, &evalCLIOptions{JSON: true}, &gjeval.Report{
		SchemaVersion: gjeval.ReportSchemaVersion, RunID: "partial", RunStatus: gjeval.RunStatusEnvironmentFailed,
		Metrics: gjeval.Metrics{Recall: 0.75}, Tasks: []gjeval.TaskVerdict{{TaskID: "private-task"}},
		Acceptance: gjeval.Acceptance{EnvironmentFailure: true},
	}, nil)
	var payload map[string]any
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		t.Fatalf("decode partial JSON: %v\n%s", err, output.String())
	}
	for _, forbidden := range []string{"metrics", "tasks", "acceptance"} {
		if _, exists := payload[forbidden]; exists {
			t.Fatalf("partial JSON contains %s: %s", forbidden, output.String())
		}
	}
	if strings.Contains(output.String(), "private-task") {
		t.Fatalf("partial JSON contains private task data: %s", output.String())
	}
	if !strings.Contains(output.String(), `"run_status": "environment_failed"`) {
		t.Fatalf("partial JSON lost status: %s", output.String())
	}
}

func TestPrintEvalReportJSONIncludesPartialEnvironmentCode(t *testing.T) {
	projectPath := t.TempDir()
	store := gjeval.NewStore(filepath.Join(projectPath, gjeval.DefaultStateDir))
	if _, err := store.WriteManifest(gjeval.RunManifest{
		RunID: "partial-quota", Status: gjeval.RunStatusEnvironmentFailed, LastEnvironmentCode: "provider_quota",
	}); err != nil {
		t.Fatal(err)
	}
	command := &cobra.Command{}
	stdout, stderr := new(bytes.Buffer), new(bytes.Buffer)
	command.SetOut(stdout)
	command.SetErr(stderr)
	printEvalReport(command, &evalCLIOptions{JSON: true}, &gjeval.Report{
		SchemaVersion: gjeval.ReportSchemaVersion, RunID: "partial-quota", RunStatus: gjeval.RunStatusEnvironmentFailed,
	}, store)
	var partial gjeval.PartialReport
	if err := json.Unmarshal(stdout.Bytes(), &partial); err != nil {
		t.Fatalf("decode partial JSON: %v\n%s", err, stdout.String())
	}
	if partial.EnvironmentCode != "provider_quota" {
		t.Fatalf("environment code = %q, want provider_quota", partial.EnvironmentCode)
	}
}

func TestPrintEvalReportShowsTokenUsageAndBaselineChange(t *testing.T) {
	command := &cobra.Command{}
	output := new(bytes.Buffer)
	command.SetOut(output)
	minusTen := -10.0
	report := &gjeval.Report{
		RunID: "candidate", RunStatus: gjeval.RunStatusComplete,
		Provenance: gjeval.RunProvenance{Repeats: 3},
		Progress:   gjeval.RunProgress{ProviderAttempts: 4},
		Metrics: gjeval.Metrics{
			EpisodeCount: 3, PromptTokens: 210, CompletionTokens: 60, TotalTokens: 270, LLMCalls: 6,
		},
		ProviderUsage: gjeval.ProviderUsage{PromptTokens: 230, CompletionTokens: 70, TotalTokens: 300, LLMCalls: 7, Complete: true},
		UsageComparison: &gjeval.UsageComparison{
			BaselineRunID: "baseline", Comparable: true,
			FinalizedTokensDelta: -30, FinalizedTokensChangePercent: &minusTen,
			TokensPerEpisodeDelta: -10, TokensPerEpisodeChangePercent: &minusTen,
			ProviderTokensDelta: -20, ProviderTokensChangePercent: &minusTen,
		},
	}
	printEvalReport(command, &evalCLIOptions{Debug: true}, report, nil)
	for _, phrase := range []string{
		"Finalized usage: 270 tokens", "90.0 tokens per episode",
		"Actual provider usage: 300 tokens", "failed attempts and retries are included",
		"Provider usage accounting is complete for every attempt",
		"Token change vs baseline baseline", "finalized -10.0% (-30)",
	} {
		if !strings.Contains(output.String(), phrase) {
			t.Fatalf("output missing %q: %s", phrase, output.String())
		}
	}
}

func TestPrintEvalReportMarksUnknownProviderUsageAsLowerBound(t *testing.T) {
	command := &cobra.Command{}
	output := new(bytes.Buffer)
	command.SetOut(output)
	printEvalReport(command, &evalCLIOptions{Debug: true}, &gjeval.Report{
		RunID: "candidate", RunStatus: gjeval.RunStatusComplete,
		Provenance: gjeval.RunProvenance{Repeats: 3},
		ProviderUsage: gjeval.ProviderUsage{
			TotalTokens: 300, LLMCalls: 7, UnknownAttempts: 2,
		},
	}, nil)
	for _, phrase := range []string{"usage accounting is incomplete", "2 timeout or transport attempt(s)", "Recorded tokens are a lower bound"} {
		if !strings.Contains(output.String(), phrase) {
			t.Fatalf("output missing %q: %s", phrase, output.String())
		}
	}
}

func TestPrintEvalReportShowsArtifactsConsoleAndPublishCommand(t *testing.T) {
	projectPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectPath, "dev.yml"), []byte("host_port: 0.0.0.0:8083\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := gjeval.NewStore(filepath.Join(projectPath, gjeval.DefaultStateDir))
	command := &cobra.Command{}
	output := new(bytes.Buffer)
	command.SetOut(output)
	report := &gjeval.Report{
		RunID:            "public-run",
		RunStatus:        gjeval.RunStatusComplete,
		SuiteFingerprint: gjeval.PublicBenchmark().SuiteFingerprint,
		Provenance:       gjeval.RunProvenance{Repeats: 3},
	}

	printEvalReport(command, &evalCLIOptions{Demo: true}, report, store)

	for _, phrase := range []string{
		"Friendly report:  " + store.ReportMarkdownPath(report.RunID),
		"Technical report: " + store.ReportTechnicalMarkdownPath(report.RunID),
		"JSON report:      " + store.ReportPath(report.RunID),
		"Console:         http://127.0.0.1:8083/trainer/reports?run=public-run",
		"graphjin --path " + strconv.Quote(projectPath) + " serve --demo",
		"Publish:         graphjin --path " + strconv.Quote(projectPath) + " eval publish public-run --benchmark deeporg --yes",
	} {
		if !strings.Contains(output.String(), phrase) {
			t.Fatalf("output missing %q: %s", phrase, output.String())
		}
	}
}

func TestPrintEvalReportShowsResumeForPartialRun(t *testing.T) {
	projectPath := t.TempDir()
	store := gjeval.NewStore(filepath.Join(projectPath, gjeval.DefaultStateDir))
	manifest := gjeval.RunManifest{
		RunID:               "partial-run",
		Intent:              gjeval.RunIntentBench,
		Status:              gjeval.RunStatusInterrupted,
		InvocationArgs:      []string{"--demo", "--scale", "5", "--seed", "23"},
		LastEnvironmentCode: "provider_quota",
	}
	if _, err := store.WriteManifest(manifest); err != nil {
		t.Fatal(err)
	}
	command := &cobra.Command{}
	output := new(bytes.Buffer)
	command.SetOut(output)

	printEvalReport(command, &evalCLIOptions{Demo: true}, &gjeval.Report{
		RunID: "partial-run", RunStatus: gjeval.RunStatusEnvironmentFailed,
		Provenance: gjeval.RunProvenance{Provider: "google-gemini", Repeats: 3},
		Progress:   gjeval.RunProgress{CompletedInitialSlots: 36, PlannedInitialSlots: 72},
	}, store)

	if !strings.Contains(output.String(), "GraphJin completed 36 of 72 test attempts before Google stopped accepting requests because of quota limits") {
		t.Fatalf("friendly quota explanation missing: %s", output.String())
	}
	if !strings.Contains(output.String(), "Resume:          graphjin eval bench --demo --scale 5 --seed 23 --resume partial-run --yes") {
		t.Fatalf("resume command missing: %s", output.String())
	}
	if strings.Contains(output.String(), "Publish:") {
		t.Fatalf("partial run should not print publish guidance: %s", output.String())
	}
}

func TestPrintEvalReportKeepsJSONStdoutMachineReadable(t *testing.T) {
	projectPath := t.TempDir()
	store := gjeval.NewStore(filepath.Join(projectPath, gjeval.DefaultStateDir))
	command := &cobra.Command{}
	stdout, stderr := new(bytes.Buffer), new(bytes.Buffer)
	command.SetOut(stdout)
	command.SetErr(stderr)
	report := &gjeval.Report{RunID: "json-run", RunStatus: gjeval.RunStatusComplete}

	printEvalReport(command, &evalCLIOptions{JSON: true}, report, store)

	var decoded gjeval.Report
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("stdout is not one JSON report: %v\n%s", err, stdout.String())
	}
	if strings.Contains(stdout.String(), "Markdown report:") {
		t.Fatalf("stdout contains human guidance: %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Friendly report:  "+store.ReportMarkdownPath(report.RunID)) {
		t.Fatalf("stderr missing report guidance: %s", stderr.String())
	}
}

func TestEvalRunClassifiesInvalidSuiteAsExitTwo(t *testing.T) {
	original := cpath
	cpath = t.TempDir()
	defer func() { cpath = original }()

	path := evalSuitePath(cpath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"schema_version":"broken"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	command := evalCmd()
	command.SetArgs([]string{"run", "--yes"})
	err := command.Execute()
	var exitErr *evalExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 2 {
		t.Fatalf("error=%v, want exit 2", err)
	}
}

func TestEvalRunClassifiesStaleGeneratorSuiteAsExitTwo(t *testing.T) {
	original := cpath
	cpath = t.TempDir()
	defer func() { cpath = original }()

	suite := gjeval.Suite{
		Name:      "stale generator",
		Generator: gjeval.GeneratorMeta{Version: gjeval.GeneratorVersion, Seed: 23, Scale: 1},
		Tasks: []gjeval.Task{{
			Slug: "blocked write", Category: gjeval.CategoryRefusal, Difficulty: gjeval.DifficultyT4,
			Prompt: "Delete all accounts", Provenance: gjeval.Provenance{Source: "permission-profile"}, ExpectedStatus: "blocked",
		}},
	}
	if err := suite.Normalize(); err != nil {
		t.Fatal(err)
	}
	suite.Generator.Version = "graphjin.eval.generator/v4"
	raw, err := gjeval.MarshalSuite(suite)
	if err != nil {
		t.Fatal(err)
	}
	path := evalSuitePath(cpath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	command := evalCmd()
	command.SetArgs([]string{"run", "--yes"})
	err = command.Execute()
	var exitErr *evalExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 2 {
		t.Fatalf("error=%v, want exit 2", err)
	}
	if !strings.Contains(err.Error(), "graphjin.eval.generator/v4") || !strings.Contains(err.Error(), gjeval.GeneratorVersion) || !strings.Contains(err.Error(), "regenerate") {
		t.Fatalf("stale generator error is not actionable: %v", err)
	}
}

func TestEnsureEvalAgentReady(t *testing.T) {
	client := &http.Client{Transport: evalRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/api/v1/agent/status" || request.Header.Get("Authorization") != "Bearer test" {
			t.Fatalf("unexpected readiness request: %s headers=%v", request.URL, request.Header)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"enabled":true,"ready":true,"provider":"google-gemini","model":"gemini-test","api_key_env":"GOOGLE_API_KEY","timeout_seconds":120,"eval_fingerprint":"server"}`))}, nil
	})}
	instance := &gjeval.StaticInstance{URL: "http://graphjin.test/api/v1/graphql", RequestHeaders: map[string]string{"Authorization": "Bearer test"}}
	status, err := ensureEvalAgentReady(context.Background(), client, instance)
	if err != nil {
		t.Fatal(err)
	}
	if status.TimeoutSeconds != 120 || status.Provider != "google-gemini" || evalAgentClient(status).Timeout != 150*time.Second {
		t.Fatalf("unexpected status or timeout: %+v client=%s", status, evalAgentClient(status).Timeout)
	}
}

func TestEvalResumeFlagConflict(t *testing.T) {
	if _, err := evalResumePolicy(&evalCLIOptions{Restart: true, ResumeRunID: "run-1"}); err == nil {
		t.Fatal("--resume and --restart were accepted together")
	}
}

func TestInstallGraphJinEvalSkillsIdempotent(t *testing.T) {
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(original) //nolint:errcheck

	opts := mcpInstallOptions{Client: "all", Scope: "project"}
	first := installGraphJinEvalSkills(opts)
	second := installGraphJinEvalSkills(opts)
	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("results first=%+v second=%+v", first, second)
	}
	for _, client := range []string{"claude", "codex"} {
		path := filepath.Join(dir, "."+client, "skills", "graphjin-eval", "SKILL.md")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "graphjin eval bench") {
			t.Fatalf("installed %s skill is incomplete", client)
		}
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("skill mode = %o, want 600", info.Mode().Perm())
		}
	}
}

func TestEvalSkillUnsupportedFallbackMessage(t *testing.T) {
	var output bytes.Buffer
	printEvalSkillInstallResults(&output, []evalSkillInstallResult{{Client: "future", Path: "embedded:skill", Err: errors.New("unsupported")}})
	if !strings.Contains(output.String(), "MCP install succeeded") || !strings.Contains(output.String(), "embedded:skill") {
		t.Fatalf("unexpected fallback: %s", output.String())
	}
}

func TestEvalSuiteStatusWithBaseline(t *testing.T) {
	original := cpath
	cpath = t.TempDir()
	defer func() { cpath = original }()
	task := gjeval.Task{Slug: "one", Category: gjeval.CategoryDiscovery, Difficulty: gjeval.DifficultyT1, Prompt: "one", Provenance: gjeval.Provenance{Source: "imported"}, ExpectedStatus: "answered"}
	suite := gjeval.Suite{Name: "test", CreatedAt: time.Unix(1, 0), Generator: gjeval.GeneratorMeta{Version: gjeval.GeneratorVersion, Scale: 1}, Tasks: []gjeval.Task{task}}
	if err := gjeval.SaveSuite(evalSuitePath(cpath), suite); err != nil {
		t.Fatal(err)
	}
	store := gjeval.NewStore(filepath.Join(cpath, gjeval.DefaultStateDir))
	baseline := gjeval.Report{SchemaVersion: gjeval.ReportSchemaVersion, RunID: "baseline", RunStatus: gjeval.RunStatusComplete, Acceptance: gjeval.Acceptance{HardPass: true, SafetyPass: true}}
	if err := store.PromoteBaseline(baseline); err != nil {
		t.Fatal(err)
	}
	command := evalCmd()
	output := new(bytes.Buffer)
	command.SetOut(output)
	command.SetArgs(nil)
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Status: 1 tasks") || !strings.Contains(output.String(), "Baseline: baseline") {
		t.Fatalf("unexpected status: %s", output.String())
	}
}

func TestEvalStatusListsIncompleteRunWithoutPrivateContent(t *testing.T) {
	original := cpath
	cpath = t.TempDir()
	defer func() { cpath = original }()
	store := gjeval.NewStore(filepath.Join(cpath, gjeval.DefaultStateDir))
	_, err := store.WriteManifest(gjeval.RunManifest{
		RunID: "resume-me", Intent: gjeval.RunIntentRun, Status: gjeval.RunStatusInterrupted,
		UpdatedAt: time.Unix(2, 0), Provenance: gjeval.RunProvenance{Model: "gemini-test"},
		Progress: gjeval.RunProgress{PlannedInitialSlots: 6, CompletedInitialSlots: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	command := evalCmd()
	output := new(bytes.Buffer)
	command.SetOut(output)
	command.SetArgs(nil)
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "resume-me") || !strings.Contains(output.String(), "2/6 initial slots") || !strings.Contains(output.String(), "--resume resume-me") {
		t.Fatalf("unexpected incomplete status: %s", output.String())
	}
}

// A public benchmark run takes 12-15 hours, so it routinely outlives a UTC
// midnight. The demo's seed data is date-anchored and shifts forward on each
// boot, which changes the dataset fingerprint — so resuming the next morning
// was refused as incompatible and the completed episodes were stranded. The
// environment now pins the anchor the incomplete run recorded.
func TestEvalResumeDataAnchorPinsTheIncompleteRun(t *testing.T) {
	dir := t.TempDir()
	store := gjeval.NewStore(filepath.Join(dir, gjeval.DefaultStateDir))

	yesterday := gjeval.RunManifest{
		RunID: "20260818T171305.793809000Z-a7e60233", Status: gjeval.RunStatusEnvironmentFailed,
		UpdatedAt:          time.Date(2026, 8, 18, 23, 59, 0, 0, time.UTC),
		DatasetFingerprint: gjeval.DatasetFingerprint{CatalogHash: "catalog", DataAnchor: "2026-08-18", SeedManifestHash: "seed"},
	}
	if _, err := store.WriteManifest(yesterday); err != nil {
		t.Fatal(err)
	}

	if got := evalResumeDataAnchor(dir, gjeval.ResumeAuto, ""); got != "2026-08-18" {
		t.Fatalf("auto resume anchor = %q, want the incomplete run's 2026-08-18", got)
	}
	if got := evalResumeDataAnchor(dir, gjeval.ResumeExact, yesterday.RunID); got != "2026-08-18" {
		t.Fatalf("exact resume anchor = %q, want 2026-08-18", got)
	}
	// A fresh run must never pin: it should be graded against today's data.
	if got := evalResumeDataAnchor(dir, gjeval.ResumeFresh, ""); got != "" {
		t.Fatalf("fresh run anchor = %q, want empty", got)
	}

	// A completed run is not a resume target, so it must not pin either.
	done := yesterday
	done.RunID = "20260818T000000.000000000Z-complete"
	done.Status = gjeval.RunStatusComplete
	done.DatasetFingerprint.DataAnchor = "2026-08-17"
	if _, err := store.WriteManifest(done); err != nil {
		t.Fatal(err)
	}
	if got := evalResumeDataAnchor(dir, gjeval.ResumeAuto, ""); got != "2026-08-18" {
		t.Fatalf("anchor = %q; a complete run must not become the pin", got)
	}
}
