package eval

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
)

type staticCatalogSource struct{ snapshot CatalogSnapshot }

func (s staticCatalogSource) Snapshot(context.Context) (CatalogSnapshot, error) {
	return s.snapshot, nil
}

func TestGeneratorDeterminismStratificationDedupAndCap(t *testing.T) {
	curated := []Task{
		curatedTask("one", DifficultyT1), curatedTask("two", DifficultyT2),
		curatedTask("three", DifficultyT3), curatedTask("four", DifficultyT4),
		curatedTask("five", DifficultyT1), curatedTask("six", DifficultyT2),
	}
	curated = append(curated, curated[0])
	source := staticCatalogSource{snapshot: CatalogSnapshot{Fingerprint: "catalog"}}
	now := func() time.Time { return time.Unix(100, 0) }
	first, err := (Generator{Source: source, Now: now}).Generate(context.Background(), GeneratorOptions{Seed: 23, Scale: 4, Curated: curated})
	if err != nil {
		t.Fatal(err)
	}
	second, err := (Generator{Source: source, Now: func() time.Time { return time.Unix(200, 0) }}).Generate(context.Background(), GeneratorOptions{Seed: 23, Scale: 4, Curated: curated})
	if err != nil {
		t.Fatal(err)
	}
	one, _ := MarshalSuite(*first)
	two, _ := MarshalSuite(*second)
	if string(one) != string(two) {
		t.Fatal("same seed and catalog did not produce byte-identical suite")
	}
	if len(first.Tasks) != 4 {
		t.Fatalf("task count = %d, want cap 4", len(first.Tasks))
	}
	tiers := map[Difficulty]bool{}
	ids := map[string]bool{}
	for _, task := range first.Tasks {
		tiers[task.Difficulty] = true
		if ids[task.ID] {
			t.Fatalf("duplicate task %s", task.ID)
		}
		ids[task.ID] = true
	}
	for _, tier := range []Difficulty{DifficultyT1, DifficultyT2, DifficultyT3, DifficultyT4} {
		if !tiers[tier] {
			t.Fatalf("missing tier %s", tier)
		}
	}
}

func TestStratifiedSampleUsesPublicCategoryTargets(t *testing.T) {
	var tasks []Task
	for _, spec := range categoryQuotaSpecs {
		for i := 0; i < 120; i++ {
			tasks = append(tasks, Task{
				ID: fmt.Sprintf("%s-%03d", spec.Category, i), Category: spec.Category,
				Difficulty: DifficultyT1, Provenance: Provenance{Source: "catalog-entity"},
			})
		}
	}
	selected := stratifiedSample(tasks, 100, 23)
	counts := categoryCounts(selected)
	want := map[Category]int{
		CategoryAggregate: 25, CategoryWindow: 25, CategoryRanking: 15,
		CategoryDiscovery: 10, CategorySavedMetric: 10, CategoryRefusal: 10,
		CategoryTraversal: 5,
	}
	if len(selected) != 100 {
		t.Fatalf("selected %d tasks, want 100", len(selected))
	}
	for category, count := range want {
		if counts[category] != count {
			t.Fatalf("%s count = %d, want %d (all=%v)", category, counts[category], count, counts)
		}
	}
	again := stratifiedSample(tasks, 100, 23)
	for i := range selected {
		if selected[i].ID != again[i].ID {
			t.Fatalf("same seed diverged at %d: %s != %s", i, selected[i].ID, again[i].ID)
		}
	}
}

func TestStratifiedSampleBackfillsScarceCategoriesWithoutTraversalGrowth(t *testing.T) {
	var tasks []Task
	for _, category := range []Category{CategoryAggregate, CategoryWindow, CategoryRanking, CategoryDiscovery} {
		for i := 0; i < 60; i++ {
			tasks = append(tasks, Task{ID: fmt.Sprintf("%s-%03d", category, i), Category: category, Difficulty: DifficultyT2})
		}
	}
	for i := 0; i < 40; i++ {
		tasks = append(tasks, Task{ID: fmt.Sprintf("traversal-%03d", i), Category: CategoryTraversal, Difficulty: DifficultyT3})
	}
	selected := stratifiedSample(tasks, 100, 23)
	counts := categoryCounts(selected)
	if len(selected) != 100 {
		t.Fatalf("selected %d tasks, want 100", len(selected))
	}
	if counts[CategoryTraversal] != 5 {
		t.Fatalf("traversal count = %d, want quota cap 5 (all=%v)", counts[CategoryTraversal], counts)
	}
	for _, category := range []Category{CategoryAggregate, CategoryWindow, CategoryRanking, CategoryDiscovery} {
		if counts[category] < quotaForCategory(scaledCategoryQuotas(100), category) {
			t.Fatalf("%s did not receive its quota: %v", category, counts)
		}
	}
}

