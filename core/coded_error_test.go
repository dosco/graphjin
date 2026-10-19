package core

import (
	"fmt"
	"testing"
)

func TestNewErrorPutsCodeInExtensions(t *testing.T) {
	err := fmt.Errorf("resolve: %w", NewCodedError("keystore_key_required", "secrets.keystore.key must be set"))
	got := newError("query { x }", err)
	if len(got) != 1 {
		t.Fatalf("got %d errors", len(got))
	}
	if got[0].Message != "resolve: secrets.keystore.key must be set" {
		t.Fatalf("message = %q", got[0].Message)
	}
	if code := got[0].Extensions["code"]; code != "keystore_key_required" {
		t.Fatalf("extensions.code = %v", code)
	}

	plain := newError("query { x }", fmt.Errorf("boom"))
	if _, ok := plain[0].Extensions["code"]; ok {
		t.Fatal("an error without a code has extensions.code")
	}
}
