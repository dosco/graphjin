package serv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/core/v3"
)

func TestWatchFlowReviewPreviewThenStoredApproval(t *testing.T) {
	db, svc := newSQLiteWatchService(t, 2)
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)
	client := &watchFlowTestClient{content: `{"verdict":"digest","severity":"warn","summary":"Roast drift is worth batching."}`}
	svc.conf.Agent = AgentConfig{Enabled: true, Provider: "openai", APIKeyEnv: "IGNORED", TimeoutSeconds: 5}
	svc.agentClientFactory = func(gjagent.Config) (ax.AIClient, error) { return client, nil }

	cp := newWatchControlPlane(svc)
	ctx := artifactUserCtx("flow_review_owner")
	watch := insertFlowReviewWatch(t, cp, ctx, "stored_preview", "")
	watchID := stringFromAny(watch["id"])
	flowHash := stringFromAny(watch["flow_hash"])

	previewed, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table:     watchesRootTable,
		Operation: "update",
		Where:     map[string]interface{}{"id": map[string]interface{}{"eq": watchID}},
		Input: map[string]interface{}{"flow_review_json": map[string]any{
			"decision":           "preview",
			"expected_flow_hash": flowHash,
			"samples_json":       []any{map[string]any{"temperature": 410}},
		}},
	})
	if err != nil {
		t.Fatalf("preview flow: %v", err)
	}
	if previewed["flow_approval"] != watchReviewPending || previewed["approval"] != watchReviewPending {
		t.Fatalf("preview must not approve: %+v", previewed)
	}
	preview := mapFromAny(previewed["flow_preview_json"])
	if preview["status"] != "ok" || intFromAny(preview["digest_count"]) != 1 {
		t.Fatalf("unexpected preview: %+v", preview)
	}

	approved, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table:     watchesRootTable,
		Operation: "update",
		Where:     map[string]interface{}{"id": map[string]interface{}{"eq": watchID}},
		Input: map[string]interface{}{"flow_review_json": map[string]any{
			"decision":           "approve",
			"expected_flow_hash": flowHash,
		}},
	})
	if err != nil {
		t.Fatalf("approve stored preview: %v", err)
	}
	if approved["flow_approval"] != watchReviewApproved || approved["approval"] != watchReviewApproved ||
		approved["status"] != "active" || approved["enabled"] != true {
		t.Fatalf("stored preview did not activate watch: %+v", approved)
	}
	if client.calls != 1 {
		t.Fatalf("stored-preview approval reran the model: calls=%d", client.calls)
	}
}

