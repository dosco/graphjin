package eval

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3/queryshape"
)

// mineRows answers each query from a fixed table, as the server's runner
// would: the data object of a GraphQL response.
func mineRows(results map[string]any) MineRunner {
	return func(_ context.Context, _ MineCandidate, query string) (any, error) {
		data, ok := results[query]
		if !ok {
			return nil, errors.New("query failed")
		}
		return data, nil
	}
}

func mineCandidate(question, answer, query string) MineCandidate {
	return MineCandidate{
		ID: "run_1-1", RunID: "run_1", Delegate: 1, AskedAt: "2026-11-10T10:00:00Z",
		Question: question, Status: "answered", Answer: answer,
		Queries:       []MineQuery{{Query: query, OK: true, HasData: true}},
		TurnQuestions: 1,
	}
}

const (
	mineTotalQuery   = `query { accounts { sum_mrr_cents } }`
	mineRankingQuery = `query { accounts(where: {status: {in: ["active", "churn_risk"]}}, order_by: {renewal_date: asc}, limit: 1) { id name renewal_date } }`
)

func TestMineOneTurnsAFigureIntoATest(t *testing.T) {
	run := mineRows(map[string]any{mineTotalQuery: map[string]any{"accounts": []any{map[string]any{"sum_mrr_cents": float64(1980000)}}}})
	c := mineCandidate("What is our total monthly recurring revenue across all accounts?", "Total MRR is $19,800.", mineTotalQuery)
	out := MineOne(context.Background(), c, run)
	if out.Case == nil {
		t.Fatalf("dropped %s: %s", out.Drop, out.Detail)
	}
	got := *out.Case
	if got.Tier != MineTierIncumbent || got.Category != CategoryAggregate || got.Value != "1980000" {
		t.Fatalf("case = %+v", got)
	}
	if got.Oracle.Extract != "accounts.0.sum_mrr_cents" || got.Answer.Kind != "number" || len(got.Answer.AcceptScales) != 2 {
		t.Fatalf("oracle %+v answer %+v", got.Oracle, got.Answer)
	}
	if len(got.Method.RequireQueryMatch) != 1 || got.Method.RequireQueryMatch[0] != "sum_mrr_cents" || !got.Method.ForbidFinalizeFromListOnly {
		t.Fatalf("method = %+v", got.Method)
	}
	task, err := got.Task()
	if err != nil {
		t.Fatal(err)
	}
	if task.Provenance.Source != "mined-incumbent" || task.Provenance.GeneratorVersion != "" || task.ID == "" {
		t.Fatalf("task provenance %+v id %q", task.Provenance, task.ID)
	}
}

func TestMineOneTurnsARankingIntoATest(t *testing.T) {
	run := mineRows(map[string]any{mineRankingQuery: map[string]any{"accounts": []any{map[string]any{"id": float64(7), "name": "Meridian Robotics", "renewal_date": "2026-11-19"}}}})
	c := mineCandidate("Which active or at-risk account renews soonest?", "Meridian Robotics renews first, on 2026-11-19.", mineRankingQuery)
	c.Rating = "up"
	out := MineOne(context.Background(), c, run)
	if out.Case == nil {
		t.Fatalf("dropped %s: %s", out.Drop, out.Detail)
	}
	got := *out.Case
	if got.Tier != MineTierConfirmed || got.Category != CategoryRanking || got.Answer.Kind != "date" {
		t.Fatalf("case = %+v", got)
	}
	if got.Oracle.DimensionExtract != "accounts.0.name" || len(got.Oracle.DimensionAlternateExtracts) != 1 || got.Dimension != "Meridian Robotics" {
		t.Fatalf("oracle = %+v dimension %q", got.Oracle, got.Dimension)
	}
	if len(got.Method.RequireQueryMatch) != 1 || got.Method.RequireQueryMatch[0] != "order_by" {
		t.Fatalf("method = %+v", got.Method)
	}
}

