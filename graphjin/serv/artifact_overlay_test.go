package serv

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/spf13/afero"
)

func newArtifactOverlayTestService(t *testing.T, files map[string]string) *graphjinService {
	t.Helper()
	autoInit := true
	return newArtifactOverlayTestServiceWithArtifacts(t, files, core.ArtifactsConfig{Enabled: true, Source: "main", AutoInit: &autoInit, GlobalsPath: "."})
}

func newArtifactOverlayTestServiceWithArtifacts(t *testing.T, files map[string]string, artifacts core.ArtifactsConfig) *graphjinService {
	t.Helper()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "app.sqlite3")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)`); err != nil {
		t.Fatalf("create users: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO users (id, name) VALUES (1, 'Alice'), (2, 'Bob')`); err != nil {
		t.Fatalf("insert users: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close setup db: %v", err)
	}

	mem := afero.NewMemMapFs()
	for path, body := range files {
		if err := afero.WriteFile(mem, path, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	fs := newAferoFS(mem, "/")
	sourceEnabled := true
	conf := &Config{
		Core: core.Config{
			DBType:     "sqlite",
			Mode:       "agentic",
			Production: false,
			Sources: []core.SourceConfig{
				{Name: "main", Kind: "database", Type: "sqlite", Path: dbPath, Default: true, Access: core.SourceAccessConfig{Read: core.AccessModePublic}},
				{Name: "graphjin", Kind: "graphjin", Catalog: &sourceEnabled, ControlPlane: &sourceEnabled},
				{Name: "workflows", Kind: "workflow"},
			},
			Artifacts: artifacts,
		},
		Serv: Serv{
			Production: false,
			ConfigPath: tmp,
			MCP:        MCPConfig{AllowWorkflowUpdates: true, AllowWorkflowExecution: true},
		},
	}
	svc, err := newGraphJinService(conf, nil, OptionSetFS(fs))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	t.Cleanup(func() { closeTestService(svc) })
	return svc
}

func artifactUserCtx(userID string) context.Context {
	ctx := context.WithValue(context.Background(), core.UserRoleKey, "user")
	ctx = context.WithValue(ctx, core.UserIDKey, userID)
	return context.WithValue(ctx, core.IdentityVarsKey, map[string]interface{}{
		"user_id":     userID,
		"user_ref":    safeArtifactIdentity(userID, false),
		"account_id":  "acct_1",
		"account_ref": safeArtifactIdentity("acct_1", false),
	})
}

func TestSavedQueryAutoSaveUsesUserArtifactWhenConfigured(t *testing.T) {
	svc := newArtifactOverlayTestService(t, nil)
	ctx := artifactUserCtx("user_1")
	res, err := svc.gj.GraphQL(ctx, `query auto_users { users(order_by: { id: asc }) { id name } }`, nil, &core.RequestConfig{})
	if err != nil || len(res.Errors) != 0 {
		t.Fatalf("GraphQL auto save: err=%v errors=%+v", err, res.Errors)
	}
	if _, ok, err := svc.userArtifactRow(ctx, artifactKindSavedQuery, "auto_users"); err != nil || !ok {
		t.Fatalf("expected saved query artifact, ok=%v err=%v", ok, err)
	}
	if ok, _ := svc.fs.Exists("/queries/auto_users.gql"); ok {
		t.Fatal("user-scoped auto-save should not write global query file")
	}

	_, err = svc.gj.GraphQL(context.Background(), `query global_users { users(order_by: { id: asc }) { id name } }`, nil, &core.RequestConfig{})
	if err != nil {
		t.Fatalf("GraphQL global fallback save: %v", err)
	}
	if ok, _ := svc.fs.Exists("/queries/global_users.gql"); !ok {
		t.Fatal("anonymous dev auto-save should write global query file")
	}
}

func TestUserArtifactSavedQueryOverridesGlobalOnlyForOwner(t *testing.T) {
	svc := newArtifactOverlayTestService(t, map[string]string{
		"/queries/user_lookup.gql": `query user_lookup { users(where: { id: { eq: 1 } }) { id name } }`,
	})
	ctx := artifactUserCtx("user_1")
	if _, err := svc.saveUserArtifact(ctx, artifactKindSavedQuery, "user_lookup", `query user_lookup { users(where: { id: { eq: 2 } }) { id name } }`, map[string]any{"operation": "query"}); err != nil {
		t.Fatalf("save artifact query: %v", err)
	}

	userRes, err := svc.executeSavedQueryByName(ctx, "user_lookup", nil, &core.RequestConfig{})
	if err != nil || len(userRes.Errors) != 0 {
		t.Fatalf("execute user query: err=%v errors=%+v", err, userRes.Errors)
	}
	if !strings.Contains(string(userRes.Data), "Bob") || strings.Contains(string(userRes.Data), "Alice") {
		t.Fatalf("expected user artifact to override global, got %s", userRes.Data)
	}

	otherRes, err := svc.executeSavedQueryByName(artifactUserCtx("user_2"), "user_lookup", nil, &core.RequestConfig{})
	if err != nil || len(otherRes.Errors) != 0 {
		t.Fatalf("execute other query: err=%v errors=%+v", err, otherRes.Errors)
	}
	if !strings.Contains(string(otherRes.Data), "Alice") || strings.Contains(string(otherRes.Data), "Bob") {
		t.Fatalf("expected other user to see global query, got %s", otherRes.Data)
	}
}

func TestArtifactSavedQueryImportsArtifactFragmentsAndRejectsTraversal(t *testing.T) {
	svc := newArtifactOverlayTestService(t, nil)
	ctx := artifactUserCtx("user_1")
	if _, err := svc.saveUserArtifact(ctx, artifactKindFragment, "user_fields", `fragment UserFields on users { id name }`, nil); err != nil {
		t.Fatalf("save artifact fragment: %v", err)
	}
	if _, err := svc.saveUserArtifact(ctx, artifactKindSavedQuery, "user_card", `#import "./fragments/user_fields"

query user_card { users(where: { id: { eq: 2 } }) { ...UserFields } }`, map[string]any{"operation": "query"}); err != nil {
		t.Fatalf("save artifact query: %v", err)
	}
	res, err := svc.executeSavedQueryByName(ctx, "user_card", nil, &core.RequestConfig{})
	if err != nil || len(res.Errors) != 0 {
		t.Fatalf("execute imported artifact query: err=%v errors=%+v", err, res.Errors)
	}
	if !strings.Contains(string(res.Data), "Bob") {
		t.Fatalf("expected artifact fragment result, got %s", res.Data)
	}

	if _, err := svc.saveUserArtifact(ctx, artifactKindSavedQuery, "bad_import", `#import "../secret"
query bad_import { users { id } }`, map[string]any{"operation": "query"}); err != nil {
		t.Fatalf("save bad import query: %v", err)
	}
	if _, err := svc.executeSavedQueryByName(ctx, "bad_import", nil, &core.RequestConfig{}); err == nil {
		t.Fatal("expected traversal import to be rejected")
	}
}

func TestUserArtifactWorkflowOverridesGlobalOnlyForOwner(t *testing.T) {
	svc := newArtifactOverlayTestService(t, map[string]string{
		"/workflows/report.js": `function main(input) { return { source: "global", value: input.value }; }`,
	})
	ctx := artifactUserCtx("user_1")
	meta := WorkflowMeta{Description: "User report", Variables: []WorkflowVariable{{Name: "value", Required: true}}}
	if _, err := svc.saveUserArtifact(ctx, artifactKindWorkflow, "report", `function main(input) { return { source: "user", value: input.value }; }`, workflowMetaMap(meta)); err != nil {
		t.Fatalf("save workflow artifact: %v", err)
	}
	userOut, err := svc.runNamedWorkflow(ctx, "report", map[string]any{"value": 7}, nil)
	if err != nil {
		t.Fatalf("run user workflow: %v", err)
	}
	if got := workflowSource(t, userOut); got != "user" {
		t.Fatalf("expected user workflow, got %v", userOut)
	}
	otherOut, err := svc.runNamedWorkflow(artifactUserCtx("user_2"), "report", map[string]any{"value": 7}, nil)
	if err != nil {
		t.Fatalf("run other workflow: %v", err)
	}
	if got := workflowSource(t, otherOut); got != "global" {
		t.Fatalf("expected global workflow, got %v", otherOut)
	}
}

func TestCatalogSnapshotMergesCallerScopedArtifacts(t *testing.T) {
	svc := newArtifactOverlayTestService(t, map[string]string{
		"/queries/global_query.gql": `query global_query { users { id } }`,
	})
	ctx := artifactUserCtx("user_1")
	if _, err := svc.saveUserArtifact(ctx, artifactKindSavedQuery, "user_query", `query user_query { users { id name } }`, map[string]any{"operation": "query"}); err != nil {
		t.Fatalf("save query artifact: %v", err)
	}
	if _, err := svc.saveUserArtifact(ctx, artifactKindFragment, "user_fields", `fragment UserFields on users { id name }`, nil); err != nil {
		t.Fatalf("save fragment artifact: %v", err)
	}
	if _, err := svc.saveUserArtifact(ctx, artifactKindWorkflow, "user_flow", `function main() { return { ok: true }; }`, workflowMetaMap(WorkflowMeta{Description: "User flow"})); err != nil {
		t.Fatalf("save workflow artifact: %v", err)
	}

	snap, err := svc.catalogSnapshotForContext(ctx)
	if err != nil {
		t.Fatalf("catalog snapshot: %v", err)
	}
	for _, id := range []string{"saved_query:user_query", "fragment:user_fields", "workflow:user_flow"} {
		if _, ok := snap.Card(id); !ok {
			t.Fatalf("expected caller catalog card %s", id)
		}
	}
	res, err := svc.gj.GraphQL(ctx, `query {
		gj_catalog(where: { kind: { in: ["saved_query", "fragment", "workflow"] } }) {
			id
			kind
			name
		}
	}`, nil, &core.RequestConfig{})
	if err != nil || len(res.Errors) != 0 {
		t.Fatalf("gj_catalog overlay query: err=%v errors=%+v", err, res.Errors)
	}
	if !strings.Contains(string(res.Data), "saved_query:user_query") ||
		!strings.Contains(string(res.Data), "fragment:user_fields") ||
		!strings.Contains(string(res.Data), "workflow:user_flow") {
		t.Fatalf("gj_catalog should include caller artifacts, got %s", res.Data)
	}
	otherSnap, err := svc.catalogSnapshotForContext(artifactUserCtx("user_2"))
	if err != nil {
		t.Fatalf("other catalog snapshot: %v", err)
	}
	for _, id := range []string{"saved_query:user_query", "fragment:user_fields", "workflow:user_flow"} {
		if _, ok := otherSnap.Card(id); ok {
			t.Fatalf("other user should not see %s", id)
		}
	}
	if _, ok := otherSnap.Card("saved_query:global_query"); !ok {
		t.Fatal("other user should still see global saved query")
	}
}

func TestArtifactNanoProjectionScopesRowsAndHidesRawOwnerIDs(t *testing.T) {
	svc := newArtifactOverlayTestService(t, map[string]string{
		"/queries/global_query.gql": `query global_query { users { id } }`,
	})
	ctx := artifactUserCtx("user_1")
	if _, err := svc.saveUserArtifact(ctx, artifactKindSavedQuery, "private_query", `query private_query { users { id name } }`, map[string]any{"operation": "query"}); err != nil {
		t.Fatalf("save private artifact: %v", err)
	}

	userRows := queryArtifactProjection(t, svc, ctx, `query {
		gj_artifacts(order_by: { name: asc }) {
			name
			kind
			source
			visibility
			content_truncated
		}
	}`)
	if !artifactRowsContain(userRows, "private_query") || !artifactRowsContain(userRows, "global_query") {
		t.Fatalf("owner should see private and global artifact rows, got %+v", userRows)
	}
	private := artifactRowByName(userRows, "private_query")
	if private["visibility"] != "user" {
		t.Fatalf("expected private row to be visible to owner, got %+v", private)
	}

	otherRows := queryArtifactProjection(t, svc, artifactUserCtx("user_2"), `query {
		gj_artifacts(order_by: { name: asc }) {
			name
			kind
			source
			visibility
		}
	}`)
	if artifactRowsContain(otherRows, "private_query") || !artifactRowsContain(otherRows, "global_query") {
		t.Fatalf("other user should see only global artifact rows, got %+v", otherRows)
	}

	res, err := svc.gj.GraphQL(ctx, `query { gj_artifacts { owner_id account_id owner_ref account_ref } }`, nil, &core.RequestConfig{})
	if err == nil && len(res.Errors) == 0 {
		t.Fatalf("owner/account ids and refs should be hidden from non-admin projection queries, got %s", res.Data)
	}
}

func TestArtifactNanoProjectionRefreshesAfterMutation(t *testing.T) {
	svc := newArtifactOverlayTestService(t, nil)
	ctx := artifactUserCtx("user_1")
	content := strings.Repeat("espresso ", 5000)
	if _, err := newArtifactControlPlane(svc).mutateRow(ctx, core.ManagedMutationRoot{
		Table:     artifactsRootTable,
		Operation: "insert",
		Input: map[string]interface{}{
			"name":    "direct_projection",
			"kind":    "query",
			"content": content,
		},
	}); err != nil {
		t.Fatalf("direct artifact mutation: %v", err)
	}

	rows := queryArtifactProjection(t, svc, ctx, `query {
		gj_artifacts(search: "espresso", where: { name: { eq: "direct_projection" } }) {
			name
			content
			content_truncated
		}
	}`)
	if len(rows) != 1 || rows[0]["name"] != "direct_projection" {
		t.Fatalf("mutation should refresh artifact projection, got %+v", rows)
	}
	if rows[0]["content_truncated"] != true {
		t.Fatalf("large projection content should be marked truncated, got %+v", rows[0])
	}
	if contentValue, _ := rows[0]["content"].(string); len(contentValue) > artifactProjectionContentMaxBytes {
		t.Fatalf("projection content length = %d, want <= %d", len(contentValue), artifactProjectionContentMaxBytes)
	}
}

func TestNormalizeArtifactNanoRowCapsJSONFields(t *testing.T) {
	row := normalizeArtifactNanoRow(map[string]any{
		"name":          "unit",
		"kind":          "query",
		"content":       "small",
		"content_json":  strings.Repeat("x", 100),
		"metadata_json": strings.Repeat("y", 100),
	}, 64)
	if row["content_json"] != nil || row["metadata_json"] != nil {
		t.Fatalf("oversized JSON fields should be dropped, got %+v", row)
	}
	if row["content_truncated"] != true {
		t.Fatalf("dropping JSON should mark the row truncated, got %+v", row)
	}
	if row["content"] != "small" {
		t.Fatalf("small content should stay intact, got %+v", row)
	}
	sv, _ := row["search_vector"].(string)
	if !strings.Contains(sv, strings.Repeat("y", 64)) {
		t.Fatalf("search vector should keep a capped metadata prefix, got %q", sv)
	}
	if strings.Contains(sv, strings.Repeat("y", 65)) || strings.Contains(sv, "x") {
		t.Fatalf("search vector must not carry uncapped metadata or content_json, got %q", sv)
	}
}

func TestArtifactProjectionCapsJSONFieldsButStoreKeepsFull(t *testing.T) {
	svc := newArtifactOverlayTestService(t, nil)
	ctx := artifactUserCtx("user_1")
	bigJSON := `{"data":"` + strings.Repeat("x", artifactProjectionContentMaxBytes) + `"}`
	if _, err := newArtifactControlPlane(svc).mutateRow(ctx, core.ManagedMutationRoot{
		Table:     artifactsRootTable,
		Operation: "insert",
		Input: map[string]interface{}{
			"name":          "big_json",
			"kind":          artifactKindSavedQuery,
			"content":       "query big_json { users { id } }",
			"content_json":  bigJSON,
			"metadata_json": bigJSON,
		},
	}); err != nil {
		t.Fatalf("artifact mutation: %v", err)
	}

	rows := queryArtifactProjection(t, svc, ctx, `query {
		gj_artifacts(where: { name: { eq: "big_json" } }) {
			name
			content
			content_json
			metadata_json
			content_truncated
		}
	}`)
	if len(rows) != 1 {
		t.Fatalf("expected one projection row, got %+v", rows)
	}
	row := rows[0]
	if row["content_json"] != nil || row["metadata_json"] != nil {
		t.Fatalf("oversized JSON fields should be dropped from the projection, got %+v", row)
	}
	if row["content_truncated"] != true {
		t.Fatalf("dropped JSON should mark the row truncated, got %+v", row)
	}
	if row["content"] != "query big_json { users { id } }" {
		t.Fatalf("small content should stay intact, got %+v", row)
	}

	stored, ok, err := svc.userArtifactRow(ctx, artifactKindSavedQuery, "big_json")
	if err != nil || !ok {
		t.Fatalf("store row lookup: ok=%v err=%v", ok, err)
	}
	if got := len(fmt.Sprint(stored["content_json"])); got <= artifactProjectionContentMaxBytes {
		t.Fatalf("store should keep the full content_json, got %d bytes", got)
	}
}

func TestArtifactProjectionHonorsConfiguredCap(t *testing.T) {
	autoInit := true
	svc := newArtifactOverlayTestServiceWithArtifacts(t, nil, core.ArtifactsConfig{
		Enabled: true, Source: "main", AutoInit: &autoInit, GlobalsPath: ".",
		ProjectionContentMaxBytes: 64,
	})
	ctx := artifactUserCtx("user_1")
	if _, err := newArtifactControlPlane(svc).mutateRow(ctx, core.ManagedMutationRoot{
		Table:     artifactsRootTable,
		Operation: "insert",
		Input: map[string]interface{}{
			"name":    "tiny_cap",
			"kind":    artifactKindSavedQuery,
			"content": strings.Repeat("m", 200),
		},
	}); err != nil {
		t.Fatalf("artifact mutation: %v", err)
	}
	rows := queryArtifactProjection(t, svc, ctx, `query {
		gj_artifacts(where: { name: { eq: "tiny_cap" } }) {
			content
			content_truncated
		}
	}`)
	if len(rows) != 1 {
		t.Fatalf("expected one projection row, got %+v", rows)
	}
	content, _ := rows[0]["content"].(string)
	if len(content) != 64 || rows[0]["content_truncated"] != true {
		t.Fatalf("configured cap should truncate content at 64 bytes, got len=%d row=%+v", len(content), rows[0])
	}
}

func TestArtifactProjectionPollerCatchesExternalRevision(t *testing.T) {
	svc := newArtifactOverlayTestService(t, nil)
	ctx := artifactUserCtx("user_1")
	lastRevision, err := svc.artifactRevision(context.Background(), "artifacts")
	if err != nil {
		t.Fatalf("initial artifact revision: %v", err)
	}

	query := `query {
		gj_artifacts(where: { name: { eq: "external_projection" } }) {
			name
			content_hash
		}
	}`
	if rows := queryArtifactProjection(t, svc, ctx, query); len(rows) != 0 {
		t.Fatalf("external artifact should not exist before write, got %+v", rows)
	}

	db, dbType, table, ok := svc.artifactDB()
	if !ok {
		t.Fatal("artifact DB not configured")
	}
	content := `query external_projection { users { id } }`
	now := time.Now().UTC().Format(time.RFC3339)
	externalWriteSQL := fmt.Sprintf(`INSERT INTO %s (id, name, kind, path, source, visibility, read_only, account_id, owner_id, content, content_json, metadata_json, content_hash, status, created_at, updated_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET name=excluded.name, kind=excluded.kind, content=excluded.content, content_json=excluded.content_json, metadata_json=excluded.metadata_json, content_hash=excluded.content_hash, status=excluded.status, revision=revision+1, updated_at=excluded.updated_at`, table)
	if dbType == "postgres" || dbType == "" {
		externalWriteSQL = fmt.Sprintf(`INSERT INTO %s (id, name, kind, path, source, visibility, read_only, account_id, owner_id, content, content_json, metadata_json, content_hash, status, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
ON CONFLICT (id) DO UPDATE SET name=EXCLUDED.name, kind=EXCLUDED.kind, content=EXCLUDED.content, content_json=EXCLUDED.content_json, metadata_json=EXCLUDED.metadata_json, content_hash=EXCLUDED.content_hash, status=EXCLUDED.status, revision=%s.revision+1, updated_at=EXCLUDED.updated_at`, table, quotePGIdent("artifacts"))
	}
	if _, err := db.ExecContext(context.Background(), externalWriteSQL,
		artifactID("user_1", artifactKindSavedQuery, "external_projection"),
		"external_projection",
		artifactKindSavedQuery,
		"",
		"database",
		"user",
		false,
		"acct_1",
		"user_1",
		content,
		"",
		"",
		artifactContentHash(content, ""),
		"approved",
		now,
		now,
	); err != nil {
		t.Fatalf("external artifact write: %v", err)
	}
	if err := svc.bumpArtifactRevision(context.Background(), "artifacts"); err != nil {
		t.Fatalf("external revision bump: %v", err)
	}
	if rows := queryArtifactProjection(t, svc, ctx, query); len(rows) != 0 {
		t.Fatalf("projection should stay stale until poller refresh, got %+v", rows)
	}

	changed, err := svc.pollArtifactProjectionRevision(context.Background(), &lastRevision)
	if err != nil {
		t.Fatalf("poll artifact projection: %v", err)
	}
	if !changed {
		t.Fatal("poller should detect external artifact revision")
	}
	rows := queryArtifactProjection(t, svc, ctx, query)
	if len(rows) != 1 || rows[0]["name"] != "external_projection" || rows[0]["content_hash"] != artifactContentHash(content, "") {
		t.Fatalf("poller should refresh external artifact projection, got %+v", rows)
	}
}

func queryArtifactProjection(t *testing.T, svc *graphjinService, ctx context.Context, gql string) []map[string]any {
	t.Helper()
	res, err := svc.gj.GraphQL(ctx, gql, nil, &core.RequestConfig{})
	if err != nil || len(res.Errors) != 0 {
		t.Fatalf("query artifact projection: err=%v errors=%+v data=%s", err, res.Errors, res.Data)
	}
	var out struct {
		Rows []map[string]any `json:"gj_artifacts"`
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		t.Fatalf("decode artifact projection: %v\n%s", err, res.Data)
	}
	return out.Rows
}

func artifactRowsContain(rows []map[string]any, name string) bool {
	return artifactRowByName(rows, name) != nil
}

func artifactRowByName(rows []map[string]any, name string) map[string]any {
	for _, row := range rows {
		if row["name"] == name {
			return row
		}
	}
	return nil
}

func workflowSource(t *testing.T, out any) string {
	t.Helper()
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal workflow output: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("decode workflow output: %v", err)
	}
	source, _ := decoded["source"].(string)
	return source
}
