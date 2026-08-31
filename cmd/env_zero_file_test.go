package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
	gjagent "github.com/dosco/graphjin/agent/v3"
	gjeval "github.com/dosco/graphjin/agent/v3/eval"
)

// An environment must be able to boot with nothing mounted.
//
// This is the whole premise of shipping one as an image: the binary already
// carries the demo project and the frozen public suite, so a container should
// need no files, no volume and no prior `eval create`. On master `--suite`
// could only name a path, and the extracted demo has no eval/ directory — so
// the server could not start without somebody first generating a suite.
func TestEnvServesTheEmbeddedSuiteWithNoFilesOnDisk(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded service integration")
	}
	// An empty working directory: nothing here but what the binary brings.
	workdir := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workdir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(previous) //nolint:errcheck

	if entries, err := os.ReadDir("."); err != nil || len(entries) != 0 {
		t.Fatalf("the test needs an empty directory: %d entries, %v", len(entries), err)
	}

	suite, source, err := resolveEnvSuite("public")
	if err != nil {
		t.Fatalf("the embedded suite must load with nothing on disk: %v", err)
	}
	if source != "public" {
		t.Fatalf("source = %q", source)
	}
	if len(suite.Tasks) < 100 {
		t.Fatalf("the frozen suite should carry its full task set, got %d", len(suite.Tasks))
	}

	originalPath, originalConf, originalDB, originalOpened := cpath, conf, db, dbOpened
	t.Setenv("GO_ENV", "dev")
	defer func() { cpath, conf, db, dbOpened = originalPath, originalConf, originalDB, originalOpened }()

	// resolveDemoPath with no --path extracts the built-in demo here.
	resolved, err := resolveDemoPath(false, os.Stderr)
	if err != nil {
		t.Fatalf("the built-in demo must extract into an empty directory: %v", err)
	}

	client := &evalScriptClient{code: `await final({status:"blocked",answer:"not configured"});`}
	environment := evalEnvironment{
		ClientFactory: func(gjagent.Config) (ax.AIClient, error) { return client, nil },
	}
	writable, reactive, resettable := evalSuiteEnvironmentRequirements(suite)
	pool, err := newEvalInstancePool(context.Background(),
		func(int) evalEnvironment { return environment },
		gjeval.EnvSpec{
			Target: gjeval.TargetDemo, ConfigPath: resolved, Seed: suite.Generator.Seed,
			Writable: writable, Reactive: reactive, Resettable: resettable,
			FreezeTime: "2026-08-01T12:00:00Z",
		}, 1)
	if err != nil {
		t.Fatalf("a pool must boot from the extracted demo alone: %v", err)
	}
	defer pool.Close() //nolint:errcheck

	// The frozen suite was verified against this very world, so the drift guard
	// must accept it — if it does not, one of the two has moved.
	if err := assertSuiteMatchesWorld(suite, pool.instances[0].Fingerprint(), false); err != nil {
		t.Fatalf("the embedded suite no longer describes the embedded demo: %v", err)
	}

	server, err := newEnvServer(suite, gjeval.RewardProfileRL, "train", nil, "2026-08-01T12:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	server.pool = pool
	server.suiteSource = source
	server.splitLabel = "none"

	rec := httptest.NewRecorder()
	server.handleHealth(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var health envHealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &health); err != nil {
		t.Fatal(err)
	}
	if health.Status != "ready" || health.Tasks != len(suite.Tasks) {
		t.Fatalf("health = %+v, want ready with %d tasks", health, len(suite.Tasks))
	}
	t.Logf("served %d tasks from the embedded suite with nothing on disk", health.Tasks)
}