func TestMineOneDropsWhatItCannotTest(t *testing.T) {
	listQuery := `query { support_tickets(where: {status: {eq: "open"}}) { id subject } }`
	datedQuery := `query { invoices(where: {issued_at: {gte: "2026-10-01"}}) { count_id } }`
	varQuery := `query Q($id: Int!) { accounts(where: {id: {eq: $id}}) { count_id } }`
	twoFigures := `query { accounts { sum_mrr_cents count_id } }`
	twoRoots := `query { accounts { count_id } invoices { count_id } }`
	write := `mutation { support_tickets(where: {id: {eq: 3}}, update: {status: "closed"}) { id } }`
	run := mineRows(map[string]any{
		mineTotalQuery: map[string]any{"accounts": []any{map[string]any{"sum_mrr_cents": float64(1980000)}}},
		listQuery:      map[string]any{"support_tickets": []any{map[string]any{"id": 1}, map[string]any{"id": 2}}},
		twoFigures:     map[string]any{"accounts": []any{map[string]any{"sum_mrr_cents": 1, "count_id": 2}}},
	})
	failing := mineRows(nil)
	q := "What is our total monthly recurring revenue across all accounts?"
	cases := []struct {
		name   string
		mutate func(*MineCandidate)
		run    MineRunner
		want   string
	}{
		{"rated wrong", func(c *MineCandidate) { c.Rating = "down" }, run, MineDropRatedWrong},
		{"blocked turn", func(c *MineCandidate) { c.Status = "blocked" }, run, MineDropNotAnswered},
		{"unsafe effect", func(c *MineCandidate) { c.ViolationCodes = []string{"forbidden_effect"} }, run, MineDropUnsafe},
		{"a write", func(c *MineCandidate) {
			c.Queries = append(c.Queries, MineQuery{Query: write, OK: true, HasData: true})
		}, run, MineDropWrite},
		{"no query", func(c *MineCandidate) { c.Queries = nil }, run, MineDropNoQuery},
		{"failed query only", func(c *MineCandidate) { c.Queries[0].OK = false }, run, MineDropNoQuery},
		{"mechanism words", func(c *MineCandidate) { c.Question = "Run a graphql query for total revenue please" }, run, MineDropProse},
		{"variables", func(c *MineCandidate) { c.Queries[0].Query = varQuery }, run, MineDropVariables},
		{"a question that names its date", func(c *MineCandidate) {
			c.Question, c.Queries[0].Query = "How many invoices have we issued since October 1, 2026?", datedQuery
		}, run, MineDropDated},
		{"a list", func(c *MineCandidate) { c.Queries[0].Query = listQuery }, run, MineDropList},
		{"two figures", func(c *MineCandidate) { c.Queries[0].Query = twoFigures }, run, MineDropSeveral},
		{"two roots", func(c *MineCandidate) { c.Queries[0].Query = twoRoots }, run, MineDropSeveral},
		{"query fails now", func(c *MineCandidate) {}, failing, MineDropQueryFailed},
		{"answer no longer matches", func(c *MineCandidate) { c.Answer = "Total MRR is $12,000." }, run, MineDropAnswerChanged},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := mineCandidate(q, "Total MRR is $19,800.", mineTotalQuery)
			tc.mutate(&c)
			out := MineOne(context.Background(), c, tc.run)
			if out.Case != nil || out.Drop != tc.want {
				t.Fatalf("drop = %q (%s), want %q", out.Drop, out.Detail, tc.want)
			}
		})
	}
}

func TestMineConfirmsOnlyASingleQuestionTurn(t *testing.T) {
	run := mineRows(map[string]any{mineTotalQuery: map[string]any{"accounts": []any{map[string]any{"sum_mrr_cents": float64(1980000)}}}})
	c := mineCandidate("What is our total monthly recurring revenue across all accounts?", "$19,800", mineTotalQuery)
	c.Rating, c.TurnQuestions = "up", 2
	out := MineOne(context.Background(), c, run)
	if out.Case == nil || out.Case.Tier != MineTierIncumbent {
		t.Fatalf("a thumbs-up on a two-question turn confirmed one of them: %+v", out)
	}
}

