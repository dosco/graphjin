package main

import (
	"strconv"
	"strings"
	"testing"

	gjeval "github.com/dosco/graphjin/agent/v3/eval"
	"github.com/spf13/cobra"
)

// findSubcommand returns the named child of a command.
func findSubcommand(t *testing.T, parent *cobra.Command, name string) *cobra.Command {
	t.Helper()
	for _, c := range parent.Commands() {
		if c.Name() == name {
			return c
		}
	}
	t.Fatalf("%q has no %q subcommand", parent.Name(), name)
	return nil
}

// benchCommand returns eval bench from the real CLI tree, so parsing sees the
// persistent flags its parents contribute — --path lives on the root and
// --demo on eval, and a resume command carries both.
func benchCommand(t *testing.T) *cobra.Command {
	t.Helper()
	root := newRootCmd()
	return findSubcommand(t, findSubcommand(t, root, "eval"), "bench")
}

// shellSplit undoes the strconv.Quote applied to the project path so the
// recorded arguments can be replayed the way a shell would hand them over.
func shellSplit(t *testing.T, args []string) []string {
	t.Helper()
	out := make([]string, 0, len(args))
	for _, a := range args {
		if unquoted, err := strconv.Unquote(a); err == nil {
			a = unquoted
		}
		out = append(out, a)
	}
	return out
}

// TestResumeCommandForPublicRunParses feeds the printed resume command back
// through the real bench command. A public run that records its arguments
// without --public resumes against the generated suite and is rejected as
// incompatible with the frozen run it means to continue; recording --public
// beside the scale and seed it pins fails validation just as hard. Parsing the
// arguments with the actual flag set is what makes both failures visible here
// rather than only on a resume hours into a benchmark.
func TestResumeCommandForPublicRunParses(t *testing.T) {
	spec := gjeval.PublicBenchmark()
	opts := &evalCLIOptions{Demo: true, Concurrency: 3, AutoResume: true, Yes: true}

	recorded := evalInvocationArgs(opts, "/tmp/project", spec.Scale, spec.Seed, true)
	manifest := gjeval.RunManifest{Intent: gjeval.RunIntentBench, RunID: "run-1", InvocationArgs: recorded}
	printed := manifest.ResumeCommand()

	if !strings.Contains(printed, "--public") {
		t.Fatalf("resume command drops --public, so it would load the generated suite: %s", printed)
	}

	bench := benchCommand(t)
	replay := shellSplit(t, append(recorded, "--resume", "run-1", "--yes"))
	if err := bench.ParseFlags(replay); err != nil {
		t.Fatalf("printed resume command does not parse: %v (%s)", err, printed)
	}

	// --public pins scale and seed; the bench command refuses the combination
	// when either is set explicitly, which is exactly what re-recording them
	// would do.
	for _, flag := range []string{"scale", "seed"} {
		if bench.Flags().Changed(flag) {
			t.Errorf("resume command re-sets --%s, which --public rejects: %s", flag, printed)
		}
	}
	if !bench.Flags().Changed("public") {
		t.Errorf("bench command did not see --public: %s", printed)
	}
}

// TestInvocationArgsWithoutPublicKeepsScaleAndSeed guards the other direction:
// a generated run is only reproducible if it replays the scale and seed it was
// generated with, so suppressing them must stay tied to --public.
func TestInvocationArgsWithoutPublicKeepsScaleAndSeed(t *testing.T) {
	opts := &evalCLIOptions{Demo: true}
	args := strings.Join(evalInvocationArgs(opts, "/tmp/project", 40, 7, false), " ")

	for _, want := range []string{"--scale 40", "--seed 7", "--demo"} {
		if !strings.Contains(args, want) {
			t.Errorf("generated run drops %q, losing reproducibility: %s", want, args)
		}
	}
	if strings.Contains(args, "--public") {
		t.Errorf("non-public run recorded --public: %s", args)
	}
}
