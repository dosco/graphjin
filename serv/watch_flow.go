package serv

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	ax "github.com/ax-llm/ax/packages/go"
	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/core/v3"
)

const (
	watchFlowPreviewRootTable = "gj_watch_flow_preview"
	defaultWatchTriageFlow    = `flowchart TD
  %%ax triage: event:json, watch:json, evidence:json -> verdict:class "notify, digest, discard", severity:class "info, warn, critical", summary:string(max 280)
  triage`
	defaultWatchFlowMaxCalls = 4
	defaultWatchFlowSamples  = 20
)

type watchFlowResult struct {
	Verdict  string `json:"verdict"`
	Severity string `json:"severity"`
	Summary  string `json:"summary"`
}

type watchFlowRun struct {
	watchFlowResult
	FlowHash   string
	Usage      any
	Duration   time.Duration
	ModelCalls int
}

func watchFlowPreviewColumns() []core.ManagedColumn {
	return []core.ManagedColumn{
		cpCol("id", "text", true),
		cpCol("watch_id", "text", false),
		cpCol("samples_json", "json", false),
		cpCol("event_limit", "integer", false),
		cpCol("approve", "boolean", false),
		cpCol("flow_hash", "text", false),
		cpCol("sample_count", "integer", false),
		cpCol("notify_count", "integer", false),
		cpCol("digest_count", "integer", false),
		cpCol("discard_count", "integer", false),
		cpCol("status", "text", false),
		cpCol("result_json", "json", false),
		cpCol("usage_json", "json", false),
		cpCol("error", "text", false),
		cpCol("duration_ms", "integer", false),
		cpCol("approved", "boolean", false),
		cpCol("created_at", "text", false),
	}
}

func watchFlowPreviewNanoColumns() []core.NanoColumn {
	return []core.NanoColumn{
		{Name: "id", Type: "text", PrimaryKey: true, NotNull: true},
		{Name: "watch_id", Type: "text", Index: true, FKeyTable: "gj_watch", FKeyColumn: "id", FKeyUnique: true},
		{Name: "samples_json", Type: "json"},
		{Name: "event_limit", Type: "integer"},
		{Name: "approve", Type: "boolean"},
		{Name: "flow_hash", Type: "text", Index: true},
		{Name: "sample_count", Type: "integer"},
		{Name: "notify_count", Type: "integer"},
		{Name: "digest_count", Type: "integer"},
		{Name: "discard_count", Type: "integer"},
		{Name: "status", Type: "text", Index: true},
		{Name: "result_json", Type: "json"},
		{Name: "usage_json", Type: "json"},
		{Name: "error", Type: "text"},
		{Name: "duration_ms", Type: "integer"},
		{Name: "approved", Type: "boolean"},
		{Name: "created_at", Type: "text"},
	}
}

// canonicalWatchFlow compiles Mermaid through AxFlow and returns Ax's stable
// rendering. The recover is intentional: NewFlow currently exposes parser
// failures as panics through its generated mustCore bridge.
func canonicalWatchFlow(source string) (canonical, hash string, err error) {
	source = strings.TrimSpace(source)
	if source == "default_watch_triage" {
		source = defaultWatchTriageFlow
	}
	if source == "" {
		return "", "", fmt.Errorf("watch flow is empty")
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			canonical, hash = "", ""
			err = fmt.Errorf("invalid watch flow: %v", recovered)
		}
	}()
	flow := ax.NewFlow(source)
	canonical = strings.TrimSpace(flow.String())
	if canonical == "" {
		return "", "", fmt.Errorf("watch flow rendered empty")
	}
	if !watchFlowHasTriageContract(canonical) {
		return "", "", fmt.Errorf("watch flow must return verdict:class \"notify, digest, discard\", severity:class \"info, warn, critical\", and summary:string(max 280); rendered flow: %s", canonical)
	}
	sum := sha256.Sum256([]byte(canonical))
	return canonical, hex.EncodeToString(sum[:]), nil
}

