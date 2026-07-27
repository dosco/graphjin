package serv

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/core/v3"
)

func annotationAccountCtx(userID, accountID, role string) context.Context {
	ctx := context.WithValue(context.Background(), core.UserRoleKey, role)
	ctx = context.WithValue(ctx, core.UserIDKey, userID)
	vars := map[string]interface{}{
		"user_id":  userID,
		"user_ref": safeArtifactIdentity(userID, false),
	}
	if accountID != "" {
		vars["account_id"] = accountID
		vars["account_ref"] = safeArtifactIdentity(accountID, false)
	}
	return context.WithValue(ctx, core.IdentityVarsKey, vars)
}

func annotationTableTarget(t *testing.T, svc *graphjinService, ctx context.Context, table string) string {
	t.Helper()
	snapshot, err := svc.catalogSnapshotForContext(ctx)
	if err != nil {
		t.Fatalf("catalog snapshot: %v", err)
	}
	for _, card := range snapshot.Cards {
		if card.Kind == "table" && card.TableName == table {
			return card.ID
		}
	}
	t.Fatalf("table target for %s not found", table)
	return ""
}

func insertAnnotationForTest(t *testing.T, cp artifactControlPlane, ctx context.Context, target, content string) map[string]any {
	t.Helper()
	row, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: artifactsRootTable, Operation: "insert",
		Input: map[string]any{"kind": artifactKindAnnotation, "target_ref": target, "content": content},
	})
	if err != nil {
		t.Fatalf("insert annotation: %v", err)
	}
	return row
}

func updateAnnotationForTest(t *testing.T, cp artifactControlPlane, ctx context.Context, id string, input map[string]any) map[string]any {
	t.Helper()
	row, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: artifactsRootTable, Operation: "update", Where: map[string]any{"id": id}, Input: input,
	})
	if err != nil {
		t.Fatalf("update annotation: %v", err)
	}
	return row
}

func annotationDetailForCard(t *testing.T, card CatalogItem) map[string]any {
	t.Helper()
	details, err := catalogCardDetails(card)
	if err != nil {
		t.Fatal(err)
	}
	for _, detail := range details {
		if detail.Section != "annotations" {
			continue
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(detail.DataJSON), &data); err != nil {
			t.Fatalf("decode annotation detail: %v", err)
		}
		return data
	}
	t.Fatalf("annotation detail missing from %+v", card)
	return nil
}

