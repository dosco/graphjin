package serv

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/dosco/graphjin/core/v3"
)

const (
	taskVerifySweepInterval = 30 * time.Second
	taskVerifyClaimTimeout  = 10 * time.Minute
	taskVerifyMinWindow     = time.Minute
	taskVerifyMaxWindow     = 30 * 24 * time.Hour
	maxVerifyObservedBytes  = 1024
)

type taskVerifyExpectation struct {
	Path  string          `json:"path,omitempty"`
	Op    string          `json:"op"`
	Value json.RawMessage `json:"value,omitempty"`
}

type taskVerifySpec struct {
	SavedQueryName string                `json:"saved_query_name"`
	Variables      json.RawMessage       `json:"variables,omitempty"`
	Expect         taskVerifyExpectation `json:"expect"`
	Recheck        string                `json:"recheck,omitempty"`

	Normalized    string        `json:"-"`
	Hash          string        `json:"-"`
	RecheckWindow time.Duration `json:"-"`
	expectedValue any
}

type taskVerificationResult struct {
	Passed   bool
	Observed any
	Error    string
	Duration time.Duration
}

func (s *graphjinService) normalizeTaskVerifyJSON(ctx context.Context, raw string, maxBytes int) (string, taskVerifySpec, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return "", taskVerifySpec{}, nil
	}
	if maxBytes > 0 && len(raw) > maxBytes {
		return "", taskVerifySpec{}, fmt.Errorf("gj_task verify_json exceeds tasks.snapshot_max_bytes (%d)", maxBytes)
	}
	spec, err := parseTaskVerifySpec(raw)
	if err != nil {
		return "", taskVerifySpec{}, fmt.Errorf("gj_task verify_json %w", err)
	}
	if s == nil || s.gj == nil {
		return "", taskVerifySpec{}, fmt.Errorf("gj_task verify_json cannot be validated: GraphJin engine is not initialized")
	}
	details, _, err := s.getSavedQueryForContext(ctx, spec.SavedQueryName)
	if err != nil {
		return "", taskVerifySpec{}, fmt.Errorf("gj_task verify_json saved query %q is not resolvable: %w", spec.SavedQueryName, err)
	}
	if details == nil {
		return "", taskVerifySpec{}, fmt.Errorf("gj_task verify_json saved query %q is not resolvable", spec.SavedQueryName)
	}
	header, err := core.Operation(details.Query)
	if err != nil || header.Type != core.OpQuery || (strings.TrimSpace(details.Operation) != "" && !strings.EqualFold(details.Operation, "query")) {
		return "", taskVerifySpec{}, fmt.Errorf("gj_task verify_json saved_query_name must resolve to a query")
	}
	if maxBytes > 0 && len(spec.Normalized) > maxBytes {
		return "", taskVerifySpec{}, fmt.Errorf("gj_task verify_json exceeds tasks.snapshot_max_bytes (%d)", maxBytes)
	}
	return spec.Normalized, spec, nil
}

