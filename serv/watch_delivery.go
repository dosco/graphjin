package serv

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	ax "github.com/ax-llm/ax/packages/go"
	gjagent "github.com/dosco/graphjin/agent/v3"
)

const (
	watchDeliveryBatchSize       = 20
	watchDeliveryMaxAttempts     = 3
	watchDeliveryHTTPTimeout     = 10 * time.Second
	watchDeliveryMaxResponseBody = 4 * 1024
	watchEventSweepInterval      = time.Hour
)

type watchDeliveryConfig struct {
	Kind     string
	Webhook  watchWebhookConfig
	Workflow watchWorkflowConfig
}

type watchWebhookConfig struct {
	URL       string
	SecretEnv string
	Headers   map[string]string
}

type watchWorkflowConfig struct {
	Name string
}

type watchEnrichmentConfig struct {
	Enabled       bool
	Kind          string
	Flow          string
	CanonicalFlow string
	FlowHash      string
	Instruction   string
	MaxSteps      int
}

type watchDeliveryEvent struct {
	ID               string
	WatchID          string
	DataHash         string
	DataJSON         string
	EvidenceJSON     string
	EnrichmentJSON   string
	DeliveryJSON     string
	DeliveryAttempts int64
	AccountID        string
	OwnerID          string
	CreatedAt        string
}

func (s *graphjinService) watchDeliveryLoop(ctx context.Context) {
	interval := time.Duration(s.conf.Core.EffectiveArtifactsConfig().PollSeconds) * time.Second
	if interval < watchRunnerMinInterval {
		interval = watchRunnerMinInterval
	}
	var lastSweep time.Time
	process := func() {
		now := time.Now()
		if lastSweep.IsZero() || now.Sub(lastSweep) >= watchEventSweepInterval {
			lastSweep = now
			if _, err := s.sweepWatchEvents(ctx); err != nil {
				s.recordWatchRunnerError("sweep watch events", err, nil)
			}
		}
		if err := s.processPendingWatchDeliveries(ctx); err != nil {
			s.recordWatchRunnerError("deliver watch events", err, nil)
		}
	}
	process()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			process()
		}
	}
}

