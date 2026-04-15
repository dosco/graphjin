package serv

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/spf13/afero"
)

func TestHandleListWorkflows_IsSorted(t *testing.T) {
	mem := afero.NewMemMapFs()
	if err := mem.MkdirAll("/workflows", 0o755); err != nil {
		t.Fatal(err)
	}
	// Write files in non-sorted order. MemMapFs does iterate these in
	// an order that depends on the implementation; regardless, the handler
	// must produce alphabetical output.
	files := []string{"zulu.js", "alpha.js", "mike.js", "bravo.js"}
	for _, f := range files {
		if err := afero.WriteFile(mem, "/workflows/"+f, []byte("function main() {}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	s := &graphjinService{fs: newAferoFS(mem, "/"), conf: &Config{}}
	ms := s.newMCPServerWithContext(context.Background())

	res, err := ms.handleListWorkflows(context.Background(), newToolRequest(map[string]any{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := assertToolSuccess(t, res)

	var out struct {
		Workflows []WorkflowInfo `json:"workflows"`
		Count     int            `json:"count"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := []string{"alpha", "bravo", "mike", "zulu"}
	if len(out.Workflows) != len(want) {
		t.Fatalf("got %d workflows, want %d", len(out.Workflows), len(want))
	}
	for i, w := range out.Workflows {
		if w.Name != want[i] {
			t.Fatalf("position %d: got %q, want %q (full: %+v)", i, w.Name, want[i], out.Workflows)
		}
	}
}