func parseTaskVerifySpec(raw string) (taskVerifySpec, error) {
	var spec taskVerifySpec
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&spec); err != nil {
		return taskVerifySpec{}, fmt.Errorf("is invalid: %w", err)
	}
	if err := ensureJSONEOF(dec); err != nil {
		return taskVerifySpec{}, fmt.Errorf("is invalid: %w", err)
	}
	spec.SavedQueryName = strings.TrimSpace(spec.SavedQueryName)
	if spec.SavedQueryName == "" {
		return taskVerifySpec{}, fmt.Errorf("saved_query_name is required")
	}
	if len(spec.Variables) != 0 && string(bytes.TrimSpace(spec.Variables)) != "null" {
		var variables map[string]any
		if err := decodeJSONUseNumber(spec.Variables, &variables); err != nil || variables == nil {
			return taskVerifySpec{}, fmt.Errorf("variables must be a JSON object")
		}
		spec.Variables, _ = json.Marshal(variables)
	} else {
		spec.Variables = nil
	}
	spec.Expect.Path = strings.TrimSpace(spec.Expect.Path)
	if err := validateTaskVerifyPath(spec.Expect.Path); err != nil {
		return taskVerifySpec{}, err
	}
	spec.Expect.Op = strings.ToLower(strings.TrimSpace(spec.Expect.Op))
	switch spec.Expect.Op {
	case "empty", "not_empty":
		if len(spec.Expect.Value) != 0 {
			return taskVerifySpec{}, fmt.Errorf("expect.value is not allowed for %s", spec.Expect.Op)
		}
	case "count_le", "count_ge", "eq", "neq", "le", "ge":
		if len(spec.Expect.Value) == 0 {
			return taskVerifySpec{}, fmt.Errorf("expect.value is required for %s", spec.Expect.Op)
		}
		if err := decodeJSONUseNumber(spec.Expect.Value, &spec.expectedValue); err != nil {
			return taskVerifySpec{}, fmt.Errorf("expect.value is invalid: %w", err)
		}
		canonicalValue, _ := json.Marshal(spec.expectedValue)
		spec.Expect.Value = canonicalValue
		if spec.Expect.Op == "le" || spec.Expect.Op == "ge" || strings.HasPrefix(spec.Expect.Op, "count_") {
			number, ok := taskVerifyNumber(spec.expectedValue)
			if !ok {
				return taskVerifySpec{}, fmt.Errorf("expect.value must be numeric for %s", spec.Expect.Op)
			}
			if strings.HasPrefix(spec.Expect.Op, "count_") && (number < 0 || math.Trunc(number) != number) {
				return taskVerifySpec{}, fmt.Errorf("expect.value must be a non-negative integer for %s", spec.Expect.Op)
			}
		}
	default:
		return taskVerifySpec{}, fmt.Errorf("expect.op must be one of empty, not_empty, count_le, count_ge, eq, neq, le, or ge")
	}
	if strings.TrimSpace(spec.Recheck) != "" {
		window, err := parseClampedWindow(spec.Recheck, taskVerifyMinWindow, taskVerifyMaxWindow)
		if err != nil {
			return taskVerifySpec{}, fmt.Errorf("recheck is invalid: %w", err)
		}
		spec.RecheckWindow = window
		spec.Recheck = window.String()
	}
	canonical, err := json.Marshal(spec)
	if err != nil {
		return taskVerifySpec{}, fmt.Errorf("is invalid: %w", err)
	}
	spec.Normalized = string(canonical)
	sum := sha256.Sum256(canonical)
	spec.Hash = hex.EncodeToString(sum[:])
	return spec, nil
}

func ensureJSONEOF(dec *json.Decoder) error {
	var extra any
	if err := dec.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return err
	}
	return fmt.Errorf("multiple JSON values are not allowed")
}

func decodeJSONUseNumber(data []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(out); err != nil {
		return err
	}
	return ensureJSONEOF(dec)
}

func validateTaskVerifyPath(path string) error {
	if path == "" {
		return nil
	}
	for _, part := range strings.Split(path, ".") {
		if part == "" || strings.ContainsAny(part, "*[]") {
			return fmt.Errorf("expect.path must be a dotted field path with numeric array indices")
		}
		if index, err := strconv.Atoi(part); err == nil {
			if index < 0 {
				return fmt.Errorf("expect.path must use non-negative numeric array indices")
			}
			continue
		}
		for i, r := range part {
			if (i == 0 && r != '_' && !unicode.IsLetter(r)) || (i != 0 && r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r)) {
				return fmt.Errorf("expect.path must be a dotted field path with numeric array indices")
			}
		}
	}
	return nil
}