func (s *graphjinService) processPendingWatchDeliveries(ctx context.Context) error {
	rows, err := s.internalStoreRows(ctx, "watch_events", `where: { delivery_status: { eq: "pending" } }`, watchEventStoreFields, nil)
	if err != nil {
		return err
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return stringMapValue(rows[i], "created_at") < stringMapValue(rows[j], "created_at")
	})
	var firstErr error
	processed := 0
	for _, row := range rows {
		if processed >= watchDeliveryBatchSize {
			break
		}
		processed++
		if err := s.processWatchDeliveryRow(ctx, row); err != nil {
			s.recordWatchRunnerError("deliver watch event", err, map[string]any{"event_id": stringMapValue(row, "id")})
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (s *graphjinService) sweepWatchEvents(ctx context.Context) (int, error) {
	if _, _, _, _, ok := s.watchDB(); !ok {
		return 0, nil
	}
	cfg := s.conf.Core.EffectiveWatchesConfig()
	eventRows, err := s.internalStoreRows(ctx, "watch_events", "", `id watch_id created_at`, nil)
	if err != nil {
		return 0, err
	}
	if len(eventRows) == 0 {
		return 0, nil
	}
	watchRows, err := s.internalStoreRows(ctx, "watches", "", `id`, nil)
	if err != nil {
		return 0, err
	}
	watchIDs := make(map[string]struct{}, len(watchRows))
	for _, row := range watchRows {
		if id := stringMapValue(row, "id"); id != "" {
			watchIDs[id] = struct{}{}
		}
	}
	var cutoff time.Time
	if cfg.EventRetentionHours > 0 {
		cutoff = time.Now().UTC().Add(-time.Duration(cfg.EventRetentionHours) * time.Hour)
	}
	deleted := 0
	for _, row := range eventRows {
		id := stringMapValue(row, "id")
		if id == "" {
			continue
		}
		watchID := stringMapValue(row, "watch_id")
		_, watchExists := watchIDs[watchID]
		expired := false
		if !cutoff.IsZero() {
			if ts, ok := parseWatchTime(stringMapValue(row, "created_at")); ok && ts.Before(cutoff) {
				expired = true
			}
		}
		if !expired && watchExists {
			continue
		}
		n, err := s.deleteWatchEventByID(ctx, id)
		if err != nil {
			return deleted, err
		}
		deleted += n
	}
	if deleted == 0 {
		return 0, nil
	}
	if err := s.bumpArtifactRevision(ctx, "watch_events"); err != nil {
		return deleted, err
	}
	s.markWatchChanged("watch event sweep")
	return deleted, nil
}

func (s *graphjinService) processWatchDeliveryRow(ctx context.Context, row map[string]any) error {
	event := watchDeliveryEventFromRow(row)
	if event.ID == "" {
		return nil
	}
	_, ok, err := s.claimWatchDelivery(ctx, event)
	if err != nil || !ok {
		return err
	}
	watchRow, err := s.internalWatchStoreRow(ctx, event.WatchID)
	if err != nil {
		return err
	}
	if watchRow == nil {
		return s.completeWatchDelivery(ctx, event, "failed", 0, map[string]any{
			"status": "failed",
			"error":  "watch definition no longer exists",
		})
	}
	def := watchRuntimeDefinition{
		ID:             stringMapValue(watchRow, "id"),
		Name:           stringMapValue(watchRow, "name"),
		Query:          stringMapValue(watchRow, "query"),
		SavedQueryName: stringMapValue(watchRow, "saved_query_name"),
		VariablesJSON:  jsonMapString(watchRow, "variables_json"),
		DeliveryJSON:   event.DeliveryJSON,
		EnrichJSON:     jsonMapString(watchRow, "enrich_json"),
		Lifecycle:      watchLifecycle(stringMapValue(watchRow, "lifecycle")),
		LeaseExpiresAt: stringMapValue(watchRow, "lease_expires_at"),
		LeaseOwnerID:   stringMapValue(watchRow, "lease_owner_id"),
		AccountID:      stringMapValue(watchRow, "account_id"),
		OwnerID:        stringMapValue(watchRow, "owner_id"),
		OwnerRole:      s.trustedWatchRunnerRole(stringMapValue(watchRow, "owner_role")),
		LastDataHash:   stringMapValue(watchRow, "last_data_hash"),
	}
	receipt, attempts, err := s.deliverWatchEvent(ctx, def, event)
	if err != nil {
		if receipt == nil {
			receipt = map[string]any{}
		}
		receipt["status"] = "failed"
		receipt["error"] = err.Error()
		return s.completeWatchDelivery(ctx, event, "failed", attempts, receipt)
	}
	return s.completeWatchDelivery(ctx, event, "delivered", attempts, receipt)
}

func (s *graphjinService) claimWatchDelivery(ctx context.Context, event watchDeliveryEvent) (map[string]any, bool, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	claimStatus := "claimed:" + hashString(fmt.Sprintf("%s:%d", event.ID, time.Now().UnixNano()))
	if _, err := s.internalStoreMutationRows(ctx, "watch_events",
		`where: { id: { eq: $id }, delivery_status: { eq: "pending" } }, update: $input`,
		watchEventStoreFields,
		map[string]any{
			"id":    event.ID,
			"input": map[string]any{"delivery_status": claimStatus, "updated_at": now},
		}); err != nil {
		return nil, false, err
	}
	row, err := s.internalWatchEventStoreRow(ctx, event.ID)
	if err != nil || row == nil {
		return nil, false, err
	}
	if stringMapValue(row, "delivery_status") != claimStatus {
		return nil, false, nil
	}
	return row, true, nil
}

func (s *graphjinService) deliverWatchEvent(ctx context.Context, def watchRuntimeDefinition, event watchDeliveryEvent) (map[string]any, int64, error) {
	cfg, enabled, err := parseWatchDeliveryConfig(event.DeliveryJSON)
	if err != nil {
		return nil, 0, err
	}
	if !enabled {
		return map[string]any{
			"status":       "delivered",
			"kind":         "inbox",
			"delivered_at": time.Now().UTC().Format(time.RFC3339),
		}, 0, nil
	}
	var attempts int64
	var receipt map[string]any
	for attempt := 1; attempt <= watchDeliveryMaxAttempts; attempt++ {
		attempts++
		switch cfg.Kind {
		case "webhook":
			receipt, err = s.deliverWatchWebhook(ctx, def, event, cfg.Webhook)
		case "workflow":
			receipt, err = s.deliverWatchWorkflow(ctx, def, event, cfg.Workflow)
		case "inbox":
			return map[string]any{
				"status":       "delivered",
				"kind":         "inbox",
				"delivered_at": time.Now().UTC().Format(time.RFC3339),
			}, 0, nil
		default:
			return nil, attempts, fmt.Errorf("unsupported watch delivery kind %q", cfg.Kind)
		}
		if err == nil {
			return receipt, attempts, nil
		}
		if attempt == watchDeliveryMaxAttempts {
			break
		}
		if err := sleepContext(ctx, time.Duration(attempt)*100*time.Millisecond); err != nil {
			return receipt, attempts, err
		}
	}
	return receipt, attempts, err
}

func (s *graphjinService) completeWatchDelivery(ctx context.Context, event watchDeliveryEvent, status string, attempts int64, receipt map[string]any) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if receipt == nil {
		receipt = map[string]any{}
	}
	receipt["completed_at"] = now
	rows, err := s.internalStoreMutationRows(ctx, "watch_events",
		`where: { id: { eq: $id } }, update: $input`,
		watchEventStoreFields,
		map[string]any{
			"id": event.ID,
			"input": map[string]any{
				"delivery_status":   watchDeliveryStatus(status),
				"delivery_attempts": event.DeliveryAttempts + attempts,
				"receipt_json":      nullableJSONString(mustMarshalString(receipt)),
				"updated_at":        now,
			},
		})
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	if err := s.bumpArtifactRevision(ctx, "watch_events"); err != nil {
		return err
	}
	s.markWatchChanged("watch delivery")
	s.notifyWatchEventsResource(event.OwnerID, event.AccountID, event.WatchID)
	return nil
}

func (s *graphjinService) deliverWatchWebhook(ctx context.Context, def watchRuntimeDefinition, event watchDeliveryEvent, cfg watchWebhookConfig) (map[string]any, error) {
	u, err := s.validateWatchWebhookURL(ctx, cfg.URL)
	if err != nil {
		return nil, err
	}
	payload := watchDeliveryPayload(def, event)
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "GraphJin-Watch-Delivery/1")
	req.Header.Set("Idempotency-Key", event.ID)
	for key, value := range cfg.Headers {
		headerName, ok := safeWebhookHeaderName(key)
		if !ok {
			return nil, fmt.Errorf("unsafe webhook header %q", key)
		}
		req.Header.Set(headerName, value)
	}
	if cfg.SecretEnv != "" {
		secret := os.Getenv(cfg.SecretEnv)
		if secret == "" {
			return nil, fmt.Errorf("webhook secret env %s is not set", cfg.SecretEnv)
		}
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write(body)
		req.Header.Set("X-GraphJin-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	client := &http.Client{
		Timeout:       watchDeliveryHTTPTimeout,
		Transport:     s.watchWebhookTransport(),
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, watchDeliveryMaxResponseBody))
	receipt := map[string]any{
		"status":      "delivered",
		"kind":        "webhook",
		"url":         u.Redacted(),
		"status_code": resp.StatusCode,
		"body":        string(respBody),
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return receipt, fmt.Errorf("webhook returned HTTP %d", resp.StatusCode)
	}
	return receipt, nil
}