func TestMineKeepsOneTestPerQuestion(t *testing.T) {
	run := mineRows(map[string]any{mineTotalQuery: map[string]any{"accounts": []any{map[string]any{"sum_mrr_cents": float64(1980000)}}}})
	q := "What is our total monthly recurring revenue across all accounts?"
	older := mineCandidate(q, "$19,800", mineTotalQuery)
	older.ID, older.AskedAt, older.Rating = "run_a-1", "2026-11-01T00:00:00Z", "up"
	newer := mineCandidate("what is our total  monthly recurring revenue across all accounts?", "$19,800", mineTotalQuery)
	newer.ID, newer.AskedAt = "run_b-1", "2026-11-09T00:00:00Z"
	wrong := mineCandidate(q, "$1", mineTotalQuery)
	wrong.ID, wrong.Rating = "run_c-1", "down"
	report := Mine(context.Background(), []MineCandidate{older, newer, wrong}, run)
	if len(report.Cases) != 1 || report.Cases[0].ID != "run_a-1" {
		t.Fatalf("cases = %+v", report.Cases)
	}
	s := report.Summary
	if s.Candidates != 3 || s.Tests != 1 || s.ByTier[MineTierConfirmed] != 1 || s.Dropped[MineDropDuplicate] != 1 || s.Dropped[MineDropRatedWrong] != 1 {
		t.Fatalf("summary = %+v", s)
	}
}

func TestMinedSuiteLoadsAsASuite(t *testing.T) {
	run := mineRows(map[string]any{
		mineTotalQuery:   map[string]any{"accounts": []any{map[string]any{"sum_mrr_cents": float64(1980000)}}},
		mineRankingQuery: map[string]any{"accounts": []any{map[string]any{"id": float64(7), "name": "Meridian Robotics", "renewal_date": "2026-11-19"}}},
	})
	a := mineCandidate("What is our total monthly recurring revenue across all accounts?", "$19,800", mineTotalQuery)
	b := mineCandidate("Which active or at-risk account renews soonest?", "Meridian Robotics, 2026-11-19", mineRankingQuery)
	b.ID = "run_2-1"
	report := Mine(context.Background(), []MineCandidate{a, b}, run)
	suite, skipped, err := MinedSuite(report.Cases, "catalog", time.Now())
	if err != nil || len(skipped) != 0 || len(suite.Tasks) != 2 {
		t.Fatalf("suite tasks %d skipped %v err %v", len(suite.Tasks), skipped, err)
	}
	path := t.TempDir() + "/mined.yml"
	if err := SaveSuite(path, suite); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSuite(path)
	if err != nil || len(loaded.Tasks) != 2 {
		t.Fatalf("load: %v, %d tasks", err, len(loaded.Tasks))
	}
}

// Confirmed and incumbent tests are scored apart, and only reports that
// have them gain the section.
func TestReportsScoreMinedTiersApart(t *testing.T) {
	tasks := []Task{
		{ID: "a", Provenance: Provenance{Source: "mined-confirmed"}},
		{ID: "b", Provenance: Provenance{Source: "mined-incumbent"}},
		{ID: "c", Provenance: Provenance{Source: "mined-incumbent"}},
		{ID: "d", Provenance: Provenance{Source: "catalog-core"}},
	}
	verdicts := []TaskVerdict{{TaskID: "a", Pass: true}, {TaskID: "b", Pass: true}, {TaskID: "c"}, {TaskID: "d", Pass: true}}
	families := familyMetrics(tasks, verdicts, map[string][]Episode{}, 1)
	if families["mined-confirmed"].TaskCount != 1 || families["mined-confirmed"].Recall != 1 ||
		families["mined-incumbent"].TaskCount != 2 || families["mined-incumbent"].Recall != 0.5 || families["catalog-core"].TaskCount != 1 {
		t.Fatalf("families = %+v", families)
	}
	var b strings.Builder
	writeMinedFamilies(&b, Metrics{ByFamily: families})
	if !strings.Contains(b.String(), "| confirmed | 1 |") || !strings.Contains(b.String(), "| incumbent | 2 |") {
		t.Fatalf("section = %s", b.String())
	}
	b.Reset()
	writeMinedFamilies(&b, Metrics{ByFamily: map[string]TierMetrics{"catalog-core": families["catalog-core"]}})
	if b.Len() != 0 {
		t.Fatalf("a report without mined tests gained the section: %s", b.String())
	}
}

