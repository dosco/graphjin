package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
)

// Conversation state belongs to the lead. A worker handed history, memories or
// a profile anyway runs exactly the program it would have run without them.
func TestWorkerDropsConversationState(t *testing.T) {
	run := func(req Request) *fakeProgram {
		t.Helper()
		program := &fakeProgram{output: map[string]ax.Value{"status": StatusAnswered, "answer": "done"}}
		runner := newAgent(Config{TimeoutSeconds: 5}, &fakeRuntime{},
			WithClientFactory(func(Config) (ax.AIClient, error) { return fakeClient{}, nil }),
			WithProgramFactory(func(_ string, options map[string]ax.Value) Program {
				program.options = options
				program.onForward = func(p *fakeProgram) {
					callProgramTool(t, p, "query_catalog", map[string]ax.Value{"id": "help:discovery"})
				}
				return program
			}),
		)
		if _, err := runner.Run(context.Background(), req); err != nil {
			t.Fatalf("Run: %v", err)
		}
		return program
	}

	plain := run(Request{Instruction: "show products"})
	stateful := run(Request{
		Instruction: "show products",
		History:     []Turn{{Role: "user", Content: "Harbor Cafe orders"}, {Role: "assistant", Content: "Harbor Cafe has 3 orders"}},
		Memories:    []string{"Always answer in metric units"},
		Profile:     &ProfileInstructions{Name: "Ops", Instructions: "Focus on late shipments."},
	})
	if fmt.Sprint(normalizeValue(plain.options["instructionAddenda"])) != fmt.Sprint(normalizeValue(stateful.options["instructionAddenda"])) {
		t.Fatal("conversation state changed the worker's instruction addenda")
	}
	seen := fmt.Sprint(normalizeValue(stateful.options), normalizeValue(stateful.forwardValues))
	for _, leaked := range []string{"Harbor Cafe", "metric units", "late shipments"} {
		if strings.Contains(seen, leaked) {
			t.Fatalf("worker program received conversation state %q", leaked)
		}
	}
}
