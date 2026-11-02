package serv

import (
	"context"
	"errors"
	"strings"
	"testing"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/core/v3"
)

func newSQLiteMemoryService(t *testing.T) (*graphjinService, artifactControlPlane) {
	t.Helper()
	svc := newSQLiteThreadService(t)
	svc.conf.Memory = MemoryConfig{Enabled: true, RecallLimit: 2, MaxPerOwner: 3}
	return svc, newArtifactControlPlane(svc)
}

func addMemory(t *testing.T, cp artifactControlPlane, ctx context.Context, content string) map[string]any {
	t.Helper()
	row, err := cp.upsertMemory(ctx, core.ManagedMutationRoot{Operation: "insert", Input: map[string]any{"content": content}}, nil)
	if err != nil {
		t.Fatalf("add memory %q: %v", content, err)
	}
	return row
}

// topicEmbedder maps text onto fixed topic axes, so similarity is predictable.
type topicEmbedder struct{}

func (topicEmbedder) Embed(_ context.Context, texts []string, _ *int) ([][]float32, error) {
	topics := []string{"currency", "region", "fiscal"}
	out := make([][]float32, len(texts))
	for i, text := range texts {
		v := make([]float32, len(topics))
		for j, topic := range topics {
			if strings.Contains(strings.ToLower(text), topic) {
				v[j] = 1
			}
		}
		out[i] = v
	}
	return out, nil
}

func TestMemoryScopesAndPublishing(t *testing.T) {
	svc, cp := newSQLiteMemoryService(t)
	alice := threadUserCtx("alice")
	bob := threadUserCtx("bob")

	personal := addMemory(t, cp, alice, "Report revenue in EUR currency.")
	if personal["scope"] != "personal" {
		t.Fatalf("new memory scope = %v", personal["scope"])
	}
	rows, _ := svc.visibleMemoryRows(bob)
	if len(rows) != 0 {
		t.Fatalf("bob sees alice's personal memory: %+v", rows)
	}

	existing, _ := svc.internalArtifactStoreRow(alice, personal["id"].(string))
	team, err := cp.upsertMemory(alice, core.ManagedMutationRoot{Operation: "update", Input: map[string]any{"tier": "approved"}}, existing)
	if err != nil || team["scope"] != "team" {
		t.Fatalf("publish = %+v err=%v", team, err)
	}
	if rows, _ := svc.visibleMemoryRows(bob); len(rows) != 1 {
		t.Fatalf("bob should see the published team memory, got %+v", rows)
	}
	existing, _ = svc.internalArtifactStoreRow(alice, personal["id"].(string))
	if _, err := cp.upsertMemory(bob, core.ManagedMutationRoot{Operation: "update", Input: map[string]any{"content": "hijack"}}, existing); !errors.Is(err, errMemoryNotFound) {
		t.Fatalf("bob edited alice's memory: %v", err)
	}
	edited, err := cp.upsertMemory(alice, core.ManagedMutationRoot{Operation: "update", Input: map[string]any{"content": "Report revenue in USD currency."}}, existing)
	if err != nil || edited["scope"] != "personal" {
		t.Fatalf("editing a team memory must take it back to personal: %+v err=%v", edited, err)
	}

	addMemory(t, cp, alice, "two")
	addMemory(t, cp, alice, "three")
	if _, err := cp.upsertMemory(alice, core.ManagedMutationRoot{Operation: "insert", Input: map[string]any{"content": "four"}}, nil); err == nil {
		t.Fatal("memory limit was not enforced")
	}
	if _, err := cp.upsertMemory(alice, core.ManagedMutationRoot{Operation: "insert", Input: map[string]any{"content": "x", "visibility": "account"}}, nil); err == nil {
		t.Fatal("memory accepted a server-managed field")
	}
}

func TestMemoryRecallRanksAndInjects(t *testing.T) {
	svc, cp := newSQLiteMemoryService(t)
	svc.conf.Memory.MaxPerOwner = 10
	ctx := threadUserCtx("alice")
	addMemory(t, cp, ctx, "Fiscal year starts in April.")
	addMemory(t, cp, ctx, "Use EUR as the currency for revenue.")
	addMemory(t, cp, ctx, "The EMEA region excludes Turkey.")

	// Keyword fallback: only memories that share a word with the question.
	got, err := svc.recallMemories(ctx, "What revenue did we book? Show currency.")
	if err != nil || len(got) != 1 || !strings.Contains(got[0]["content"].(string), "EUR") {
		t.Fatalf("keyword recall = %+v err=%v", got, err)
	}

	svc.semanticEmbedder = topicEmbedder{}
	got, err = svc.recallMemories(ctx, "Which region and fiscal quarter?")
	if err != nil || len(got) != 2 {
		t.Fatalf("embedding recall = %+v err=%v", got, err)
	}
	joined := got[0]["content"].(string) + got[1]["content"].(string)
	if !strings.Contains(joined, "Fiscal") || !strings.Contains(joined, "region") {
		t.Fatalf("embedding recall picked the wrong memories: %+v", got)
	}

	inner := &scriptedAgentRunner{resp: gjagent.Response{Status: "answered"}}
	if _, err := svc.withMemoryRecall(inner).Run(ctx, gjagent.Request{Instruction: "fiscal region split", Context: map[string]any{"keep": 1}}); err != nil {
		t.Fatal(err)
	}
	if len(inner.req.Memories) == 0 || inner.req.Context["keep"] != 1 {
		t.Fatalf("recall did not reach the run request: %+v", inner.req)
	}
}