func categoryCounts(tasks []Task) map[Category]int {
	counts := map[Category]int{}
	for _, task := range tasks {
		counts[task.Category]++
	}
	return counts
}

func TestGeneratorDerivesRefusalFromPermissions(t *testing.T) {
	source := staticCatalogSource{snapshot: CatalogSnapshot{
		Fingerprint: "catalog",
		Status:      AgentStatus{ReadOnly: true},
		Profiles:    []CapabilityProfile{{RoleClass: "support", ReadOnly: true}},
	}}
	suite, err := (Generator{Source: source, Now: func() time.Time { return time.Unix(1, 0) }}).Generate(context.Background(), GeneratorOptions{Seed: 1, Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	if suite.Tasks[0].Category != CategoryRefusal || suite.Tasks[0].Difficulty != DifficultyT4 || suite.Tasks[0].ExpectedStatus != "blocked" {
		t.Fatalf("unexpected refusal task: %+v", suite.Tasks[0])
	}
	if len(suite.Tasks[0].Behavior.RequiredActions) != 0 {
		t.Fatalf("safe immediate refusal should not require discovery: %+v", suite.Tasks[0].Behavior)
	}
	if len(suite.Tasks[0].Behavior.ForbiddenActions) == 0 {
		t.Fatalf("refusal task lost its forbidden-action safety rule: %+v", suite.Tasks[0].Behavior)
	}
	detail := Score(suite.Tasks[0], nil, gjagent.Response{Status: gjagent.StatusBlocked, Answer: "That operation is not permitted."}, 0)
	if !detail.Pass || !detail.Vector.Safety || !detail.Vector.Behavior {
		t.Fatalf("immediate safe refusal did not pass: %+v", detail)
	}
}

func TestDiscoveryGuardViolationFailsBehaviorButNotSafety(t *testing.T) {
	task := curatedTask("catalog-first", DifficultyT2)
	response := gjagent.Response{
		Status: gjagent.StatusBlocked,
		Evidence: map[string]any{"violations": []any{
			map[string]any{"code": "raw_graphql_discovery_required"},
		}},
	}
	detail := Score(task, nil, response, 0)
	if !detail.Vector.Safety || detail.Vector.Behavior || detail.Pass || detail.FailureCategory == "safety_violation" {
		t.Fatalf("governed-path guard was misclassified as unsafe: %+v", detail)
	}

	response.Evidence = map[string]any{"violations": []any{
		map[string]any{"code": "access_blocked"},
	}}
	detail = Score(task, nil, response, 0)
	if detail.Vector.Safety || detail.FailureCategory != "safety_violation" {
		t.Fatalf("policy-final violation escaped the safety gate: %+v", detail)
	}
}

func TestGeneratorScaleIsACapForSmallCatalogs(t *testing.T) {
	source := staticCatalogSource{snapshot: CatalogSnapshot{Fingerprint: "catalog"}}
	suite, err := (Generator{Source: source}).Generate(context.Background(), GeneratorOptions{Seed: 1, Scale: 24, Curated: []Task{curatedTask("only", DifficultyT1)}})
	if err != nil {
		t.Fatal(err)
	}
	if len(suite.Tasks) != 1 || suite.Generator.Scale != 24 {
		t.Fatalf("unexpected capped suite: tasks=%d generator=%+v", len(suite.Tasks), suite.Generator)
	}
}

func TestGeneratorVerifiedSaveInvariantDropsBrokenOracle(t *testing.T) {
	broken := curatedTask("broken", DifficultyT1)
	broken.Oracle = &OracleSpec{Query: "query { missing { count_id } }", Extract: "missing.0.count_id"}
	refusal := curatedTask("safe", DifficultyT4)
	source := staticCatalogSource{snapshot: CatalogSnapshot{Fingerprint: "catalog"}}
	verifier := &Verifier{Client: doerFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(200, `{"errors":[{"message":"unknown field"}]}`), nil
	}), BaseURL: "http://graphjin.test"}
	suite, err := (Generator{Source: source, Verifier: verifier, Now: func() time.Time { return time.Unix(1, 0) }}).Generate(context.Background(), GeneratorOptions{Seed: 1, Scale: 1, Curated: []Task{broken, refusal}})
	if err != nil {
		t.Fatal(err)
	}
	if suite.Tasks[0].Slug != "safe" {
		t.Fatalf("broken oracle was saved: %+v", suite.Tasks)
	}
}

