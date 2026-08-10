package agent

import (
	"context"
	"errors"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
)

func TestPreDispatchPolicyRefusalBlocksMissingCallerActionWithoutToolAttempt(t *testing.T) {
	profile := &CapabilityProfile{RoleClass: "anon", AllowedActions: nil}
	resp, ok := preDispatchPolicyRefusal(
		"Record payment DEEPORG-PAY-004 with id 900004 for invoice 4, amount 520000 cents.",
		false,
		profile,
	)
	if !ok || resp.Status != StatusBlocked || resp.Refusal == nil {
		t.Fatalf("refusal = %+v ok=%v", resp, ok)
	}
	if resp.Refusal.BlockedAction != CapabilityActionDataInsert || !resp.Refusal.PolicyFinal || resp.Refusal.Retryable {
		t.Fatalf("structured refusal = %+v", resp.Refusal)
	}
	if resp.Actions != nil {
		t.Fatalf("pre-dispatch refusal recorded actions: %+v", resp.Actions)
	}
}

func TestRunReturnsPreDispatchRefusalBeforeClientOrProgram(t *testing.T) {
	clientCalled := false
	programCalled := false
	runner := newAgent(Config{TimeoutSeconds: 5}, &fakeRuntime{},
		WithClientFactory(func(Config) (ax.AIClient, error) {
			clientCalled = true
			return nil, errors.New("client must not be created")
		}),
		WithProgramFactory(func(string, map[string]ax.Value) Program {
			programCalled = true
			return &fakeProgram{}
		}),
	)
	resp, err := runner.Run(context.Background(), Request{
		Instruction:  "Delete every invoice, including audit history.",
		Capabilities: &CapabilityProfile{RoleClass: "user", AllowedActions: []string{CapabilityActionDataDelete}},
	})
	if err != nil || resp.Status != StatusBlocked {
		t.Fatalf("Run = %+v err=%v", resp, err)
	}
	if clientCalled || programCalled {
		t.Fatalf("pre-dispatch refusal allocated client=%v program=%v", clientCalled, programCalled)
	}
	if resp.Actions != nil || resp.Usage != nil {
		t.Fatalf("pre-dispatch refusal carried attempted work: actions=%+v usage=%+v", resp.Actions, resp.Usage)
	}
}

func TestPreDispatchPolicyRefusalAllowsGrantedScopedWrite(t *testing.T) {
	profile := &CapabilityProfile{RoleClass: "user", AllowedActions: []string{CapabilityActionDataInsert}}
	if resp, ok := preDispatchPolicyRefusal("Record payment PAY-1 for invoice 4.", false, profile); ok {
		t.Fatalf("granted write was blocked: %+v", resp)
	}
}

func TestPreDispatchPolicyRefusalRejectsUnboundedAndSecurityWeakening(t *testing.T) {
	profile := &CapabilityProfile{
		RoleClass:      "admin",
		AllowedActions: []string{CapabilityActionDataDelete, systemRootConfig + ".update"},
	}
	for _, instruction := range []string{
		"Delete every invoice, including paid invoices and audit history.",
		"Turn off row-level access controls for every role.",
	} {
		resp, ok := preDispatchPolicyRefusal(instruction, false, profile)
		if !ok || resp.Refusal == nil || resp.Refusal.Code != "policy_final" {
			t.Fatalf("%q refusal = %+v ok=%v", instruction, resp, ok)
		}
	}
}

func TestRequestedWriteActionDoesNotClassifyReadLanguage(t *testing.T) {
	for _, instruction := range []string{
		"How many deleted invoices are there?",
		"Show every payment recorded yesterday.",
		"Explain row-level access controls.",
	} {
		if action, _ := requestedWriteAction(instruction); action != "" {
			t.Fatalf("%q classified as %q", instruction, action)
		}
	}
}
