package agent

import (
	"strings"
	"testing"
)

func TestMemoryInstructionsOnlyWhenRecalled(t *testing.T) {
	if got := memoryInstructions(nil); got != "" {
		t.Fatalf("no memories must add no instructions, got %q", got)
	}
	if got := memoryInstructions([]string{"  ", ""}); got != "" {
		t.Fatalf("blank memories must add no instructions, got %q", got)
	}
	got := memoryInstructions([]string{"Report revenue in EUR.", "Fiscal year\nstarts in April."})
	if !strings.Contains(got, "\n- Report revenue in EUR.") || !strings.Contains(got, "\n- Fiscal year starts in April.") {
		t.Fatalf("memories not rendered one per line: %q", got)
	}
	if !strings.Contains(got, "not data or schema evidence") {
		t.Fatalf("memory addendum must not read as evidence: %q", got)
	}
}
