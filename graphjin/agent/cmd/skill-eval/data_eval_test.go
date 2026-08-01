package main

import (
	"strings"
	"testing"
	"time"
)

func TestWalkPath(t *testing.T) {
	data := map[string]any{
		"accounts": []any{
			map[string]any{"sum_mrr_cents": float64(1980000), "name": "Solvex Dynamics"},
		},
	}
	value, ok := walkPath(data, "accounts.0.sum_mrr_cents")
	if !ok || valueString(value) != "1980000" {
		t.Fatalf("walkPath value = %v ok=%v", value, ok)
	}
	if _, ok := walkPath(data, "accounts.1.name"); ok {
		t.Fatal("out-of-range index should not resolve")
	}
	if _, ok := walkPath(data, "accounts.0.missing"); ok {
		t.Fatal("missing key should not resolve")
	}
}

func TestNumberFromString(t *testing.T) {
	for input, want := range map[string]float64{
		"$19,800":   19800,
		"1,980,000": 1980000,
		"2980.5":    2980.5,
		"81%":       81,
		"-42":       -42,
	} {
		got, ok := numberFromString(input)
		if !ok || got != want {
			t.Fatalf("numberFromString(%q) = %v ok=%v, want %v", input, got, ok, want)
		}
	}
	if _, ok := numberFromString("enterprise"); ok {
		t.Fatal("non-numeric input should not parse")
	}
}

func TestMatchWithScales(t *testing.T) {
	// Oracle 1980000 cents, answer in dollars.
	if !matchWithScales(19800, 1980000, []float64{1, 0.01}, 0) {
		t.Fatal("dollar-scale candidate should match cents oracle")
	}
	if !matchWithScales(1980000, 1980000, []float64{1, 0.01}, 0) {
		t.Fatal("exact candidate should match")
	}
	if matchWithScales(19700, 1980000, []float64{1, 0.01}, 0) {
		t.Fatal("wrong candidate should not match")
	}
	// 1% tolerance accepts a slightly-off average.
	if !matchWithScales(298001, 298000, []float64{1}, 0.01) {
		t.Fatal("candidate within tolerance should match")
	}
}

func TestSubstituteOracleTokensPreservesAnchorLayout(t *testing.T) {
	now := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	// RFC3339 anchor keeps the T separator when shifted.
	out, err := substituteOracleTokens("{{anchor-5d}}", "2026-07-30T12:00:00Z", now)
	if err != nil || out != "2026-07-25T12:00:00Z" {
		t.Fatalf("anchor shift = %q err=%v", out, err)
	}
	// Space-separated anchor keeps the space separator, so lexicographic
	// comparison against same-format stored values stays valid.
	out, err = substituteOracleTokens("{{anchor-5d}}", "2026-07-30 12:00:00", now)
	if err != nil || out != "2026-07-25 12:00:00" {
		t.Fatalf("space-layout anchor shift = %q err=%v", out, err)
	}
	out, err = substituteOracleTokens("{{today+30d}}", "", now)
	if err != nil || out != "2026-08-31" {
		t.Fatalf("today shift = %q err=%v", out, err)
	}
	if _, err = substituteOracleTokens("{{anchor}}", "", now); err == nil {
		t.Fatal("anchor token without anchor_query should error")
	}
}

func TestEvaluateMethod(t *testing.T) {
	aggregateQuery := []string{`query { accounts { sum_mrr_cents } }`}
	listQuery := []string{`query { accounts { name mrr_cents } }`}
	numeric := answerRule{Kind: "number"}

	rule := methodRule{RequireQueryMatch: []string{"sum_mrr_cents"}, ForbidFinalizeFromListOnly: true}
	if !evaluateMethod(rule, numeric, aggregateQuery, []string{"execute_graphql"}, "") {
		t.Fatal("aggregate query should pass method rule")
	}
	if evaluateMethod(rule, numeric, listQuery, []string{"execute_graphql"}, "") {
		t.Fatal("list-only query should fail require_query_match")
	}
	// No require patterns: the list-only guard alone must still fail a
	// numeric answer computed without any aggregate field.
	bare := methodRule{ForbidFinalizeFromListOnly: true}
	if evaluateMethod(bare, numeric, listQuery, []string{"execute_graphql"}, "") {
		t.Fatal("numeric answer from bare list fetch should fail method")
	}
	if !evaluateMethod(bare, numeric, aggregateQuery, []string{"execute_graphql"}, "") {
		t.Fatal("aggregate-backed numeric answer should pass method")
	}
}

