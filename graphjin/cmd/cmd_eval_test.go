package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gjeval "github.com/dosco/graphjin/agent/v3/eval"
)

type evalRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn evalRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestEvalCommandSurfaceAndContextualFlags(t *testing.T) {
	command := evalCmd()
	want := map[string]bool{"create": false, "add": false, "rm": false, "run": false, "baseline": false, "bench": false}
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
	if bench.Flags().Lookup("scale") == nil || bench.Flags().Lookup("seed") == nil {
		t.Fatal("bench is missing --scale or --seed")
	}
	create, _, err := command.Find([]string{"create"})
	if err != nil {
		t.Fatal(err)
	}
	if create.Flags().Lookup("scale") != nil || create.Flags().Lookup("seed") != nil {
		t.Fatal("create exposed bench-only flags")
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
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"enabled":true,"ready":true}`))}, nil
	})}
	instance := &gjeval.StaticInstance{URL: "http://graphjin.test/api/v1/graphql", RequestHeaders: map[string]string{"Authorization": "Bearer test"}}
	if err := ensureEvalAgentReady(context.Background(), client, instance); err != nil {
		t.Fatal(err)
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
	baseline := gjeval.Report{SchemaVersion: gjeval.ReportSchemaVersion, RunID: "baseline", Acceptance: gjeval.Acceptance{HardPass: true}}
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
