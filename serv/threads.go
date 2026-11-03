package serv

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	gjagent "github.com/dosco/graphjin/agent/v3"
)

// Threads persist agent conversations in the control-plane store so every
// replica can serve them. A thread holds messages; each user turn starts a run
// whose streamed events are appended to run_events as they happen. Only one
// run is active per thread: threads.active_run_id is claimed with a
// compare-and-set, which also makes the active run the only writer of the
// thread's message sequence.

const (
	threadStoreFields   = `id title profile status channel channel_ref active_run_id account_id owner_id owner_role last_message_at last_seen_at created_at updated_at`
	messageStoreFields  = `id thread_id seq role content status run_id catalog_ids response_json account_id owner_id created_at`
	runStoreFields      = `id thread_id status lease_until_ms replica tokens_in tokens_out error account_id owner_id created_at updated_at finished_at`
	runEventStoreFields = `id run_id seq kind payload created_at`

	maxThreadTitleBytes   = 200
	maxMessageBytes       = 64 * 1024
	threadSweepInterval   = 15 * time.Second
	runCancelPollInterval = time.Second
)

const (
	runStatusRunning         = "running"
	runStatusCancelRequested = "cancel_requested"
	runStatusDone            = "done"
	runStatusCancelled       = "cancelled"
	runStatusFailed          = "failed"
)

var (
	errThreadNotFound = errors.New("thread_not_found")
	errThreadBusy     = errors.New("thread_busy: a run is already active on this thread")
	errRunNotFound    = errors.New("run_not_found")
	errThreadLimit    = errors.New("thread_limit_reached")
)

// localRuns holds the cancel functions of runs executing on this replica, so a
// cancel that lands here stops the run at once instead of on the next poll.
var localRuns sync.Map // run id -> context.CancelFunc

var replicaName = func() string {
	host, _ := os.Hostname()
	return fmt.Sprintf("%s:%d", host, os.Getpid())
}()

func (s *graphjinService) threadsEnabled() bool {
	return s != nil && configThreadsEnabled(s.conf)
}

func configThreadsEnabled(conf *Config) bool {
	return conf != nil && conf.Core.Artifacts.Enabled && conf.Core.Threads.Enabled
}

func (s *graphjinService) threadDB() (*sql.DB, string, bool) {
	if !s.threadsEnabled() {
		return nil, "", false
	}
	db, dbType, _, ok := s.artifactDB()
	return db, dbType, ok
}

func runIsTerminal(status string) bool {
	switch status {
	case runStatusDone, runStatusCancelled, runStatusFailed:
		return true
	}
	return false
}

func storeNow() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// truncateUTF8 cuts value to at most maxBytes without splitting a rune.
func truncateUTF8(value string, maxBytes int) string {
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut]
}

// --- threads -----------------------------------------------------------------

type threadSpec struct {
	// ID is set for threads with a derived id (a profile's home thread);
	// empty means a new random id.
	ID         string
	Title      string
	Profile    string
	Channel    string
	ChannelRef string
}

func (s *graphjinService) createThread(ctx context.Context, spec threadSpec) (map[string]any, error) {
	if _, _, ok := s.threadDB(); !ok {
		return nil, fmt.Errorf("thread store is not configured")
	}
	ownerID, ok := artifactUserID(ctx)
	if !ok {
		return nil, fmt.Errorf("threads require a user identity")
	}
	if err := s.enforceThreadLimit(ctx, ownerID); err != nil {
		return nil, err
	}
	accountID, _ := identityVarString(ctx, "account_id")
	channel := strings.TrimSpace(spec.Channel)
	if channel == "" {
		channel = "web"
	}
	id := strings.TrimSpace(spec.ID)
	if id == "" {
		id = "th_" + taskRandomHex()
	}
	now := storeNow()
	input := map[string]any{
		"id":    id,
		"title": truncateUTF8(strings.TrimSpace(spec.Title), maxThreadTitleBytes),
		// profile names an agent_profile artifact; empty means the default agent.
		"profile":       strings.TrimSpace(spec.Profile),
		"status":        "open",
		"channel":       channel,
		"channel_ref":   strings.TrimSpace(spec.ChannelRef),
		"active_run_id": "",
		"account_id":    accountID,
		"owner_id":      ownerID,
		"owner_role":    runtimeRoleClass(ctx),
		// A new thread has nothing unread; only a stored-null last_seen_at
		// marks a thread from before the column existed.
		"last_seen_at": now,
		"created_at":   now,
		"updated_at":   now,
	}
	rows, err := s.internalStoreMutationRows(ctx, "threads", `insert: $input`, threadStoreFields, map[string]any{"input": input})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return s.threadForCaller(ctx, stringMapValue(input, "id"))
	}
	return threadAPIRow(rows[0]), nil
}