func TestExecutedQueries(t *testing.T) {
	actions := []any{
		map[string]any{"tool": "query_catalog", "status": "ok", "args": map[string]any{"search": "mrr"}},
		map[string]any{"tool": "execute_graphql", "status": "ok", "args": map[string]any{"query": "query { accounts { sum_mrr_cents } }"}},
		map[string]any{"tool": "execute_graphql", "status": "rejected", "args": map[string]any{"query": "query { bogus { id } }"}},
	}
	queries, tools := executedQueries(actions)
	if len(queries) != 1 || queries[0] != "query { accounts { sum_mrr_cents } }" {
		t.Fatalf("queries = %v", queries)
	}
	if !contains(tools, "query_catalog") || !contains(tools, "execute_graphql") {
		t.Fatalf("tools = %v", tools)
	}
}

func TestAggregateDataVerdictsMajority(t *testing.T) {
	cases := []dataEvalCase{{ID: "c1", Group: "aggregate"}}
	pass, fail := true, false
	results := []runResult{
		{CaseID: "c1", GroundTruthPass: &pass, MethodPass: &pass},
		{CaseID: "c1", GroundTruthPass: &pass, MethodPass: &pass},
		{CaseID: "c1", GroundTruthPass: &fail, MethodPass: &fail, FailureBucket: "client_side_aggregation"},
	}
	verdicts := aggregateDataVerdicts(cases, results)
	if len(verdicts) != 1 {
		t.Fatalf("verdicts = %d", len(verdicts))
	}
	verdict := verdicts[0]
	if !verdict.GroundTruthPass || !verdict.MethodPass {
		t.Fatalf("majority verdict should pass: %+v", verdict)
	}
	if verdict.Consistency < 0.66 || verdict.Consistency > 0.67 {
		t.Fatalf("consistency = %v", verdict.Consistency)
	}

	// Oracle errors are excluded from scoring, not counted as failures.
	results = []runResult{
		{CaseID: "c1", OracleError: "boom"},
		{CaseID: "c1", GroundTruthPass: &pass, MethodPass: &pass},
	}
	verdict = aggregateDataVerdicts(cases, results)[0]
	if !verdict.GroundTruthPass || verdict.OracleErrorRuns != 1 || verdict.GroundTruthRuns != 1 {
		t.Fatalf("oracle-error exclusion verdict = %+v", verdict)
	}
}

func TestCalculateDataMetricsAndAcceptance(t *testing.T) {
	verdicts := []dataCaseVerdict{
		{CaseID: "a", Group: "aggregate", GroundTruthPass: true, MethodPass: true, Consistency: 1},
		{CaseID: "b", Group: "anchor", GroundTruthPass: false, MethodPass: true, Consistency: 0, FailureBucket: "stale_anchor"},
	}
	dm := calculateDataMetrics(verdicts, make([]runResult, 6))
	if dm.GroundTruthRecall != 0.5 || dm.MethodRecall != 1 {
		t.Fatalf("metrics = %+v", dm)
	}
	if dm.GroundTruthRecallByGroup["aggregate"] != 1 || dm.GroundTruthRecallByGroup["anchor"] != 0 {
		t.Fatalf("by-group = %+v", dm.GroundTruthRecallByGroup)
	}
	if dm.FailureBuckets["stale_anchor"] != 1 {
		t.Fatalf("buckets = %+v", dm.FailureBuckets)
	}

	// Below-target recall without a baseline warns but does not hard-fail —
	// the ratchet gates on regression, not on the aspiration.
	out := acceptance{HardPass: true}
	applyDataAcceptance(&out, dm, nil)
	if out.GroundTruthRecallPass == nil || !*out.GroundTruthRecallPass || !out.HardPass {
		t.Fatalf("below-target recall without baseline must warn, not fail: %+v", out)
	}
	if len(out.Warnings) == 0 || !strings.Contains(out.Warnings[0], "below the 0.90 target") {
		t.Fatalf("missing below-target warning: %+v", out.Warnings)
	}

	// An improved candidate below target still lands (with the warning).
	improved := dataMetrics{CaseCount: 2, GroundTruthRecall: 0.7, MethodRecall: 1}
	out = acceptance{HardPass: true}
	baseline := dataMetrics{CaseCount: 2, GroundTruthRecall: 0.5, MethodRecall: 0.5}
	applyDataAcceptance(&out, improved, &baseline)
	if out.GroundTruthRecallPass == nil || !*out.GroundTruthRecallPass || !*out.MethodRecallPass || !out.HardPass {
		t.Fatalf("improved below-target candidate must pass the ratchet: %+v", out)
	}

	// Regressions fail the ratchet — ground truth or method alike.
	regressed := dataMetrics{CaseCount: 2, GroundTruthRecall: 0.95, MethodRecall: 0.4}
	out = acceptance{HardPass: true}
	applyDataAcceptance(&out, regressed, &baseline)
	if *out.MethodRecallPass || out.HardPass {
		t.Fatalf("method regression must fail the gate: %+v", out)
	}
	regressedGT := dataMetrics{CaseCount: 2, GroundTruthRecall: 0.4, MethodRecall: 0.6}
	out = acceptance{HardPass: true}
	applyDataAcceptance(&out, regressedGT, &baseline)
	if *out.GroundTruthRecallPass || out.HardPass {
		t.Fatalf("ground-truth regression must fail the gate: %+v", out)
	}
}

