package agent

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

// dataEvalCorpusCase mirrors the schema consumed by cmd/skill-eval's
// ground-truth mode; this shape test keeps the corpus honest offline.
type dataEvalCorpusCase struct {
	ID                string                     `json:"id"`
	Group             string                     `json:"group"`
	Prompt            string                     `json:"prompt"`
	CapabilityProfile skillEvalCapabilityProfile `json:"capability_profile"`
	ExpectedStatus    string                     `json:"expected_status"`
	Oracle            struct {
		Query         string `json:"query"`
		Extract       string `json:"extract"`
		AnchorQuery   string `json:"anchor_query"`
		AnchorExtract string `json:"anchor_extract"`
		PickMax       *struct {
			List      string `json:"list"`
			Value     string `json:"value"`
			Dimension string `json:"dimension"`
		} `json:"pick_max"`
	} `json:"oracle"`
	Answer struct {
		Kind         string  `json:"kind"`
		TolerancePct float64 `json:"tolerance_pct"`
	} `json:"answer"`
	Method struct {
		RequireQueryMatch []string `json:"require_query_match"`
	} `json:"method"`
}

var dataEvalAggregatePrefix = regexp.MustCompile(`\b(count|sum|avg|min|max)_[a-zA-Z0-9_]+`)

func TestDataEvalCorpusShape(t *testing.T) {
	data, err := os.ReadFile("testdata/data_eval_cases.json")
	if err != nil {
		t.Fatalf("read data eval corpus: %v", err)
	}
	var cases []dataEvalCorpusCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("parse data eval corpus: %v", err)
	}
	if len(cases) < 12 {
		t.Fatalf("corpus cases = %d, want at least 12 across the three failure classes", len(cases))
	}

	validGroups := map[string]bool{"aggregate": true, "anchor": true, "ranking": true}
	toolNames := []string{"query_catalog", "execute_graphql", "execute_saved_query", "graphql_help", "validate_where_clause"}
	ids := map[string]bool{}
	groups := map[string]int{}

	for _, testCase := range cases {
		if testCase.ID == "" || testCase.Prompt == "" {
			t.Fatalf("case %q missing id or prompt", testCase.ID)
		}
		if ids[testCase.ID] {
			t.Fatalf("duplicate case id %q", testCase.ID)
		}
		ids[testCase.ID] = true
		if !validGroups[testCase.Group] {
			t.Fatalf("case %s has unknown group %q", testCase.ID, testCase.Group)
		}
		groups[testCase.Group]++
		if testCase.ExpectedStatus != "answered" && testCase.ExpectedStatus != "blocked" {
			t.Fatalf("case %s expected_status %q is not answered or blocked", testCase.ID, testCase.ExpectedStatus)
		}
		if testCase.CapabilityProfile.RoleClass == "" {
			t.Fatalf("case %s missing capability_profile.role_class", testCase.ID)
		}

		// Natural-language-first: prompts never coach tool usage.
		lowered := strings.ToLower(testCase.Prompt)
		for _, tool := range toolNames {
			if strings.Contains(lowered, tool) {
				t.Fatalf("case %s prompt coaches tool %q; prompts must stay natural language", testCase.ID, tool)
			}
		}

		// Oracle discipline: pure aggregates or limit-1 group-bys only, so
		// oracles stay immune to the low default limit the harness boots
		// with (pure aggregate roots self-set NoLimit).
		for name, query := range map[string]string{"oracle.query": testCase.Oracle.Query, "oracle.anchor_query": testCase.Oracle.AnchorQuery} {
			if query == "" {
				if name == "oracle.query" {
					t.Fatalf("case %s missing oracle.query", testCase.ID)
				}
				continue
			}
			if !dataEvalAggregatePrefix.MatchString(query) && !strings.Contains(query, "limit: 1") {
				t.Fatalf("case %s %s is neither a DB-side aggregate nor a limit-1 query: %s", testCase.ID, name, query)
			}
		}
		if testCase.Oracle.Extract == "" && testCase.Oracle.PickMax == nil {
			t.Fatalf("case %s missing oracle.extract or oracle.pick_max", testCase.ID)
		}
		if pm := testCase.Oracle.PickMax; pm != nil && (pm.List == "" || pm.Value == "" || pm.Dimension == "") {
			t.Fatalf("case %s pick_max needs list, value, and dimension", testCase.ID)
		}
		if testCase.Oracle.AnchorQuery != "" && testCase.Oracle.AnchorExtract == "" {
			t.Fatalf("case %s has anchor_query without anchor_extract", testCase.ID)
		}

		if testCase.Answer.TolerancePct < 0 || testCase.Answer.TolerancePct > 0.05 {
			t.Fatalf("case %s tolerance %v outside [0, 0.05]", testCase.ID, testCase.Answer.TolerancePct)
		}

		// Ranking cases must demand DB-side ranking evidence: order_by for
		// plain-column rankings, or an aggregate field for grouped rankings
		// (whose complete group rows the model may compare directly —
		// ordering by the aggregate is avoided until the engine compiles it
		// grouped; see the order_by-on-aggregate un-grouping bug).
		if testCase.Group == "ranking" {
			found := false
			for _, pattern := range testCase.Method.RequireQueryMatch {
				if strings.Contains(pattern, "order_by") || dataEvalAggregatePrefix.MatchString(pattern) || strings.Contains(pattern, "count_") {
					found = true
				}
			}
			if !found {
				t.Fatalf("ranking case %s must require order_by or an aggregate in method.require_query_match", testCase.ID)
			}
		}
	}

	for _, group := range []string{"aggregate", "anchor", "ranking"} {
		if groups[group] < 3 {
			t.Fatalf("group %s has %d cases, want at least 3", group, groups[group])
		}
	}
}
