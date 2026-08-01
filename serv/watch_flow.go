package serv

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	ax "github.com/ax-llm/ax/packages/go"
	gjagent "github.com/dosco/graphjin/agent/v3"
)

const (
	defaultWatchTriageFlow = `flowchart TD
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
	switch kind {
	case "flow":
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
	case "", "agent":
		// Blank is the legacy agent-enrichment spelling.
	default:
		return "", watchEnrichmentConfig{}, false, fmt.Errorf("unsupported enrich_json kind %q", kind)
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
	var client ax.AIClient
	var err error
	if s.agentClientFactory != nil {
		client, err = s.agentClientFactory(agentConf)
	} else {
		client, err = gjagent.DefaultClientFactory(agentConf)
	}
	if err != nil {
		if errors.Is(err, gjagent.ErrMissingAPIKey) {
			return watchFlowRun{}, fmt.Errorf("%s: GraphJin-owned model credentials are required: %w", modelCredentialsRequiredCode, err)
		}
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

func (h watchControlPlane) watchFlowReviewSamples(ctx context.Context, input map[string]any, watchID string) ([]any, error) {
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
	args := fmt.Sprintf(
		`where: { watch_id: { eq: $watch_id } }, order_by: { created_at: desc }, limit: %d`,
		limit,
	)
	rows, err := h.service.internalStoreRows(ctx, "watch_events", args, `id data_json created_at`, map[string]any{"watch_id": watchID})
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