func TestWatchUnifiedReviewGraphQLSurface(t *testing.T) {
	db, svc := newSQLiteWatchService(t, 2)
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)
	client := &watchFlowTestClient{content: `{"verdict":"notify","severity":"warn","summary":"Roast temperature needs attention."}`}
	svc.conf.Agent = AgentConfig{Enabled: true, Provider: "openai", APIKeyEnv: "IGNORED", TimeoutSeconds: 5}
	svc.agentClientFactory = func(gjagent.Config) (ax.AIClient, error) { return client, nil }
	ctx := artifactUserCtx("graphql_review_owner")

	res, err := svc.gj.GraphQL(ctx, `mutation {
		gj_watch(insert: {
			name: "graphql_review"
			query: "subscription graphql_review { orders(first: 25, after: $cursor) { id status } orders_cursor }"
			enrich_json: { enabled: true, kind: "flow", flow: "default_watch_triage" }
		}) {
			id
			flow_hash
			flow_approval
			action_approval
			status
			enabled
		}
	}`, nil, &core.RequestConfig{})
	if err != nil {
		t.Fatalf("create watch GraphQL: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("create watch GraphQL errors: %+v", res.Errors)
	}
	var created struct {
		Watch struct {
			ID             string `json:"id"`
			FlowHash       string `json:"flow_hash"`
			FlowApproval   string `json:"flow_approval"`
			ActionApproval string `json:"action_approval"`
			Status         string `json:"status"`
			Enabled        bool   `json:"enabled"`
		} `json:"gj_watch"`
	}
	if err := json.Unmarshal(res.Data, &created); err != nil {
		t.Fatalf("decode create: %v\n%s", err, string(res.Data))
	}
	if created.Watch.ID == "" || created.Watch.FlowHash == "" ||
		created.Watch.FlowApproval != watchReviewPending ||
		created.Watch.ActionApproval != watchReviewNotRequired ||
		created.Watch.Status != "paused" || created.Watch.Enabled {
		t.Fatalf("unexpected created watch: %+v", created.Watch)
	}

	reviewMutation := fmt.Sprintf(`mutation {
		gj_watch(
			where: { id: { eq: %q } }
			update: {
				flow_review_json: {
					decision: "approve"
					expected_flow_hash: %q
					samples_json: [{ temperature: 421 }]
				}
			}
		) {
			id
			flow_hash
			flow_approval
			flow_preview_json
			status
			enabled
		}
	}`, created.Watch.ID, created.Watch.FlowHash)
	res, err = svc.gj.GraphQL(ctx, reviewMutation, nil, &core.RequestConfig{})
	if err != nil {
		t.Fatalf("review watch GraphQL: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("review watch GraphQL errors: %+v", res.Errors)
	}
	var reviewed struct {
		Watch struct {
			FlowApproval string         `json:"flow_approval"`
			Preview      map[string]any `json:"flow_preview_json"`
			Status       string         `json:"status"`
			Enabled      bool           `json:"enabled"`
		} `json:"gj_watch"`
	}
	if err := json.Unmarshal(res.Data, &reviewed); err != nil {
		t.Fatalf("decode review: %v\n%s", err, string(res.Data))
	}
	if reviewed.Watch.FlowApproval != watchReviewApproved ||
		reviewed.Watch.Preview["status"] != "ok" ||
		reviewed.Watch.Status != "active" || !reviewed.Watch.Enabled {
		t.Fatalf("unexpected reviewed watch: %+v", reviewed.Watch)
	}
}

func TestWatchReviewsRejectStaleHashesAndMixedDefinitionChanges(t *testing.T) {
	db, svc := newSQLiteWatchService(t, 2)
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)
	cp := newWatchControlPlane(svc)
	ctx := artifactUserCtx("review_safety_owner")
	watch := insertFlowReviewWatch(t, cp, ctx, "review_safety", "")
	watchID := stringFromAny(watch["id"])

	for _, tc := range []struct {
		name  string
		input map[string]interface{}
		want  string
	}{
		{
			name: "stale flow hash",
			input: map[string]interface{}{"flow_review_json": map[string]any{
				"decision": "reject", "expected_flow_hash": "stale",
			}},
			want: "stale",
		},
		{
			name: "review mixed with definition",
			input: map[string]interface{}{
				"name": "review_safety",
				"flow_review_json": map[string]any{
					"decision": "reject", "expected_flow_hash": stringFromAny(watch["flow_hash"]),
				},
			},
			want: "cannot be combined",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
				Table: watchesRootTable, Operation: "update",
				Where: map[string]interface{}{"id": map[string]interface{}{"eq": watchID}},
				Input: tc.input,
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v, want %q", err, tc.want)
			}
		})
	}
}

func TestWatchFlowAndActionApprovalsAreIndependent(t *testing.T) {
	db, svc := newSQLiteWatchService(t, 2)
	if err := svc.fs.Put("/workflows/notify.js", []byte(`function main(input) { return {event: input.event.id}; }`)); err != nil {
		t.Fatalf("write workflow: %v", err)
	}
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)
	client := &watchFlowTestClient{content: `{"verdict":"notify","severity":"critical","summary":"Act on this roast."}`}
	svc.conf.Agent = AgentConfig{Enabled: true, Provider: "openai", APIKeyEnv: "IGNORED", TimeoutSeconds: 5}
	svc.agentClientFactory = func(gjagent.Config) (ax.AIClient, error) { return client, nil }

	cp := newWatchControlPlane(svc)
	ctx := artifactUserCtx("combined_review_owner")
	watch := insertFlowReviewWatch(t, cp, ctx, "combined_review", "notify")
	if watch["flow_approval"] != watchReviewPending || watch["action_approval"] != watchReviewPending {
		t.Fatalf("new combined watch gates: %+v", watch)
	}
	watchID := stringFromAny(watch["id"])
	flowApproved, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: watchesRootTable, Operation: "update",
		Where: map[string]interface{}{"id": map[string]interface{}{"eq": watchID}},
		Input: map[string]interface{}{"flow_review_json": map[string]any{
			"decision": "approve", "expected_flow_hash": watch["flow_hash"],
			"samples_json": []any{map[string]any{"temperature": 421}},
		}},
	})
	if err != nil {
		t.Fatalf("approve flow: %v", err)
	}
	if flowApproved["flow_approval"] != watchReviewApproved ||
		flowApproved["action_approval"] != watchReviewPending ||
		flowApproved["approval"] != watchReviewPending {
		t.Fatalf("flow approval incorrectly approved action: %+v", flowApproved)
	}

	approved := approveWatchActionForTest(t, cp, ctx, flowApproved)
	if approved["flow_approval"] != watchReviewApproved || approved["action_approval"] != watchReviewApproved ||
		approved["approval"] != watchReviewApproved || approved["status"] != "active" {
		t.Fatalf("combined approvals did not activate: %+v", approved)
	}
}