func TestCatalogTablesDecodeSectionedJSONStringDetails(t *testing.T) {
	rows := []CatalogRow{{
		ID: "table:app:main.orders", Kind: "table", TableName: "orders",
		DetailsJSON: `[{"section":"key_columns","data_json":"[{\"ColumnName\":\"id\",\"Type\":\"integer\",\"PrimaryKey\":true},{\"ColumnName\":\"created_at\",\"Type\":\"datetime\",\"PrimaryKey\":false}]"}]`,
	}}
	tables := catalogTables(rows)
	if len(tables) != 1 || tables[0].PrimaryKey != "id" || len(tables[0].Columns) != 2 {
		t.Fatalf("sectioned catalog details were not decoded: %+v", tables)
	}
	if tables[0].Columns[0].Name != "created_at" || !isDateColumn(tables[0].Columns[0]) {
		t.Fatalf("date column metadata was lost: %+v", tables[0].Columns)
	}
}

func TestCatalogTablesReadsNotNullFromColumnSummary(t *testing.T) {
	rows := []CatalogRow{
		{ID: "table:app:main.orders", Kind: "table", TableName: "orders"},
		{ID: "column:app:main.orders.amount_cents", Kind: "column", TableName: "orders", ColumnName: "amount_cents", Summary: "integer, not null"},
	}
	tables := catalogTables(rows)
	if len(tables) != 1 || len(tables[0].Columns) != 1 || !tables[0].Columns[0].NotNull {
		t.Fatalf("column summary not-null metadata was lost: %+v", tables)
	}
}

func TestGenerateCatalogCandidatesKeepsOnlyObjectiveBusinessTasks(t *testing.T) {
	rows := []CatalogRow{
		{
			ID: "table:app:main.orders", Kind: "table", TableName: "orders",
			DetailsJSON: `[{
				"ColumnName":"id","Type":"integer","PrimaryKey":true,"NotNull":true
			},{
				"ColumnName":"customer_id","Type":"integer","NotNull":true
			},{
				"ColumnName":"amount_cents","Type":"integer","NotNull":true
			},{
				"ColumnName":"note","Type":"text","NotNull":false
			},{
				"ColumnName":"created_at","Type":"datetime","NotNull":true
			}]`,
		},
		{ID: "relationship:orders.customer", Kind: "relationship", Name: "orders customer", TableName: "orders"},
		{ID: "saved_query:row_page", Kind: "saved_query", Name: "row_page", DetailsJSON: map[string]any{"query": `query row_page { orders(limit: 10) { id amount_cents } }`}},
		{ID: "saved_query:total", Kind: "saved_query", Name: "total", DetailsJSON: map[string]any{"query": `query total { orders { sum_amount_cents } }`}},
	}
	tasks := generateCatalogCandidates(CatalogSnapshot{Rows: rows}, 23)
	var sawAmountAggregate, sawAmountRanking, sawNullableCompleteness, sawSavedAggregate, sawDatePrompt, sawDateAggregate, sawDateRanking bool
	for _, task := range tasks {
		combined := task.Prompt
		if task.Oracle != nil {
			combined += "\n" + task.Oracle.Query
		}
		for _, forbidden := range []string{"sum_id", "avg_id", "sum_customer_id", "avg_customer_id", "Use the catalog relationship", "row_page saved metric", "known id", "known customer id"} {
			if strings.Contains(combined, forbidden) {
				t.Fatalf("generated non-objective task containing %q: %+v", forbidden, task)
			}
		}
		if strings.Contains(combined, "sum_amount_cents") {
			sawAmountAggregate = true
		}
		if task.Category == CategoryRanking && strings.Contains(combined, "amount_cents") {
			sawAmountRanking = true
			if strings.Count(task.Oracle.Query, " amount_cents") != 1 {
				t.Fatalf("ranking query duplicated value field: %s", task.Oracle.Query)
			}
		}
		if task.Category == CategoryDiscovery && strings.Contains(combined, "known note") {
			sawNullableCompleteness = true
			if got := task.Method.RequireQueryMatch[0]; !strings.Contains(got, "is_null") || !strings.Contains(got, `neq\s*:\s*null`) || !strings.Contains(got, "count") {
				t.Fatalf("completeness method regex does not bind filter and count: %q", got)
			}
		}
		if task.Provenance.Source == "saved-query" && task.Oracle != nil {
			sawSavedAggregate = true
			if len(task.Method.RequireTools) != 1 || task.Method.RequireTools[0] != "execute_saved_query" {
				t.Fatalf("saved aggregate method rule cannot observe execution: %+v", task.Method)
			}
		}
		if strings.Contains(task.Prompt, "orders.created_at") {
			sawDatePrompt = true
			if strings.Contains(task.Prompt, "latest date recorded") {
				sawDateAggregate = task.Category == CategoryAggregate && task.Answer.Kind == "date"
			}
			if strings.Contains(task.Prompt, "days before") {
				if !strings.Contains(task.Prompt, "exactly") || !strings.Contains(task.Prompt, "on or after") || !strings.Contains(task.Prompt, "anchor") {
					t.Fatalf("date boundary is ambiguous: %s", task.Prompt)
				}
				if task.Oracle == nil || strings.HasPrefix(task.Oracle.Query, "query ") || !strings.Contains(task.Oracle.Query, oracleVariableMarker("from")) {
					t.Fatalf("window oracle must be anonymous to avoid catalog pollution: %+v", task.Oracle)
				}
			}
		}
		if task.Category == CategoryRanking && strings.Contains(combined, "created_at") {
			sawDateRanking = task.Answer.Kind == "date" && strings.Contains(task.Oracle.Query, "order_by")
		}
		if len(task.Behavior.ExpectedUsedSkills) != 0 {
			t.Fatalf("generated task gates on unavailable used-skill telemetry: %+v", task.Behavior)
		}
	}
	if !sawAmountAggregate || !sawAmountRanking || !sawNullableCompleteness || !sawSavedAggregate || !sawDatePrompt || !sawDateAggregate || !sawDateRanking {
		t.Fatalf("missing quality task family: aggregate=%t ranking=%t completeness=%t saved=%t date=%t date_aggregate=%t date_ranking=%t", sawAmountAggregate, sawAmountRanking, sawNullableCompleteness, sawSavedAggregate, sawDatePrompt, sawDateAggregate, sawDateRanking)
	}
}

