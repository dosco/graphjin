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

func TestEvalProvenanceIncludesConfiguredMaxSteps(t *testing.T) {
	instance := &gjeval.StaticInstance{TargetLabel: "demo"}
	provenance := evalProvenance(instance, 23, gjeval.AgentStatus{
		Provider: "google-gemini", Model: "gemini-test", MaxSteps: 8,
	})
	if provenance.MaxSteps != 8 {
		t.Fatalf("max steps = %d, want 8", provenance.MaxSteps)
	}
}

func TestEmbeddedPublicBenchmarkSuiteMatchesPinnedSpec(t *testing.T) {
	suite, err := loadPublicEvalSuite()
	if err != nil {
		t.Fatal(err)
	}
	spec := gjeval.PublicBenchmark()
	if len(suite.Tasks) != spec.Scale || suite.Generator.Scale != spec.Scale || suite.Generator.Seed != spec.Seed {
		t.Fatalf("suite shape = tasks:%d generator:%+v spec:%+v", len(suite.Tasks), suite.Generator, spec)
	}
	if got := gjeval.SuiteFingerprint(*suite); got != spec.SuiteFingerprint {
		t.Fatalf("suite fingerprint = %s, want %s", got, spec.SuiteFingerprint)
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
	})
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
	printEvalReport(command, &evalCLIOptions{}, report)
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
	printEvalReport(command, &evalCLIOptions{}, &gjeval.Report{
		RunID: "candidate", RunStatus: gjeval.RunStatusComplete,
		Provenance: gjeval.RunProvenance{Repeats: 3},
		ProviderUsage: gjeval.ProviderUsage{
			TotalTokens: 300, LLMCalls: 7, UnknownAttempts: 2,
		},
	})
	for _, phrase := range []string{"usage accounting is incomplete", "2 timeout or transport attempt(s)", "Recorded tokens are a lower bound"} {
		if !strings.Contains(output.String(), phrase) {
			t.Fatalf("output missing %q: %s", phrase, output.String())
		}
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