func (s *graphjinService) runTaskVerification(ctx context.Context, spec taskVerifySpec) (taskVerificationResult, error) {
	if s == nil || s.gj == nil {
		return taskVerificationResult{}, fmt.Errorf("GraphJin engine is not initialized")
	}
	started := time.Now()
	result := taskVerificationResult{}
	res, err := s.executeSavedQueryByName(ctx, spec.SavedQueryName, spec.Variables, nil)
	result.Duration = time.Since(started)
	if err != nil {
		result.Error = err.Error()
		result.Observed = map[string]any{"error": boundedTaskVerifyText(err.Error(), maxVerifyObservedBytes)}
		return result, nil
	}
	if res == nil {
		result.Error = "saved query returned no result"
		result.Observed = map[string]any{"error": result.Error}
		return result, nil
	}
	if len(res.Errors) != 0 {
		messages := make([]string, 0, len(res.Errors))
		for _, item := range res.Errors {
			messages = append(messages, item.Message)
		}
		result.Error = strings.Join(messages, "; ")
		result.Observed = map[string]any{"errors": boundedTaskVerifyText(result.Error, maxVerifyObservedBytes)}
		return result, nil
	}
	var data any
	if err := decodeJSONUseNumber(res.Data, &data); err != nil {
		result.Error = "saved query returned invalid JSON: " + err.Error()
		result.Observed = map[string]any{"error": boundedTaskVerifyText(result.Error, maxVerifyObservedBytes)}
		return result, nil
	}
	observed, err := taskVerifyPathValue(data, spec.Expect.Path)
	if err != nil {
		result.Error = err.Error()
		result.Observed = map[string]any{"error": boundedTaskVerifyText(err.Error(), maxVerifyObservedBytes)}
		return result, nil
	}
	result.Observed = boundedTaskVerifyObserved(observed)
	result.Passed, err = evaluateTaskVerifyExpectation(observed, spec)
	if err != nil {
		result.Error = err.Error()
	}
	return result, nil
}

func taskVerifyPathValue(data any, path string) (any, error) {
	if path == "" {
		root, ok := data.(map[string]any)
		if !ok || len(root) != 1 {
			return nil, fmt.Errorf("default verification path requires exactly one root field")
		}
		for _, value := range root {
			if _, ok := value.([]any); !ok {
				return nil, fmt.Errorf("default verification path requires the sole root field to be an array")
			}
			return value, nil
		}
	}
	current := data
	for _, part := range strings.Split(path, ".") {
		switch value := current.(type) {
		case map[string]any:
			var ok bool
			current, ok = value[part]
			if !ok {
				return nil, fmt.Errorf("verification path %q was not found", path)
			}
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(value) {
				return nil, fmt.Errorf("verification path %q has an invalid array index", path)
			}
			current = value[index]
		default:
			return nil, fmt.Errorf("verification path %q cannot traverse %q", path, part)
		}
	}
	return current, nil
}

func evaluateTaskVerifyExpectation(observed any, spec taskVerifySpec) (bool, error) {
	expected := spec.expectedValue
	if expected == nil && len(spec.Expect.Value) != 0 {
		if err := decodeJSONUseNumber(spec.Expect.Value, &expected); err != nil {
			return false, err
		}
	}
	switch spec.Expect.Op {
	case "empty":
		return taskVerifyEmpty(observed), nil
	case "not_empty":
		return !taskVerifyEmpty(observed), nil
	case "count_le", "count_ge":
		count, ok := taskVerifyCount(observed)
		if !ok {
			return false, fmt.Errorf("verification value at path is not countable")
		}
		limit, _ := taskVerifyNumber(expected)
		if spec.Expect.Op == "count_le" {
			return float64(count) <= limit, nil
		}
		return float64(count) >= limit, nil
	case "eq", "neq":
		equal := taskVerifyEqual(observed, expected)
		if spec.Expect.Op == "neq" {
			equal = !equal
		}
		return equal, nil
	case "le", "ge":
		left, leftOK := taskVerifyNumber(observed)
		right, rightOK := taskVerifyNumber(expected)
		if !leftOK || !rightOK {
			return false, fmt.Errorf("verification comparison requires numeric observed and expected values")
		}
		if spec.Expect.Op == "le" {
			return left <= right, nil
		}
		return left >= right, nil
	default:
		return false, fmt.Errorf("unsupported verification operator %q", spec.Expect.Op)
	}
}

func taskVerifyEmpty(value any) bool {
	if value == nil {
		return true
	}
	switch item := value.(type) {
	case []any:
		return len(item) == 0
	case map[string]any:
		return len(item) == 0
	case string:
		return item == ""
	default:
		return false
	}
}

func taskVerifyCount(value any) (int, bool) {
	switch item := value.(type) {
	case []any:
		return len(item), true
	case map[string]any:
		return len(item), true
	case string:
		return utf8.RuneCountInString(item), true
	default:
		return 0, false
	}
}

