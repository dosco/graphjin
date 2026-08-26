package eval

import (
	"testing"

	gjagent "github.com/dosco/graphjin/agent/v3"
)

// The recorded case: three attempts at "Which record in accounts has the
// earliest last active at, and what is the value?" all returned the right row
// and the right date. Two named the row by its primary key and were scored
// wrong because the oracle only projected the name.
func TestMentionsAnyIdentifierAcceptsEitherIdentifier(t *testing.T) {
	ids := []string{"Tidegate Press", "8"}
	for _, answer := range []string{
		"the account with the earliest last active at is **8** with a value of **2026-07-11t09:00:00z**.",
		"the account with the earliest last active date is account id **8**, with a last active time of **2026-07-11t09:00:00z**.",
		"the account with the earliest last active at is **tidegate press** (id: 8), with a last active time of **2026-07-11t09:00:00z**.",
	} {
		if !mentionsAnyIdentifier(answer, ids) {
			t.Fatalf("answer names the row but was rejected: %s", answer)
		}
	}
}

// Widening which identifiers count must not widen which row counts. An answer
// naming a different row still fails.
func TestMentionsAnyIdentifierRejectsAnotherRow(t *testing.T) {
	if mentionsAnyIdentifier("the account with the earliest last active at is **northwind co** (id: 12).", []string{"Tidegate Press", "8"}) {
		t.Fatal("an answer naming a different row must not pass")
	}
}

// The reason a bare number needs boundaries: "8" appears inside dates, money
// and larger ids, so a substring test would accept an answer that never named
// the row at all.
func TestNumericIdentifierNeedsBoundaries(t *testing.T) {
	for _, answer := range []string{
		"using the latest recorded anchor 2026-08-25, there are 2 records.",
		"account 1 current mrr is 480000 cents.",
		"the earliest last active at is 2026-07-11t09:00:00z for account 18.",
	} {
		if mentionsAnyIdentifier(answer, []string{"8"}) {
			t.Fatalf("a digit inside another token must not count as naming row 8: %s", answer)
		}
	}
	for _, answer := range []string{
		"account 8 has the earliest value",
		"the record is id: 8, last active 2026-07-11",
		"row 8.",
		"(8)",
	} {
		if !mentionsAnyIdentifier(answer, []string{"8"}) {
			t.Fatalf("a genuine mention of row 8 must count: %s", answer)
		}
	}
}

// A name identifier keeps plain substring semantics.
func TestNameIdentifierStillMatchesSubstring(t *testing.T) {
	if !mentionsAnyIdentifier("the account is tidegate press, last active 2026-07-11", []string{"Tidegate Press"}) {
		t.Fatal("a name identifier must still match as a substring")
	}
}

// End-to-end through the scorer, using the recorded episode verbatim. Testing
// mentionsAnyIdentifier alone did not prove evaluateGroundTruth calls it: the
// old single-field check still passed those tests.
func TestGroundTruthAcceptsRowNamedByPrimaryKey(t *testing.T) {
	task := Task{ExpectedStatus: "answered", Answer: AnswerRule{Kind: "date"}}
	oracle := OracleResult{
		Value:               "2026-07-11T09:00:00Z",
		Dimension:           "Tidegate Press",
		DimensionAlternates: []string{"8"},
	}
	resp := gjagent.Response{
		Status: "answered",
		Answer: "The account with the earliest last active date is account ID **8**, with a last active time of **2026-07-11T09:00:00Z**.",
	}
	ok, why := evaluateGroundTruth(task, oracle, resp)
	if !ok {
		t.Fatalf("an answer naming the right row by primary key must pass: %s", why)
	}
}

// The same wiring must still reject a wrong row, so widening the identifiers
// did not widen which row counts.
func TestGroundTruthRejectsWrongRowWithRightValue(t *testing.T) {
	task := Task{ExpectedStatus: "answered", Answer: AnswerRule{Kind: "date"}}
	oracle := OracleResult{
		Value:               "2026-07-11T09:00:00Z",
		Dimension:           "Tidegate Press",
		DimensionAlternates: []string{"8"},
	}
	resp := gjagent.Response{
		Status: "answered",
		Answer: "The account with the earliest last active date is **Northwind Co** (ID: 12), last active **2026-07-11T09:00:00Z**.",
	}
	if ok, _ := evaluateGroundTruth(task, oracle, resp); ok {
		t.Fatal("naming a different row must still fail even with the right value")
	}
}

// An oracle with no alternates keeps its original behaviour.
func TestGroundTruthWithoutAlternatesIsUnchanged(t *testing.T) {
	task := Task{ExpectedStatus: "answered", Answer: AnswerRule{Kind: "date"}}
	oracle := OracleResult{Value: "2026-07-11T09:00:00Z", Dimension: "Tidegate Press"}
	resp := gjagent.Response{Status: "answered", Answer: "Row **8** was last active **2026-07-11T09:00:00Z**."}
	if ok, _ := evaluateGroundTruth(task, oracle, resp); ok {
		t.Fatal("without an alternate identifier the dimension is still required")
	}
}
