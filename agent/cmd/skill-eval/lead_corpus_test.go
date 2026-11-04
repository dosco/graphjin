package main

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

var leadOracleAggregate = regexp.MustCompile(`\b(count|sum|avg|min|max)_[a-zA-Z0-9_]+`)

// The lead corpus is decoded with the runner's own case type and validator,
// so a field the runner does not read is caught here, not by a silent run.
func TestLeadEvalCorpusShape(t *testing.T) {
	data, err := os.ReadFile("../../testdata/lead_eval_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var cases []dataEvalCase
	if err := decoder.Decode(&cases); err != nil {
		t.Fatalf("parse lead corpus: %v", err)
	}
	if err := validateDataCases(cases); err != nil {
		t.Fatal(err)
	}
	tools := []string{"query_catalog", "execute_graphql", "execute_saved_query", "ask_graphjin", "graphql_help"}
	groups := map[string]int{}
	for _, c := range cases {
		groups[c.Group]++
		// Only the thread route reaches the lead; a direct case here would
		// quietly measure the worker instead.
		if c.Route != routeThread {
			t.Errorf("%s: lead cases must use the thread route", c.ID)
		}
		switch c.Group {
		case "single":
		case "multi":
			if len(c.Parts) < 2 {
				t.Errorf("%s: a multi-part case needs at least two parts", c.ID)
			}
		case "followup":
			if len(c.Turns) == 0 {
				t.Errorf("%s: a follow-up needs the earlier turn it refers back to", c.ID)
			}
		default:
			t.Errorf("%s: unknown group %q", c.ID, c.Group)
		}
		for _, text := range append([]string{c.Prompt}, c.Turns...) {
			for _, tool := range tools {
				if strings.Contains(text, tool) {
					t.Errorf("%s: prompts are natural language and never name tools (%s)", c.ID, tool)
				}
			}
		}
		// The engine computes: every oracle is a DB-side aggregate or a
		// limit-1 ranking, and every part must see a matching query.
		for _, part := range c.scoredParts() {
			if !leadOracleAggregate.MatchString(part.Oracle.Query) && !strings.Contains(part.Oracle.Query, "limit: 1") {
				t.Errorf("%s/%s: oracle must be an aggregate or limit: 1", c.ID, part.Label)
			}
			if len(part.Method.RequireQueryMatch) == 0 {
				t.Errorf("%s/%s: every part needs a method rule", c.ID, part.Label)
			}
			if part.Answer.TolerancePct > 0.05 {
				t.Errorf("%s/%s: tolerance stays within 5%%", c.ID, part.Label)
			}
		}
	}
	for _, group := range []string{"single", "multi", "followup"} {
		if groups[group] < 2 {
			t.Errorf("group %s has %d cases, want at least 2", group, groups[group])
		}
	}
}