func taskVerifyNumber(value any) (float64, bool) {
	var number float64
	var err error
	switch item := value.(type) {
	case json.Number:
		number, err = item.Float64()
	case float64:
		number = item
	case float32:
		number = float64(item)
	case int:
		number = float64(item)
	case int64:
		number = float64(item)
	case int32:
		number = float64(item)
	default:
		return 0, false
	}
	return number, err == nil && !math.IsNaN(number) && !math.IsInf(number, 0)
}

func taskVerifyEqual(left, right any) bool {
	if leftNumber, ok := taskVerifyNumber(left); ok {
		if rightNumber, ok := taskVerifyNumber(right); ok {
			return leftNumber == rightNumber
		}
	}
	return reflect.DeepEqual(left, right)
}

func boundedTaskVerifyObserved(value any) any {
	encoded, err := json.Marshal(value)
	if err != nil {
		return map[string]any{"error": "observed value could not be encoded"}
	}
	if len(encoded) <= maxVerifyObservedBytes {
		return value
	}
	return map[string]any{
		"truncated": true,
		"preview":   boundedTaskVerifyText(string(encoded), maxVerifyObservedBytes),
	}
}

func boundedTaskVerifyText(value string, maxBytes int) string {
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value
	}
	value = value[:maxBytes]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value + "..."
}

func taskVerifyExpectationSummary(spec taskVerifySpec) string {
	path := spec.Expect.Path
	if path == "" {
		path = "<sole-root-array>"
	}
	if len(spec.Expect.Value) == 0 {
		return fmt.Sprintf("%s %s", path, spec.Expect.Op)
	}
	return fmt.Sprintf("%s %s %s", path, spec.Expect.Op, boundedTaskVerifyText(string(spec.Expect.Value), 256))
}

func taskVerificationEntrySpec(taskID string, spec taskVerifySpec, result taskVerificationResult, attempt int64) taskEntrySpec {
	verdict := "failed"
	if result.Passed {
		verdict = "passed"
	}
	observedJSON, _ := json.Marshal(result.Observed)
	body := fmt.Sprintf("Verification %s: expected %s, observed %s", verdict, taskVerifyExpectationSummary(spec), boundedTaskVerifyText(string(observedJSON), 512))
	if result.Error != "" {
		body += " (" + boundedTaskVerifyText(result.Error, 256) + ")"
	}
	detail := map[string]any{
		"saved_query_name": spec.SavedQueryName,
		"expect":           parseJSONValue(mustMarshalString(spec.Expect)),
		"observed":         result.Observed,
		"spec_hash":        spec.Hash,
		"attempt":          attempt,
		"duration_ms":      result.Duration.Milliseconds(),
	}
	if result.Error != "" {
		detail["error"] = boundedTaskVerifyText(result.Error, 512)
	}
	return taskEntrySpec{
		TaskID: taskID, Origin: "verification", Body: boundedTaskVerifyText(body, maxTaskEntryBodyBytes),
		DetailJSON: mustMarshalString(detail), Status: verdict,
		VerificationHash: spec.Hash, VerificationAttempt: attempt,
	}
}

