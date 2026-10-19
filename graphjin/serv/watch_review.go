package serv

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	ax "github.com/ax-llm/ax/packages/go"
	"github.com/dosco/graphjin/core/v3"
)

const (
	watchFlowReviewKey   = "flow_review"
	watchActionReviewKey = "action_review"
	watchReviewResumeKey = "review_resume"

	watchReviewPending     = "pending"
	watchReviewApproved    = "approved"
	watchReviewRejected    = "rejected"
	watchReviewNotRequired = "not_required"
)

type watchActionProposal struct {
	Required           bool
	Hash               string
	Kind               string
	WorkflowName       string
	WorkflowSourceHash string
}

func normalizeWatchDeliveryJSON(raw string) (string, watchDeliveryConfig, bool, error) {
	cfg, enabled, err := parseWatchDeliveryConfig(raw)
	if err != nil {
		return "", watchDeliveryConfig{}, false, err
	}
	value := map[string]any{"kind": cfg.Kind}
	if cfg.Digest.Enabled {
		value["digest"] = map[string]any{"window": cfg.Digest.WindowText}
	}
	if !enabled {
		if !cfg.Digest.Enabled && (strings.TrimSpace(raw) == "" || strings.TrimSpace(raw) == "null" || strings.TrimSpace(raw) == "{}") {
			return "", cfg, false, nil
		}
		out, err := json.Marshal(value)
		if err != nil {
			return "", watchDeliveryConfig{}, false, err
		}
		return string(out), cfg, false, nil
	}
	switch cfg.Kind {
	case "webhook":
		value["url"] = cfg.Webhook.URL
		if cfg.Webhook.SecretEnv != "" {
			value["secret_env"] = cfg.Webhook.SecretEnv
		}
		if len(cfg.Webhook.Headers) != 0 {
			value["headers"] = cfg.Webhook.Headers
		}
	case "workflow":
		value["name"] = cfg.Workflow.Name
	}
	out, err := json.Marshal(value)
	if err != nil {
		return "", watchDeliveryConfig{}, false, err
	}
	return string(out), cfg, true, nil
}

func (s *graphjinService) watchActionProposal(
	ctx context.Context,
	query, savedQuery, variablesJSON, enrichJSON, deliveryJSON, absenceJSON string,
) (watchActionProposal, error) {
	normalizedDelivery, deliveryCfg, enabled, err := normalizeWatchDeliveryJSON(deliveryJSON)
	if err != nil {
		return watchActionProposal{}, err
	}
	if !enabled || deliveryCfg.Kind == "inbox" {
		return watchActionProposal{}, nil
	}

	queryKind := "inline"
	queryName := ""
	querySource := strings.TrimSpace(query)
	if strings.TrimSpace(savedQuery) != "" {
		if s == nil {
			return watchActionProposal{}, fmt.Errorf("saved-query action hashing requires an initialized service")
		}
		details, _, err := s.getSavedQueryForContext(ctx, savedQuery)
		if err != nil {
			return watchActionProposal{}, fmt.Errorf("resolve watch saved query %q: %w", savedQuery, err)
		}
		queryKind = "saved"
		queryName = strings.TrimSpace(savedQuery)
		querySource = strings.TrimSpace(details.Query)
	}
	if querySource == "" {
		return watchActionProposal{}, fmt.Errorf("watch action hashing requires query content")
	}

	var variables any = map[string]any{}
	if strings.TrimSpace(variablesJSON) != "" && strings.TrimSpace(variablesJSON) != "null" {
		if err := json.Unmarshal([]byte(variablesJSON), &variables); err != nil {
			return watchActionProposal{}, fmt.Errorf("variables_json is invalid: %w", err)
		}
	}
	_, enrichCfg, enrichEnabled, err := normalizeWatchEnrichmentJSON(enrichJSON)
	if err != nil {
		return watchActionProposal{}, err
	}
	flow := map[string]any{"enabled": false}
	if enrichEnabled && enrichCfg.Kind == "flow" {
		flow = map[string]any{
			"enabled":   true,
			"kind":      "flow",
			"flow_hash": enrichCfg.FlowHash,
		}
	}

	proposal := watchActionProposal{
		Required: true,
		Kind:     deliveryCfg.Kind,
	}
	action := map[string]any{
		"query": map[string]any{
			"kind":        queryKind,
			"name":        queryName,
			"source_hash": workflowHashJSON(querySource),
		},
		"variables": variables,
		"flow":      flow,
		"delivery":  parseJSONValue(normalizedDelivery),
	}
	normalizedAbsence, _, absenceEnabled, err := normalizeWatchAbsenceJSON(absenceJSON)
	if err != nil {
		return watchActionProposal{}, err
	}
	if absenceEnabled {
		action["absence"] = parseJSONValue(normalizedAbsence)
	}
	if deliveryCfg.Kind == "workflow" {
		proposal.WorkflowName = strings.TrimSpace(deliveryCfg.Workflow.Name)
		if s == nil {
			return watchActionProposal{}, fmt.Errorf("workflow action hashing requires an initialized service")
		}
		wf, _, _, err := s.resolveWorkflowForContext(ctx, proposal.WorkflowName)
		if err != nil {
			return watchActionProposal{}, fmt.Errorf("resolve watch workflow %q: %w", proposal.WorkflowName, err)
		}
		proposal.WorkflowSourceHash = wf.SourceHash
		action["workflow_source_hash"] = wf.SourceHash
	}
	proposal.Hash = workflowHashJSON(action)
	return proposal, nil
}

