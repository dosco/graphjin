package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
	gjagent "github.com/dosco/graphjin/agent/v3"
	gjeval "github.com/dosco/graphjin/agent/v3/eval"
)

// Models write Hasura mutations constantly — across 2043 stored benchmark
// episodes insert_X appeared 28 times, update_X 20, _set 18, pk_columns 7 —
// and every one of them used to die at the root. This drives the real forms
// against a booted GraphJin and asserts both halves: the row actually changes,
// and the response comes back in the shape a Hasura client expects.
func TestDemoHasuraMutationsExecuteAndReshape(t *testing.T) {
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
		Target: gjeval.TargetDemo, ConfigPath: project, Seed: 23, Writable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close() //nolint:errcheck

	base := strings.TrimRight(strings.TrimSpace(instance.BaseURL()), "/")
	for _, suffix := range []string{"/api/v1/agent/status", "/api/v1/agent", "/api/v1/graphql"} {
		base = strings.TrimSuffix(base, suffix)
	}
	run := func(query string) map[string]any {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"query": query})
		request, err := http.NewRequest(http.MethodPost, base+"/api/v1/graphql", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		for key, value := range instance.Headers() {
			request.Header.Set(key, value)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close() //nolint:errcheck
		var decoded map[string]any
		if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
			t.Fatalf("decode %q: %v", query, err)
		}
		return decoded
	}

	// --- update with _set, returning and affected_rows --------------------
	out := run(`mutation {
		update_support_tickets(where: { id: { _eq: 1 } }, _set: { status: "resolved" }) {
			returning { id status }
			affected_rows
		}
	}`)
	if out["errors"] != nil {
		t.Fatalf("Hasura update must execute: %v", out["errors"])
	}
	root, _ := out["data"].(map[string]any)["update_support_tickets"].(map[string]any)
	if root == nil {
		t.Fatalf("response must be nested in Hasura shape: %v", out["data"])
	}
	returning, _ := root["returning"].([]any)
	if len(returning) != 1 {
		t.Fatalf("returning must carry the written row: %v", root)
	}
	if row, _ := returning[0].(map[string]any); row["status"] != "resolved" {
		t.Fatalf("the write must actually land: %v", root)
	}
	if affected, _ := root["affected_rows"].(float64); affected != 1 {
		t.Fatalf("affected_rows = %v, want 1", root["affected_rows"])
	}

	// The row really changed, read back natively.
	check := run(`query { support_tickets(where: { id: { eq: 1 } }) { id status } }`)
	rows, _ := check["data"].(map[string]any)["support_tickets"].([]any)
	if len(rows) != 1 {
		t.Fatalf("read-back failed: %v", check)
	}
	if row, _ := rows[0].(map[string]any); row["status"] != "resolved" {
		t.Fatalf("the database was not changed: %v", rows[0])
	}

	// --- insert with objects and returning --------------------------------
	out = run(`mutation {
		insert_payments(objects: { id: 990901, invoice_id: 1, amount_cents: 4242, reference: "HASURA-1", recorded_at: "2027-01-15T12:00:00Z" }) {
			returning { id reference }
		}
	}`)
	if out["errors"] != nil {
		t.Fatalf("Hasura insert must execute: %v", out["errors"])
	}
	inserted, _ := out["data"].(map[string]any)["insert_payments"].(map[string]any)
	returning, _ = inserted["returning"].([]any)
	if len(returning) != 1 {
		t.Fatalf("insert returning must carry the row: %v", inserted)
	}
	if row, _ := returning[0].(map[string]any); row["reference"] != "HASURA-1" {
		t.Fatalf("the insert must land: %v", returning[0])
	}

	// --- _by_pk returns a single object, not a list -----------------------
	out = run(`mutation {
		update_support_tickets_by_pk(pk_columns: { id: 2 }, _set: { status: "pending" }) { id status }
	}`)
	if out["errors"] != nil {
		t.Fatalf("Hasura by_pk update must execute: %v", out["errors"])
	}
	single, ok := out["data"].(map[string]any)["update_support_tickets_by_pk"].(map[string]any)
	if !ok {
		t.Fatalf("a _by_pk root must return one object, got %T: %v", out["data"], out["data"])
	}
	if single["status"] != "pending" {
		t.Fatalf("the by_pk write must land: %v", single)
	}

	// --- an unsupported shape fails loudly rather than writing the wrong thing
	out = run(`mutation {
		update_support_tickets(where: { id: { _eq: 3 } }, _inc: { id: 1 }) { affected_rows }
	}`)
	if out["errors"] == nil {
		t.Fatalf("an unimplemented Hasura construct must fail loudly: %v", out)
	}
	if !strings.Contains(string(mustJSON(t, out["errors"])), "_inc") {
		t.Fatalf("the refusal must name the unsupported construct: %v", out["errors"])
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