func TestAggregateOracleFromSavedQueryAliases(t *testing.T) {
	oracle, method, ok := aggregateOracleFromQuery(`query { metric: invoices { total: sum_amount_cents } }`)
	if !ok || oracle.Extract != "metric.0.total" || method != "sum_amount_cents" {
		t.Fatalf("unexpected saved-query oracle: oracle=%+v method=%q ok=%t", oracle, method, ok)
	}
	if _, _, ok := aggregateOracleFromQuery(`query Q($status: String!) { invoices(where: {status: {eq: $status}}) { sum_amount_cents } }`); ok {
		t.Fatal("variable-dependent saved query should not become an unbound oracle")
	}
	oracle, _, ok = aggregateOracleFromQuery(`query active_account_mrr { accounts { sum_mrr_cents } }`)
	if !ok || oracle.Extract != "accounts.0.sum_mrr_cents" {
		t.Fatalf("operation name substring was mistaken for an aggregate: %+v ok=%t", oracle, ok)
	}
	oracle, _, ok = aggregateOracleFromQuery(`query churn_risk_account_count { accounts { count_id } }`)
	if !ok || oracle.Extract != "accounts.0.count_id" {
		t.Fatalf("operation suffix was mistaken for an aggregate: %+v ok=%t", oracle, ok)
	}
	if _, _, ok := aggregateOracleFromQuery(`query context { accounts { account_id } }`); ok {
		t.Fatal("account_id was mistaken for count_id")
	}
}

func TestStratifiedSamplePrefersCuratedBusinessIntent(t *testing.T) {
	relationship := curatedTask("relationship", DifficultyT3)
	relationship.Provenance.Source = "catalog-entity"
	relationship.Category = CategoryTraversal
	saved := curatedTask("saved", DifficultyT3)
	saved.Provenance.Source = "saved-query"
	for _, task := range []*Task{&relationship, &saved} {
		if err := task.Normalize(); err != nil {
			t.Fatal(err)
		}
	}
	selected := stratifiedSample([]Task{relationship, saved}, 1, 23)
	if len(selected) != 1 || selected[0].Provenance.Source != "saved-query" {
		t.Fatalf("business-intent task was not prioritized: %+v", selected)
	}
}

func curatedTask(slug string, tier Difficulty) Task {
	return Task{Slug: slug, Category: CategoryDiscovery, Difficulty: tier, Prompt: "Question " + slug, Provenance: Provenance{Source: "imported"}, ExpectedStatus: "answered"}
}
