package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	ax "github.com/ax-llm/ax/packages/go"
	gjagent "github.com/dosco/graphjin/agent/v3"
	gjeval "github.com/dosco/graphjin/agent/v3/eval"
)

// A container is configured by whoever wrote its manifest. Every serve flag has
// to be reachable that way, or the image can only be run one way.
func TestEnvServeReadsItsConfigurationFromTheEnvironment(t *testing.T) {
	cmd := envServeCmd()
	flags := cmd.Flags()

	applied, err := applyEnvServeSettings(flags, []string{
		"GJ_ENV_LISTEN=0.0.0.0:8090",
		"GJ_ENV_SUITE=public",
		"GJ_ENV_SPLIT=auto:0.75",
		"GJ_ENV_SIDE=eval",
		"GJ_ENV_POOL=4",
		"GJ_ENV_STEP=true",
		"GJ_ENV_STEP_TIMEOUT=7m",
		"GJ_ENV_ALLOW_CATALOG_DRIFT=true",
		"GJ_ENV_WORK_DIR=/tmp/graphjin-env",
		"PATH=/usr/bin", // an unrelated variable must be ignored, not refused
	})
	if err != nil {
		t.Fatal(err)
	}
	for flag, want := range map[string]string{
		"listen": "0.0.0.0:8090", "suite": "public", "split": "auto:0.75", "side": "eval",
		"pool": "4", "step": "true", "step-timeout": "7m0s", "allow-catalog-drift": "true",
		"work-dir": "/tmp/graphjin-env",
	} {
		if got := flags.Lookup(flag).Value.String(); got != want {
			t.Fatalf("--%s = %q, want %q", flag, got, want)
		}
	}
	if len(applied.Lines) != 9 {
		t.Fatalf("the banner should name every applied variable, got %v", applied.Lines)
	}
	for _, flag := range []string{"listen", "suite", "split", "side", "pool", "step", "step-timeout"} {
		if !applied.Set[flag] {
			t.Fatalf("--%s was configured by the environment but is not reported as such", flag)
		}
	}
}

// Precedence has to hold even when the flag was passed the value it already
// had: "I set this on the command line" is the fact, not "this differs".
func TestEnvServeFlagsOutrankTheEnvironment(t *testing.T) {
	cmd := envServeCmd()
	flags := cmd.Flags()
	if err := flags.Set("pool", "2"); err != nil { // 2 is also the default
		t.Fatal(err)
	}
	if err := flags.Set("side", "train"); err != nil {
		t.Fatal(err)
	}
	applied, err := applyEnvServeSettings(flags, []string{"GJ_ENV_POOL=9", "GJ_ENV_SIDE=eval"})
	if err != nil {
		t.Fatal(err)
	}
	if got := flags.Lookup("pool").Value.String(); got != "2" {
		t.Fatalf("--pool = %q; a passed flag must outrank the environment", got)
	}
	if got := flags.Lookup("side").Value.String(); got != "train" {
		t.Fatalf("--side = %q", got)
	}
	// And it must say so — a variable silently overridden looks like a variable
	// that was never read.
	if len(applied.Lines) != 2 {
		t.Fatalf("applied = %v", applied.Lines)
	}
	if len(applied.Set) != 0 {
		t.Fatalf("an overridden variable must not be reported as having configured anything: %v", applied.Set)
	}
	for _, line := range applied.Lines {
		if !strings.Contains(line, "ignored") {
			t.Fatalf("an overridden variable must be reported as ignored: %q", line)
		}
	}
}

func TestEnvServeRefusesConfigurationItCannotUse(t *testing.T) {
	for _, tc := range []struct {
		name    string
		environ []string
		wants   []string
	}{
		{"a misspelled variable", []string{"GJ_ENV_POOLS=4"}, []string{"GJ_ENV_POOLS", "GJ_ENV_POOL"}},
		{"the wrong namespace", []string{"GJ_ENV_MODEL=gpt-5"}, []string{"GJ_ENV_MODEL", "GJ_AGENT_"}},
		{"a pool that is not a number", []string{"GJ_ENV_POOL=lots"}, []string{"GJ_ENV_POOL", "lots", "--pool"}},
		{"a duration that is not one", []string{"GJ_ENV_STEP_TIMEOUT=5"}, []string{"GJ_ENV_STEP_TIMEOUT", "--step-timeout"}},
		{"a boolean that is not one", []string{"GJ_ENV_STEP=please"}, []string{"GJ_ENV_STEP", "please"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := applyEnvServeSettings(envServeCmd().Flags(), tc.environ)
			if err == nil {
				t.Fatal("must be refused rather than ignored")
			}
			for _, want := range tc.wants {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("the refusal must mention %q: %v", want, err)
				}
			}
		})
	}
}