func (s *graphjinService) deliverWatchWorkflow(ctx context.Context, def watchRuntimeDefinition, event watchDeliveryEvent, cfg watchWorkflowConfig) (map[string]any, error) {
	if strings.TrimSpace(cfg.Name) == "" {
		return nil, errors.New("workflow delivery requires name")
	}
	ownerCtx := s.watchOwnerContext(ctx, def)
	out, err := s.runNamedWorkflow(ownerCtx, cfg.Name, watchDeliveryPayload(def, event), nil)
	receipt := map[string]any{
		"status": "delivered",
		"kind":   "workflow",
		"name":   cfg.Name,
		"result": out,
	}
	if err != nil {
		return receipt, err
	}
	return receipt, nil
}

func (s *graphjinService) enrichWatchEvent(ctx context.Context, def *watchRuntimeDefinition, eventID, dataJSON, evidenceJSON string, cfg watchEnrichmentConfig) (bool, error) {
	if def == nil {
		return true, nil
	}
	if cfg.Kind == "flow" {
		return s.triageWatchEvent(ctx, def, eventID, dataJSON, evidenceJSON, cfg)
	}
	status := "pending"
	enrichment := map[string]any{}
	if cfg.MaxSteps <= 0 || cfg.MaxSteps > 4 {
		cfg.MaxSteps = 4
	}
	if strings.TrimSpace(cfg.Instruction) == "" {
		cfg.Instruction = "Summarize this watch event, explain why it matters, and suggest the next safe action."
	}
	if s.watchEnrichmentDailyCapReached(ctx, def.ID) {
		enrichment = map[string]any{"status": "skipped", "reason": "daily_cap"}
		return true, s.storeWatchEnrichment(ctx, eventID, status, false, enrichment)
	}
	ownerCtx := s.watchOwnerContext(ctx, *def)
	agentConf := agentConfigFromService(s.conf)
	agentConf.ReadOnly = true
	agentConf.Sampling = gjagent.SamplingOff
	agentConf.MaxSteps = cfg.MaxSteps
	runner, err := newGraphJinAgentRunner(s, agentConf)
	if err != nil {
		enrichment = map[string]any{"status": "error", "error": err.Error()}
		if storeErr := s.storeWatchEnrichment(ctx, eventID, status, false, enrichment); storeErr != nil {
			return true, storeErr
		}
		return true, err
	}
	req := gjagent.Request{
		Instruction: cfg.Instruction,
		Context: map[string]any{
			"_watch_events": []any{map[string]any{
				"id":            eventID,
				"watch_id":      def.ID,
				"data_json":     parseJSONValue(dataJSON),
				"evidence_json": parseJSONValue(evidenceJSON),
			}},
			"watch": map[string]any{"id": def.ID, "name": def.Name},
		},
		MaxSteps:     cfg.MaxSteps,
		Capabilities: s.agentCapabilityProfile(ownerCtx),
	}
	resp, err := runner.Run(ownerCtx, req)
	if err != nil {
		enrichment = map[string]any{"status": "error", "error": err.Error()}
		if storeErr := s.storeWatchEnrichment(ctx, eventID, status, false, enrichment); storeErr != nil {
			return true, storeErr
		}
		return true, err
	}
	enrichment = map[string]any{
		"status":       "ok",
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"response":     resp,
	}
	return true, s.storeWatchEnrichment(ctx, eventID, status, false, enrichment)
}