func (h taskControlPlane) insertTaskVerificationEntry(ctx context.Context, task map[string]any, spec taskVerifySpec, result taskVerificationResult, attempt int64, now string) (bool, error) {
	entrySpec := taskVerificationEntrySpec(stringMapValue(task, "id"), spec, result, attempt)
	id := taskEntryID(entrySpec.TaskID, entrySpec.Origin, "", "", entrySpec.VerificationHash, entrySpec.VerificationAttempt)
	if existing, err := h.service.internalTaskEntryStoreRow(ctx, id); err != nil {
		return false, err
	} else if existing != nil {
		return false, nil
	}
	detailJSON, err := normalizeTaskJSON(entrySpec.DetailJSON, h.service.conf.Core.EffectiveTasksConfig().SnapshotMaxBytes, "detail_json")
	if err != nil {
		return false, err
	}
	input := map[string]any{
		"id": id, "task_id": entrySpec.TaskID, "origin": entrySpec.Origin, "body": entrySpec.Body,
		"detail_json": nullableJSONString(detailJSON), "status": entrySpec.Status, "trace_id": "", "watch_id": "",
		"account_id": stringMapValue(task, "account_id"), "owner_id": stringMapValue(task, "owner_id"),
		"created_at": now, "updated_at": now,
	}
	if _, err := h.service.internalStoreMutationRows(ctx, "task_entries", `insert: $input`, taskEntryStoreFields, map[string]any{"input": input}); err != nil {
		if existing, readErr := h.service.internalTaskEntryStoreRow(ctx, id); readErr == nil && existing != nil {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *graphjinService) startTaskVerifier(parent context.Context) {
	if s == nil || s.gj == nil || !s.tasksEnabled() {
		return
	}
	if _, _, _, _, ok := s.taskDB(); !ok {
		return
	}
	ctx := parent
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	s.addCloseFn(cancel)
	s.revisionConsumerWG.Add(1)
	go func() {
		defer s.revisionConsumerWG.Done()
		s.taskVerifierLoop(ctx, taskVerifySweepInterval)
	}()
}

func (s *graphjinService) taskVerifierLoop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if _, err := s.sweepTaskVerifications(ctx, time.Now().UTC()); err != nil && ctx.Err() == nil && s.log != nil {
			s.log.Warnf("task verifier sweep failed: %v", err)
		}
	}
}

func (s *graphjinService) sweepTaskVerifications(ctx context.Context, now time.Time) (int, error) {
	if s == nil || s.gj == nil || !s.tasksEnabled() {
		return 0, nil
	}
	if _, _, _, _, ok := s.taskDB(); !ok {
		return 0, nil
	}
	pending, err := s.internalStoreAllRows(ctx, "tasks", `where: { verify_status: { eq: "pending" } }`, taskStoreFields, nil)
	if err != nil {
		return 0, err
	}
	claimed, err := s.internalStoreAllRows(ctx, "tasks", `where: { verify_status: { like: "claimed:%" } }`, taskStoreFields, nil)
	if err != nil {
		return 0, err
	}
	rows := append(pending, claimed...)
	completed := 0
	for _, row := range rows {
		status := stringMapValue(row, "verify_status")
		due := false
		switch {
		case status == "pending":
			verifyAfter, ok := parseWatchTime(stringMapValue(row, "verify_after"))
			due = ok && !verifyAfter.After(now)
		case strings.HasPrefix(status, "claimed:"):
			updatedAt, ok := parseWatchTime(stringMapValue(row, "updated_at"))
			due = ok && !updatedAt.After(now.Add(-taskVerifyClaimTimeout))
		}
		if !due || taskStatus(stringMapValue(row, "status")) != "verifying" {
			continue
		}
		claimed, claimStatus, err := s.claimTaskVerification(ctx, row, now)
		if err != nil {
			return completed, err
		}
		if claimed == nil {
			continue
		}
		rawSpec := jsonMapString(claimed, "verify_json")
		spec, err := parseTaskVerifySpec(rawSpec)
		if err != nil {
			sum := sha256.Sum256([]byte(rawSpec))
			spec.Hash = hex.EncodeToString(sum[:])
			spec.Normalized = rawSpec
			result := taskVerificationResult{Error: err.Error(), Observed: map[string]any{"error": boundedTaskVerifyText(err.Error(), maxVerifyObservedBytes)}}
			if err := s.completeTaskVerification(ctx, claimed, spec, result, claimStatus, now); err != nil {
				return completed, err
			}
			completed++
			continue
		}
		ownerCtx := s.ownerContext(ctx, stringMapValue(claimed, "owner_id"), stringMapValue(claimed, "owner_role"), stringMapValue(claimed, "account_id"))
		result, err := s.runTaskVerification(ownerCtx, spec)
		if err != nil {
			return completed, err
		}
		if err := s.completeTaskVerification(ctx, claimed, spec, result, claimStatus, now); err != nil {
			return completed, err
		}
		completed++
	}
	return completed, nil
}

