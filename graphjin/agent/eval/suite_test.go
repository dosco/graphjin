package eval

import "testing"

// TestTaskStructureKeySeparatesConfirmationFlows pins the dedup gap that collapsed
// two confirmation flows into one. They share an approval prompt and differ only
// in the preceding history and the post-state verified, so a key omitting Turns or
// Mutation silently drops the second.
func TestTaskStructureKeySeparatesConfirmationFlows(t *testing.T) {
	base := Task{
		Category: CategoryMultiTurn, Difficulty: DifficultyT4,
		Prompt: "Yes, go ahead and set that up.",
	}
	invoices := base
	invoices.Turns = []TurnSpec{{Role: "assistant", Content: "watch named finance_failed_invoices over invoices"}}
	invoices.Mutation = &MutationSpec{ExpectedValue: "1", PostState: OracleSpec{Query: `{invoices_cursor}`}}

	tickets := base
	tickets.Turns = []TurnSpec{{Role: "assistant", Content: "watch named support_urgent_tickets over support tickets"}}
	tickets.Mutation = &MutationSpec{ExpectedValue: "1", PostState: OracleSpec{Query: `{support_tickets_cursor}`}}

	if taskStructureKey(invoices) == taskStructureKey(tickets) {
		t.Fatal("confirmation flows differing only in history and post-state must not share a structure key")
	}
	// Genuine duplicates must still collapse.
	if taskStructureKey(invoices) != taskStructureKey(invoices) {
		t.Fatal("structure key is not stable")
	}
}