func TestWatchFlowAndActionRejectionRequireCurrentHashes(t *testing.T) {
	db, svc := newSQLiteWatchService(t, 2)
	if err := svc.fs.Put("/workflows/notify.js", []byte(`function main(input) { return {event: input.event}; }`)); err != nil {
		t.Fatalf("write workflow: %v", err)
	}
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)
	cp := newWatchControlPlane(svc)
	ctx := artifactUserCtx("review_reject_owner")
	watch := insertFlowReviewWatch(t, cp, ctx, "review_reject", "notify")
	watchID := stringFromAny(watch["id"])

	for _, tc := range []struct {
		name  string
		field string
		input map[string]any
		want  string
	}{
		{
			name: "flow missing hash", field: "flow_review_json",
			input: map[string]any{"decision": "reject"}, want: "expected_flow_hash",
		},
		{
			name: "action missing hash", field: "action_review_json",
			input: map[string]any{"decision": "reject"}, want: "expected_action_hash",
		},
		{
			name: "action stale hash", field: "action_review_json",
			input: map[string]any{"decision": "approve", "expected_action_hash": "stale"}, want: "stale",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
				Table: watchesRootTable, Operation: "update",
				Where: map[string]interface{}{"id": map[string]interface{}{"eq": watchID}},
				Input: map[string]interface{}{tc.field: tc.input},
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v, want %q", err, tc.want)
			}
		})
	}

	flowRejected, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: watchesRootTable, Operation: "update",
		Where: map[string]interface{}{"id": map[string]interface{}{"eq": watchID}},
		Input: map[string]interface{}{"flow_review_json": map[string]any{
			"decision": "reject", "expected_flow_hash": watch["flow_hash"],
		}},
	})
	if err != nil {
		t.Fatalf("reject flow: %v", err)
	}
	if flowRejected["flow_approval"] != watchReviewRejected ||
		flowRejected["action_approval"] != watchReviewPending ||
		flowRejected["approval"] != watchReviewRejected {
		t.Fatalf("flow rejection changed the wrong gate: %+v", flowRejected)
	}

	actionRejected, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: watchesRootTable, Operation: "update",
		Where: map[string]interface{}{"id": map[string]interface{}{"eq": watchID}},
		Input: map[string]interface{}{"action_review_json": map[string]any{
			"decision": "reject", "expected_action_hash": watch["action_hash"],
		}},
	})
	if err != nil {
		t.Fatalf("reject action: %v", err)
	}
	if actionRejected["action_approval"] != watchReviewRejected ||
		actionRejected["flow_approval"] != watchReviewRejected ||
		actionRejected["approval"] != watchReviewRejected {
		t.Fatalf("action rejection changed the wrong gate: %+v", actionRejected)
	}
}

func TestWatchReviewRequiresExactOwnerScopedID(t *testing.T) {
	db, svc := newSQLiteWatchService(t, 2)
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)
	cp := newWatchControlPlane(svc)
	ownerCtx := artifactUserCtx("review_scope_owner")
	watch := insertFlowReviewWatch(t, cp, ownerCtx, "review_scope", "")
	review := map[string]any{
		"decision": "reject", "expected_flow_hash": watch["flow_hash"],
	}
	for _, tc := range []struct {
		name  string
		ctx   context.Context
		where map[string]interface{}
		want  string
	}{
		{
			name: "not exact", ctx: ownerCtx,
			where: map[string]interface{}{"id": map[string]interface{}{"eq": watch["id"]}, "status": map[string]interface{}{"eq": "paused"}},
			want:  "requires where",
		},
		{
			name: "foreign owner", ctx: artifactUserCtx("review_scope_other"),
			where: map[string]interface{}{"id": map[string]interface{}{"eq": watch["id"]}},
			want:  "denied",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := cp.mutateRow(tc.ctx, core.ManagedMutationRoot{
				Table: watchesRootTable, Operation: "update", Where: tc.where,
				Input: map[string]interface{}{"flow_review_json": review},
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v, want %q", err, tc.want)
			}
		})
	}
}