func watchFlowHasTriageContract(canonical string) bool {
	compact := strings.Join(strings.Fields(canonical), " ")
	compact = strings.ReplaceAll(compact, " | ", ", ")
	return strings.Contains(compact, `verdict:class "notify, digest, discard"`) &&
		strings.Contains(compact, `severity:class "info, warn, critical"`) &&
		strings.Contains(compact, `summary:string(max 280)`)
}

func watchFlowPreviewEvidenceMatches(evidence map[string]any, flowHash string) bool {
	preview := mapFromAny(evidence["flow_preview"])
	return strings.EqualFold(strings.TrimSpace(stringFromAny(preview["status"])), "ok") &&
		strings.TrimSpace(stringFromAny(preview["flow_hash"])) == strings.TrimSpace(flowHash)
}

// normalizeWatchEnrichmentJSON validates flow-backed enrichment and stores the
// canonical Mermaid inline with the watch. Legacy agent enrichment is retained
// unchanged apart from stable JSON encoding.
func normalizeWatchEnrichmentJSON(raw string) (string, watchEnrichmentConfig, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" || raw == "{}" {
		return "", watchEnrichmentConfig{}, false, nil
	}
	var value map[string]any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return "", watchEnrichmentConfig{}, false, fmt.Errorf("enrich_json is invalid: %w", err)
	}
	enabled, _ := value["enabled"].(bool)
	kind := strings.ToLower(strings.TrimSpace(stringFromAny(value["kind"])))
	if kind == "flow" {
		flowRef := strings.TrimSpace(stringFromAny(value["flow"]))
		canonical, flowHash, err := canonicalWatchFlow(flowRef)
		if err != nil {
			return "", watchEnrichmentConfig{}, false, err
		}
		storedFlow := canonical
		if flowRef == "default_watch_triage" {
			storedFlow = flowRef
		}
		value = map[string]any{
			"enabled":   enabled,
			"kind":      "flow",
			"flow":      storedFlow,
			"flow_hash": flowHash,
		}
		out, _ := json.Marshal(value)
		return string(out), watchEnrichmentConfig{
			Enabled: enabled, Kind: "flow", Flow: storedFlow,
			CanonicalFlow: canonical, FlowHash: flowHash,
		}, enabled, nil
	}
	out, err := json.Marshal(value)
	if err != nil {
		return "", watchEnrichmentConfig{}, false, err
	}
	cfg := watchEnrichmentConfig{
		Enabled:     enabled,
		Kind:        "agent",
		Instruction: stringFromAny(value["instruction"]),
		MaxSteps:    intFromAny(value["max_steps"]),
	}
	return string(out), cfg, enabled, nil
}

