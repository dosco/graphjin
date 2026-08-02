package eval

import (
	"context"
	"net/http"
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

func TestAggregateOracleFromSavedQueryAliases(t *testing.T) {
	oracle, method, ok := aggregateOracleFromQuery(`query { metric: invoices { total: sum_amount_cents } }`)
	if !ok || oracle.Extract != "metric.0.total" || method != "sum_amount_cents" {
		t.Fatalf("unexpected saved-query oracle: oracle=%+v method=%q ok=%t", oracle, method, ok)
	}
	if _, _, ok := aggregateOracleFromQuery(`query Q($status: String!) { invoices(where: {status: {eq: $status}}) { sum_amount_cents } }`); ok {
		t.Fatal("variable-dependent saved query should not become an unbound oracle")
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
