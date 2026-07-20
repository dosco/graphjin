package featurecap

import "testing"

func TestRegistry(t *testing.T) {
	for _, kind := range Kinds() {
		if len(Definitions(kind)) == 0 {
			t.Fatalf("kind %q has no definitions", kind)
		}
		for _, def := range Definitions(kind) {
			if _, ok := Lookup(kind, def.Key); !ok {
				t.Fatalf("missing lookup for %s.%s", kind, def.Key)
			}
		}
	}
}

func TestModeDefaults(t *testing.T) {
	runtimeRead, _ := Lookup(KindSystem, KeyRuntimeRead)
	if !runtimeRead.Default(ModeDev) || runtimeRead.Default(ModeProd) || !runtimeRead.Default(ModeAgentic) {
		t.Fatalf("unexpected runtime.read defaults: %+v", runtimeRead)
	}
	execute, _ := Lookup(KindWorkflows, KeyWorkflowExecute)
	if !execute.Default(ModeDev) || execute.Default(ModeProd) || !execute.Default(ModeAgentic) {
		t.Fatalf("unexpected workflows.execute defaults: %+v", execute)
	}
}