func (s *graphjinService) runWatchFlow(ctx context.Context, cfg watchEnrichmentConfig, inputs map[string]ax.Value) (watchFlowRun, error) {
	started := time.Now()
	if cfg.Kind != "flow" || !cfg.Enabled {
		return watchFlowRun{}, fmt.Errorf("watch flow is not enabled")
	}
	canonical := cfg.CanonicalFlow
	flowHash := cfg.FlowHash
	if canonical == "" || flowHash == "" {
		var err error
		canonical, flowHash, err = canonicalWatchFlow(cfg.Flow)
		if err != nil {
			return watchFlowRun{}, err
		}
	}
	if s == nil || s.conf == nil {
		return watchFlowRun{}, fmt.Errorf("watch flow service is not configured")
	}
	agentConf := agentConfigFromService(s.conf)
	agentConf.Sampling = gjagent.SamplingOff
	var client ax.AIClient
	var err error
	if s.agentClientFactory != nil {
		client, err = s.agentClientFactory(agentConf)
	} else {
		client, err = gjagent.DefaultClientFactory(agentConf)
	}
	if err != nil {
		return watchFlowRun{}, err
	}
	limited := &watchFlowAIClient{inner: client, maxCalls: defaultWatchFlowMaxCalls}
	timeout := time.Duration(gjagent.EffectiveTimeoutSeconds(agentConf.TimeoutSeconds)) * time.Second
	flowCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	flow := ax.NewFlow(canonical)
	output, err := flow.Forward(flowCtx, limited, inputs, nil)
	if err != nil {
		return watchFlowRun{}, err
	}
	plain, err := watchFlowPlainValue(output)
	if err != nil {
		return watchFlowRun{}, err
	}
	resultMap, ok := plain.(map[string]any)
	if !ok {
		return watchFlowRun{}, fmt.Errorf("watch flow returned %T, want object", plain)
	}
	result := watchFlowResult{
		Verdict:  strings.ToLower(strings.TrimSpace(stringFromAny(resultMap["verdict"]))),
		Severity: strings.ToLower(strings.TrimSpace(stringFromAny(resultMap["severity"]))),
		Summary:  strings.TrimSpace(stringFromAny(resultMap["summary"])),
	}
	if err := validateWatchFlowResult(result); err != nil {
		return watchFlowRun{}, err
	}
	return watchFlowRun{
		watchFlowResult: result,
		FlowHash:        flowHash,
		Usage:           limited.usageValue(),
		Duration:        time.Since(started),
		ModelCalls:      limited.calls,
	}, nil
}

func validateWatchFlowResult(result watchFlowResult) error {
	switch result.Verdict {
	case "notify", "digest", "discard":
	default:
		return fmt.Errorf("watch flow verdict %q is invalid", result.Verdict)
	}
	switch result.Severity {
	case "info", "warn", "critical":
	default:
		return fmt.Errorf("watch flow severity %q is invalid", result.Severity)
	}
	if result.Summary == "" {
		return fmt.Errorf("watch flow summary is empty")
	}
	if len([]rune(result.Summary)) > 280 {
		return fmt.Errorf("watch flow summary exceeds 280 characters")
	}
	return nil
}

func watchFlowPlainValue(value any) (any, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

type watchFlowAIClient struct {
	inner    ax.AIClient
	maxCalls int
	mu       sync.Mutex
	calls    int
	usage    []any
}

func (c *watchFlowAIClient) Chat(ctx context.Context, request, options map[string]ax.Value) (ax.Value, error) {
	if err := c.claim(); err != nil {
		return nil, err
	}
	value, err := c.inner.Chat(ctx, request, options)
	c.captureUsage(value)
	return value, err
}

func (c *watchFlowAIClient) Embed(ctx context.Context, request, options map[string]ax.Value) (ax.Value, error) {
	if err := c.claim(); err != nil {
		return nil, err
	}
	value, err := c.inner.Embed(ctx, request, options)
	c.captureUsage(value)
	return value, err
}

func (c *watchFlowAIClient) Stream(ctx context.Context, request, options map[string]ax.Value) ([]ax.Value, error) {
	if err := c.claim(); err != nil {
		return nil, err
	}
	values, err := c.inner.Stream(ctx, request, options)
	for _, value := range values {
		c.captureUsage(value)
	}
	return values, err
}

func (c *watchFlowAIClient) claim() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.calls >= c.maxCalls {
		return fmt.Errorf("watch flow exceeded %d model calls", c.maxCalls)
	}
	c.calls++
	return nil
}

func (c *watchFlowAIClient) captureUsage(value ax.Value) {
	plain, err := watchFlowPlainValue(value)
	if err != nil {
		return
	}
	m, ok := plain.(map[string]any)
	if !ok || m["model_usage"] == nil {
		return
	}
	c.mu.Lock()
	c.usage = append(c.usage, m["model_usage"])
	c.mu.Unlock()
}

func (c *watchFlowAIClient) usageValue() any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.usage) == 0 {
		return map[string]any{"model_calls": c.calls}
	}
	return map[string]any{"model_calls": c.calls, "responses": append([]any(nil), c.usage...)}
}