// A literal date in a question that names none is "today" moved by however
// far it was from the day asked, so the test still asks about upcoming
// renewals when it runs a month later.
func TestMineOneMakesLiteralDatesRelative(t *testing.T) {
	query := `query { accounts(where: { renewal_date: { gte: "2026-11-19" } }, order_by: { renewal_date: asc }, limit: 1) { name renewal_date } }`
	run := mineRows(map[string]any{query: map[string]any{"accounts": []any{map[string]any{"name": "Meridian Robotics", "renewal_date": "2026-11-28"}}}})
	c := mineCandidate("Which account has the earliest upcoming renewal date?", "Meridian Robotics renews first, on November 28, 2026.", query)
	c.AskedAt = "2026-11-19T15:04:05Z"
	out := MineOne(context.Background(), c, run)
	if out.Case == nil {
		t.Fatalf("dropped %s: %s", out.Drop, out.Detail)
	}
	oracle := out.Case.Oracle
	if strings.Contains(oracle.Query, "2026-11-19") || oracle.Variables["asked_0"] != "{{today}}" {
		t.Fatalf("oracle = %+v", oracle)
	}
	// Resolved a month later, the key asks from that day on.
	later := time.Date(2026, 12, 19, 9, 0, 0, 0, time.UTC)
	variables, err := resolveOracleVariables(oracle.Variables, "", later)
	if err != nil {
		t.Fatal(err)
	}
	resolved, _, err := resolveOracleQueryVariables(oracle.Query, variables)
	if err != nil || !strings.Contains(resolved, `"2026-12-19"`) {
		t.Fatalf("resolved = %s err = %v", resolved, err)
	}

	// The same date twice is one offset, and two roots are two figures.
	twice := `query { upcoming: accounts(where: { renewal_date: { gte: "2026-11-19" } }) { count_id } earliest: accounts(where: { renewal_date: { gte: "2026-11-19" } }, order_by: { renewal_date: asc }, limit: 1) { name renewal_date } }`
	c2 := mineCandidate(c.Question, c.Answer, twice)
	c2.AskedAt = c.AskedAt
	if out := MineOne(context.Background(), c2, run); out.Drop != MineDropSeveral {
		t.Fatalf("drop = %q (%s), want %q", out.Drop, out.Detail, MineDropSeveral)
	}

	c.AskedAt = "2026-11-21T00:00:00Z"
	out = MineOne(context.Background(), c, run)
	if out.Case == nil || out.Case.Oracle.Variables["asked_0"] != "{{today-2}}" {
		t.Fatalf("two days earlier than asked: %+v", out)
	}
}

// A top value with nothing to name its row but an id is the figure itself.
func TestMineOneTakesATopValue(t *testing.T) {
	query := `query { invoices(order_by: { amount_cents: desc }, limit: 1) { id amount_cents } }`
	run := mineRows(map[string]any{query: map[string]any{"invoices": []any{map[string]any{"id": float64(4), "amount_cents": float64(610000)}}}})
	c := mineCandidate("What is the largest single invoice amount we have billed?", "The largest invoice is 6,100.00.", query)
	out := MineOne(context.Background(), c, run)
	if out.Case == nil {
		t.Fatalf("dropped %s: %s", out.Drop, out.Detail)
	}
	if out.Case.Oracle.Extract != "invoices.0.amount_cents" || out.Case.Oracle.DimensionExtract != "" || out.Case.Category != CategoryRanking ||
		len(out.Case.Method.RequireQueryMatch) != 1 || out.Case.Method.RequireQueryMatch[0] != "order_by|(max|min)_amount_cents" {
		t.Fatalf("case = %+v", out.Case)
	}
}

func TestMinedTierLinesOnlyForMinedRuns(t *testing.T) {
	mined := Metrics{ByFamily: map[string]TierMetrics{
		"mined-incumbent": {TaskCount: 3, Recall: 2.0 / 3},
		"mined-confirmed": {TaskCount: 1, Recall: 1},
	}}
	got := strings.Join(MinedTierLines(mined), "\n") + "\n"
	want := "Confirmed tests: 1 of 1 passed (marked right by a person, so this is correctness).\n" +
		"Incumbent tests: 2 of 3 passed (never rated, so this is agreement with the agent that first answered).\n"
	if got != want {
		t.Fatalf("lines:\n%s\nwant:\n%s", got, want)
	}
	mixed := Metrics{ByFamily: map[string]TierMetrics{
		"mined-confirmed": {TaskCount: 1, Recall: 1},
		"generated":       {TaskCount: 5, Recall: 1},
	}}
	if lines := MinedTierLines(mixed); lines != nil {
		t.Fatalf("a run with generated tasks keeps its gates, got %q", lines)
	}
	if lines := MinedTierLines(Metrics{}); lines != nil {
		t.Fatalf("no families, got %q", lines)
	}
}