func TestAnnotationLifecycleScopeCatalogMergeAndDemotion(t *testing.T) {
	svc := newArtifactOverlayTestService(t, nil)
	cp := newArtifactControlPlane(svc)
	owner := annotationAccountCtx("annotation_owner", "acct_1", "user")
	teammate := annotationAccountCtx("annotation_teammate", "acct_1", "user")
	foreign := annotationAccountCtx("annotation_foreign", "acct_2", "user")
	admin := annotationAccountCtx("annotation_admin", "acct_2", "admin")
	target := annotationTableTarget(t, svc, owner, "users")

	draft := insertAnnotationForTest(t, cp, owner, target, "Chargeback reviews use the original processor reference.")
	id := fmt.Sprint(draft["id"])
	if !strings.HasPrefix(id, "annotation:") || draft["tier"] != annotationTierObserved || draft["visibility"] != "user" {
		t.Fatalf("draft annotation = %+v", draft)
	}
	if draft["approved_ref"] != "" || draft["approved_at"] != "" || draft["catalog_revision"] == "" {
		t.Fatalf("draft server fields = %+v", draft)
	}

	query := `query { gj_artifacts(search: "chargeback", where: { kind: { eq: "annotation" } }) { id target_ref tier author_ref approved_ref content } }`
	if rows := queryArtifactProjection(t, svc, teammate, query); len(rows) != 0 {
		t.Fatalf("same-account teammate saw draft: %+v", rows)
	}

	approved := updateAnnotationForTest(t, cp, owner, id, map[string]any{"tier": annotationTierApproved})
	if approved["tier"] != annotationTierApproved || approved["visibility"] != "account" || approved["approved_at"] == "" {
		t.Fatalf("approved annotation = %+v", approved)
	}
	if approved["approved_ref"] != safeArtifactIdentity("annotation_owner", false) {
		t.Fatalf("approval attribution = %+v", approved)
	}
	if rows := queryArtifactProjection(t, svc, teammate, query); len(rows) != 1 || rows[0]["id"] != id {
		t.Fatalf("same-account approved projection = %+v", rows)
	}
	if rows := queryArtifactProjection(t, svc, foreign, query); len(rows) != 0 {
		t.Fatalf("foreign account saw approved annotation: %+v", rows)
	}
	raw, err := svc.gj.GraphQL(owner, fmt.Sprintf(`query { gj_catalog(where: { id: { eq: %q } }) { id details_json } }`, target), nil, &core.RequestConfig{})
	if err != nil || len(raw.Errors) != 0 {
		t.Fatalf("raw gj_catalog detail: err=%v errors=%+v", err, raw.Errors)
	}
	if strings.Contains(string(raw.Data), "Chargeback reviews") || strings.Contains(string(raw.Data), `"section":"annotations"`) {
		t.Fatalf("raw gj_catalog must not merge annotations: %s", raw.Data)
	}

	ms := &mcpServer{service: svc, ctx: owner}
	rows, err := ms.queryCatalogRows(owner, catalogGraphQLQuery{ID: target})
	if err != nil || len(rows) != 1 {
		t.Fatalf("query catalog detail: rows=%+v err=%v", rows, err)
	}
	detail := annotationDetailForCard(t, rows[0])
	if detail["framing"] != annotationCatalogFraming {
		t.Fatalf("annotation framing = %#v", detail["framing"])
	}
	notes, _ := detail["annotations"].([]any)
	if len(notes) != 1 {
		t.Fatalf("annotation details = %+v", detail)
	}
	note, _ := notes[0].(map[string]any)
	if note["content"] != "Chargeback reviews use the original processor reference." || note["stale"] != false {
		t.Fatalf("annotation detail note = %+v", note)
	}
	foreignRows, err := (&mcpServer{service: svc, ctx: foreign}).queryCatalogRows(foreign, catalogGraphQLQuery{ID: target})
	if err != nil || len(foreignRows) != 1 {
		t.Fatalf("foreign catalog detail: rows=%+v err=%v", foreignRows, err)
	}
	for _, item := range foreignRows {
		if strings.Contains(item.DetailsJSON, "Chargeback reviews") {
			t.Fatalf("foreign catalog leaked annotation: %+v", item)
		}
	}

	demoted := updateAnnotationForTest(t, cp, admin, id, map[string]any{"tier": annotationTierObserved})
	if demoted["tier"] != annotationTierObserved || demoted["visibility"] != "user" || demoted["approved_ref"] != "" || demoted["approved_at"] != "" {
		t.Fatalf("demoted annotation = %+v", demoted)
	}
	latestRuntimeEventDetails(t, svc, "catalog", "annotation_id", id)
	if rows := queryArtifactProjection(t, svc, teammate, query); len(rows) != 0 {
		t.Fatalf("demoted annotation remained shared: %+v", rows)
	}
	adminApprovedDraft := insertAnnotationForTest(t, cp, owner, target, "A reviewer-approved organizational convention.")
	adminApproved := updateAnnotationForTest(t, cp, admin, fmt.Sprint(adminApprovedDraft["id"]), map[string]any{"tier": annotationTierApproved})
	if adminApproved["approved_ref"] != safeArtifactIdentity("annotation_admin", false) || adminApproved["author_ref"] != safeArtifactIdentity("annotation_owner", false) {
		t.Fatalf("admin approval attribution = %+v", adminApproved)
	}
	autoDemoted := updateAnnotationForTest(t, cp, owner, fmt.Sprint(adminApprovedDraft["id"]), map[string]any{"content": "The reviewed convention changed."})
	if autoDemoted["tier"] != annotationTierObserved || autoDemoted["approved_ref"] != "" || autoDemoted["approved_at"] != "" {
		t.Fatalf("definition edit must demote approval = %+v", autoDemoted)
	}
	var resp gjagent.Response
	svc.appendAnnotationNotices(owner, &resp)
	if len(resp.Notices) != 1 || resp.Notices[0].Kind != "annotations_unshared" || len(resp.Notices[0].AnnotationIDs) != 2 {
		t.Fatalf("annotation notice = %+v", resp.Notices)
	}
}

