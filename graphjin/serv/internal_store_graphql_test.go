package serv

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3"
)

func TestInternalStoreRoleCanQueryBlockedArtifactTable(t *testing.T) {
	svc := newArtifactOverlayTestService(t, nil)
	ctx := artifactUserCtx("user_1")

	res, err := svc.gj.GraphQL(ctx, `query { _graphjin_artifacts { id name } }`, nil, nil)
	if err == nil && len(res.Errors) == 0 {
		t.Fatalf("user role queried physical artifact table: %s", res.Data)
	}
	if err == nil {
		err = graphQLError(res)
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("physical artifact table should be blocked for user role, got %v", err)
	}

	res, err = svc.gj.GraphQL(svc.internalStoreContext(ctx), `query { _graphjin_artifacts { id name } }`, nil, nil)
	if err != nil || len(res.Errors) != 0 {
		t.Fatalf("internal role query failed: err=%v errors=%+v", err, res.Errors)
	}
}

func TestExternalReservedInternalStoreRoleCannotQueryBlockedArtifactTable(t *testing.T) {
	svc := newArtifactOverlayTestService(t, nil)
	ctx := artifactUserCtx("attacker")
	ctx = context.WithValue(ctx, core.UserRoleKey, graphjinInternalStoreRole)
	ctx = context.WithValue(ctx, core.IdentityRolesKey, []string{graphjinInternalStoreRole})

	res, err := svc.gj.GraphQL(ctx, `query { _graphjin_artifacts { id name owner_id } }`, nil, nil)
	if err == nil && len(res.Errors) == 0 {
		t.Fatalf("external context assumed internal store role: %s", res.Data)
	}
	if err == nil {
		err = graphQLError(res)
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("external internal-role claim should hit the blocked-table guard, got %v", err)
	}
}

func TestExtractClaimRolesStripsReservedRoles(t *testing.T) {
	roles := extractClaimRoles(map[string]interface{}{
		"roles": []interface{}{graphjinInternalStoreRole, "analyst", "__other_reserved", "analyst"},
	}, []string{"roles"})
	if len(roles) != 1 || roles[0] != "analyst" {
		t.Fatalf("extractClaimRoles roles = %#v, want analyst only", roles)
	}
}

func TestSQLiteInternalStoreMutationSupportsTextPrimaryKeys(t *testing.T) {
	svc := newArtifactOverlayTestService(t, nil)
	ctx := svc.internalStoreContext(artifactUserCtx("user_1"))
	vars := map[string]any{"input": map[string]any{
		"id":           "artifact:text-key",
		"name":         "text_key",
		"kind":         "artifact",
		"path":         "",
		"source":       "database",
		"visibility":   "user",
		"read_only":    false,
		"account_id":   "acct_1",
		"owner_id":     "user_1",
		"content":      "hello",
		"content_hash": "hash",
		"status":       "approved",
	}}
	res, err := svc.internalStoreGraphQL(ctx, `mutation { _graphjin_artifacts(insert: $input) { id name owner_id } }`, vars)
	if err != nil {
		t.Fatalf("insert text-key artifact: %v", err)
	}
	if !strings.Contains(string(res["_graphjin_artifacts"]), "artifact:text-key") {
		t.Fatalf("insert response missing text key: %s", res["_graphjin_artifacts"])
	}
	if _, err := svc.internalStoreGraphQL(ctx, `mutation { _graphjin_artifacts(where: { id: { eq: "artifact:text-key" } }, update: { content: "updated" }) { id content } }`, nil); err != nil {
		t.Fatalf("update text-key artifact: %v", err)
	}
	if _, err := svc.internalStoreGraphQL(ctx, `mutation { _graphjin_artifacts(where: { id: { eq: "artifact:text-key" } }, delete: true) { id } }`, nil); err != nil {
		t.Fatalf("delete text-key artifact: %v", err)
	}
}

func graphQLError(res *core.Result) error {
	if res == nil || len(res.Errors) == 0 {
		return nil
	}
	return fmt.Errorf("%s", res.Errors[0].Message)
}
