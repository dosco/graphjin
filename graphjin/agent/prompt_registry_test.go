package agent

import "testing"

func TestPromptRegistryHashStable(t *testing.T) {
	first := PromptRegistryHash()
	second := PromptRegistryHash()
	if first != second {
		t.Fatalf("prompt registry hash changed between calls: %q != %q", first, second)
	}
	if len(first) != 64 {
		t.Fatalf("prompt registry hash length = %d, want 64", len(first))
	}
}
