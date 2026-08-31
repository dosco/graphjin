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

// A phrase that begins with a number needs the same boundary a bare number
// gets. The frozen service-level tasks state "4 hours" and "24 hours", and
// "24 hours" contains "4 hours" — so an answer giving the wrong service level
// scored as though it had given the right one. The cheater battery found it.
func TestNumericPhraseIsNotSatisfiedByALargerNumber(t *testing.T) {
	for _, answer := range []string{
		"there are 6 open urgent tickets, and the standard is 24 hours.",
		"we must respond within 124 hours of the report.",
	} {
		if mentionsAnyIdentifier(answer, []string{"4 hours"}) {
			t.Fatalf("a larger number must not satisfy a smaller one: %s", answer)
		}
	}
	// It removes false passes only: every answer that genuinely states the
	// requirement still counts, however it is phrased around it.
	for _, answer := range []string{
		"the standard is 4 hours.",
		"we are required to respond within 4 hours of the report.",
		"response time: 4 hours (urgent).",
		"4 hours is the requirement, and 6 tickets are open.",
	} {
		if !mentionsAnyIdentifier(answer, []string{"4 hours"}) {
			t.Fatalf("a correct answer was rejected: %s", answer)
		}
	}
	// A phrase that does not start with a number keeps plain substring
	// semantics, so nothing else about matching moves.
	if !mentionsAnyIdentifier("the health colour is green today", []string{"green"}) {
		t.Fatal("a word dimension must still match as a substring")
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

// Known and deliberate limitation, pinned so a future suite regeneration does
// not ship it unnoticed.
//
// A low-cardinality primary key is weak evidence that the answer named the row:
// an answer identifying a different record can contain the correct row's id
// incidentally. Tightening this — requiring an id-cue word, or the id in the
// structured data — would reject "the account with the earliest last active at
// is **8** with a value of ...", which is a correct answer to "which record"
// and is exactly what this change exists to accept. The value check still has
// to pass independently, so an incidental id alone cannot carry a wrong answer.
//
// If a generator revision starts projecting primary keys as alternates, revisit
// this before freezing a suite: prefer a name-like column as the dimension
// whenever the table has one.
func TestNumericIdentifierIsWeakEvidenceByDesign(t *testing.T) {
	answer := "the account with the highest mrr is acme (id: 5), with 1 subscription and 480000 cents"
	if !mentionsNumericIdentifier(answer, "1") {
		t.Fatal("behaviour changed: an incidental id no longer satisfies the identifier check")
	}
	// The tokenizer must still refuse digits that are part of another value,
	// which is the failure mode that would be genuinely unsafe.
	for _, hay := range []string{"row 10 is the answer", "value is 1.5", "on 2026-01-15", "480000 cents"} {
		if mentionsNumericIdentifier(hay, "1") {
			t.Fatalf("a digit inside another value must never count: %s", hay)
		}
	}
}
