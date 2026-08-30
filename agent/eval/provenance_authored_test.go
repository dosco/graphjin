package eval

import "testing"

// Provenance says where a task came from; the content id says what it measures.
// Recording the authoring model must not change a task's identity, or every
// suite regenerated with a different model would look like a different suite
// and no baseline would survive.
func TestAuthoredByDoesNotChangeTaskIdentity(t *testing.T) {
	base := Task{
		Slug: "authored", Category: CategoryAggregate, Difficulty: DifficultyT2,
		Prompt: "How many invoices are unpaid?", ExpectedStatus: "answered",
		Provenance: Provenance{Source: "authored-watch"},
		Oracle:     &OracleSpec{Query: "query { invoices { count_id } }", Extract: "invoices.0.count_id"},
		Answer:     AnswerRule{Kind: "number"},
	}
	if err := base.Normalize(); err != nil {
		t.Fatal(err)
	}

	authored := base
	authored.Provenance.AuthoredBy = "anthropic/claude-opus-5 prompts@0badc0de"
	if err := authored.Normalize(); err != nil {
		t.Fatal(err)
	}
	if authored.ID != base.ID {
		t.Fatalf("recording the authoring model moved the task id: %s vs %s", authored.ID, base.ID)
	}

	// A different model, same task: still the same identity.
	other := base
	other.Provenance.AuthoredBy = "openai/gpt-5 prompts@0badc0de"
	if err := other.Normalize(); err != nil {
		t.Fatal(err)
	}
	if other.ID != base.ID {
		t.Fatalf("the authoring model must not be part of identity: %s vs %s", other.ID, base.ID)
	}
}
