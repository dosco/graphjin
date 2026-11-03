package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
)

func TestProfileInstructionsOnlyWhenSet(t *testing.T) {
	if got := profileInstructions(nil); got != "" {
		t.Fatalf("no profile must add no instructions, got %q", got)
	}
	if got := profileInstructions(&ProfileInstructions{Name: "Finance", Instructions: "  "}); got != "" {
		t.Fatalf("blank instructions must add no instructions, got %q", got)
	}
	got := profileInstructions(&ProfileInstructions{Name: "Finance\nbot", Instructions: "Answer in EUR."})
	if !strings.Contains(got, "for Finance bot:") || !strings.HasSuffix(got, "\nAnswer in EUR.") || !strings.Contains(got, "grant no access") {
		t.Fatalf("profile addendum = %q", got)
	}
}

// The profile reaches the executor's instruction addenda, and a run without
// one keeps exactly the default addenda.
func TestRunAddsProfileInstructionsToExecutorAddenda(t *testing.T) {
	addenda := func(req Request) []any {
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
		out, _ := normalizeValue(program.options["instructionAddenda"]).([]any)
		return out
	}

	plain := addenda(Request{Instruction: "show products"})
	withProfile := addenda(Request{Instruction: "show products", Profile: &ProfileInstructions{Name: "Ops", Instructions: "Focus on late shipments."}})
	if len(withProfile) != len(plain)+1 {
		t.Fatalf("profile addenda = %d entries, want %d", len(withProfile), len(plain)+1)
	}
	for i := range plain {
		if fmt.Sprint(plain[i]) != fmt.Sprint(withProfile[i]) {
			t.Fatalf("addendum %d changed with a profile", i)
		}
	}
	if last := fmt.Sprint(withProfile[len(withProfile)-1]); !strings.Contains(last, "Focus on late shipments.") {
		t.Fatalf("profile addendum missing: %q", last)
	}
	if strings.Contains(fmt.Sprint(plain), "Agent profile instructions") {
		t.Fatalf("run without a profile carries profile instructions: %v", plain)
	}
}