func TestWatchDefinitionChangesInvalidateOnlyRelevantApprovals(t *testing.T) {
	db, svc := newSQLiteWatchService(t, 10)
	if err := svc.fs.Put("/workflows/notify.js", []byte(`function main(input) { return {event: input.event}; }`)); err != nil {
		t.Fatalf("write workflow: %v", err)
	}
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)
	cp := newWatchControlPlane(svc)
	ctx := artifactUserCtx("invalidation_owner")

	for _, tc := range []struct {
		name   string
		mutate func(map[string]interface{})
	}{
		{
			name: "query",
			mutate: func(input map[string]interface{}) {
				input["query"] = cursorOrdersWatchQuery("query_changed")
			},
		},
		{
			name: "variables",
			mutate: func(input map[string]interface{}) {
				input["variables_json"] = map[string]any{"region": "west"}
			},
		},
		{
			name: "delivery",
			mutate: func(input map[string]interface{}) {
				input["delivery_json"] = map[string]any{"kind": "webhook", "url": "https://hooks.example.com/changed"}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			name := "invalidate_" + tc.name
			watch, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
				Table: watchesRootTable, Operation: "insert",
				Input: map[string]interface{}{
					"name": name, "query": cursorOrdersWatchQuery(name),
					"delivery_json": map[string]any{"kind": "workflow", "name": "notify"},
				},
			})
			if err != nil {
				t.Fatalf("insert action watch: %v", err)
			}
			approved := approveWatchActionForTest(t, cp, ctx, watch)
			oldHash := stringFromAny(approved["action_hash"])
			input := map[string]interface{}{
				"name": name, "query": cursorOrdersWatchQuery(name),
				"delivery_json": map[string]any{"kind": "workflow", "name": "notify"},
			}
			tc.mutate(input)
			changed, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
				Table: watchesRootTable, Operation: "update", Input: input,
			})
			if err != nil {
				t.Fatalf("change action definition: %v", err)
			}
			if changed["action_hash"] == oldHash || changed["action_approval"] != watchReviewPending ||
				changed["approval"] != watchReviewPending || changed["status"] != "paused" || changed["enabled"] != false {
				t.Fatalf("change did not invalidate action approval: %+v", changed)
			}
			if changed["flow_approval"] != watchReviewNotRequired {
				t.Fatalf("deterministic action unexpectedly requires flow review: %+v", changed)
			}
		})
	}

	client := &watchFlowTestClient{content: `{"verdict":"notify","severity":"warn","summary":"Review the changed flow."}`}
	svc.conf.Agent = AgentConfig{Enabled: true, Provider: "openai", APIKeyEnv: "IGNORED", TimeoutSeconds: 5}
	svc.agentClientFactory = func(gjagent.Config) (ax.AIClient, error) { return client, nil }
	flowWatch := insertFlowReviewWatch(t, cp, ctx, "invalidate_flow", "notify")
	flowApproved, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: watchesRootTable, Operation: "update",
		Where: map[string]interface{}{"id": map[string]interface{}{"eq": flowWatch["id"]}},
		Input: map[string]interface{}{"flow_review_json": map[string]any{
			"decision": "approve", "expected_flow_hash": flowWatch["flow_hash"],
			"samples_json": []any{map[string]any{"temperature": 421}},
		}},
	})
	if err != nil {
		t.Fatalf("approve original flow: %v", err)
	}
	fullyApproved := approveWatchActionForTest(t, cp, ctx, flowApproved)
	alternateFlow := `flowchart TD
  %%ax triage_v2: event:json, watch:json, evidence:json -> verdict:class "notify, digest, discard", severity:class "info, warn, critical", summary:string(max 280)
  triage_v2`
	changed, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: watchesRootTable, Operation: "update",
		Input: map[string]interface{}{
			"name": "invalidate_flow", "query": cursorOrdersWatchQuery("invalidate_flow"),
			"enrich_json":   map[string]any{"enabled": true, "kind": "flow", "flow": alternateFlow},
			"delivery_json": map[string]any{"kind": "workflow", "name": "notify"},
		},
	})
	if err != nil {
		t.Fatalf("change flow: %v", err)
	}
	if changed["flow_hash"] == fullyApproved["flow_hash"] ||
		changed["action_hash"] == fullyApproved["action_hash"] ||
		changed["flow_approval"] != watchReviewPending ||
		changed["action_approval"] != watchReviewPending ||
		changed["approval"] != watchReviewPending ||
		changed["status"] != "paused" || changed["enabled"] != false {
		t.Fatalf("flow change did not invalidate both approvals: %+v", changed)
	}
}