func (s *graphjinService) claimTaskVerification(ctx context.Context, row map[string]any, now time.Time) (map[string]any, string, error) {
	db, dbType, tasks, _, ok := s.taskDB()
	if !ok {
		return nil, "", nil
	}
	id := stringMapValue(row, "id")
	previous := stringMapValue(row, "verify_status")
	claimStatus := "claimed:" + hashString(fmt.Sprintf("%s:%d", id, time.Now().UnixNano()))
	nowText := now.Format(time.RFC3339Nano)
	// This is the deliberately narrow runtime direct-SQL exception for the
	// task store. The internal GraphQL mutation API cannot expose affected-row
	// count atomically, and a read-back-only nonce can be overwritten by a
	// concurrent SQLite writer after both replicas believe they won.
	args := []any{claimStatus, nowText, id, previous}
	query := fmt.Sprintf("UPDATE %s SET %s = %s, %s = %s WHERE %s = %s AND %s = %s",
		tasks,
		quoteStoreIdent(dbType, "verify_status"), taskVerifySQLPlaceholder(dbType, 1),
		quoteStoreIdent(dbType, "updated_at"), taskVerifySQLPlaceholder(dbType, 2),
		quoteStoreIdent(dbType, "id"), taskVerifySQLPlaceholder(dbType, 3),
		quoteStoreIdent(dbType, "verify_status"), taskVerifySQLPlaceholder(dbType, 4))
	res, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, "", err
	}
	affected, err := res.RowsAffected()
	if err != nil || affected != 1 {
		return nil, "", err
	}
	claimed, err := s.internalTaskStoreRow(ctx, id)
	if err != nil || claimed == nil {
		return nil, "", err
	}
	if stringMapValue(claimed, "verify_status") != claimStatus {
		return nil, "", nil
	}
	return claimed, claimStatus, nil
}

func (s *graphjinService) completeTaskVerification(ctx context.Context, task map[string]any, spec taskVerifySpec, result taskVerificationResult, claimStatus string, now time.Time) error {
	attempt := int64MapValue(task, "verify_attempts")
	if attempt <= 0 {
		attempt = 1
	}
	nowText := now.Format(time.RFC3339Nano)
	cp := newTaskControlPlane(s)
	inserted, err := cp.insertTaskVerificationEntry(ctx, task, spec, result, attempt, nowText)
	if err != nil {
		return err
	}
	status := "open"
	verifyStatus := "failed"
	closedAt := any(nil)
	outcome := ""
	if result.Passed {
		status = "closed"
		verifyStatus = "verified"
		closedAt = nowText
		outcome = stringMapValue(task, "outcome")
	}
	db, dbType, tasks, _, ok := s.taskDB()
	if !ok {
		return nil
	}
	query := fmt.Sprintf("UPDATE %s SET %s = %s, %s = %s, %s = %s, %s = NULL, %s = %s, %s = %s, %s = %s WHERE %s = %s AND %s = %s",
		tasks,
		quoteStoreIdent(dbType, "status"), taskVerifySQLPlaceholder(dbType, 1),
		quoteStoreIdent(dbType, "outcome"), taskVerifySQLPlaceholder(dbType, 2),
		quoteStoreIdent(dbType, "verify_status"), taskVerifySQLPlaceholder(dbType, 3),
		quoteStoreIdent(dbType, "verify_after"),
		quoteStoreIdent(dbType, "last_entry_at"), taskVerifySQLPlaceholder(dbType, 4),
		quoteStoreIdent(dbType, "updated_at"), taskVerifySQLPlaceholder(dbType, 5),
		quoteStoreIdent(dbType, "closed_at"), taskVerifySQLPlaceholder(dbType, 6),
		quoteStoreIdent(dbType, "id"), taskVerifySQLPlaceholder(dbType, 7),
		quoteStoreIdent(dbType, "verify_status"), taskVerifySQLPlaceholder(dbType, 8))
	res, err := db.ExecContext(ctx, query, status, outcome, verifyStatus, nowText, nowText, closedAt, stringMapValue(task, "id"), claimStatus)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 && !inserted {
		return nil
	}
	if err := cp.pruneTaskEntries(ctx, stringMapValue(task, "id")); err != nil {
		return err
	}
	if err := s.bumpArtifactRevision(ctx, "tasks"); err != nil {
		return err
	}
	s.markTaskChanged("task verification")
	return nil
}

func taskVerifySQLPlaceholder(dbType string, position int) string {
	if dbType == "postgres" || dbType == "" {
		return "$" + strconv.Itoa(position)
	}
	return "?"
}
