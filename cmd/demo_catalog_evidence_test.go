package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
	gjagent "github.com/dosco/graphjin/agent/v3"
	gjeval "github.com/dosco/graphjin/agent/v3/eval"
)

// TestDemoCatalogServesJoinAndValueEvidence pins, against a fully booted demo
// service and with zero provider tokens, the two catalog payloads a week of agent
// fixes turned out to depend on:
//
//   - The status column card carries its sampled values. A startup race once made
//     this vanish at random for a whole process lifetime, and nothing noticed for
//     five benchmark runs because no test read the live card.
//
//   - The account_health table detail names its remote-join route in edges_json.
//     The redirect that saves models from doomed top-level queries learns the join
//     from exactly this payload.
//
// Both are asserted through the same bootstrapping real benchmark runs use, so a
// regression here is a regression there.
func TestDemoCatalogServesJoinAndValueEvidence(t *testing.T) {
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
	catalogCard := func(id string, fields string) map[string]any {
		t.Helper()
		body, _ := json.Marshal(map[string]any{
			"query": fmt.Sprintf(`query { gj_catalog(where: {id: {eq: %q}}) { %s } }`, id, fields),
		})
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
		var decoded struct {
			Data struct {
				Cards []map[string]any `json:"gj_catalog"`
			} `json:"data"`
			Errors any `json:"errors"`
		}
		if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
			t.Fatal(err)
		}
		if len(decoded.Data.Cards) == 0 {
			t.Fatalf("no card for %s (errors=%v)", id, decoded.Errors)
		}
		return decoded.Data.Cards[0]
	}

	status := catalogCard("column:app:main.support_tickets.status", "id evidence_json examples_json")
	evidence, _ := status["evidence_json"].(string)
	for _, want := range []string{"observed_values", "open", "pending", "resolved"} {
		if !strings.Contains(evidence, want) {
			t.Fatalf("status column evidence missing %q: %s", want, evidence)
		}
	}
	if examples, _ := status["examples_json"].(string); !strings.Contains(examples, "open") {
		t.Fatalf("status examples should use a sampled value: %s", examples)
	}

	health := catalogCard("table:app:main.account_health", "id summary edges_json examples_json")
	if summary, _ := health["summary"].(string); !strings.Contains(summary, "4 columns") {
		t.Fatalf("account_health should publish its four columns: %q", summary)
	}
	edges, _ := health["edges_json"].(string)
	// The arrow arrives as -> or as Go's escaped -\u003e depending on the encoder.
	if !regexp.MustCompile(`relationship:[^"]*account_health\.__account_health_[^"]*-(>|\\u003e)[^"]*accounts`).MatchString(edges) {
		t.Fatalf("account_health edges must name its remote-join route: %s", edges)
	}
	// The example on this card is the one models copy. It used to show a
	// top-level read of a table that has no rows of its own, and benchmark
	// episodes reproduced that shape until their step budgets ran out.
	examples, _ := health["examples_json"].(string)
	if strings.Contains(examples, "{ account_health(limit") {
		t.Fatalf("account_health example still teaches the closed route: %s", examples)
	}
	if !strings.Contains(examples, "accounts(where:") || !strings.Contains(examples, "account_health {") {
		t.Fatalf("account_health example must teach the nested route: %s", examples)
	}
}
