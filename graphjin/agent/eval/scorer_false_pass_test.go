package eval

import (
	"strings"
	"testing"

	gjagent "github.com/dosco/graphjin/agent/v3"
)

// A timestamp is not a quantity the model reported. Reading its digits invented
// candidates — "2026-08-25T00:00:00Z" yielded 2026, -8, -25 and three zeros —
// so an oracle expecting 0 matched any answer carrying a timestamp, whatever
// number the model actually gave. That is a false pass: it certifies an answer
// nobody checked.
func TestZeroOracleIsNotSatisfiedByATimestamp(t *testing.T) {
	task := Task{ExpectedStatus: "answered", Answer: AnswerRule{Kind: "number"}}
	oracle := OracleResult{Value: "0"}
	resp := gjagent.Response{
		Status: "answered",
		Answer: "There are 10 support tickets as of 2026-08-25T00:00:00Z.",
	}
	ok, why := evaluateGroundTruth(task, oracle, resp)
	if ok {
		t.Fatal("an answer reporting 10 must not pass an oracle of 0 via a timestamp")
	}
	if !strings.Contains(why, "candidate") {
		t.Fatalf("unexpected reason: %s", why)
	}
}

// The number the model did report still has to be read.
func TestNumbersOutsideTimestampsStillCount(t *testing.T) {
	task := Task{ExpectedStatus: "answered", Answer: AnswerRule{Kind: "number"}}
	for _, tc := range []struct {
		oracle, answer string
	}{
		{"10", "There are 10 support tickets as of 2026-08-25T00:00:00Z."},
		{"0", "There are 0 support tickets as of 2026-08-25T00:00:00Z."},
		{"480000", "MRR is 480000 cents, recorded at 09:00:00."},
		{"3", "There are 3 accounts, checked 2026-01-15."},
	} {
		ok, why := evaluateGroundTruth(task, OracleResult{Value: tc.oracle}, gjagent.Response{Status: "answered", Answer: tc.answer})
		if !ok {
			t.Fatalf("oracle %s should match %q: %s", tc.oracle, tc.answer, why)
		}
	}
}

// A task that supplies its own extract regex has said exactly what to read,
// including from a date, so filtering must not apply to it.
func TestCustomExtractRegexStillSeesDates(t *testing.T) {
	task := Task{ExpectedStatus: "answered", Answer: AnswerRule{Kind: "number", ExtractRegex: `\d{4}`}}
	ok, why := evaluateGroundTruth(task, OracleResult{Value: "2026"}, gjagent.Response{
		Status: "answered", Answer: "the window ends 2026-08-25",
	})
	if !ok {
		t.Fatalf("a custom extractor must still read the year it asked for: %s", why)
	}
}

// tolerance_pct is a percentage. Applied as a raw fraction, 5 meant a
// five-hundred percent window that accepted almost any number.
func TestTolerancePctIsAPercentage(t *testing.T) {
	task := Task{ExpectedStatus: "answered", Answer: AnswerRule{Kind: "number", TolerancePct: 5}}
	// 105 is within 5% of 100.
	if ok, why := evaluateGroundTruth(task, OracleResult{Value: "100"}, gjagent.Response{Status: "answered", Answer: "the total is 105"}); !ok {
		t.Fatalf("105 is within 5%% of 100: %s", why)
	}
	// 500 is not, and used to pass.
	if ok, _ := evaluateGroundTruth(task, OracleResult{Value: "100"}, gjagent.Response{Status: "answered", Answer: "the total is 500"}); ok {
		t.Fatal("500 must not pass a 5% tolerance around 100")
	}
}

// A tolerance over 100% is a unit mistake, caught at load rather than trusted.
// Asserted on the tolerance rule alone: a task fixture must satisfy every other
// rule too, and those are not what this guards.
func TestTaskRejectsOutOfRangeTolerance(t *testing.T) {
	withTolerance := func(pct float64) string {
		task := Task{
			SchemaVersion: TaskSchemaVersion,
			Slug:          "t", Category: CategoryAggregate, Difficulty: DifficultyT1,
			Prompt: "how many?", ExpectedStatus: "answered",
			Answer:     AnswerRule{Kind: "number", TolerancePct: pct},
			Provenance: Provenance{Source: "catalog-entity", GeneratorVersion: GeneratorVersion},
		}
		if err := task.Validate(); err != nil {
			return err.Error()
		}
		return ""
	}
	if got := withTolerance(500); !strings.Contains(got, "tolerance_pct") {
		t.Fatalf("a 500%% tolerance must be rejected as a unit mistake, got: %s", got)
	}
	if got := withTolerance(-1); !strings.Contains(got, "tolerance_pct") {
		t.Fatalf("a negative tolerance must be rejected, got: %s", got)
	}
	if got := withTolerance(5); strings.Contains(got, "tolerance_pct") {
		t.Fatalf("5%% is a legitimate tolerance: %s", got)
	}
}