func (s *graphjinService) enforceThreadLimit(ctx context.Context, ownerID string) error {
	limit := s.conf.Core.EffectiveThreadsConfig().MaxPerOwner
	if limit <= 0 {
		return nil
	}
	rows, err := s.internalStoreRows(ctx, "threads",
		fmt.Sprintf(`where: { owner_id: { eq: $owner_id }, status: { eq: "open" } }, limit: %d`, limit),
		"id", map[string]any{"owner_id": ownerID})
	if err != nil {
		return err
	}
	if len(rows) >= limit {
		return fmt.Errorf("%w: archive a thread before starting another (limit %d)", errThreadLimit, limit)
	}
	return nil
}

// threadStoreRow reads one raw thread row with no owner check.
func (s *graphjinService) threadStoreRow(ctx context.Context, id string) (map[string]any, error) {
	rows, err := s.internalStoreRows(ctx, "threads", `where: { id: { eq: $id } }`, threadStoreFields, map[string]any{"id": id})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

// threadForCaller returns a thread the caller owns; admins may read any.
func (s *graphjinService) threadForCaller(ctx context.Context, id string) (map[string]any, error) {
	row, err := s.ownedThreadStoreRow(ctx, id)
	if err != nil {
		return nil, err
	}
	return threadAPIRow(row), nil
}

func (s *graphjinService) ownedThreadStoreRow(ctx context.Context, id string) (map[string]any, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, errThreadNotFound
	}
	ownerID, ok := artifactUserID(ctx)
	if !ok {
		return nil, errThreadNotFound
	}
	row, err := s.threadStoreRow(ctx, id)
	if err != nil {
		return nil, err
	}
	if row == nil || (stringMapValue(row, "owner_id") != ownerID && !s.identityRoleIsAdmin(ctx)) {
		return nil, errThreadNotFound
	}
	return row, nil
}

func (s *graphjinService) listThreads(ctx context.Context, includeArchived bool) ([]map[string]any, error) {
	ownerID, ok := artifactUserID(ctx)
	if !ok {
		return nil, nil
	}
	args := `where: { owner_id: { eq: $owner_id }, status: { eq: "open" } }`
	if includeArchived {
		args = `where: { owner_id: { eq: $owner_id } }`
	}
	rows, err := s.internalStoreAllRows(ctx, "threads", args, threadStoreFields, map[string]any{"owner_id": ownerID})
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, threadAPIRow(row))
	}
	sort.SliceStable(out, func(i, j int) bool {
		return threadSortKey(out[i]) > threadSortKey(out[j])
	})
	return out, nil
}

func threadSortKey(row map[string]any) string {
	if v := stringMapValue(row, "last_message_at"); v != "" {
		return v
	}
	return stringMapValue(row, "created_at")
}

func (s *graphjinService) updateThread(ctx context.Context, id string, input map[string]any) (map[string]any, error) {
	if _, err := s.ownedThreadStoreRow(ctx, id); err != nil {
		return nil, err
	}
	update := map[string]any{"updated_at": storeNow()}
	for field := range input {
		switch field {
		case "title", "status", "profile", "seen":
		default:
			return nil, fmt.Errorf("thread %s is not editable", field)
		}
	}
	if v, ok := input["title"].(string); ok {
		update["title"] = truncateUTF8(strings.TrimSpace(v), maxThreadTitleBytes)
	}
	if v, ok := input["profile"].(string); ok {
		update["profile"] = strings.TrimSpace(v)
	}
	if seen, ok := input["seen"].(bool); ok && seen {
		update["last_seen_at"] = storeNow()
	}
	if v, ok := input["status"].(string); ok {
		switch v {
		case "open", "archived":
			update["status"] = v
		default:
			return nil, fmt.Errorf("thread status must be open or archived")
		}
	}
	if _, err := s.internalStoreMutationRows(ctx, "threads", `where: { id: { eq: $id } }, update: $input`, "id",
		map[string]any{"id": id, "input": update}); err != nil {
		return nil, err
	}
	return s.threadForCaller(ctx, id)
}