func TestWatchActionHashCoversBehaviorAndWorkflowDrift(t *testing.T) {
	db, svc := newSQLiteWatchService(t, 2)
	if err := svc.fs.Put("/workflows/notify.js", []byte(`function main(input) { return {version: 1}; }`)); err != nil {
		t.Fatalf("write workflow: %v", err)
	}
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)
	ctx := artifactUserCtx("hash_owner")
	query := cursorOrdersWatchQuery("hash_watch")
	base, err := svc.watchActionProposal(ctx, query, "", `{"region":"west"}`, "", `{"kind":"workflow","name":"notify"}`)
	if err != nil {
		t.Fatalf("base proposal: %v", err)
	}
	flowJSON, _, _, err := normalizeWatchEnrichmentJSON(`{"enabled":true,"kind":"flow","flow":"default_watch_triage"}`)
	if err != nil {
		t.Fatalf("normalize flow: %v", err)
	}
	cases := []struct {
		name      string
		query     string
		variables string
		enrich    string
		delivery  string
	}{
		{name: "query", query: query + "\n# changed", variables: `{"region":"west"}`, delivery: `{"kind":"workflow","name":"notify"}`},
		{name: "variables", query: query, variables: `{"region":"east"}`, delivery: `{"kind":"workflow","name":"notify"}`},
		{name: "flow", query: query, variables: `{"region":"west"}`, enrich: flowJSON, delivery: `{"kind":"workflow","name":"notify"}`},
		{name: "delivery", query: query, variables: `{"region":"west"}`, delivery: `{"kind":"webhook","url":"https://hooks.example.com/watch"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			proposal, err := svc.watchActionProposal(ctx, tc.query, "", tc.variables, tc.enrich, tc.delivery)
			if err != nil {
				t.Fatalf("proposal: %v", err)
			}
			if proposal.Hash == base.Hash {
				t.Fatalf("%s did not change action hash %s", tc.name, base.Hash)
			}
		})
	}

	cp := newWatchControlPlane(svc)
	watch, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: watchesRootTable, Operation: "insert",
		Input: map[string]interface{}{
			"name": "hash_watch", "query": query,
			"delivery_json": map[string]any{"kind": "workflow", "name": "notify"},
		},
	})
	if err != nil {
		t.Fatalf("insert watch: %v", err)
	}
	watch = approveWatchActionForTest(t, cp, ctx, watch)
	oldHash := stringFromAny(watch["action_hash"])

	if err := svc.fs.Put("/workflows/notify.js", []byte(`function main(input) { return {version: 2}; }`)); err != nil {
		t.Fatalf("change workflow: %v", err)
	}
	svc.invalidateWorkflowRegistry()
	stored, err := svc.internalWatchStoreRow(ctx, stringFromAny(watch["id"]))
	if err != nil {
		t.Fatalf("load watch: %v", err)
	}
	projected := cp.projectWatchRow(ctx, stored, false)
	if projected["action_hash"] == oldHash || projected["action_approval"] != watchReviewPending {
		t.Fatalf("workflow drift was not projected as pending: old=%s row=%+v", oldHash, projected)
	}
	if _, err := svc.loadRunnableWatches(context.Background()); err != nil {
		t.Fatalf("load runnable watches: %v", err)
	}
	stored, err = svc.internalWatchStoreRow(ctx, stringFromAny(watch["id"]))
	if err != nil {
		t.Fatalf("reload watch: %v", err)
	}
	if stringMapValue(stored, "approval") != watchReviewPending ||
		stringMapValue(stored, "status") != "paused" || boolMapValue(stored, "enabled") {
		t.Fatalf("workflow drift did not pause watch: %+v", stored)
	}
}

func TestWatchFlowFailureNotifiesButDoesNotRunAction(t *testing.T) {
	db, svc := newSQLiteWatchService(t, 2)
	if err := svc.fs.Put("/workflows/notify.js", []byte(`function main(input) { return {ran: true}; }`)); err != nil {
		t.Fatalf("write workflow: %v", err)
	}
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)
	client := &watchFlowTestClient{content: `{"verdict":"notify","severity":"critical","summary":"Preview succeeds."}`}
	svc.conf.Agent = AgentConfig{Enabled: true, Provider: "openai", APIKeyEnv: "IGNORED", TimeoutSeconds: 5}
	svc.agentClientFactory = func(gjagent.Config) (ax.AIClient, error) { return client, nil }
	cp := newWatchControlPlane(svc)
	ctx := artifactUserCtx("fail_closed_owner")
	watch := insertFlowReviewWatch(t, cp, ctx, "fail_closed", "notify")
	watchID := stringFromAny(watch["id"])
	flowApproved, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: watchesRootTable, Operation: "update",
		Where: map[string]interface{}{"id": map[string]interface{}{"eq": watchID}},
		Input: map[string]interface{}{"flow_review_json": map[string]any{
			"decision": "approve", "expected_flow_hash": watch["flow_hash"],
			"samples_json": []any{map[string]any{"temperature": 421}},
		}},
	})
	if err != nil {
		t.Fatalf("approve flow: %v", err)
	}
	approved := approveWatchActionForTest(t, cp, ctx, flowApproved)

	svc.agentClientFactory = func(gjagent.Config) (ax.AIClient, error) {
		return nil, errors.New("provider unavailable")
	}
	def := watchRuntimeDefinition{
		ID: stringFromAny(approved["id"]), Name: "fail_closed",
		Query: cursorOrdersWatchQuery("fail_closed"), DeliveryJSON: `{"kind":"workflow","name":"notify"}`,
		EnrichJSON: jsonMapString(approved, "enrich_json"), OwnerID: "fail_closed_owner", OwnerRole: "user",
	}
	if _, inserted, err := svc.persistWatchResult(ctx, &def, &core.Result{Data: json.RawMessage(`{"data":{"orders":[{"id":9}]}}`)}); err != nil {
		t.Fatalf("persist flow failure: %v", err)
	} else if !inserted {
		t.Fatal("expected failed-flow event")
	}
	var status string
	var receipt any
	if err := db.QueryRow(`SELECT delivery_status, receipt_json FROM "_graphjin_watch_events"`).Scan(&status, &receipt); err != nil {
		t.Fatalf("query event: %v", err)
	}
	if status != "flow_failed" || receipt != nil {
		t.Fatalf("flow failure status=%q receipt=%v", status, receipt)
	}
	if err := svc.processPendingWatchDeliveries(context.Background()); err != nil {
		t.Fatalf("process deliveries: %v", err)
	}
	if err := db.QueryRow(`SELECT delivery_status, receipt_json FROM "_graphjin_watch_events"`).Scan(&status, &receipt); err != nil {
		t.Fatalf("query event after delivery loop: %v", err)
	}
	if status != "flow_failed" || receipt != nil {
		t.Fatalf("autonomous action ran after flow failure: status=%q receipt=%v", status, receipt)
	}
}

func insertFlowReviewWatch(
	t *testing.T,
	cp watchControlPlane,
	ctx context.Context,
	name, workflowName string,
) map[string]any {
	t.Helper()
	input := map[string]interface{}{
		"name": name, "query": cursorOrdersWatchQuery(name),
		"enrich_json": map[string]any{"enabled": true, "kind": "flow", "flow": "default_watch_triage"},
	}
	if workflowName != "" {
		input["delivery_json"] = map[string]any{"kind": "workflow", "name": workflowName}
	}
	watch, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: watchesRootTable, Operation: "insert", Input: input,
	})
	if err != nil {
		t.Fatalf("insert flow review watch: %v", err)
	}
	return watch
}