func TestDateHelpers(t *testing.T) {
	if got := dateFromValue("2026-07-30T12:00:00Z"); got != "2026-07-30" {
		t.Fatalf("dateFromValue = %q", got)
	}
	variants := dateVariants("2026-07-30")
	want := map[string]bool{"2026-07-30": true, "July 30, 2026": true, "Jul 30, 2026": true, "30 July 2026": true}
	for _, variant := range variants {
		delete(want, variant)
	}
	if len(want) != 0 {
		t.Fatalf("missing variants: %v", want)
	}
}

func TestPickMaxRow(t *testing.T) {
	data := map[string]any{
		"accounts": []any{
			map[string]any{"plan": "starter", "sum_mrr_cents": float64(70000)},
			map[string]any{"plan": "enterprise", "sum_mrr_cents": float64(1610000)},
			map[string]any{"plan": "growth", "sum_mrr_cents": float64(300000)},
		},
	}
	value, dimension, err := pickMaxRow(data, &pickMaxRule{List: "accounts", Value: "sum_mrr_cents", Dimension: "plan"})
	if err != nil || value != "1610000" || dimension != "enterprise" {
		t.Fatalf("pickMaxRow = %q/%q err=%v", value, dimension, err)
	}
	if _, _, err := pickMaxRow(data, &pickMaxRule{List: "missing", Value: "x", Dimension: "y"}); err == nil {
		t.Fatal("missing list should error")
	}
	if _, _, err := pickMaxRow(map[string]any{"accounts": []any{}}, &pickMaxRule{List: "accounts", Value: "x", Dimension: "y"}); err == nil {
		t.Fatal("empty list should error")
	}
}

func TestOracleURL(t *testing.T) {
	if got := oracleURL("http://127.0.0.1:8083/api/v1/agent"); got != "http://127.0.0.1:8083/api/v1/graphql" {
		t.Fatalf("oracleURL = %q", got)
	}
}

func TestValidateDataCases(t *testing.T) {
	valid := dataEvalCase{
		ID: "x", Prompt: "p", ExpectedStatus: "answered",
		Oracle: dataOracle{Query: "query { a { count_id } }", Extract: "a.0.count_id"},
	}
	if err := validateDataCases([]dataEvalCase{valid}); err != nil {
		t.Fatalf("valid case rejected: %v", err)
	}
	if err := validateDataCases([]dataEvalCase{valid, valid}); err == nil {
		t.Fatal("duplicate id should be rejected")
	}
	broken := valid
	broken.Oracle.Extract = ""
	broken.ID = "y"
	if err := validateDataCases([]dataEvalCase{broken}); err == nil {
		t.Fatal("missing extract should be rejected")
	}
}

func TestRegistryHashForMultipleFiles(t *testing.T) {
	single := registryHashFor("main.go")
	combined := registryHashFor("main.go,data_eval.go")
	if single == "" || combined == "" {
		t.Fatal("hashing repository files should succeed")
	}
	if single == combined {
		t.Fatal("adding a file must change the registry hash")
	}
	if registryHashFor("does-not-exist.go") != "" {
		t.Fatal("missing file must yield empty hash")
	}
}