func watchFlowApproval(evidence map[string]any, cfg watchEnrichmentConfig, required bool) string {
	if !required {
		return watchReviewNotRequired
	}
	review := mapFromAny(evidence[watchFlowReviewKey])
	if strings.TrimSpace(stringFromAny(review["flow_hash"])) != strings.TrimSpace(cfg.FlowHash) {
		return watchReviewPending
	}
	return normalizedWatchReviewDecision(stringFromAny(review["approval"]))
}

func watchActionApproval(evidence map[string]any, proposal watchActionProposal) string {
	if !proposal.Required {
		return watchReviewNotRequired
	}
	review := mapFromAny(evidence[watchActionReviewKey])
	if strings.TrimSpace(stringFromAny(review["action_hash"])) != strings.TrimSpace(proposal.Hash) {
		return watchReviewPending
	}
	return normalizedWatchReviewDecision(stringFromAny(review["approval"]))
}

func normalizedWatchReviewDecision(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case watchReviewApproved:
		return watchReviewApproved
	case watchReviewRejected:
		return watchReviewRejected
	default:
		return watchReviewPending
	}
}

func combinedWatchApproval(flowApproval, actionApproval string) string {
	for _, value := range []string{flowApproval, actionApproval} {
		if value == watchReviewRejected {
			return watchReviewRejected
		}
	}
	for _, value := range []string{flowApproval, actionApproval} {
		if value == watchReviewPending {
			return watchReviewPending
		}
	}
	return watchReviewApproved
}

func ensureWatchReviewEvidence(
	evidence map[string]any,
	enrichCfg watchEnrichmentConfig,
	flowRequired bool,
	proposal watchActionProposal,
	now string,
) {
	if flowRequired {
		review := mapFromAny(evidence[watchFlowReviewKey])
		if strings.TrimSpace(stringFromAny(review["flow_hash"])) != strings.TrimSpace(enrichCfg.FlowHash) {
			evidence[watchFlowReviewKey] = map[string]any{
				"flow_hash":  enrichCfg.FlowHash,
				"approval":   watchReviewPending,
				"updated_at": now,
			}
		}
	} else {
		delete(evidence, watchFlowReviewKey)
	}
	if proposal.Required {
		review := mapFromAny(evidence[watchActionReviewKey])
		if strings.TrimSpace(stringFromAny(review["action_hash"])) != strings.TrimSpace(proposal.Hash) {
			evidence[watchActionReviewKey] = actionReviewEvidence(proposal, watchReviewPending, now)
		}
	} else {
		delete(evidence, watchActionReviewKey)
	}
}