func (h watchControlPlane) previewWatchFlow(ctx context.Context, root core.ManagedMutationRoot) (map[string]any, error) {
	s := h.service
	if s == nil {
		return nil, fmt.Errorf("watch flow preview service is unavailable")
	}
	ownerID, ok := artifactUserID(ctx)
	if !ok {
		return nil, fmt.Errorf("gj_watch_flow_preview requires user identity")
	}
	watchID := strings.TrimSpace(stringInput(root.Input, "watch_id", ""))
	if watchID == "" {
		watchID = strings.TrimSpace(stringWhere(root.Where, "watch_id"))
	}
	if watchID == "" {
		return nil, fmt.Errorf("gj_watch_flow_preview requires watch_id")
	}
	watchRow, err := s.internalWatchStoreRow(ctx, watchID)
	if err != nil {
		return nil, err
	}
	admin := s.identityRoleIsAdmin(ctx)
	if watchRow == nil || (!admin && stringMapValue(watchRow, "owner_id") != ownerID) {
		return nil, fmt.Errorf("gj_watch_flow_preview denied")
	}
	_, cfg, enabled, err := normalizeWatchEnrichmentJSON(jsonMapString(watchRow, "enrich_json"))
	if err != nil {
		return nil, err
	}
	if !enabled || cfg.Kind != "flow" {
		return nil, fmt.Errorf("watch does not have an enabled flow")
	}
	samples, err := h.watchFlowPreviewSamples(ctx, root.Input, watchID)
	if err != nil {
		return nil, err
	}
	if len(samples) == 0 {
		return nil, fmt.Errorf("gj_watch_flow_preview requires samples_json or prior watch events")
	}
	started := time.Now()
	results := make([]map[string]any, 0, len(samples))
	usages := make([]any, 0, len(samples))
	counts := map[string]int{"notify": 0, "digest": 0, "discard": 0}
	for index, sample := range samples {
		run, runErr := s.runWatchFlow(ctx, cfg, map[string]ax.Value{
			"event":    sample,
			"watch":    map[string]any{"id": watchID, "name": stringMapValue(watchRow, "name")},
			"evidence": parseJSONValue(jsonMapString(watchRow, "evidence_json")),
		})
		if runErr != nil {
			return h.watchFlowPreviewError(ctx, watchRow, cfg, len(samples), results, usages, started, index, runErr)
		}
		counts[run.Verdict]++
		results = append(results, map[string]any{
			"index": index, "verdict": run.Verdict, "severity": run.Severity,
			"summary": run.Summary, "duration_ms": run.Duration.Milliseconds(),
		})
		usages = append(usages, run.Usage)
	}
	approve := boolInput(root.Input, "approve", false)
	createdAt := time.Now().UTC().Format(time.RFC3339)
	preview := map[string]any{
		"flow_hash": cfg.FlowHash, "previewed_at": createdAt, "sample_count": len(samples),
		"notify_count": counts["notify"], "digest_count": counts["digest"], "discard_count": counts["discard"],
		"status": "ok", "results": results,
	}
	evidence := mapFromAny(parseJSONValue(jsonMapString(watchRow, "evidence_json")))
	if evidence == nil {
		evidence = map[string]any{}
	}
	evidence["flow_preview"] = preview
	update := map[string]any{
		"evidence_json": nullableJSONString(mustMarshalString(evidence)),
		"updated_at":    createdAt,
	}
	if approve {
		update["approval"] = "approved"
		update["status"] = "active"
		update["enabled"] = true
	}
	if _, err := s.internalStoreMutationRows(ctx, "watches", `where: { id: { eq: $id } }, update: $input`, watchStoreFields, map[string]any{"id": watchID, "input": update}); err != nil {
		return nil, err
	}
	if err := s.bumpArtifactRevision(ctx, "watches"); err != nil {
		return nil, err
	}
	s.markWatchChanged("watch flow preview")
	s.publishWatchRunnerChanged(ctx)
	s.recordWatchFlowRuntimeEvent(ctx, watchID, cfg.FlowHash, "preview", "ok", "", time.Since(started), preview)
	return map[string]any{
		"id": "preview:" + cfg.FlowHash[:16], "watch_id": watchID, "flow_hash": cfg.FlowHash,
		"sample_count": len(samples), "notify_count": counts["notify"], "digest_count": counts["digest"], "discard_count": counts["discard"],
		"status": "ok", "result_json": results, "usage_json": usages, "error": "",
		"duration_ms": time.Since(started).Milliseconds(), "approved": approve, "created_at": createdAt,
	}, nil
}