func TestAnnotationStaleDetailDeploymentScopeAndServerFields(t *testing.T) {
	svc := newArtifactOverlayTestService(t, nil)
	cp := newArtifactControlPlane(svc)
	owner := annotationAccountCtx("deployment_owner", "", "user")
	reader := annotationAccountCtx("deployment_reader", "", "user")
	target := "column:retired.legacy_code"
	draft := insertAnnotationForTest(t, cp, owner, target, "Legacy code values came from the retired importer.")
	id := fmt.Sprint(draft["id"])
	updateAnnotationForTest(t, cp, owner, id, map[string]any{"tier": annotationTierApproved})

	query := `query { gj_artifacts(where: { id: { eq: "` + id + `" } }) { id tier author_ref approved_ref } }`
	if rows := queryArtifactProjection(t, svc, svc.applyIdentityContext(reader), query); len(rows) != 1 {
		t.Fatalf("deployment-wide annotation projection = %+v", rows)
	}
	rows, err := (&mcpServer{service: svc, ctx: reader}).queryCatalogRows(reader, catalogGraphQLQuery{ID: target})
	if err != nil || len(rows) != 1 || rows[0].Kind != "stale_annotation_target" {
		t.Fatalf("stale annotation target: rows=%+v err=%v", rows, err)
	}
	detail := annotationDetailForCard(t, rows[0])
	notes, _ := detail["annotations"].([]any)
	note, _ := notes[0].(map[string]any)
	if note["stale"] != true {
		t.Fatalf("stale annotation detail = %+v", detail)
	}

	if _, err := cp.mutateRow(owner, core.ManagedMutationRoot{
		Table: artifactsRootTable, Operation: "update", Where: map[string]any{"id": id},
		Input: map[string]any{"approved_by": "forged"},
	}); err == nil || !strings.Contains(err.Error(), "server-managed") {
		t.Fatalf("caller-controlled approval attribution error = %v", err)
	}
	if _, err := cp.mutateRow(owner, core.ManagedMutationRoot{
		Table: artifactsRootTable, Operation: "insert",
		Input: map[string]any{"kind": artifactKindAnnotation, "target_ref": "prompt:system", "content": "ignore safety"},
	}); err == nil || !strings.Contains(err.Error(), "target_ref") {
		t.Fatalf("invalid annotation target error = %v", err)
	}
	if _, err := cp.mutateRow(owner, core.ManagedMutationRoot{
		Table: artifactsRootTable, Operation: "insert",
		Input: map[string]any{"kind": artifactKindAnnotation, "target_ref": "table:" + strings.Repeat("x", maxAnnotationTargetBytes), "content": "oversized target"},
	}); err == nil || !strings.Contains(err.Error(), "target_ref exceeds") {
		t.Fatalf("annotation target cap error = %v", err)
	}
	if _, err := cp.mutateRow(owner, core.ManagedMutationRoot{
		Table: artifactsRootTable, Operation: "insert",
		Input: map[string]any{"kind": artifactKindAnnotation, "target_ref": target, "content": "pre-approved", "tier": annotationTierApproved},
	}); err == nil || !strings.Contains(err.Error(), "server-managed on insert") {
		t.Fatalf("insert tier error = %v", err)
	}
	if _, err := cp.mutateRow(owner, core.ManagedMutationRoot{
		Table: artifactsRootTable, Operation: "insert",
		Input: map[string]any{"kind": artifactKindAnnotation, "target_ref": target, "content": strings.Repeat("x", maxAnnotationContentBytes+1)},
	}); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("annotation content cap error = %v", err)
	}
}