func (s *graphjinService) triageWatchEvent(ctx context.Context, def *watchRuntimeDefinition, eventID, dataJSON, evidenceJSON string, cfg watchEnrichmentConfig) (bool, error) {
	started := time.Now()
	failOpen := func(reason string, runErr error) (bool, error) {
		enrichment := map[string]any{
			"status": "error", "kind": "flow", "flow_hash": cfg.FlowHash,
			"fail_open": true, "error": reason,
		}
		if err := s.storeWatchEnrichment(ctx, eventID, "pending", false, enrichment); err != nil {
			return true, err
		}
		s.recordWatchFlowRuntimeEvent(ctx, def.ID, cfg.FlowHash, "runtime", "failed", reason, time.Since(started), map[string]any{"event_id": eventID, "fail_open": true})
		return true, runErr
	}
	if s.watchEnrichmentDailyCapReached(ctx, def.ID) {
		return failOpen("daily_cap", nil)
	}
	run, err := s.runWatchFlow(s.watchOwnerContext(ctx, *def), cfg, map[string]ax.Value{
		"event":    parseJSONValue(dataJSON),
		"watch":    map[string]any{"id": def.ID, "name": def.Name},
		"evidence": parseJSONValue(evidenceJSON),
	})
	if err != nil {
		return failOpen(err.Error(), err)
	}
	status := "pending"
	seen := false
	notify := true
	switch run.Verdict {
	case "digest":
		status, seen, notify = "digest_queued", true, false
	case "discard":
		status, seen, notify = "suppressed", true, false
	}
	enrichment := map[string]any{
		"status": "ok", "kind": "flow", "flow_hash": run.FlowHash,
		"verdict": run.Verdict, "severity": run.Severity, "summary": run.Summary,
		"usage": run.Usage, "model_calls": run.ModelCalls,
		"duration_ms": run.Duration.Milliseconds(), "generated_at": time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.storeWatchEnrichment(ctx, eventID, status, seen, enrichment); err != nil {
		return true, err
	}
	s.recordWatchFlowRuntimeEvent(ctx, def.ID, run.FlowHash, "runtime", "ok", "", time.Since(started), map[string]any{
		"event_id": eventID, "verdict": run.Verdict, "severity": run.Severity, "model_calls": run.ModelCalls,
	})
	return notify, nil
}

func (s *graphjinService) storeWatchEnrichment(ctx context.Context, eventID, deliveryStatus string, seen bool, enrichment map[string]any) error {
	now := time.Now().UTC().Format(time.RFC3339)
	input := map[string]any{
		"delivery_status": deliveryStatus,
		"enrichment_json": nullableJSONString(mustMarshalString(enrichment)),
		"seen":            seen,
		"seen_at":         nullableSeenAt(seen, now),
		"updated_at":      now,
	}
	if _, err := s.internalStoreMutationRows(ctx, "watch_events",
		`where: { id: { eq: $id } }, update: $input`, watchEventStoreFields,
		map[string]any{"id": eventID, "input": input}); err != nil {
		return err
	}
	if err := s.bumpArtifactRevision(ctx, "watch_events"); err != nil {
		return err
	}
	s.markWatchChanged("watch enrichment")
	return nil
}

func (s *graphjinService) watchEnrichmentDailyCapReached(ctx context.Context, watchID string) bool {
	if s == nil || s.conf == nil {
		return false
	}
	cap := s.conf.Core.EffectiveWatchesConfig().EnrichmentDailyCap
	if cap <= 0 {
		return false
	}
	rows, err := s.internalStoreRows(ctx, "watch_events", `where: { watch_id: { eq: $watch_id } }`, `id created_at enrichment_json`, map[string]any{"watch_id": watchID})
	if err != nil {
		return false
	}
	cutoff := time.Now().UTC().Add(-24 * time.Hour)
	count := 0
	for _, row := range rows {
		if strings.TrimSpace(jsonMapString(row, "enrichment_json")) == "" {
			continue
		}
		if ts, ok := parseWatchTime(stringMapValue(row, "created_at")); ok && ts.Before(cutoff) {
			continue
		}
		count++
	}
	return count >= cap
}

func parseWatchDeliveryConfig(raw string) (watchDeliveryConfig, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" || raw == "{}" {
		return watchDeliveryConfig{Kind: "inbox"}, false, nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return watchDeliveryConfig{}, false, fmt.Errorf("delivery_json is invalid: %w", err)
	}
	cfg := watchDeliveryConfig{Kind: strings.ToLower(strings.TrimSpace(fmt.Sprint(m["kind"])))}
	if webhook, ok := m["webhook"]; ok {
		cfg.Kind = "webhook"
		cfg.Webhook = parseWatchWebhookConfig(webhook)
	}
	if workflow, ok := m["workflow"]; ok {
		cfg.Kind = "workflow"
		cfg.Workflow = parseWatchWorkflowConfig(workflow)
	}
	switch cfg.Kind {
	case "", "inbox":
		cfg.Kind = "inbox"
	case "webhook":
		if cfg.Webhook.URL == "" {
			cfg.Webhook = parseWatchWebhookConfig(m)
		}
		if strings.TrimSpace(cfg.Webhook.URL) == "" {
			return cfg, true, errors.New("webhook delivery requires url")
		}
	case "workflow":
		if cfg.Workflow.Name == "" {
			cfg.Workflow = parseWatchWorkflowConfig(m)
		}
		if strings.TrimSpace(cfg.Workflow.Name) == "" {
			return cfg, true, errors.New("workflow delivery requires name")
		}
	default:
		return cfg, true, fmt.Errorf("unsupported watch delivery kind %q", cfg.Kind)
	}
	return cfg, cfg.Kind != "inbox", nil
}

