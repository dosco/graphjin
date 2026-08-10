package serv

import "testing"

func TestAnnotateAgentPolicyErrorsMakesGovernedDenialsTerminal(t *testing.T) {
	for _, test := range []struct {
		message string
		code    string
	}{
		{message: "gj_watch write requires user identity", code: "authenticated_required"},
		{message: "gj_watch write denied", code: "access_unauthorized"},
		{message: "unauthorized: table payments is blocked for role user", code: "access_unauthorized"},
		{message: "mutations blocked: database main is read-only", code: "access_blocked"},
	} {
		t.Run(test.code+"_"+test.message, func(t *testing.T) {
			out := annotateAgentPolicyErrors(map[string]any{"errors": []any{map[string]any{"message": test.message}}})
			value, ok := out.(map[string]any)
			if !ok {
				t.Fatalf("annotated result type = %T", out)
			}
			errors, _ := value["errors"].([]any)
			entry, _ := errors[0].(map[string]any)
			extensions, _ := entry["extensions"].(map[string]any)
			if extensions["code"] != test.code || extensions["policy_final"] != true || extensions["retryable"] != false {
				t.Fatalf("extensions = %+v", extensions)
			}
		})
	}
}

func TestAnnotateAgentPolicyErrorsDoesNotClassifyDomainDenial(t *testing.T) {
	original := map[string]any{"errors": []any{map[string]any{"message": "payment denied by issuing bank"}}}
	out := annotateAgentPolicyErrors(original)
	value, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("non-policy result type = %T", out)
	}
	errors, _ := value["errors"].([]any)
	entry, _ := errors[0].(map[string]any)
	if _, exists := entry["extensions"]; exists {
		t.Fatalf("non-policy result was annotated: %+v", out)
	}
}
