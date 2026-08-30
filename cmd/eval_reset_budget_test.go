package main

import (
	"context"
	"testing"
	"time"

	ax "github.com/ax-llm/ax/packages/go"
	gjagent "github.com/dosco/graphjin/agent/v3"
	gjeval "github.com/dosco/graphjin/agent/v3/eval"
)

// resetBudget is what a mutation episode may spend restoring its world.
//
// Reset brackets every write, twice, so its cost is paid once per mutation
// episode per repeat and sets the ceiling on how fast a write-heavy suite can
// run. The SQLite demo currently resets in roughly 130ms; this leaves generous
// headroom for a loaded machine while still failing if the path regresses into
// something an order of magnitude slower — which is what would happen if the
// restore stopped being a file copy and started re-provisioning from seed.
const resetBudget = time.Second

func TestDemoResetStaysWithinItsBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded service integration")
	}
	project := t.TempDir()
	if err := extractDefaultDemo(project); err != nil {
		t.Fatal(err)
	}
	originalPath, originalConf, originalDB, originalOpened := cpath, conf, db, dbOpened
	defer func() {
		cpath, conf, db, dbOpened = originalPath, originalConf, originalDB, originalOpened
	}()
	t.Setenv("GO_ENV", "dev")
	client := &evalScriptClient{code: `await final({status:"blocked",answer:"not configured"});`}
	environment := evalEnvironment{ClientFactory: func(gjagent.Config) (ax.AIClient, error) { return client, nil }}
	instance, err := environment.Start(context.Background(), gjeval.EnvSpec{
		Target: gjeval.TargetDemo, ConfigPath: project, Seed: 23,
		Writable: true, Reactive: true, Resettable: true, FreezeTime: "2026-08-01T12:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close() //nolint:errcheck

	resettable, ok := instance.(gjeval.ResettableInstance)
	if !ok {
		t.Fatal("a resettable spec produced an instance that cannot reset")
	}
	// Several resets in a row: a restore that leaked handles or re-provisioned
	// would degrade across repeats rather than on the first one.
	var worst time.Duration
	for i := 0; i < 3; i++ {
		start := time.Now()
		if err := resettable.Reset(context.Background()); err != nil {
			t.Fatalf("reset %d failed: %v", i+1, err)
		}
		if elapsed := time.Since(start); elapsed > worst {
			worst = elapsed
		}
	}
	if worst > resetBudget {
		t.Fatalf("slowest reset took %s, over the %s budget", worst.Round(time.Millisecond), resetBudget)
	}
	// A reset must leave the world usable, not merely fast.
	if _, err := askEvalAgent(t, instance, "How many accounts are there?"); err != nil {
		t.Fatalf("the instance did not serve after being reset: %v", err)
	}
}
