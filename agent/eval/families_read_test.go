package eval

import (
	"strings"
	"testing"
)

// readFamilySnapshot is a two-table schema with one closed value set, one
// numeric metric, one date column, and one relationship — the minimum shape
// each read family needs.
func readFamilySnapshot() CatalogSnapshot {
	return CatalogSnapshot{Rows: []CatalogRow{
		{
			ID: "table:accounts", Kind: "table", TableName: "accounts",
			DetailsJSON: `[{"ColumnName":"id","Type":"integer","PrimaryKey":true,"NotNull":true},
				{"ColumnName":"name","Type":"text","NotNull":true},
				{"ColumnName":"plan","Type":"text","NotNull":true},
				{"ColumnName":"mrr_cents","Type":"integer","NotNull":true},
				{"ColumnName":"created_at","Type":"datetime","NotNull":true}]`,
		},
		{
			ID: "table:invoices", Kind: "table", TableName: "invoices",
			DetailsJSON: `[{"ColumnName":"id","Type":"integer","PrimaryKey":true,"NotNull":true},
				{"ColumnName":"account_id","Type":"integer","NotNull":true},
				{"ColumnName":"amount_cents","Type":"integer","NotNull":true}]`,
		},
		columnRow("accounts", "plan", []any{
			`where: { plan: { eq: "enterprise" } }`,
			"plan values: enterprise, growth, starter",
		}),
		relRow("rel:invoices.account_id", "invoices.account_id -> accounts.id", "invoices", "account_id"),
	}}
}

func promptsOf(tasks []Task) []string {
	out := make([]string, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, task.Prompt)
	}
	return out
}

func findTaskWithQuery(t *testing.T, tasks []Task, fragments ...string) Task {
	t.Helper()
	for _, task := range tasks {
		if task.Oracle == nil {
			continue
		}
		matched := true
		for _, fragment := range fragments {
			if !strings.Contains(task.Oracle.Query, fragment) {
				matched = false
				break
			}
		}
		if matched {
			return task
		}
	}
	t.Fatalf("no task whose oracle contains %v; prompts: %v", fragments, promptsOf(tasks))
	return Task{}
}

func TestFilteredAggregateUsesOnlyValuesTheColumnHolds(t *testing.T) {
	tasks := generateFilteredAggregateCandidates(readFamilySnapshot(), 23)
	if len(tasks) == 0 {
		t.Fatal("expected filtered aggregate candidates")
	}
	count := findTaskWithQuery(t, tasks, `plan: {eq: "enterprise"}`, "count_id")
	if count.Category != CategoryAggregate || count.Difficulty != DifficultyT2 {
		t.Fatalf("unexpected classification: %s/%s", count.Category, count.Difficulty)
	}
	if count.Oracle.Extract != "accounts.0.count_id" {
		t.Fatalf("unexpected extract %q", count.Oracle.Extract)
	}
	if count.Provenance.Source != "filtered-aggregate" {
		t.Fatalf("unexpected provenance %q", count.Provenance.Source)
	}
	// A metric restricted by the same filter.
	findTaskWithQuery(t, tasks, `plan: {eq: "growth"}`, "sum_mrr_cents")

	// Every filter value must come from the published closed set. A task
	// filtering on a value the column never holds would assert a business state
	// that does not exist, and would pass only by agreeing with an empty result.
	allowed := map[string]bool{"enterprise": true, "growth": true, "starter": true}
	for _, task := range tasks {
		for _, part := range strings.Split(task.Oracle.Query, `eq: "`)[1:] {
			value := part[:strings.Index(part, `"`)]
			if !allowed[value] {
				t.Fatalf("task filters on unobserved value %q: %s", value, task.Oracle.Query)
			}
		}
	}
}

func TestFilteredAggregateSkipsTablesWithoutObservedValues(t *testing.T) {
	snapshot := readFamilySnapshot()
	for _, task := range generateFilteredAggregateCandidates(snapshot, 23) {
		if strings.Contains(task.Oracle.Query, "{ invoices(") {
			t.Fatalf("invoices publishes no closed value set: %s", task.Oracle.Query)
		}
	}
}

// The traversal family exists to give the category an objective oracle. Its
// questions must be answerable only by crossing the relationship.
func TestRelTraversalEmitsJoinCountOracles(t *testing.T) {
	tasks := generateRelTraversalCandidates(readFamilySnapshot(), 23)
	if len(tasks) == 0 {
		t.Fatal("expected traversal candidates")
	}
	for _, task := range tasks {
		if task.Category != CategoryTraversal {
			t.Fatalf("expected traversal category, got %s", task.Category)
		}
		if task.Difficulty != DifficultyT3 {
			t.Fatalf("expected T3, got %s", task.Difficulty)
		}
		if task.Answer.Kind != "number" {
			t.Fatalf("expected a numeric oracle, got %q", task.Answer.Kind)
		}
		if task.Provenance.Source != "rel-traversal" {
			t.Fatalf("unexpected provenance %q", task.Provenance.Source)
		}
	}

	// Existence is stated positively. The negated form reads the same in English
	// but is an anti-join: measured against the demo it counted every account,
	// including those with no invoice at all.
	existence := findTaskWithQuery(t, tasks, "accounts(where: {invoices:", "count_id")
	if !strings.Contains(existence.Oracle.Query, "is_null: false") {
		t.Fatalf("existence must be positive, got %s", existence.Oracle.Query)
	}
	if strings.Contains(existence.Oracle.Query, "not:") {
		t.Fatalf("existence must not be expressed as a negation: %s", existence.Oracle.Query)
	}

	// Counting one side while filtering on the other cannot be answered without
	// crossing the relationship.
	crossed := findTaskWithQuery(t, tasks, "invoices(where: {accounts:", `plan: {eq: "enterprise"}`)
	if crossed.Oracle.Extract != "invoices.0.count_id" {
		t.Fatalf("unexpected extract %q", crossed.Oracle.Extract)
	}
}