// deleteThread removes a thread with its messages, runs and run events. A
// thread with an active run is refused; cancel the run first.
func (s *graphjinService) deleteThread(ctx context.Context, id string) error {
	thread, err := s.ownedThreadStoreRow(ctx, id)
	if err != nil {
		return err
	}
	if stringMapValue(thread, "active_run_id") != "" {
		return errThreadBusy
	}
	runs, err := s.internalStoreAllRows(ctx, "runs", `where: { thread_id: { eq: $thread_id } }`, "id", map[string]any{"thread_id": id})
	if err != nil {
		return err
	}
	for _, run := range runs {
		if _, err := s.internalStoreMutationRows(ctx, "run_events", `delete: true, where: { run_id: { eq: $run_id } }`, "id",
			map[string]any{"run_id": stringMapValue(run, "id")}); err != nil {
			return err
		}
	}
	for _, table := range []string{"runs", "messages"} {
		if _, err := s.internalStoreMutationRows(ctx, table, `delete: true, where: { thread_id: { eq: $thread_id } }`, "id",
			map[string]any{"thread_id": id}); err != nil {
			return err
		}
	}
	_, err = s.internalStoreMutationRows(ctx, "threads", `delete: true, where: { id: { eq: $id } }`, "id", map[string]any{"id": id})
	return err
}

func threadAPIRow(row map[string]any) map[string]any {
	return map[string]any{
		"id":              stringMapValue(row, "id"),
		"title":           stringMapValue(row, "title"),
		"profile":         stringMapValue(row, "profile"),
		"status":          stringMapValue(row, "status"),
		"channel":         stringMapValue(row, "channel"),
		"active_run_id":   stringMapValue(row, "active_run_id"),
		"last_message_at": stringMapValue(row, "last_message_at"),
		"last_seen_at":    stringMapValue(row, "last_seen_at"),
		"unread":          threadUnread(row),
		"created_at":      stringMapValue(row, "created_at"),
		"updated_at":      stringMapValue(row, "updated_at"),
		"owner_ref":       safeArtifactIdentity(stringMapValue(row, "owner_id"), false),
	}
}

// threadUnread reports a message newer than the owner last saw. The owner's
// own messages mark the thread seen as they are written, so only assistant
// messages (answers, and watch posts to a home thread) can make it unread.
func threadUnread(row map[string]any) bool {
	last, ok := parseWatchTime(stringMapValue(row, "last_message_at"))
	if !ok {
		return false
	}
	seen, ok := parseWatchTime(stringMapValue(row, "last_seen_at"))
	return ok && last.After(seen)
}

// --- messages ----------------------------------------------------------------

func (s *graphjinService) threadMessages(ctx context.Context, threadID string) ([]map[string]any, error) {
	rows, err := s.internalStoreAllRowsOrdered(ctx, "messages", `where: { thread_id: { eq: $thread_id } }`,
		messageStoreFields, "seq", map[string]any{"thread_id": threadID})
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, messageAPIRow(row))
	}
	return out, nil
}

func messageAPIRow(row map[string]any) map[string]any {
	out := map[string]any{
		"id":         stringMapValue(row, "id"),
		"thread_id":  stringMapValue(row, "thread_id"),
		"seq":        int64MapValue(row, "seq"),
		"role":       stringMapValue(row, "role"),
		"content":    stringMapValue(row, "content"),
		"status":     stringMapValue(row, "status"),
		"run_id":     stringMapValue(row, "run_id"),
		"created_at": stringMapValue(row, "created_at"),
	}
	// JSON held in text columns can come back already decoded, so these
	// read through jsonMapString rather than stringMapValue.
	if ids := parseJSONValue(jsonMapString(row, "catalog_ids")); ids != nil {
		out["catalog_ids"] = ids
	}
	if resp := parseJSONValue(jsonMapString(row, "response_json")); resp != nil {
		out["response"] = resp
	}
	return out
}

// threadHistory loads the most recent messages as agent history turns.
func (s *graphjinService) threadHistory(ctx context.Context, threadID string, turns int) ([]gjagent.Turn, error) {
	if turns <= 0 {
		return nil, nil
	}
	rows, err := s.internalStoreRows(ctx, "messages",
		fmt.Sprintf(`where: { thread_id: { eq: $thread_id } }, order_by: { seq: desc }, limit: %d`, turns),
		`role content status catalog_ids`, map[string]any{"thread_id": threadID})
	if err != nil {
		return nil, err
	}
	history := make([]gjagent.Turn, 0, len(rows))
	for i := len(rows) - 1; i >= 0; i-- {
		row := rows[i]
		content := stringMapValue(row, "content")
		role := stringMapValue(row, "role")
		if strings.TrimSpace(content) == "" || (role != "user" && role != "assistant") {
			continue
		}
		turn := gjagent.Turn{Role: role, Content: content}
		if role == "assistant" {
			turn.Status = stringMapValue(row, "status")
			var ids []string
			if raw := jsonMapString(row, "catalog_ids"); raw != "" && json.Unmarshal([]byte(raw), &ids) == nil {
				turn.CatalogIDs = ids
			}
		}
		history = append(history, turn)
	}
	return history, nil
}

