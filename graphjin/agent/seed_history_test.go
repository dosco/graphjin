package agent

import (
	"context"
	"strings"
	"testing"
)

// The confirmation family scored 0 of 6 across two benchmark runs for one
// reason: the seed searched the catalog with "Yes, go ahead and set that up."
// and got generic cards, after which every episode invented a table that does
// not exist and looped until its step budget died. What the run is about sits
// one turn back, in the proposal being answered.
func TestSeedSearchWidensOnlyForContentlessTurns(t *testing.T) {
	history := []Turn{
		{Role: "user", Content: "Finance keeps missing invoices that fail to collect, and wants to stop finding out late."},
		{Role: "assistant", Content: "I can set up a standing watch named finance_failed_invoices over invoices filtered to failed, delivering an inbox digest every hour. Shall I create it?"},
	}
	widened := seedSearchText("Yes, go ahead and set that up.", history)
	for _, want := range []string{"invoices", "failed", "watch"} {
		if !strings.Contains(widened, want) {
			t.Fatalf("the widened seed must carry the conversation's subject %q: %q", want, widened)
		}
	}
	if !strings.Contains(widened, "Yes, go ahead") {
		t.Fatalf("the widened seed should retain the caller's own turn: %q", widened)
	}

	// Anything that names something of its own is untouched — the widening is
	// a fallback for turns with no subject, never a rewrite of real requests.
	for _, instruction := range []string{
		"How many failed invoices does account 1 have?",
		"Show me the totals",
		"Run that again",
		"Close ticket 2 and record a note",
	} {
		if got := seedSearchText(instruction, history); got != instruction {
			t.Fatalf("instruction %q must seed on itself, got %q", instruction, got)
		}
	}

	// With no usable history there is nothing better to search for.
	if got := seedSearchText("Yes, go ahead.", nil); got != "Yes, go ahead." {
		t.Fatalf("a contentless turn with no history keeps its own text: %q", got)
	}
	if got := seedSearchText("Yes.", []Turn{{Role: "user", Content: "ok"}}); got != "Yes." {
		t.Fatalf("history that is itself contentless must not be seeded from: %q", got)
	}
}

func TestInstructionCarriesNoSubject(t *testing.T) {
	for _, contentless := range []string{
		"Yes, go ahead and set that up.", "yes", "Sure, do it.", "Okay, proceed.",
		"Sounds good — please continue.", "Yes please, create that one.",
	} {
		if !instructionCarriesNoSubject(contentless) {
			t.Fatalf("%q should be recognised as naming nothing", contentless)
		}
	}
	for _, substantive := range []string{
		"Yes, go ahead and set up the invoice watch.",
		"Do it for account 5",
		"Close it off and record a note saying what resolved it",
		"What severity is that ticket?",
		"",
	} {
		if instructionCarriesNoSubject(substantive) {
			t.Fatalf("%q names something and must be left alone", substantive)
		}
	}
}

// The seed must actually run on the widened text, since the seed's forty cards
// shape every later decision in the run.
func TestSeedRunsOnTheWidenedSearch(t *testing.T) {
	base := &fakeRuntime{}
	runtime := newProtocolRuntime(base, "Yes, go ahead and set that up.", "", 8, nil, nil, CatalogSearchFeatures{})
	runtime.state.history = []Turn{
		{Role: "assistant", Content: "I can set up a standing watch named finance_failed_invoices over invoices filtered to failed."},
	}
	if _, err := runtime.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	seedArgs := runtime.state.actions[0].Args
	search := stringArg(seedArgs, "search")
	if !strings.Contains(search, "invoices filtered to failed") {
		t.Fatalf("seed search = %q, want the retained proposal", search)
	}
	// The instruction itself is untouched: every other guard reads it to decide
	// what the caller asked for.
	if runtime.state.instruction != "Yes, go ahead and set that up." {
		t.Fatalf("the instruction must not be rewritten: %q", runtime.state.instruction)
	}
}