func actionReviewEvidence(proposal watchActionProposal, approval, now string) map[string]any {
	out := map[string]any{
		"action_hash":   proposal.Hash,
		"approval":      normalizedWatchReviewDecision(approval),
		"delivery_kind": proposal.Kind,
		"updated_at":    now,
	}
	if proposal.WorkflowName != "" {
		out["workflow_name"] = proposal.WorkflowName
		out["workflow_source_hash"] = proposal.WorkflowSourceHash
	}
	return out
}

func watchReviewInput(value any) (map[string]any, error) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, nil
	case string:
		var out map[string]any
		if err := json.Unmarshal([]byte(typed), &out); err != nil {
			return nil, err
		}
		return out, nil
	default:
		normalized := mapFromAny(value)
		if normalized == nil {
			return nil, fmt.Errorf("review input must be a JSON object")
		}
		return normalized, nil
	}
}

func exactWatchReviewID(where map[string]interface{}) string {
	if len(where) != 1 {
		return ""
	}
	value, ok := where["id"].(map[string]interface{})
	if !ok || len(value) != 1 {
		return ""
	}
	id, ok := value["eq"]
	if !ok {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(id))
}

func hasWatchReviewInput(input map[string]interface{}) bool {
	if input == nil {
		return false
	}
	_, flow := input["flow_review_json"]
	_, action := input["action_review_json"]
	return flow || action
}

func hasWatchActionReviewInput(input map[string]interface{}) bool {
	if input == nil {
		return false
	}
	_, ok := input["action_review_json"]
	return ok
}

func (h watchControlPlane) reviewWatch(ctx context.Context, root core.ManagedMutationRoot) (map[string]any, error) {
	if root.Operation != "update" {
		return nil, fmt.Errorf("gj_watch review controls require update")
	}
	if len(root.Input) != 1 {
		return nil, fmt.Errorf("gj_watch review controls cannot be combined with definition changes")
	}
	_, hasFlow := root.Input["flow_review_json"]
	_, hasAction := root.Input["action_review_json"]
	if hasFlow == hasAction {
		return nil, fmt.Errorf("gj_watch update requires exactly one review control")
	}
	watchID := exactWatchReviewID(root.Where)
	if watchID == "" {
		return nil, fmt.Errorf("gj_watch review requires where: { id: { eq: \"...\" } }")
	}
	ownerID, ok := artifactUserID(ctx)
	if !ok {
		return nil, fmt.Errorf("gj_watch review requires user identity")
	}
	watchRow, err := h.service.internalWatchStoreRow(ctx, watchID)
	if err != nil {
		return nil, err
	}
	admin := h.service.identityRoleIsAdmin(ctx)
	if watchRow == nil || (!admin && stringMapValue(watchRow, "owner_id") != ownerID) {
		return nil, fmt.Errorf("gj_watch review denied")
	}

	evidence := mapFromAny(parseJSONValue(jsonMapString(watchRow, "evidence_json")))
	if evidence == nil {
		evidence = map[string]any{}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if hasFlow {
		review, err := watchReviewInput(root.Input["flow_review_json"])
		if err != nil {
			return nil, fmt.Errorf("gj_watch flow_review_json is invalid: %w", err)
		}
		if err := h.applyWatchFlowReview(ctx, watchRow, review, evidence, now); err != nil {
			return nil, err
		}
	} else {
		review, err := watchReviewInput(root.Input["action_review_json"])
		if err != nil {
			return nil, fmt.Errorf("gj_watch action_review_json is invalid: %w", err)
		}
		if err := h.applyWatchActionReview(ctx, watchRow, review, evidence, now); err != nil {
			return nil, err
		}
	}

	_, enrichCfg, enrichEnabled, err := normalizeWatchEnrichmentJSON(jsonMapString(watchRow, "enrich_json"))
	if err != nil {
		return nil, err
	}
	proposal, err := h.service.watchActionProposal(
		ctx,
		stringMapValue(watchRow, "query"),
		stringMapValue(watchRow, "saved_query_name"),
		jsonMapString(watchRow, "variables_json"),
		jsonMapString(watchRow, "enrich_json"),
		jsonMapString(watchRow, "delivery_json"),
		jsonMapString(watchRow, "absence_json"),
	)
	if err != nil {
		return nil, err
	}
	flowApproval := watchFlowApproval(evidence, enrichCfg, enrichEnabled && enrichCfg.Kind == "flow")
	actionApproval := watchActionApproval(evidence, proposal)
	approval := combinedWatchApproval(flowApproval, actionApproval)
	status := watchStatus(stringMapValue(watchRow, "status"))
	enabled := boolMapValue(watchRow, "enabled")
	resume := boolValue(evidence[watchReviewResumeKey])
	if approval != watchReviewApproved {
		status, enabled = "paused", false
	} else {
		if resume {
			status, enabled = "active", true
		}
		delete(evidence, watchReviewResumeKey)
	}

	previousUpdatedAt := stringMapValue(watchRow, "updated_at")
	rows, err := h.service.internalStoreMutationRows(ctx, "watches",
		`where: { id: { eq: $id }, updated_at: { eq: $expected_updated_at } }, update: $input`,
		watchStoreFields,
		map[string]any{
			"id":                  watchID,
			"expected_updated_at": previousUpdatedAt,
			"input": map[string]any{
				"evidence_json": nullableJSONString(mustMarshalString(evidence)),
				"approval":      approval,
				"status":        status,
				"enabled":       enabled,
				"updated_at":    now,
			},
		})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		current, reloadErr := h.service.internalWatchStoreRow(ctx, watchID)
		if reloadErr != nil {
			return nil, fmt.Errorf("gj_watch review is stale; reload the watch and retry: %w", reloadErr)
		}
		if stringMapValue(current, "updated_at") != now {
			return nil, fmt.Errorf(
				"gj_watch review is stale; reload the watch and retry (expected updated_at %q, current %q)",
				previousUpdatedAt,
				stringMapValue(current, "updated_at"),
			)
		}
		rows = []map[string]any{current}
	}
	if err := h.service.bumpArtifactRevision(ctx, "watches"); err != nil {
		return nil, err
	}
	h.service.markWatchChanged("watch review")
	h.service.publishWatchRunnerChanged(ctx)
	return h.projectWatchRow(ctx, rows[0], admin), nil
}