type messageSpec struct {
	ThreadID     string
	Role         string
	Content      string
	Status       string
	RunID        string
	CatalogIDs   []string
	ResponseJSON string
	AccountID    string
	OwnerID      string
}

// appendMessage writes the next message of a thread. Callers hold the
// thread's active run, so they are its only writer; the seq-derived primary
// key still rejects a duplicate if that invariant is ever broken.
func (s *graphjinService) appendMessage(ctx context.Context, spec messageSpec) (map[string]any, error) {
	last, err := s.internalStoreRows(ctx, "messages",
		`where: { thread_id: { eq: $thread_id } }, order_by: { seq: desc }, limit: 1`, "seq",
		map[string]any{"thread_id": spec.ThreadID})
	if err != nil {
		return nil, err
	}
	seq := int64(1)
	if len(last) == 1 {
		seq = int64MapValue(last[0], "seq") + 1
	}
	catalogIDs := ""
	if len(spec.CatalogIDs) > 0 {
		raw, _ := json.Marshal(spec.CatalogIDs)
		catalogIDs = string(raw)
	}
	now := storeNow()
	input := map[string]any{
		"id":            fmt.Sprintf("%s-%08d", spec.ThreadID, seq),
		"thread_id":     spec.ThreadID,
		"seq":           seq,
		"role":          spec.Role,
		"content":       truncateUTF8(spec.Content, maxMessageBytes),
		"status":        spec.Status,
		"run_id":        spec.RunID,
		"catalog_ids":   catalogIDs,
		"response_json": spec.ResponseJSON,
		"account_id":    spec.AccountID,
		"owner_id":      spec.OwnerID,
		"created_at":    now,
	}
	rows, err := s.internalStoreMutationRows(ctx, "messages", `insert: $input`, messageStoreFields, map[string]any{"input": input})
	if err != nil {
		return nil, err
	}
	threadUpdate := map[string]any{"last_message_at": now, "updated_at": now}
	if spec.Role == "user" {
		threadUpdate["last_seen_at"] = now
	}
	if _, err := s.internalStoreMutationRows(ctx, "threads", `where: { id: { eq: $id } }, update: $input`, "id",
		map[string]any{"id": spec.ThreadID, "input": threadUpdate}); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return messageAPIRow(input), nil
	}
	return messageAPIRow(rows[0]), nil
}

// --- runs --------------------------------------------------------------------

// claimThreadRun makes runID the thread's active run, or reports errThreadBusy.
func (s *graphjinService) claimThreadRun(ctx context.Context, threadID, runID string) error {
	if _, err := s.internalStoreMutationRows(ctx, "threads",
		`where: { id: { eq: $id }, active_run_id: { eq: "" } }, update: $input`, "id",
		map[string]any{"id": threadID, "input": map[string]any{"active_run_id": runID, "updated_at": storeNow()}}); err != nil {
		return err
	}
	row, err := s.threadStoreRow(ctx, threadID)
	if err != nil {
		return err
	}
	if row == nil {
		return errThreadNotFound
	}
	if stringMapValue(row, "active_run_id") != runID {
		return errThreadBusy
	}
	return nil
}

// releaseThreadRun clears the thread's active run if it is still runID.
func (s *graphjinService) releaseThreadRun(ctx context.Context, threadID, runID string) error {
	_, err := s.internalStoreMutationRows(ctx, "threads",
		`where: { id: { eq: $id }, active_run_id: { eq: $run_id } }, update: $input`, "id",
		map[string]any{"id": threadID, "run_id": runID, "input": map[string]any{"active_run_id": "", "updated_at": storeNow()}})
	return err
}

func (s *graphjinService) insertRun(ctx context.Context, thread map[string]any, runID string) error {
	now := storeNow()
	lease := time.Duration(s.conf.Core.EffectiveThreadsConfig().RunLeaseSeconds) * time.Second
	input := map[string]any{
		"id":             runID,
		"thread_id":      stringMapValue(thread, "id"),
		"status":         runStatusRunning,
		"lease_until_ms": time.Now().Add(lease).UnixMilli(),
		"replica":        replicaName,
		"tokens_in":      int64(0),
		"tokens_out":     int64(0),
		"error":          "",
		"account_id":     stringMapValue(thread, "account_id"),
		"owner_id":       stringMapValue(thread, "owner_id"),
		"created_at":     now,
		"updated_at":     now,
	}
	_, err := s.internalStoreMutationRows(ctx, "runs", `insert: $input`, "id", map[string]any{"input": input})
	return err
}