func parseWatchWebhookConfig(raw any) watchWebhookConfig {
	m := mapFromAny(raw)
	cfg := watchWebhookConfig{
		URL:       stringFromAny(m["url"]),
		SecretEnv: stringFromAny(m["secret_env"]),
		Headers:   map[string]string{},
	}
	for key, value := range mapFromAny(m["headers"]) {
		cfg.Headers[key] = stringFromAny(value)
	}
	return cfg
}

func parseWatchWorkflowConfig(raw any) watchWorkflowConfig {
	m := mapFromAny(raw)
	return watchWorkflowConfig{Name: stringFromAny(m["name"])}
}

func parseWatchEnrichmentConfig(raw string) (watchEnrichmentConfig, bool) {
	_, cfg, enabled, err := normalizeWatchEnrichmentJSON(raw)
	if err != nil {
		return watchEnrichmentConfig{}, false
	}
	return cfg, enabled
}

func (s *graphjinService) validateWatchWebhookURL(ctx context.Context, raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, err
	}
	u.Fragment = ""
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("webhook URL scheme must be http or https")
	}
	if u.User != nil || strings.TrimSpace(u.Host) == "" {
		return nil, fmt.Errorf("webhook URL must not include userinfo and must include host")
	}
	if s == nil || s.conf == nil || !watchWebhookAllowMatch(u, s.conf.Core.EffectiveWatchesConfig().WebhookAllow) {
		return nil, fmt.Errorf("webhook URL is not in watches.webhook_allow")
	}
	if err := validateWebhookResolvedHost(ctx, u, s.conf.Core.EffectiveWatchesConfig().WebhookAllow); err != nil {
		return nil, err
	}
	return u, nil
}