func (h watchControlPlane) applyWatchActionReview(
	ctx context.Context,
	watchRow map[string]any,
	review map[string]any,
	evidence map[string]any,
	now string,
) error {
	decision := strings.ToLower(strings.TrimSpace(stringFromAny(review["decision"])))
	if decision != "approve" && decision != "reject" {
		return fmt.Errorf("gj_watch action review decision must be approve or reject")
	}
	expectedHash := strings.TrimSpace(stringFromAny(review["expected_action_hash"]))
	if expectedHash == "" {
		return fmt.Errorf("gj_watch action review requires expected_action_hash")
	}
	proposal, err := h.service.watchActionProposal(
		ctx,
		stringMapValue(watchRow, "query"),
		stringMapValue(watchRow, "saved_query_name"),
		jsonMapString(watchRow, "variables_json"),
		jsonMapString(watchRow, "enrich_json"),
		jsonMapString(watchRow, "delivery_json"),
		jsonMapString(watchRow, "absence_json"),
	)
	if err != nil {
		return err
	}
	if !proposal.Required {
		return fmt.Errorf("gj_watch does not have a workflow or webhook action to review")
	}
	if expectedHash != proposal.Hash {
		return fmt.Errorf("gj_watch action review is stale: expected_action_hash %s does not match current action_hash %s", expectedHash, proposal.Hash)
	}
	approval := watchReviewApproved
	if decision == "reject" {
		approval = watchReviewRejected
	}
	evidence[watchActionReviewKey] = actionReviewEvidence(proposal, approval, now)
	return nil
}

