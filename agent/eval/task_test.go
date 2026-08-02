package eval

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTaskContentIDStableAndSensitive(t *testing.T) {
	task := Task{
		Slug: "Revenue total", Category: CategoryAggregate, Difficulty: DifficultyT1,
		Prompt: "What is total revenue?", Provenance: Provenance{Source: "user-added"},
		ExpectedStatus: "answered", Oracle: &OracleSpec{Query: "query { invoices { sum_amount } }", Extract: "invoices.0.sum_amount"},
		Answer: AnswerRule{Kind: "number"},
	}
	if err := task.Normalize(); err != nil {
		t.Fatal(err)
	}
	first := task.ID
	copyTask := task
	copyTask.ID = "stale"
	if err := copyTask.Normalize(); err != nil {
		t.Fatal(err)
	}
	if copyTask.ID != first {
		t.Fatalf("content ID changed: %s != %s", copyTask.ID, first)
	}
	copyTask.Slug = "renamed-for-display"
	copyTask.Provenance = Provenance{GeneratorVersion: "graphjin.eval.generator/v99", Source: "catalog-entity", Seed: 999, SourceID: "different-source"}
	if err := copyTask.Normalize(); err != nil {
		t.Fatal(err)
	}
	if copyTask.ID != first {
		t.Fatalf("content ID changed with display/provenance metadata: %s != %s", copyTask.ID, first)
	}
	copyTask.Prompt = "What is net revenue?"
	if err := copyTask.Normalize(); err != nil {
		t.Fatal(err)
	}
	if copyTask.ID == first {
		t.Fatal("content ID did not change with prompt")
	}
	oracleTask := task
	oracleTask.Oracle = &OracleSpec{Query: "query { invoices { sum_net_amount } }", Extract: "invoices.0.sum_net_amount"}
	if err := oracleTask.Normalize(); err != nil {
		t.Fatal(err)
	}
	if oracleTask.ID == first {
		t.Fatal("content ID did not change with oracle")
	}
}

func TestOracleRejectsMutation(t *testing.T) {
	oracle := OracleSpec{Query: "mutation { users(delete: true) { id } }", Extract: "users.0.id"}
	if err := oracle.Validate(); err == nil {
		t.Fatal("expected mutation oracle rejection")
	}
}

func TestOracleRejectsMixedQueryAndMutationDocument(t *testing.T) {
	oracle := OracleSpec{Query: "query Read { users { id } } mutation Write { users(delete: true) { id } }", Extract: "users.0.id"}
	if err := oracle.Validate(); err == nil {
		t.Fatal("expected mixed query/mutation oracle rejection")
	}
}

func TestOracleRejectsCommentOnlyQueryWithoutPanicking(t *testing.T) {
	oracle := OracleSpec{Query: "# no operation", Extract: "users.0.id"}
	if err := oracle.Validate(); err == nil {
		t.Fatal("expected comment-only oracle rejection")
	}
}

func TestSuiteRoundTripDeterministic(t *testing.T) {
	suite := Suite{
		Name: "test", CreatedAt: time.Unix(10, 0).UTC(), CatalogFingerprint: "catalog",
		Generator: GeneratorMeta{Version: GeneratorVersion, Seed: 23, Scale: 1},
		Tasks: []Task{{
			Slug: "count users", Category: CategoryAggregate, Difficulty: DifficultyT1,
			Prompt: "How many users?", Provenance: Provenance{Source: "catalog-entity"}, ExpectedStatus: "answered",
			Oracle: &OracleSpec{Query: "query { users { count_id } }", Extract: "users.0.count_id"}, Answer: AnswerRule{Kind: "number"},
		}},
	}
	if err := suite.Normalize(); err != nil {
		t.Fatal(err)
	}
	first, err := MarshalSuite(suite)
	if err != nil {
		t.Fatal(err)
	}
	second, err := MarshalSuite(suite)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("suite marshal is not deterministic")
	}
	path := filepath.Join(t.TempDir(), "eval", "suite.yml")
	if err := SaveSuite(path, suite); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSuite(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Tasks[0].ID != suite.Tasks[0].ID {
		t.Fatalf("round trip ID = %s, want %s", loaded.Tasks[0].ID, suite.Tasks[0].ID)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("suite mode = %o, want 600", info.Mode().Perm())
	}
}