func (s *graphjinService) watchWebhookTransport() http.RoundTripper {
	return &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ip, err := resolveWebhookDialIP(ctx, host, s.conf.Core.EffectiveWatchesConfig().WebhookAllow)
			if err != nil {
				return nil, err
			}
			d := net.Dialer{Timeout: watchDeliveryHTTPTimeout}
			return d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		},
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
	}
}

func validateWebhookResolvedHost(ctx context.Context, u *url.URL, allow []string) error {
	_, err := resolveWebhookDialIP(ctx, u.Hostname(), allow)
	return err
}

func resolveWebhookDialIP(ctx context.Context, host string, allow []string) (net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedWebhookIP(ip) && !watchAllowContainsLiteralIP(host, allow) {
			return nil, fmt.Errorf("webhook IP %s is private or local", ip)
		}
		return ip, nil
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("webhook host %s has no addresses", host)
	}
	var first net.IP
	for _, addr := range addrs {
		ip := addr.IP
		if first == nil {
			first = ip
		}
		if isBlockedWebhookIP(ip) {
			return nil, fmt.Errorf("webhook host %s resolved to private or local IP %s", host, ip)
		}
	}
	return first, nil
}

func isBlockedWebhookIP(ip net.IP) bool {
	return ip == nil ||
		ip.IsUnspecified() ||
		ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() ||
		isCGNATIP(ip)
}

func isCGNATIP(ip net.IP) bool {
	ip4 := ip.To4()
	return ip4 != nil &&
		ip4[0] == 100 &&
		ip4[1] >= 64 &&
		ip4[1] <= 127
}

