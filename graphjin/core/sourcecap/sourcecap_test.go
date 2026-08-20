package sourcecap

import (
	"strings"
	"testing"
)

func TestRegistryCompleteness(t *testing.T) {
	for _, kind := range Kinds() {
		defs := Definitions(kind)
		if len(defs) == 0 {
			t.Fatalf("kind %q has no capability definitions", kind)
		}
		seen := map[string]bool{}
		for _, def := range defs {
			if def.Kind != kind {
				t.Fatalf("definition kind mismatch: got %q want %q", def.Kind, kind)
			}
			if def.Key == "" || def.Action == "" || def.Severity == "" || def.Enforcement == "" ||
				def.Summary == "" || def.Reason == "" || def.Recommendation == "" {
				t.Fatalf("incomplete capability definition: %+v", def)
			}
			if seen[def.Key] {
				t.Fatalf("duplicate capability key for %s: %s", kind, def.Key)
			}
			seen[def.Key] = true
			if _, ok := Lookup(kind, def.Key); !ok {
				t.Fatalf("lookup failed for %s.%s", kind, def.Key)
			}
			_ = def.Default(ModeDev)
			_ = def.Default(ModeProd)
			_ = def.Default(ModeAgentic)
		}
	}
}

func TestCanonicalKindRejectsOldNames(t *testing.T) {
	tests := map[string]string{
		"sql":        "kind: database",
		"codesql":    "kind: code",
		"filesystem": "kind: file",
		"openapi":    "kind: api",
		"graphjin":   "top-level system",
		"workflow":   "top-level workflows",
		"workflows":  "top-level workflows",
	}
	for old, want := range tests {
		if _, err := CanonicalKind(old); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("CanonicalKind(%q) = %v, want error containing %q", old, err, want)
		}
	}
}

func TestRegistryContainsOnlyExternalSources(t *testing.T) {
	for _, removed := range []string{"graphjin", "workflow", "workflows"} {
		if _, err := CanonicalKind(removed); err == nil {
			t.Fatalf("internal feature kind %q should be rejected", removed)
		}
	}
}

func TestAPICapabilitiesUseRuntimeEnforcement(t *testing.T) {
	for _, key := range []string{KeyAPIRead, KeyAPIWrite, KeyAPIDelete} {
		def, ok := Lookup(KindAPI, key)
		if !ok || def.Enforcement != EnforcementRuntime {
			t.Fatalf("%s definition = %+v", key, def)
		}
	}
	deleteDef, _ := Lookup(KindAPI, KeyAPIDelete)
	if deleteDef.Default(ModeDev) || deleteDef.Default(ModeProd) || deleteDef.Default(ModeAgentic) || !deleteDef.ReadOnlyBlocks || deleteDef.Severity != "critical" {
		t.Fatalf("unsafe api.delete definition: %+v", deleteDef)
	}
}