func (s *graphjinService) runStoreRow(ctx context.Context, runID string) (map[string]any, error) {
	rows, err := s.internalStoreRows(ctx, "runs", `where: { id: { eq: $id } }`, runStoreFields, map[string]any{"id": runID})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

// runForCaller returns a run the caller owns; admins may read any.
func (s *graphjinService) runForCaller(ctx context.Context, runID string) (map[string]any, error) {
	ownerID, ok := artifactUserID(ctx)
	if !ok {
		return nil, errRunNotFound
	}
	row, err := s.runStoreRow(ctx, strings.TrimSpace(runID))
	if err != nil {
		return nil, err
	}
	if row == nil || (stringMapValue(row, "owner_id") != ownerID && !s.identityRoleIsAdmin(ctx)) {
		return nil, errRunNotFound
	}
	return row, nil
}

func runAPIRow(row map[string]any) map[string]any {
	return map[string]any{
		"id":          stringMapValue(row, "id"),
		"thread_id":   stringMapValue(row, "thread_id"),
		"status":      stringMapValue(row, "status"),
		"tokens_in":   int64MapValue(row, "tokens_in"),
		"tokens_out":  int64MapValue(row, "tokens_out"),
		"error":       stringMapValue(row, "error"),
		"created_at":  stringMapValue(row, "created_at"),
		"updated_at":  stringMapValue(row, "updated_at"),
		"finished_at": stringMapValue(row, "finished_at"),
	}
}

// heartbeatRun renews the run lease and reports its current status, so the
// executing replica notices a cancel requested through any other replica.
func (s *graphjinService) heartbeatRun(ctx context.Context, runID string) (string, error) {
	lease := time.Duration(s.conf.Core.EffectiveThreadsConfig().RunLeaseSeconds) * time.Second
	if _, err := s.internalStoreMutationRows(ctx, "runs",
		`where: { id: { eq: $id }, status: { in: ["running", "cancel_requested"] } }, update: $input`, "id",
		map[string]any{"id": runID, "input": map[string]any{"lease_until_ms": time.Now().Add(lease).UnixMilli()}}); err != nil {
		return "", err
	}
	row, err := s.runStoreRow(ctx, runID)
	if err != nil || row == nil {
		return "", err
	}
	return stringMapValue(row, "status"), nil
}

// requestRunCancel marks a run for cancellation. The executing replica sees it
// on its next status poll; a run on this replica is cancelled at once.
func (s *graphjinService) requestRunCancel(ctx context.Context, runID string) (map[string]any, error) {
	if _, err := s.runForCaller(ctx, runID); err != nil {
		return nil, err
	}
	if _, err := s.internalStoreMutationRows(ctx, "runs",
		`where: { id: { eq: $id }, status: { eq: "running" } }, update: $input`, "id",
		map[string]any{"id": runID, "input": map[string]any{"status": runStatusCancelRequested, "updated_at": storeNow()}}); err != nil {
		return nil, err
	}
	if cancel, ok := localRuns.Load(runID); ok {
		cancel.(context.CancelFunc)()
	}
	row, err := s.runStoreRow(ctx, runID)
	if err != nil || row == nil {
		return nil, err
	}
	return runAPIRow(row), nil
}

// finishRun records a run's final state unless another replica already
// failed it (a lost lease).
func (s *graphjinService) finishRun(ctx context.Context, runID, status, errMsg string, usage gjagent.UsageTotals) error {
	now := storeNow()
	_, err := s.internalStoreMutationRows(ctx, "runs",
		`where: { id: { eq: $id }, status: { in: ["running", "cancel_requested"] } }, update: $input`, "id",
		map[string]any{"id": runID, "input": map[string]any{
			"status": status, "error": truncateUTF8(errMsg, 2048),
			"tokens_in": usage.PromptTokens, "tokens_out": usage.CompletionTokens,
			"updated_at": now, "finished_at": now,
		}})
	return err
}

// --- run events --------------------------------------------------------------

func (s *graphjinService) appendRunEvent(ctx context.Context, runID string, seq int64, kind string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	input := map[string]any{
		"id":         fmt.Sprintf("%s-%06d", runID, seq),
		"run_id":     runID,
		"seq":        seq,
		"kind":       kind,
		"payload":    string(raw),
		"created_at": storeNow(),
	}
	_, err = s.internalStoreMutationRows(ctx, "run_events", `insert: $input`, "id", map[string]any{"input": input})
	return err
}

type runEvent struct {
	Seq     int64
	Kind    string
	Payload json.RawMessage
}

func (s *graphjinService) runEventsAfter(ctx context.Context, runID string, after int64) ([]runEvent, error) {
	rows, err := s.internalStoreAllRowsOrdered(ctx, "run_events",
		`where: { run_id: { eq: $run_id }, seq: { gt: $after } }`, runEventStoreFields, "seq",
		map[string]any{"run_id": runID, "after": after})
	if err != nil {
		return nil, err
	}
	out := make([]runEvent, 0, len(rows))
	for _, row := range rows {
		out = append(out, runEvent{
			Seq:     int64MapValue(row, "seq"),
			Kind:    stringMapValue(row, "kind"),
			Payload: json.RawMessage(jsonMapString(row, "payload")),
		})
	}
	return out, nil
}

// --- lease sweep -------------------------------------------------------------

func (s *graphjinService) startThreadSweeper(parent context.Context) {
	if s == nil || s.gj == nil || !s.threadsEnabled() {
		return
	}
	if _, _, ok := s.threadDB(); !ok {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	s.addCloseFn(cancel)
	s.revisionConsumerWG.Add(1)
	go func() {
		defer s.revisionConsumerWG.Done()
		ticker := time.NewTicker(threadSweepInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			if _, err := s.sweepExpiredRuns(ctx, time.Now()); err != nil && ctx.Err() == nil && s.log != nil {
				s.log.Warnf("thread run sweep failed: %v", err)
			}
		}
	}()
}

// sweepExpiredRuns fails runs whose replica stopped renewing their lease. Each
// expired run is claimed with a compare-and-set on its old lease, so exactly
// one replica fails it. Runs are never re-executed: they may have written data.
func (s *graphjinService) sweepExpiredRuns(ctx context.Context, now time.Time) (int, error) {
	rows, err := s.internalStoreAllRows(ctx, "runs",
		`where: { status: { in: ["running", "cancel_requested"] }, lease_until_ms: { lt: $now } }`,
		runStoreFields, map[string]any{"now": now.UnixMilli()})
	if err != nil {
		return 0, err
	}
	failed := 0
	for _, row := range rows {
		runID := stringMapValue(row, "id")
		lease := int64MapValue(row, "lease_until_ms")
		ts := storeNow()
		if _, err := s.internalStoreMutationRows(ctx, "runs",
			`where: { id: { eq: $id }, lease_until_ms: { eq: $lease }, status: { in: ["running", "cancel_requested"] } }, update: $input`, "id",
			map[string]any{"id": runID, "lease": lease, "input": map[string]any{
				"status": runStatusFailed, "error": "run lost: the replica executing it stopped",
				"updated_at": ts, "finished_at": ts,
			}}); err != nil {
			return failed, err
		}
		current, err := s.runStoreRow(ctx, runID)
		if err != nil || current == nil || stringMapValue(current, "status") != runStatusFailed ||
			int64MapValue(current, "lease_until_ms") != lease {
			continue
		}
		failed++
		threadID := stringMapValue(row, "thread_id")
		if _, err := s.appendMessage(ctx, messageSpec{
			ThreadID: threadID, Role: "assistant", Status: runStatusFailed, RunID: runID,
			Content:   "This run stopped before it finished because the server handling it went away. Ask again to retry.",
			AccountID: stringMapValue(row, "account_id"), OwnerID: stringMapValue(row, "owner_id"),
		}); err != nil && s.log != nil {
			s.log.Warnf("thread %s: record lost run %s: %v", threadID, runID, err)
		}
		if err := s.releaseThreadRun(ctx, threadID, runID); err != nil {
			return failed, err
		}
	}
	return failed, nil
}

// --- DDL ---------------------------------------------------------------------

func threadDDL(dbType, schema string) []string {
	threads := artifactTableName(dbType, schema, "threads")
	messages := artifactTableName(dbType, schema, "messages")
	runs := artifactTableName(dbType, schema, "runs")
	events := artifactTableName(dbType, schema, "run_events")
	switch dbType {
	case "postgres", "":
		idx := func(name, table, cols string) string {
			return fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (%s)`, quotePGIdent(name), table, cols)
		}
		return []string{
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
id TEXT PRIMARY KEY,
title TEXT NOT NULL DEFAULT '',
profile TEXT NOT NULL DEFAULT '',
status TEXT NOT NULL DEFAULT 'open',
channel TEXT NOT NULL DEFAULT 'web',
channel_ref TEXT NOT NULL DEFAULT '',
active_run_id TEXT NOT NULL DEFAULT '',
account_id TEXT NOT NULL DEFAULT '',
owner_id TEXT NOT NULL DEFAULT '',
owner_role TEXT NOT NULL DEFAULT 'user',
last_message_at TIMESTAMPTZ,
last_seen_at TIMESTAMPTZ,
created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`, threads),
			idx("idx_graphjin_threads_owner", threads, "owner_id, status"),
			idx("idx_graphjin_threads_channel", threads, "channel, channel_ref"),
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
id TEXT PRIMARY KEY,
thread_id TEXT NOT NULL,
seq BIGINT NOT NULL,
role TEXT NOT NULL,
content TEXT NOT NULL DEFAULT '',
status TEXT NOT NULL DEFAULT '',
run_id TEXT NOT NULL DEFAULT '',
catalog_ids TEXT NOT NULL DEFAULT '',
response_json TEXT NOT NULL DEFAULT '',
account_id TEXT NOT NULL DEFAULT '',
owner_id TEXT NOT NULL DEFAULT '',
created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`, messages),
			idx("idx_graphjin_messages_thread_seq", messages, "thread_id, seq"),
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
id TEXT PRIMARY KEY,
thread_id TEXT NOT NULL,
status TEXT NOT NULL DEFAULT 'running',
lease_until_ms BIGINT NOT NULL DEFAULT 0,
replica TEXT NOT NULL DEFAULT '',
tokens_in BIGINT NOT NULL DEFAULT 0,
tokens_out BIGINT NOT NULL DEFAULT 0,
error TEXT NOT NULL DEFAULT '',
account_id TEXT NOT NULL DEFAULT '',
owner_id TEXT NOT NULL DEFAULT '',
created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
finished_at TIMESTAMPTZ
)`, runs),
			idx("idx_graphjin_runs_thread", runs, "thread_id"),
			idx("idx_graphjin_runs_status_lease", runs, "status, lease_until_ms"),
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
id TEXT PRIMARY KEY,
run_id TEXT NOT NULL,
seq BIGINT NOT NULL,
kind TEXT NOT NULL,
payload TEXT NOT NULL DEFAULT '',
created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`, events),
			idx("idx_graphjin_run_events_run_seq", events, "run_id, seq"),
		}
	case "mysql", "mariadb":
		return []string{
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
id VARCHAR(191) PRIMARY KEY,
title VARCHAR(255) NOT NULL DEFAULT '',
profile VARCHAR(191) NOT NULL DEFAULT '',
status VARCHAR(32) NOT NULL DEFAULT 'open',
channel VARCHAR(32) NOT NULL DEFAULT 'web',
channel_ref VARCHAR(191) NOT NULL DEFAULT '',
active_run_id VARCHAR(191) NOT NULL DEFAULT '',
account_id VARCHAR(191) NOT NULL DEFAULT '',
owner_id VARCHAR(191) NOT NULL DEFAULT '',
owner_role VARCHAR(64) NOT NULL DEFAULT 'user',
last_message_at VARCHAR(64),
last_seen_at VARCHAR(64),
created_at VARCHAR(64) NOT NULL DEFAULT '',
updated_at VARCHAR(64) NOT NULL DEFAULT '',
KEY idx_graphjin_threads_owner (owner_id, status),
KEY idx_graphjin_threads_channel (channel, channel_ref)
) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`, threads),
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
id VARCHAR(191) PRIMARY KEY,
thread_id VARCHAR(191) NOT NULL,
seq BIGINT NOT NULL,
role VARCHAR(32) NOT NULL,
content LONGTEXT NOT NULL,
status VARCHAR(64) NOT NULL DEFAULT '',
run_id VARCHAR(191) NOT NULL DEFAULT '',
catalog_ids LONGTEXT NOT NULL,
response_json LONGTEXT NOT NULL,
account_id VARCHAR(191) NOT NULL DEFAULT '',
owner_id VARCHAR(191) NOT NULL DEFAULT '',
created_at VARCHAR(64) NOT NULL DEFAULT '',
KEY idx_graphjin_messages_thread_seq (thread_id, seq)
) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`, messages),
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
id VARCHAR(191) PRIMARY KEY,
thread_id VARCHAR(191) NOT NULL,
status VARCHAR(32) NOT NULL DEFAULT 'running',
lease_until_ms BIGINT NOT NULL DEFAULT 0,
replica VARCHAR(191) NOT NULL DEFAULT '',
tokens_in BIGINT NOT NULL DEFAULT 0,
tokens_out BIGINT NOT NULL DEFAULT 0,
error LONGTEXT NOT NULL,
account_id VARCHAR(191) NOT NULL DEFAULT '',
owner_id VARCHAR(191) NOT NULL DEFAULT '',
created_at VARCHAR(64) NOT NULL DEFAULT '',
updated_at VARCHAR(64) NOT NULL DEFAULT '',
finished_at VARCHAR(64),
KEY idx_graphjin_runs_thread (thread_id),
KEY idx_graphjin_runs_status_lease (status, lease_until_ms)
) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`, runs),
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
id VARCHAR(191) PRIMARY KEY,
run_id VARCHAR(191) NOT NULL,
seq BIGINT NOT NULL,
kind VARCHAR(32) NOT NULL,
payload LONGTEXT NOT NULL,
created_at VARCHAR(64) NOT NULL DEFAULT '',
KEY idx_graphjin_run_events_run_seq (run_id, seq)
) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`, events),
		}
	default:
		idx := func(name, table, cols string) string {
			return fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (%s)`, quoteSQLIdent(name), table, cols)
		}
		return []string{
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
id TEXT PRIMARY KEY,
title TEXT NOT NULL DEFAULT '',
profile TEXT NOT NULL DEFAULT '',
status TEXT NOT NULL DEFAULT 'open',
channel TEXT NOT NULL DEFAULT 'web',
channel_ref TEXT NOT NULL DEFAULT '',
active_run_id TEXT NOT NULL DEFAULT '',
account_id TEXT NOT NULL DEFAULT '',
owner_id TEXT NOT NULL DEFAULT '',
owner_role TEXT NOT NULL DEFAULT 'user',
last_message_at TEXT,
last_seen_at TEXT,
created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
)`, threads),
			idx("idx_graphjin_threads_owner", threads, "owner_id, status"),
			idx("idx_graphjin_threads_channel", threads, "channel, channel_ref"),
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
id TEXT PRIMARY KEY,
thread_id TEXT NOT NULL,
seq INTEGER NOT NULL,
role TEXT NOT NULL,
content TEXT NOT NULL DEFAULT '',
status TEXT NOT NULL DEFAULT '',
run_id TEXT NOT NULL DEFAULT '',
catalog_ids TEXT NOT NULL DEFAULT '',
response_json TEXT NOT NULL DEFAULT '',
account_id TEXT NOT NULL DEFAULT '',
owner_id TEXT NOT NULL DEFAULT '',
created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
)`, messages),
			idx("idx_graphjin_messages_thread_seq", messages, "thread_id, seq"),
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
id TEXT PRIMARY KEY,
thread_id TEXT NOT NULL,
status TEXT NOT NULL DEFAULT 'running',
lease_until_ms INTEGER NOT NULL DEFAULT 0,
replica TEXT NOT NULL DEFAULT '',
tokens_in INTEGER NOT NULL DEFAULT 0,
tokens_out INTEGER NOT NULL DEFAULT 0,
error TEXT NOT NULL DEFAULT '',
account_id TEXT NOT NULL DEFAULT '',
owner_id TEXT NOT NULL DEFAULT '',
created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
finished_at TEXT
)`, runs),
			idx("idx_graphjin_runs_thread", runs, "thread_id"),
			idx("idx_graphjin_runs_status_lease", runs, "status, lease_until_ms"),
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
id TEXT PRIMARY KEY,
run_id TEXT NOT NULL,
seq INTEGER NOT NULL,
kind TEXT NOT NULL,
payload TEXT NOT NULL DEFAULT '',
created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
)`, events),
			idx("idx_graphjin_run_events_run_seq", events, "run_id, seq"),
		}
	}
}

// migrateThreadSchema brings a thread store created by an older build up to
// date. Threads from before last_seen_at count as seen up to their last
// message; new threads always write last_seen_at, so the backfill touches
// only those old rows however often it runs.
func migrateThreadSchema(ctx context.Context, db *sql.DB, dbType, schema string) error {
	threads := artifactTableName(dbType, schema, "threads")
	definition := "TEXT"
	switch dbType {
	case "postgres", "":
		definition = "TIMESTAMPTZ"
	case "mysql", "mariadb":
		definition = "VARCHAR(64)"
	}
	if err := ensureSQLColumn(ctx, db, dbType, threads, "last_seen_at", definition); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, fmt.Sprintf("UPDATE %s SET last_seen_at = COALESCE(last_message_at, created_at) WHERE last_seen_at IS NULL", threads))
	return err
}
