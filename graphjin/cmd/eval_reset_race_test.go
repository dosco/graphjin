package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	ax "github.com/ax-llm/ax/packages/go"
	gjagent "github.com/dosco/graphjin/agent/v3"
	gjeval "github.com/dosco/graphjin/agent/v3/eval"
)

// resetsUnderWatchRunner is how many times the world is put back. The failure
// this guards against is a race, so it needs enough attempts to be unlucky in.
const resetsUnderWatchRunner = 20

// A reset replaces the database files on disk. That is only safe if closing the
// service has actually stopped everything that was reading and writing them.
//
// It had not. `HttpService.Close` ran a different shutdown from the one a
// listening server runs: it never cancelled the watch and revision workers
// before closing the databases, and never waited for them to stop. The reset
// then swapped the artifact store out from under goroutines still holding it
// open, and SQLite reported the result as a malformed database — sometimes
// immediately, sometimes several episodes later, always as an environment
// failure that looked like the model's fault.
//
// The watch runner only exists for reactive tasks, which is why this asks for a
// reactive instance: without one there is nothing writing during the reset and
// the race cannot happen.
func TestRepeatedResetsSurviveAnActiveWatchRunner(t *testing.T) {
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
	environment := evalEnvironment{
		ClientFactory: func(gjagent.Config) (ax.AIClient, error) { return client, nil },
	}
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
	// Install a watch so the runner has something to poll and deliver while the
	// resets happen. An idle runner still polls, but one with work to do writes.
	installEvalWatch(t, instance)

	for i := 0; i < resetsUnderWatchRunner; i++ {
		if err := resettable.Reset(context.Background()); err != nil {
			t.Fatalf("reset %d of %d failed: %v", i+1, resetsUnderWatchRunner, err)
		}
	}
	// A reset must leave the world usable, not merely survivable.
	if _, err := askEvalAgent(t, instance, "How many accounts are there?"); err != nil {
		t.Fatalf("the instance did not serve after %d resets: %v", resetsUnderWatchRunner, err)
	}
}

// installEvalWatch creates a bare inbox watch so the runner has real work.
//
// It posts the mutation directly rather than going through the oracle verifier,
// which refuses anything that is not a read — correctly, since an oracle that
// could write would be grading its own effect.
func installEvalWatch(t *testing.T, instance gjeval.Instance) {
	t.Helper()
	const mutation = `mutation { gj_watch(insert: {name: "reset_race_probe", ` +
		`description: "reset race probe", ` +
		`query: "subscription reset_race_probe { invoices(first: 25, after: $cursor) { id status } invoices_cursor }"}) ` +
		`{ id name } }`
	body, err := json.Marshal(map[string]any{"query": mutation})
	if err != nil {
		t.Fatal(err)
	}
	base := strings.TrimRight(strings.TrimSpace(instance.BaseURL()), "/")
	request, err := http.NewRequest(http.MethodPost, base+"/api/v1/graphql", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	for key, value := range instance.Headers() {
		request.Header.Set(key, value)
	}
	response, err := (&http.Client{Timeout: 60 * time.Second}).Do(request)
	if err != nil {
		t.Fatalf("could not install the watch this test needs: %v", err)
	}
	defer response.Body.Close() //nolint:errcheck
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode >= 300 || strings.Contains(string(payload), `"errors"`) {
		t.Fatalf("could not install the watch this test needs: %d %s", response.StatusCode, payload)
	}
}