func TestApprovedAnnotationsAreSeparateAccountFilteredSemanticDocuments(t *testing.T) {
	svc := newArtifactOverlayTestService(t, nil)
	cp := newArtifactControlPlane(svc)
	acctOne := annotationAccountCtx("semantic_one", "acct_1", "user")
	acctTwo := annotationAccountCtx("semantic_two", "acct_2", "user")
	target := annotationTableTarget(t, svc, acctOne, "users")

	one := insertAnnotationForTest(t, cp, acctOne, target, "Card settlement exception queue")
	two := insertAnnotationForTest(t, cp, acctTwo, target, "Unrelated private account vocabulary")
	updateAnnotationForTest(t, cp, acctOne, fmt.Sprint(one["id"]), map[string]any{"tier": annotationTierApproved})
	updateAnnotationForTest(t, cp, acctTwo, fmt.Sprint(two["id"]), map[string]any{"tier": annotationTierApproved})
	documents, err := svc.approvedAnnotationSemanticDocuments(acctOne)
	if err != nil || len(documents) != 2 {
		t.Fatalf("approved annotation documents = %+v err=%v", documents, err)
	}
	for _, document := range documents {
		if document.Kind != artifactKindAnnotation || len(document.TargetCardIDs) != 1 || document.TargetCardIDs[0] != target || document.AccountRef == "" {
			t.Fatalf("semantic annotation document = %+v", document)
		}
	}

	snapshot, err := svc.catalogSnapshotForContext(acctOne)
	if err != nil {
		t.Fatal(err)
	}
	index := &semanticPersistedIndex{
		manifest: semanticIndexManifest{ActualDimension: 2},
		docs: []semanticDocumentMap{
			{Hash: "acct-1", Kind: artifactKindAnnotation, TargetCardIDs: []string{target}, AccountRef: safeArtifactIdentity("acct_1", false), VectorOffset: 0},
			{Hash: "acct-2", Kind: artifactKindAnnotation, TargetCardIDs: []string{target}, AccountRef: safeArtifactIdentity("acct_2", false), VectorOffset: 2},
		},
		vectors: []float32{1, 0, 1, 0},
	}
	hints := (&semanticCatalogIndex{}).hintsForVector(acctOne, snapshot, core.CatalogQuery{Search: "settlement"}, index, []float32{1, 0})
	if len(hints.hints) != 1 || hints.hints[0].CardID != target || !strings.Contains(hints.hints[0].Source, "annotation") {
		t.Fatalf("account-filtered semantic hints = %+v", hints.hints)
	}
	noAccount := annotationAccountCtx("semantic_none", "", "user")
	if got := (&semanticCatalogIndex{}).hintsForVector(noAccount, snapshot, core.CatalogQuery{Search: "settlement"}, index, []float32{1, 0}); len(got.hints) != 0 {
		t.Fatalf("account-less caller saw account semantic documents: %+v", got.hints)
	}

	updateAnnotationForTest(t, cp, acctOne, fmt.Sprint(one["id"]), map[string]any{"tier": annotationTierObserved})
	documents, err = svc.approvedAnnotationSemanticDocuments(acctOne)
	if err != nil || len(documents) != 1 || documents[0].AccountRef != safeArtifactIdentity("acct_2", false) {
		t.Fatalf("demoted semantic document removal = %+v err=%v", documents, err)
	}
}

func TestAnnotationApprovalEmbedsOnlyTheNewDocument(t *testing.T) {
	svc := newArtifactOverlayTestService(t, nil)
	svc.conf.Serv.DiscoveryCache.Path = ".graphjin/annotation-semantic-test"
	svc.conf.Serv.CatalogSearch.Semantic = SemanticCatalogSearchConfig{
		Enabled: true, Provider: "openai", EmbeddingModel: "fake", Dimensions: "tiny",
	}
	client := &deterministicEmbeddingClient{dimension: 128}
	svc.semanticEmbedder = client
	index, err := newSemanticCatalogIndex(svc)
	if err != nil {
		t.Fatal(err)
	}
	ctx := annotationAccountCtx("incremental_semantic", "acct_1", "user")
	target := annotationTableTarget(t, svc, ctx, "users")
	snapshot, err := svc.catalogSnapshotForContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first, err := index.build(ctx, snapshot)
	if err != nil {
		t.Fatalf("initial semantic build: %v", err)
	}
	index.setActive(first)
	client.reset()

	draft := insertAnnotationForTest(t, newArtifactControlPlane(svc), ctx, target, "Settlement review vocabulary")
	updateAnnotationForTest(t, newArtifactControlPlane(svc), ctx, fmt.Sprint(draft["id"]), map[string]any{"tier": annotationTierApproved})
	second, err := index.build(ctx, snapshot)
	if err != nil {
		t.Fatalf("approval semantic build: %v", err)
	}
	_, texts, _ := client.stats()
	if texts != 1 {
		t.Fatalf("approval embedded %d documents, want only the new annotation", texts)
	}
	if second.manifest.DocumentCount != first.manifest.DocumentCount+1 {
		t.Fatalf("approval document count = %d, want %d", second.manifest.DocumentCount, first.manifest.DocumentCount+1)
	}
	index.setActive(second)
	client.reset()

	updateAnnotationForTest(t, newArtifactControlPlane(svc), ctx, fmt.Sprint(draft["id"]), map[string]any{"tier": annotationTierObserved})
	third, err := index.build(ctx, snapshot)
	if err != nil {
		t.Fatalf("demotion semantic build: %v", err)
	}
	_, texts, _ = client.stats()
	if texts != 0 || third.manifest.DocumentCount != first.manifest.DocumentCount {
		t.Fatalf("demotion embedded=%d documents=%d, want reuse with %d documents", texts, third.manifest.DocumentCount, first.manifest.DocumentCount)
	}
}