func (h watchControlPlane) applyWatchFlowReview(
	ctx context.Context,
	watchRow map[string]any,
	review map[string]any,
	evidence map[string]any,
	now string,
) error {
	decision := strings.ToLower(strings.TrimSpace(stringFromAny(review["decision"])))
	switch decision {
	case "preview", "approve", "reject":
	default:
		return fmt.Errorf("gj_watch flow review decision must be preview, approve, or reject")
	}
	_, cfg, enabled, err := normalizeWatchEnrichmentJSON(jsonMapString(watchRow, "enrich_json"))
	if err != nil {
		return err
	}
	if !enabled || cfg.Kind != "flow" {
		return fmt.Errorf("gj_watch does not have an enabled flow to review")
	}
	expectedHash := strings.TrimSpace(stringFromAny(review["expected_flow_hash"]))
	if decision != "preview" && expectedHash == "" {
		return fmt.Errorf("gj_watch flow review requires expected_flow_hash")
	}
	if expectedHash != "" && expectedHash != cfg.FlowHash {
		return fmt.Errorf("gj_watch flow review is stale: expected_flow_hash %s does not match current flow_hash %s", expectedHash, cfg.FlowHash)
	}

	current := mapFromAny(evidence[watchFlowReviewKey])
	if strings.TrimSpace(stringFromAny(current["flow_hash"])) != cfg.FlowHash {
		current = map[string]any{"flow_hash": cfg.FlowHash, "approval": watchReviewPending}
	}
	if decision == "reject" {
		current["approval"] = watchReviewRejected
		current["reviewed_at"] = now
		current["updated_at"] = now
		evidence[watchFlowReviewKey] = current
		return nil
	}

	_, hasSamples := review["samples_json"]
	_, hasLimit := review["event_limit"]
	preview := mapFromAny(current["preview"])
	runPreview := decision == "preview" || hasSamples || hasLimit
	if decision == "approve" && !watchFlowPreviewMatches(preview, cfg.FlowHash) {
		runPreview = true
	}
	if runPreview {
		preview, err = h.runWatchFlowPreview(ctx, watchRow, cfg, review)
		if err != nil {
			return err
		}
		current["preview"] = preview
	}
	if decision == "approve" {
		if !watchFlowPreviewMatches(preview, cfg.FlowHash) {
			current["approval"] = watchReviewPending
		} else {
			current["approval"] = watchReviewApproved
			current["reviewed_at"] = now
		}
	} else if strings.TrimSpace(stringFromAny(current["approval"])) == "" {
		current["approval"] = watchReviewPending
	}
	current["flow_hash"] = cfg.FlowHash
	current["updated_at"] = now
	evidence[watchFlowReviewKey] = current
	return nil
}

func watchFlowPreviewMatches(preview map[string]any, flowHash string) bool {
	return strings.EqualFold(strings.TrimSpace(stringFromAny(preview["status"])), "ok") &&
		strings.TrimSpace(stringFromAny(preview["flow_hash"])) == strings.TrimSpace(flowHash)
}

func (h watchControlPlane) runWatchFlowPreview(
	ctx context.Context,
	watchRow map[string]any,
	cfg watchEnrichmentConfig,
	review map[string]any,
) (map[string]any, error) {
	watchID := stringMapValue(watchRow, "id")
	samples, err := h.watchFlowReviewSamples(ctx, review, watchID)
	if err != nil {
		return nil, err
	}
	if len(samples) == 0 {
		return nil, fmt.Errorf("gj_watch flow review requires samples_json or prior watch events")
	}
	started := time.Now()
	results := make([]map[string]any, 0, len(samples))
	usages := make([]any, 0, len(samples))
	counts := map[string]int{"notify": 0, "digest": 0, "discard": 0}
	for index, sample := range samples {
		run, runErr := h.service.runWatchFlow(ctx, cfg, map[string]ax.Value{
			"event":    sample,
			"watch":    map[string]any{"id": watchID, "name": stringMapValue(watchRow, "name")},
			"evidence": parseJSONValue(jsonMapString(watchRow, "evidence_json")),
		})
		if runErr != nil {
			h.service.recordWatchFlowRuntimeEvent(ctx, watchID, cfg.FlowHash, "preview", "failed", runErr.Error(), time.Since(started), map[string]any{"failed_sample": index})
			return map[string]any{
				"flow_hash": cfg.FlowHash, "previewed_at": time.Now().UTC().Format(time.RFC3339Nano),
				"sample_count": len(samples), "status": "failed", "result_json": results, "usage_json": usages,
				"error": runErr.Error(), "duration_ms": time.Since(started).Milliseconds(),
			}, nil
		}
		counts[run.Verdict]++
		results = append(results, map[string]any{
			"index": index, "verdict": run.Verdict, "severity": run.Severity,
			"summary": run.Summary, "duration_ms": run.Duration.Milliseconds(),
		})
		usages = append(usages, run.Usage)
	}
	preview := map[string]any{
		"flow_hash": cfg.FlowHash, "previewed_at": time.Now().UTC().Format(time.RFC3339Nano),
		"sample_count": len(samples), "notify_count": counts["notify"], "digest_count": counts["digest"],
		"discard_count": counts["discard"], "status": "ok", "result_json": results,
		"usage_json": usages, "error": "", "duration_ms": time.Since(started).Milliseconds(),
	}
	h.service.recordWatchFlowRuntimeEvent(ctx, watchID, cfg.FlowHash, "preview", "ok", "", time.Since(started), preview)
	return preview, nil
}

