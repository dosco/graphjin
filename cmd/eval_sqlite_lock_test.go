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

// A write that names tables in two databases must not leave the application
// database locked.
//
// One did. A mutation with a root in the control-plane store and a root in the
// SQLite application database ran the application half as a single
// multi-statement string, and the half-finished statement it left behind kept a
// read lock on the table for the life of the process: across the instance being
// closed, across a reset, across every instance opened on that file afterwards.
// Reads still worked. The next write to that table waited forever, and no
// deadline could reach it.
func TestCrossDatabaseMutationDoesNotLockTheTableForLaterWrites(t *testing.T) {
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
	stuck := false
	defer func() {
		if !stuck {
			_ = instance.Close()
		}
	}()
	resettable, ok := instance.(gjeval.ResettableInstance)
	if !ok {
		t.Fatal("a resettable spec produced an instance that cannot reset")
	}

	// What the model wrote in the episode before the benchmark stopped: a note
	// in the control plane and the ticket update, in one mutation.
	const crossDatabase = `mutation { gj_task_entry(insert: { task_id: 3, body: "sorted out" }) { id } ` +
		`support_tickets(where: { id: { eq: 3 } }, update: { status: "resolved", resolution_note: "sorted out" }) ` +
		`{ id status resolution_note } }`
	status, payload, err := postEvalGraphQL(instance, crossDatabase, 30*time.Second)
	if err != nil {
		t.Fatalf("the cross-database mutation did not return: %v", err)
	}
	t.Logf("cross-database mutation: %d %s", status, payload)

	// On failure the instance is not closed: closing waits for the request
	// that is stuck behind the lock, and the test would hang instead of failing.
	const plainWrite = `mutation { support_tickets(where: { id: { eq: 5 } }, update: { status: "resolved" }) { id status } }`
	write := func(when string) {
		t.Helper()
		status, payload, err := postEvalGraphQL(instance, plainWrite, 20*time.Second)
		if err != nil {
			stuck = true
			t.Fatalf("a write to support_tickets %s never returned: %v", when, err)
		}
		if status >= 300 || strings.Contains(string(payload), `"errors"`) {
			t.Errorf("a write to support_tickets %s failed: %d %s", when, status, payload)
		}
	}
	// The connection the script ran on goes back to the pool with the statement
	// still open on it, so the next write that draws it cannot drop its own
	// temporary table.
	write("on the same instance")
	// A reset closes the pool, which cannot close that connection. Every
	// connection opened afterwards shares its cache, and its table lock.
	if err := resettable.Reset(context.Background()); err != nil {
		t.Fatal(err)
	}
	write("after a reset")
}

func postEvalGraphQL(instance gjeval.Instance, query string, timeout time.Duration) (int, []byte, error) {
	body, err := json.Marshal(map[string]any{"query": query})
	if err != nil {
		return 0, nil, err
	}
	base := strings.TrimRight(strings.TrimSpace(instance.BaseURL()), "/")
	request, err := http.NewRequest(http.MethodPost, base+"/api/v1/graphql", bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	for key, value := range instance.Headers() {
		request.Header.Set(key, value)
	}
	response, err := (&http.Client{Timeout: timeout}).Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close() //nolint:errcheck
	payload, err := io.ReadAll(response.Body)
	return response.StatusCode, payload, err
}