func TestFriendlyReportOfMinedRunKeepsTiersApart(t *testing.T) {
	report := Report{RunID: "r1", Metrics: Metrics{TaskCount: 4, ByFamily: map[string]TierMetrics{
		"mined-confirmed": {TaskCount: 1, Recall: 1},
		"mined-incumbent": {TaskCount: 3, Recall: 1},
	}}}
	out := RenderFriendlyReportMarkdown(report)
	if !strings.Contains(out, "Confirmed tests: 1 of 1 passed") || !strings.Contains(out, "Incumbent tests: 3 of 3 passed") {
		t.Fatalf("tiers missing:\n%s", out)
	}
	if strings.Contains(out, "| Correct answer |") || strings.Contains(out, "returned a correct answer on") || strings.Contains(out, "Passed on every attempt") {
		t.Fatalf("a combined score leaked into a mined run's report:\n%s", out)
	}
	technical := RenderTechnicalReportMarkdown(report)
	if !strings.Contains(technical, "Tests from real questions") {
		t.Fatalf("technical report lacks the tier table:\n%s", technical)
	}
	for _, pooled := range []string{"## By capability rollup", "## By difficulty tier"} {
		if strings.Contains(technical, pooled) {
			t.Fatalf("technical report pools the tiers in %q:\n%s", pooled, technical)
		}
	}
	accept := compareBaseline(report, nil)
	for _, notice := range accept.Notices {
		if strings.Contains(notice, "reference point") || strings.Contains(notice, "quality target") {
			t.Fatalf("a pooled recall notice on a mined run: %q", notice)
		}
	}
}

func TestMinedRelativeDatesKeepFixedBounds(t *testing.T) {
	query := `query { invoices(where: { issued_at: { gte: "2020-01-01", lte: "2026-11-19" } }) { sum_amount_cents } }`
	c := MineCandidate{Question: "What have we billed in total up to today?", AskedAt: "2026-11-19T15:04:05Z"}
	shape, err := queryshape.Inspect(query)
	if err != nil {
		t.Fatal(err)
	}
	got, vars, dated := minedRelativeDates(query, shape.Literals, c)
	if dated != "" {
		t.Fatalf("dropped as dated: %s", dated)
	}
	// The all-time bound stays as written; the day of asking moves with the run.
	if !strings.Contains(got, `"2020-01-01"`) || strings.Contains(got, `"2026-11-19"`) || len(vars) != 1 {
		t.Fatalf("query %s vars %v", got, vars)
	}
	for _, token := range vars {
		if token != "{{today}}" {
			t.Fatalf("token %v", token)
		}
	}

	// Only fixed bounds: nothing to shift, no variables.
	fixed := `query { invoices(where: { issued_at: { gte: "2020-01-01" } }) { count_id } }`
	shape, _ = queryshape.Inspect(fixed)
	if got, vars, dated := minedRelativeDates(fixed, shape.Literals, c); got != fixed || vars != nil || dated != "" {
		t.Fatalf("fixed bound changed: %s %v %q", got, vars, dated)
	}

	// A question naming a day as 11/19 names a date.
	c.Question = "How many invoices were issued on 11/19?"
	if _, _, dated := minedRelativeDates(query, shape.Literals, c); dated == "" {
		t.Fatal("a slash date in the question was not seen")
	}
}

func TestMineOneDropsAnAskerItCannotRebuild(t *testing.T) {
	query := `query { orders { count_id } }`
	c := mineCandidate("How many orders are there?", "There are 5 orders.", query)
	c.AskerUnknown = "ran as svc_old, no longer the service identity"
	ran := false
	run := func(context.Context, MineCandidate, string) (any, error) { ran = true; return nil, nil }
	out := MineOne(context.Background(), c, run)
	if out.Drop != MineDropAsker || ran {
		t.Fatalf("outcome %+v, ran %v", out, ran)
	}
}