func (h watchControlPlane) projectWatchRow(ctx context.Context, row map[string]any, rawIDs bool) map[string]any {
	out := watchStoreRow(row, rawIDs)
	proposal, err := h.service.watchActionProposal(
		ctx,
		stringMapValue(row, "query"),
		stringMapValue(row, "saved_query_name"),
		jsonMapString(row, "variables_json"),
		jsonMapString(row, "enrich_json"),
		jsonMapString(row, "delivery_json"),
		jsonMapString(row, "absence_json"),
	)
	if err != nil {
		if strings.TrimSpace(stringFromAny(out["action_hash"])) != "" {
			out["action_approval"] = watchReviewPending
		}
		return out
	}
	evidence := mapFromAny(parseJSONValue(jsonMapString(row, "evidence_json")))
	if evidence == nil {
		evidence = map[string]any{}
	}
	out["action_hash"] = proposal.Hash
	out["action_approval"] = watchActionApproval(evidence, proposal)
	return out
}

func (s *graphjinService) pauseWatchForReview(ctx context.Context, row map[string]any, approval string) error {
	if s == nil || row == nil {
		return nil
	}
	approval = normalizedWatchReviewDecision(approval)
	if watchStatus(stringMapValue(row, "status")) == "paused" &&
		!boolMapValue(row, "enabled") &&
		watchApproval(stringMapValue(row, "approval")) == approval {
		return nil
	}
	evidence := mapFromAny(parseJSONValue(jsonMapString(row, "evidence_json")))
	if evidence == nil {
		evidence = map[string]any{}
	}
	if _, ok := evidence[watchReviewResumeKey]; !ok {
		evidence[watchReviewResumeKey] = watchStatus(stringMapValue(row, "status")) == "active" && boolMapValue(row, "enabled")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	rows, err := s.internalStoreMutationRows(ctx, "watches",
		`where: { id: { eq: $id }, updated_at: { eq: $expected_updated_at } }, update: $input`,
		watchStoreFields,
		map[string]any{
			"id":                  stringMapValue(row, "id"),
			"expected_updated_at": stringMapValue(row, "updated_at"),
			"input": map[string]any{
				"evidence_json": nullableJSONString(mustMarshalString(evidence)),
				"approval":      approval,
				"status":        "paused",
				"enabled":       false,
				"updated_at":    now,
			},
		})
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		current, reloadErr := s.internalWatchStoreRow(ctx, stringMapValue(row, "id"))
		if reloadErr != nil || stringMapValue(current, "updated_at") != now {
			return reloadErr
		}
		rows = []map[string]any{current}
	}
	if err := s.bumpArtifactRevision(ctx, "watches"); err != nil {
		return err
	}
	s.markWatchChanged("watch approval reconciliation")
	s.publishWatchRunnerChanged(ctx)
	return nil
}