// An episode cancelled at 50 seconds is not an episode. Every sibling path
// raises the agent's deadline through applyDemoAgentEnvDefaults; env serve
// reaches none of it, so it has to resolve its own.
func TestEnvServeGivesAnEpisodeTimeToFinish(t *testing.T) {
	stock := 5 * time.Minute

	seconds, err := resolveEnvAgentTimeout("", false, stock)
	if err != nil || seconds != demoAgentTimeoutSeconds {
		t.Fatalf("an unpinned deadline must match every sibling path: %d %v", seconds, err)
	}
	// Step mode: the trainer may hold one completion for the whole idle
	// allowance, so the deadline has to clear it.
	seconds, err = resolveEnvAgentTimeout("", true, stock)
	if err != nil {
		t.Fatal(err)
	}
	if seconds <= int(stock.Seconds()) {
		t.Fatalf("a step deadline of %ds does not clear a %s allowance", seconds, stock)
	}
	// A deadline someone pinned is theirs.
	if seconds, err := resolveEnvAgentTimeout("900", false, stock); err != nil || seconds != 0 {
		t.Fatalf("a pinned deadline must be left alone: %d %v", seconds, err)
	}
	// Unless it would cancel the episode inside the allowance the same command
	// line just granted — two settings that contradict each other, one of which
	// silently wins, is the shape worth refusing.
	_, err = resolveEnvAgentTimeout("60", true, stock)
	if err == nil {
		t.Fatal("a pinned deadline shorter than --step-timeout must be refused")
	}
	for _, want := range []string{"GJ_AGENT_TIMEOUT_SECONDS", "--step-timeout"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal must name both settings: %v", err)
		}
	}
	if _, err := resolveEnvAgentTimeout("soon", false, stock); err == nil {
		t.Fatal("an unparseable deadline must be refused")
	}
}

func TestEnvServeWorkDirMustBeWritable(t *testing.T) {
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(previous) //nolint:errcheck

	// It is created if absent — a container's volume is often an empty mount.
	target := filepath.Join(t.TempDir(), "nested", "work")
	if err := enterEnvWorkDir(target); err != nil {
		t.Fatal(err)
	}
	here, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	if resolvedHere, _ := filepath.EvalSymlinks(here); resolvedHere != resolvedTarget {
		t.Fatalf("cwd = %s, want %s", here, resolvedTarget)
	}
	if err := enterEnvWorkDir(""); err != nil {
		t.Fatal("no work dir must leave the process where it is")
	}

	if os.Geteuid() == 0 || runtime.GOOS == "windows" {
		t.Skip("a read-only directory does not stop root")
	}
	readonly := filepath.Join(t.TempDir(), "readonly")
	if err := os.Mkdir(readonly, 0o500); err != nil {
		t.Fatal(err)
	}
	err = enterEnvWorkDir(filepath.Join(readonly, "work"))
	if err == nil {
		t.Fatal("an unwritable work dir must be refused at startup, not partway through provisioning")
	}
	if !strings.Contains(err.Error(), "volume") {
		t.Fatalf("the refusal should name the fix: %v", err)
	}
}

// The resolved deadline has to reach the world, not just the flag parser.
//
// A/B: on master no path carries it, so a served world reports the stock 50
// seconds and every episode is cancelled there — a step-driven one while the
// trainer is still holding the completion it was allowed to hold.
func TestTheResolvedDeadlineReachesTheServedWorld(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded service integration")
	}
	project := t.TempDir()
	if err := extractDefaultDemo(project); err != nil {
		t.Fatal(err)
	}
	originalPath, originalConf, originalDB, originalOpened := cpath, conf, db, dbOpened
	t.Setenv("GO_ENV", "dev")
	t.Setenv("GJ_AGENT_TIMEOUT_SECONDS", "")
	defer func() { cpath, conf, db, dbOpened = originalPath, originalConf, originalDB, originalOpened }()

	seconds, err := resolveEnvAgentTimeout("", true, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	client := &evalScriptClient{code: `await final({status:"blocked",answer:"not configured"});`}
	environment := evalEnvironment{ClientFactory: func(gjagent.Config) (ax.AIClient, error) { return client, nil }}
	pool, err := newEvalInstancePool(context.Background(),
		func(int) evalEnvironment { return environment },
		gjeval.EnvSpec{
			Target: gjeval.TargetDemo, ConfigPath: project, Seed: 23,
			FreezeTime: "2026-08-01T12:00:00Z", AgentTimeoutSeconds: seconds,
		}, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close() //nolint:errcheck

	response, err := http.Get(pool.instances[0].BaseURL() + "/api/v1/agent/status")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close() //nolint:errcheck
	var status struct {
		TimeoutSeconds int `json:"timeout_seconds"`
	}
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if status.TimeoutSeconds != seconds {
		t.Fatalf("the world runs episodes on a %ds deadline, but %ds was resolved",
			status.TimeoutSeconds, seconds)
	}
}
