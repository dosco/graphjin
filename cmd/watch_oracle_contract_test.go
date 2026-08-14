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

// TestWatchOracleContractExecutable pins, against a fully booted demo service,
// the exact post-state contract the reactive-need-* and multi-turn-confirm-*
// benchmark tasks verify: after a watch insert, a gj_watch read filtered by
// query LIKE plus delivery_json is_null:false must return count_id 1.
//
// Two managed-root bugs made that contract unsatisfiable for every model on
// every run (0/24 episodes across two generations, indistinguishable from
// model failure):
//
//   - managedExpToValue dropped the is_null literal, so `is_null: false`
//     inverted into `is_null: true` and excluded every populated row.
//   - managedSelectedFields dropped function fields, so `count_id` projected
//     an empty object no extract could read.
//
// The insert mutation below is byte-for-byte the one a failing episode
// executed; the oracle query is byte-for-byte the frozen task spec.
func TestWatchOracleContractExecutable(t *testing.T) {
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
	instance, err := environment.Start(context.Background(), gjeval.EnvSpec{Target: gjeval.TargetDemo, ConfigPath: project, Seed: 23})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close() //nolint:errcheck

	baseURL := strings.TrimRight(strings.TrimSpace(instance.BaseURL()), "/")
	for _, suffix := range []string{"/api/v1/agent/status", "/api/v1/agent", "/api/v1/graphql"} {
		baseURL = strings.TrimSuffix(baseURL, suffix)
	}
	post := func(query string) map[string]any {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"query": query})
		request, err := http.NewRequest(http.MethodPost, baseURL+"/api/v1/graphql", bytes.NewReader(body))
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
			t.Fatal(err)
		}
		if errs, ok := decoded["errors"]; ok && errs != nil {
			t.Fatalf("query returned errors: %v (query: %s)", errs, query)
		}
		return decoded
	}
	watchRows := func(decoded map[string]any) []any {
		t.Helper()
		data, _ := decoded["data"].(map[string]any)
		rows, _ := data["gj_watch"].([]any)
		return rows
	}

	post(`mutation { gj_watch(insert: { name: "accounting_hourly_payments", query: "subscription hourly_payments { payments(first: 25, after: $cursor) { id amount_cents reference recorded_at } payments_cursor }", delivery_json: { kind: "inbox", digest: { window: "1h" } } }) { id name status enabled delivery_json } }`)

	oracle := watchRows(post(`query { gj_watch(where: {and: [{query: {like: "%payments_cursor%"}}, {delivery_json: {is_null: false}}]}) { count_id } }`))
	if len(oracle) != 1 {
		t.Fatalf("oracle query returned %d rows, want the single aggregate row: %v", len(oracle), oracle)
	}
	if count, _ := oracle[0].(map[string]any); count["count_id"] != float64(1) {
		t.Fatalf("oracle count_id = %v, want 1", count["count_id"])
	}

	// The inverse filter must exclude the populated watch rather than repeat it.
	if rows := watchRows(post(`query { gj_watch(where: {delivery_json: {is_null: true}}) { id name } }`)); len(rows) != 0 {
		t.Fatalf("is_null: true matched populated rows: %v", rows)
	}

	// A non-matching LIKE keeps the aggregate shape and reports zero.
	empty := watchRows(post(`query { gj_watch(where: {query: {like: "%no_such_cursor%"}}) { count_id } }`))
	if len(empty) != 1 {
		t.Fatalf("empty aggregate returned %d rows, want one zero row: %v", len(empty), empty)
	}
	if count, _ := empty[0].(map[string]any); count["count_id"] != float64(0) {
		t.Fatalf("empty aggregate count_id = %v, want 0", count["count_id"])
	}

	// Plain filtered reads keep their historical row shape.
	if rows := watchRows(post(`query { gj_watch(where: {query: {like: "%payments_cursor%"}}) { id name } }`)); len(rows) != 1 {
		t.Fatalf("plain filtered read regressed: %v", rows)
	}
}