func TestAnnotationTaskMustHaveSameOwner(t *testing.T) {
	autoInit := true
	svc := newArtifactOverlayTestServiceWithOptions(t, nil, core.ArtifactsConfig{
		Enabled: true, Source: "main", AutoInit: &autoInit, GlobalsPath: ".",
	}, func(conf *Config) {
		conf.Core.Tasks = core.TasksConfig{Enabled: true, MaxPerOwner: 20, MaxEntriesPerTask: 100, EntryRetentionHours: 168, SnapshotMaxBytes: 32768}
	})
	owner := annotationAccountCtx("task_annotation_owner", "acct_1", "user")
	other := annotationAccountCtx("task_annotation_other", "acct_1", "user")
	task := insertTaskForTest(t, newTaskControlPlane(svc), owner, "Capture durable catalog context")
	taskID := fmt.Sprint(task["id"])
	target := annotationTableTarget(t, svc, owner, "users")

	row, err := newArtifactControlPlane(svc).mutateRow(owner, core.ManagedMutationRoot{
		Table: artifactsRootTable, Operation: "insert",
		Input: map[string]any{"kind": artifactKindAnnotation, "target_ref": target, "content": "Task-backed observation", "task_id": taskID},
	})
	if err != nil || row["task_id"] != taskID {
		t.Fatalf("same-owner task annotation = %+v err=%v", row, err)
	}
	if _, err := newArtifactControlPlane(svc).mutateRow(other, core.ManagedMutationRoot{
		Table: artifactsRootTable, Operation: "insert",
		Input: map[string]any{"kind": artifactKindAnnotation, "target_ref": target, "content": "Foreign task link", "task_id": taskID},
	}); err == nil || !strings.Contains(err.Error(), errTaskNotFoundOrClosed.Error()) {
		t.Fatalf("foreign task annotation error = %v", err)
	}
}

func TestCatalogAnnotationDetailTruncationIsExact(t *testing.T) {
	rows := make([]map[string]any, maxMergedAnnotations)
	for index := range rows {
		rows[index] = map[string]any{"id": fmt.Sprintf("annotation:%02d", index), "updated_at": fmt.Sprintf("%02d", index)}
	}
	detail, err := catalogAnnotationDetail("table:main.users", rows, false)
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(detail.DataJSON), &data); err != nil {
		t.Fatal(err)
	}
	if data["truncated"] != false {
		t.Fatalf("exactly %d annotations should not be truncated: %+v", maxMergedAnnotations, data)
	}
	rows = append(rows, map[string]any{"id": "annotation:overflow", "updated_at": "99"})
	detail, err = catalogAnnotationDetail("table:main.users", rows, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(detail.DataJSON), &data); err != nil {
		t.Fatal(err)
	}
	if data["truncated"] != true {
		t.Fatalf("overflow annotations should be truncated: %+v", data)
	}
}

func TestAnnotationAndTaskCloseNextGuidance(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{AllowRawQueries: true, IncludeToolsWithAgent: true})
	annotationQuery := `mutation {
		gj_artifacts(insert: { kind: "annotation", target_ref: "table:main.users", content: "Reviewed context" }) {
			id tier
		}
	}`
	next := ms.nextForToolCall("execute_graphql", map[string]any{"query": annotationQuery}, ExecuteResult{})
	if next == nil || next.StateCode != "annotation_created" || len(next.Options) != 2 {
		t.Fatalf("annotation next guidance = %+v", next)
	}
	if query := fmt.Sprint(next.Options[0].ArgsTemplate["query"]); !strings.Contains(query, `tier: "approved"`) || !strings.Contains(next.Options[0].Reason, "follow-up run") {
		t.Fatalf("annotation approval guidance = %+v", next.Options[0])
	}

	closeQuery := `mutation { gj_task(where: { id: { eq: "task:1" } }, update: { status: "closed", outcome: "done" }) { id status } }`
	next = ms.nextForToolCall("execute_graphql", map[string]any{"query": closeQuery}, ExecuteResult{})
	if next == nil || next.StateCode != "task_closed" || len(next.Options) != 1 {
		t.Fatalf("task close next guidance = %+v", next)
	}
	if query := fmt.Sprint(next.Options[0].ArgsTemplate["query"]); !strings.Contains(query, `kind: "annotation"`) || !strings.Contains(query, `task_id: "<closed_task_id>"`) {
		t.Fatalf("task distillation guidance = %+v", next.Options[0])
	}
}