func (h watchControlPlane) watchFlowPreviewSamples(ctx context.Context, input map[string]interface{}, watchID string) ([]any, error) {
	raw := jsonStringInput(input, "samples_json")
	if strings.TrimSpace(raw) != "" {
		var samples []any
		if err := json.Unmarshal([]byte(raw), &samples); err != nil {
			return nil, fmt.Errorf("samples_json must be a JSON array: %w", err)
		}
		if len(samples) > defaultWatchFlowSamples {
			return nil, fmt.Errorf("samples_json exceeds %d samples", defaultWatchFlowSamples)
		}
		return samples, nil
	}
	limit := intFromAny(input["event_limit"])
	if limit <= 0 || limit > defaultWatchFlowSamples {
		limit = defaultWatchFlowSamples
	}
	rows, err := h.service.internalStoreRows(ctx, "watch_events", `where: { watch_id: { eq: $watch_id } }`, `id data_json created_at`, map[string]any{"watch_id": watchID})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return stringMapValue(rows[i], "created_at") > stringMapValue(rows[j], "created_at")
	})
	if len(rows) > limit {
		rows = rows[:limit]
	}
	samples := make([]any, 0, len(rows))
	for _, row := range rows {
		samples = append(samples, parseJSONValue(jsonMapString(row, "data_json")))
	}
	return samples, nil
}

func (h watchControlPlane) watchFlowPreviewError(ctx context.Context, watchRow map[string]any, cfg watchEnrichmentConfig, sampleCount int, results []map[string]any, usages []any, started time.Time, index int, runErr error) (map[string]any, error) {
	watchID := stringMapValue(watchRow, "id")
	h.service.recordWatchFlowRuntimeEvent(ctx, watchID, cfg.FlowHash, "preview", "failed", runErr.Error(), time.Since(started), map[string]any{"failed_sample": index})
	return map[string]any{
		"id": "preview:" + cfg.FlowHash[:16], "watch_id": watchID, "flow_hash": cfg.FlowHash,
		"sample_count": sampleCount, "status": "failed", "result_json": results, "usage_json": usages,
		"error": runErr.Error(), "duration_ms": time.Since(started).Milliseconds(), "approved": false,
		"created_at": time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func (s *graphjinService) recordWatchFlowRuntimeEvent(ctx context.Context, watchID, flowHash, phase, status, errText string, duration time.Duration, details map[string]any) {
	if s == nil {
		return
	}
	if details == nil {
		details = map[string]any{}
	}
	details["watch_id"] = watchID
	details["flow_hash"] = flowHash
	event := runtimeEvent{
		Kind: runtimeKindEvent, Phase: "watch_flow." + phase, Status: runtimeStatusReady,
		Severity: "info", Summary: "Watch flow " + phase + " completed", DurationMS: duration.Milliseconds(), Details: details,
	}
	if status == "failed" {
		event.Status = runtimeStatusFailed
		event.Severity = "error"
		event.Summary = "Watch flow " + phase + " failed open"
		event.ErrorCode = "watch_flow_error"
		event.Details["error"] = errText
	}
	s.recordRuntimeEvent(ctx, event)
}
