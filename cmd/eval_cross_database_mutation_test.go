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

// A mutation that is refused changes nothing, including the half of it that
// would have been allowed.
//
// This is the mutation a model wrote during a benchmark run: a note in the
// control plane and a ticket update in the application database, in one
// request. The control plane takes no SQL writes, so the request was answered
// "mutations blocked", and the ticket was updated anyway, because each
// database's half ran on its own and the errors were collected afterwards. The
// model told the user nothing had changed.
//
// The engine's own tests cover the rule (core/cross_database_mutation_test.go)
// and the lock the same request used to leave behind
// (serv/cross_database_lock_test.go). This one holds the request itself, end to
// end, against the databases it was sent to.
func TestRefusedCrossDatabaseMutationChangesNothing(t *testing.T) {
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
		Writable: true, FreezeTime: "2026-08-01T12:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close() //nolint:errcheck

	post := func(query string) string {
		t.Helper()
		status, payload, err := postEvalGraphQL(instance, query, 30*time.Second)
		if err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		if status >= 300 {
			t.Fatalf("%s: %d %s", query, status, payload)
		}
		return string(payload)
	}
	const ticket = `query { support_tickets(where: { id: { eq: 3 } }) { id status resolution_note } }`
	before := post(ticket)

	refused := post(`mutation { gj_task_entry(insert: { task_id: 3, body: "sorted out" }) { id } ` +
		`support_tickets(where: { id: { eq: 3 } }, update: { status: "resolved", resolution_note: "sorted out" }) ` +
		`{ id status resolution_note } }`)
	if !strings.Contains(refused, `"errors"`) || !strings.Contains(refused, "mutations blocked") {
		t.Fatalf("the cross-database mutation was not refused: %s", refused)
	}
	if after := post(ticket); after != before {
		t.Fatalf("the mutation was refused, but the ticket changed:\nbefore %s\nafter  %s", before, after)
	}

	// The ticket is writable: on its own, the same update goes through.
	alone := post(`mutation { support_tickets(where: { id: { eq: 3 } }, update: { status: "resolved" }) { id status } }`)
	if strings.Contains(alone, `"errors"`) || !strings.Contains(alone, `"resolved"`) {
		t.Fatalf("the ticket update on its own failed: %s", alone)
	}
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