// A method rule that recognised only the nested form would score a correct
// two-step traversal as a method failure, and the suite would report an agent
// defect that is really a grading defect.
func TestRelTraversalMethodAcceptsTwoStepResolution(t *testing.T) {
	tasks := generateRelTraversalCandidates(readFamilySnapshot(), 23)
	crossed := findTaskWithQuery(t, tasks, "invoices(where: {accounts:")
	twoStep := `query { invoices(where: {account_id: {in: [1,2]}}) { count_id } }`
	if !evaluateMethod(crossed.Method, crossed.Answer, []string{twoStep}, nil, nil) {
		t.Fatalf("two-step resolution must satisfy the method rule; rule: %v", crossed.Method.RequireQueryMatch)
	}
	nested := crossed.Oracle.Query
	if !evaluateMethod(crossed.Method, crossed.Answer, []string{nested}, nil, nil) {
		t.Fatalf("the nested form must satisfy the method rule; rule: %v", crossed.Method.RequireQueryMatch)
	}
	// It must still require a database-side count rather than a client-side tally.
	listOnly := `query { invoices(limit: 100) { id } }`
	if evaluateMethod(crossed.Method, crossed.Answer, []string{listOnly}, nil, nil) {
		t.Fatal("a bare list must not satisfy the method rule")
	}
}

// The filtered-aggregate rule has the same hazard: grouping is a legitimate way
// to answer "how many are in state Y".
func TestFilteredAggregateMethodAcceptsGroupedProjection(t *testing.T) {
	tasks := generateFilteredAggregateCandidates(readFamilySnapshot(), 23)
	count := findTaskWithQuery(t, tasks, `plan: {eq: "enterprise"}`, "count_id")
	grouped := `query { accounts { plan count_id } }`
	if !evaluateMethod(count.Method, count.Answer, []string{grouped}, nil, nil) {
		t.Fatalf("a grouped projection must satisfy the method rule; rule: %v", count.Method.RequireQueryMatch)
	}
	if evaluateMethod(count.Method, count.Answer, []string{`query { accounts(limit: 50) { id name } }`}, nil, nil) {
		t.Fatal("a bare list must not satisfy the method rule")
	}
}

func TestWindowedFilterAnchorsToTheTablesOwnLatestDate(t *testing.T) {
	tasks := generateWindowedFilterCandidates(readFamilySnapshot(), 23)
	if len(tasks) == 0 {
		t.Fatal("expected windowed filter candidates")
	}
	task := tasks[0]
	if task.Category != CategoryWindow || task.Difficulty != DifficultyT3 {
		t.Fatalf("unexpected classification: %s/%s", task.Category, task.Difficulty)
	}
	if task.Oracle.AnchorQuery != "query { accounts { max_created_at } }" {
		t.Fatalf("unexpected anchor query %q", task.Oracle.AnchorQuery)
	}
	if task.Oracle.AnchorExtract != "accounts.0.max_created_at" {
		t.Fatalf("unexpected anchor extract %q", task.Oracle.AnchorExtract)
	}
	from, ok := task.Oracle.Variables["from"].(string)
	if !ok || !strings.HasPrefix(from, "{{anchor-") {
		t.Fatalf("window must be relative to the anchor, got %v", task.Oracle.Variables)
	}
	if !strings.Contains(task.Oracle.Query, "created_at") || !strings.Contains(task.Oracle.Query, "plan") {
		t.Fatalf("window must compose date and value filters: %s", task.Oracle.Query)
	}
}

func TestWindowedFilterSkipsTablesWithoutADateColumn(t *testing.T) {
	snapshot := readFamilySnapshot()
	for _, task := range generateWindowedFilterCandidates(snapshot, 23) {
		if strings.Contains(task.Oracle.Query, "{ invoices(") {
			t.Fatalf("invoices has no date column: %s", task.Oracle.Query)
		}
	}
}

// Generation must not depend on map iteration order: two runs over the same
// snapshot have to produce identical task sets, or a suite stops being
// reproducible from its recorded seed.
func TestReadFamiliesAreDeterministic(t *testing.T) {
	for _, family := range []struct {
		name     string
		generate func(CatalogSnapshot, int64) []Task
	}{
		{"filtered-aggregate", generateFilteredAggregateCandidates},
		{"rel-traversal", generateRelTraversalCandidates},
		{"windowed-filter", generateWindowedFilterCandidates},
	} {
		first := family.generate(readFamilySnapshot(), 23)
		for i := 0; i < 5; i++ {
			again := family.generate(readFamilySnapshot(), 23)
			if len(first) != len(again) {
				t.Fatalf("%s: emitted %d then %d tasks", family.name, len(first), len(again))
			}
			for j := range first {
				if first[j].Prompt != again[j].Prompt || first[j].Oracle.Query != again[j].Oracle.Query {
					t.Fatalf("%s: task %d differs between runs", family.name, j)
				}
			}
		}
	}
}
