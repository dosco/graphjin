package serv

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3"
)

// TestAttachExecuteTruncationFlagsLimitClampedLists covers the serv-side
// truncation surface shared by the MCP execute tools and the agent
// saved-query path: a list that reaches its compiled limit carries the
// truncation marker; errors and nil results never do.
func TestAttachExecuteTruncationFlagsLimitClampedLists(t *testing.T) {
	autoInit := true
	svc := newArtifactOverlayTestServiceWithOptions(t, nil, core.ArtifactsConfig{
		Enabled: true, Source: "main", AutoInit: &autoInit, GlobalsPath: ".",
	}, nil)
	ctx := artifactUserCtx("user_1")
	cp := newArtifactControlPlane(svc)

	for i := 0; i < 3; i++ {
		name := fmt.Sprintf("trunc_probe_%02d", i)
		if _, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
			Table:     artifactsRootTable,
			Operation: "insert",
			Input: map[string]interface{}{
				"name":    name,
				"kind":    artifactKindSavedQuery,
				"content": fmt.Sprintf("query %s { users { id } }", name),
			},
		}); err != nil {
			t.Fatalf("insert artifact %s: %v", name, err)
		}
	}

	res, err := svc.gj.GraphQL(ctx, `query { gj_artifacts(limit: 2) { name } }`, nil, &core.RequestConfig{})
	if err != nil || len(res.Errors) != 0 {
		t.Fatalf("query: err=%v errors=%+v", err, res.Errors)
	}
	out := attachExecuteTruncation(ExecuteResult{Data: res.Data}, res)
	if out.Truncation == nil || len(out.Truncation.Roots) != 1 {
		t.Fatalf("limit-clamped list not marked truncated: %+v", out.Truncation)
	}
	root := out.Truncation.Roots[0]
	if root.Path != "gj_artifacts" || root.Limit != 2 || root.Rows != 2 {
		t.Fatalf("truncated root = %+v", root)
	}
	if !strings.Contains(out.Truncation.Message, "pages, not the population") {
		t.Fatalf("truncation message lost its warning: %s", out.Truncation.Message)
	}

	// A result with errors keeps its shape; recovery guidance owns that path.
	if got := attachExecuteTruncation(ExecuteResult{Errors: []ErrorInfo{{Message: "boom"}}}, res); got.Truncation != nil {
		t.Fatalf("errored result gained truncation: %+v", got.Truncation)
	}
	if got := attachExecuteTruncation(ExecuteResult{}, nil); got.Truncation != nil {
		t.Fatalf("nil core result gained truncation: %+v", got.Truncation)
	}

	// A partial page stays unmarked.
	res, err = svc.gj.GraphQL(ctx, `query { gj_artifacts(limit: 50) { name } }`, nil, &core.RequestConfig{})
	if err != nil || len(res.Errors) != 0 {
		t.Fatalf("partial-page query: err=%v errors=%+v", err, res.Errors)
	}
	if got := attachExecuteTruncation(ExecuteResult{Data: res.Data}, res); got.Truncation != nil {
		t.Fatalf("partial page marked truncated: %+v", got.Truncation)
	}
}