func watchWebhookAllowMatch(u *url.URL, allow []string) bool {
	targetOrigin := strings.ToLower(u.Scheme + "://" + u.Host)
	for _, entry := range allow {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		allowed, err := url.Parse(entry)
		if err != nil || (allowed.Scheme != "http" && allowed.Scheme != "https") || allowed.Host == "" || allowed.User != nil {
			continue
		}
		if strings.ToLower(allowed.Scheme+"://"+allowed.Host) != targetOrigin {
			continue
		}
		path := strings.TrimRight(allowed.Path, "/")
		if path == "" || strings.HasPrefix(u.EscapedPath(), path) {
			return true
		}
	}
	return false
}

func watchAllowContainsLiteralIP(host string, allow []string) bool {
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, entry := range allow {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		u, err := url.Parse(entry)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			continue
		}
		if parsed := net.ParseIP(u.Hostname()); parsed != nil && parsed.Equal(ip) {
			return true
		}
	}
	return false
}

func safeWebhookHeaderName(name string) (string, bool) {
	name = textproto.CanonicalMIMEHeaderKey(strings.TrimSpace(name))
	if name == "" || strings.EqualFold(name, "host") || strings.EqualFold(name, "content-length") {
		return "", false
	}
	for _, r := range name {
		if r <= 32 || r >= 127 || strings.ContainsRune("()<>@,;:\\\"/[]?={}", r) {
			return "", false
		}
	}
	return name, true
}

func watchDeliveryEventFromRow(row map[string]any) watchDeliveryEvent {
	return watchDeliveryEvent{
		ID:               stringMapValue(row, "id"),
		WatchID:          stringMapValue(row, "watch_id"),
		DataHash:         stringMapValue(row, "data_hash"),
		DataJSON:         jsonMapString(row, "data_json"),
		EvidenceJSON:     jsonMapString(row, "evidence_json"),
		EnrichmentJSON:   jsonMapString(row, "enrichment_json"),
		DeliveryJSON:     jsonMapString(row, "delivery_json"),
		DeliveryAttempts: int64MapValue(row, "delivery_attempts"),
		AccountID:        stringMapValue(row, "account_id"),
		OwnerID:          stringMapValue(row, "owner_id"),
		CreatedAt:        stringMapValue(row, "created_at"),
	}
}

func watchDeliveryPayload(def watchRuntimeDefinition, event watchDeliveryEvent) map[string]any {
	enrichment := mapFromAny(parseJSONValue(event.EnrichmentJSON))
	eventPayload := map[string]any{
		"id":              event.ID,
		"watch_id":        event.WatchID,
		"data_hash":       event.DataHash,
		"data_json":       parseJSONValue(event.DataJSON),
		"evidence_json":   parseJSONValue(event.EvidenceJSON),
		"enrichment_json": parseJSONValue(event.EnrichmentJSON),
		"created_at":      event.CreatedAt,
	}
	for _, key := range []string{"verdict", "severity", "summary"} {
		if value := strings.TrimSpace(stringFromAny(enrichment[key])); value != "" {
			eventPayload[key] = value
		}
	}
	return map[string]any{
		"watch": map[string]any{
			"id":               def.ID,
			"name":             def.Name,
			"query":            def.Query,
			"saved_query_name": def.SavedQueryName,
			"lifecycle":        def.Lifecycle,
			"lease_expires_at": def.LeaseExpiresAt,
		},
		"event": eventPayload,
	}
}

func parseJSONValue(raw string) any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return raw
	}
	return out
}

func mapFromAny(v any) map[string]any {
	switch t := v.(type) {
	case map[string]any:
		return t
	default:
		var out map[string]any
		data, err := json.Marshal(t)
		if err != nil {
			return nil
		}
		if err := json.Unmarshal(data, &out); err != nil {
			return nil
		}
		return out
	}
}

func stringFromAny(v any) string {
	if v == nil {
		return ""
	}
	s, ok := v.(string)
	if ok {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func intFromAny(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case json.Number:
		n, _ := strconv.Atoi(t.String())
		return n
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(t))
		return n
	default:
		return 0
	}
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
