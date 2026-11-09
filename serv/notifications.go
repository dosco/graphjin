package serv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dosco/graphjin/auth/v3"
	"github.com/dosco/graphjin/core/v3"
)

// Notifications are how GraphJin reaches a person about something that
// happened while they were not in the conversation: a change waits for their
// approval, a routine posted an answer, a background run failed. Each one is
// an owner-scoped row in the store, read and marked seen through the
// gj_notification root or /api/v1/notifications, and streamed live.
//
// Producers call notify. It is idempotent per owner and dedupe key, so a retried
// producer never posts twice, and it fails open: a notification that cannot be
// stored is logged and never fails the action that posted it.

const (
	notificationsRootTable  = "gj_notification"
	notificationStoreFields = `id owner_id account_id delegated_by kind severity title body ref_kind ref_id link dedupe_key seen seen_at created_at`

	notificationSeverityInfo    = "info"
	notificationSeverityAction  = "action"
	notificationSeverityWarning = "warning"

	notificationListDefaultLimit = 50
	notificationListMaxLimit     = 200
	notificationStreamPing       = 30 * time.Second
	notificationStreamRetry      = 2 * time.Second

	// notificationTimeLayout is fixed-width so text order is time order on
	// every store dialect.
	notificationTimeLayout = "2006-01-02T15:04:05.000000Z"
)

// Notification kinds.
const (
	notificationApprovalPending = "approval_pending"
	notificationApprovalDecided = "approval_decided"
	notificationRoutineAnswer   = "routine_answer"
	notificationRunFailed       = "run_failed"
	notificationTaskVerifyFail  = "task_verify_failed"
	notificationAutonomousWrite = "autonomous_write"
)

var errNotificationNotFound = errors.New("notification not found")

// notification is what a producer posts. OwnerID is who it is for.
type notification struct {
	OwnerID     string
	AccountID   string
	DelegatedBy string
	Kind        string
	Severity    string
	Title       string
	Body        string
	RefKind     string
	RefID       string
	Link        string
	// DedupeKey makes the post idempotent: the same owner and key is one
	// notification however often it is posted.
	DedupeKey string
}

func configNotificationsEnabled(conf *Config) bool {
	return conf != nil && conf.Core.Artifacts.Enabled &&
		(configWatchesEnabled(conf) || configTasksEnabled(conf) || configThreadsEnabled(conf) || configApprovalsEnabled(conf))
}

func (s *graphjinService) notificationsEnabled() bool {
	return s != nil && configNotificationsEnabled(s.conf)
}

func notificationTime(t time.Time) string {
	return t.UTC().Format(notificationTimeLayout)
}

func notificationID(ownerID, dedupeKey string) string {
	return "ntf_" + hashString(ownerID + "\x00" + dedupeKey)[:24]
}

// notify stores a notification unless the owner already has one with its
// dedupe key. It never returns an error to the producer.
func (s *graphjinService) notify(ctx context.Context, n notification) {
	if !s.notificationsEnabled() || strings.TrimSpace(n.OwnerID) == "" {
		return
	}
	if err := s.storeNotification(ctx, n, time.Now()); err != nil && s.log != nil {
		s.log.Warnf("notification %s for %s: %v", n.Kind, n.OwnerID, err)
	}
}

func (s *graphjinService) storeNotification(ctx context.Context, n notification, now time.Time) error {
	if n.DedupeKey == "" {
		n.DedupeKey = n.Kind + ":" + n.RefKind + ":" + n.RefID + ":" + notificationTime(now)
	}
	if n.Severity == "" {
		n.Severity = notificationSeverityInfo
	}
	// The store context outlives the producer's request: a decision made over
	// HTTP still records its notification after the response is written.
	ctx = context.WithoutCancel(ctx)
	id := notificationID(n.OwnerID, n.DedupeKey)
	existing, err := s.internalStoreRows(ctx, "notifications", `where: { id: { eq: $id } }, limit: 1`, "id", map[string]any{"id": id})
	if err != nil {
		return err
	}
	if len(existing) != 0 {
		return nil
	}
	input := map[string]any{
		"id": id, "owner_id": n.OwnerID, "account_id": n.AccountID, "delegated_by": n.DelegatedBy,
		"kind": n.Kind, "severity": n.Severity, "title": truncateUTF8(n.Title, 200), "body": truncateUTF8(n.Body, 2000),
		"ref_kind": n.RefKind, "ref_id": n.RefID, "link": n.Link, "dedupe_key": n.DedupeKey,
		"seen": false, "created_at": notificationTime(now),
	}
	if _, err := s.internalStoreMutationRows(ctx, "notifications", `insert: $input`, "id", map[string]any{"input": input}); err != nil {
		// A concurrent producer inserted the same id first.
		if again, rerr := s.internalStoreRows(ctx, "notifications", `where: { id: { eq: $id } }, limit: 1`, "id", map[string]any{"id": id}); rerr == nil && len(again) != 0 {
			return nil
		}
		return err
	}
	return s.bumpArtifactRevision(ctx, "notifications")
}

// markNotificationSeenByKey marks the owner's notification with this dedupe
// key seen, for a producer whose follow-up settles it (a decided approval
// settles its "waiting for you" notification).
func (s *graphjinService) markNotificationSeenByKey(ctx context.Context, ownerID, dedupeKey string) {
	if !s.notificationsEnabled() || ownerID == "" {
		return
	}
	ctx = context.WithoutCancel(ctx)
	if _, err := s.setNotificationsSeen(ctx, ownerID, []string{notificationID(ownerID, dedupeKey)}, true); err != nil && s.log != nil {
		s.log.Warnf("settle notification for %s: %v", ownerID, err)
	}
}

func notificationAPIRow(row map[string]any) map[string]any {
	return map[string]any{
		"id": stringMapValue(row, "id"), "kind": stringMapValue(row, "kind"), "severity": stringMapValue(row, "severity"),
		"title": stringMapValue(row, "title"), "body": stringMapValue(row, "body"),
		"ref_kind": stringMapValue(row, "ref_kind"), "ref_id": stringMapValue(row, "ref_id"), "link": stringMapValue(row, "link"),
		"delegated_by": stringMapValue(row, "delegated_by"),
		"seen": boolMapValue(row, "seen"), "seen_at": stringMapValue(row, "seen_at"), "created_at": stringMapValue(row, "created_at"),
	}
}

// listNotifications is the caller's notifications, newest first.
func (s *graphjinService) listNotifications(ctx context.Context, unseenOnly bool, limit int, after string) ([]map[string]any, error) {
	ownerID, ok := artifactUserID(ctx)
	if !ok {
		return nil, nil
	}
	if limit <= 0 {
		limit = notificationListDefaultLimit
	}
	if limit > notificationListMaxLimit {
		limit = notificationListMaxLimit
	}
	where := `owner_id: { eq: $owner_id }`
	vars := map[string]any{"owner_id": ownerID}
	if unseenOnly {
		where += `, seen: { eq: false }`
	}
	if after != "" {
		where += `, created_at: { gt: $after }`
		vars["after"] = after
	}
	order := "desc"
	if after != "" {
		// A stream reads forward from its cursor.
		order = "asc"
	}
	rows, err := s.internalStoreRows(ctx, "notifications",
		fmt.Sprintf(`where: { %s }, order_by: { created_at: %s }, limit: %d`, where, order, limit), notificationStoreFields, vars)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if stringMapValue(row, "owner_id") == ownerID {
			out = append(out, notificationAPIRow(row))
		}
	}
	return out, nil
}

// unseenNotificationCount counts in the store, not in Go.
func (s *graphjinService) unseenNotificationCount(ctx context.Context) (int64, error) {
	ownerID, ok := artifactUserID(ctx)
	if !ok {
		return 0, nil
	}
	root, schema, ok := s.internalStoreRoot("notifications")
	if !ok {
		return 0, nil
	}
	field := internalStoreQueryField(root, schema, `where: { owner_id: { eq: $owner_id }, seen: { eq: false } }`)
	out, err := s.internalStoreGraphQL(ctx, fmt.Sprintf("query { %s { count_id } }", field), map[string]any{"owner_id": ownerID})
	if err != nil {
		return 0, err
	}
	return countFromAggregate(out[root]), nil
}

// countFromAggregate reads count_id from an aggregate root, which GraphJin
// returns as one object or as a one-row list depending on the dialect.
func countFromAggregate(raw json.RawMessage) int64 {
	var one map[string]any
	if json.Unmarshal(raw, &one) == nil && one != nil {
		return int64MapValue(one, "count_id")
	}
	var rows []map[string]any
	if json.Unmarshal(raw, &rows) == nil && len(rows) != 0 {
		return int64MapValue(rows[0], "count_id")
	}
	return 0
}

// setNotificationsSeen marks the owner's notifications seen or unseen. With no
// ids it marks every unseen one seen.
func (s *graphjinService) setNotificationsSeen(ctx context.Context, ownerID string, ids []string, seen bool) (int, error) {
	now := notificationTime(time.Now())
	input := map[string]any{"seen": seen, "seen_at": nullableSeenAt(seen, now)}
	if len(ids) == 0 {
		// Collect the ids first: an update filtered on seen re-reads its rows
		// after setting them seen and would report none.
		unseen, err := s.internalStoreAllRows(ctx, "notifications", `where: { owner_id: { eq: $owner_id }, seen: { eq: false } }`, "id",
			map[string]any{"owner_id": ownerID})
		if err != nil {
			return 0, err
		}
		for _, row := range unseen {
			ids = append(ids, stringMapValue(row, "id"))
		}
		if len(ids) == 0 {
			return 0, nil
		}
	}
	rows, err := s.internalStoreMutationRows(ctx, "notifications",
		`where: { owner_id: { eq: $owner_id }, id: { in: $ids } }, update: $input`, "id",
		map[string]any{"owner_id": ownerID, "ids": ids, "input": input})
	if err != nil {
		return 0, err
	}
	if len(rows) != 0 {
		if err := s.bumpArtifactRevision(ctx, "notifications"); err != nil {
			return len(rows), err
		}
	}
	return len(rows), nil
}

// --- GraphQL root -------------------------------------------------------------

type notificationControlPlane struct {
	service *graphjinService
}

func notificationColumns() []core.ManagedColumn {
	return []core.ManagedColumn{
		cpCol("id", "text", true), cpCol("kind", "text", false), cpCol("severity", "text", false),
		cpCol("title", "text", false), cpCol("body", "text", false),
		cpCol("ref_kind", "text", false), cpCol("ref_id", "text", false), cpCol("link", "text", false),
		cpCol("delegated_by", "text", false),
		cpCol("seen", "boolean", false), cpCol("seen_at", "text", false), cpCol("created_at", "text", false),
	}
}

func (h notificationControlPlane) ManagedQueryTables() []core.ManagedTable {
	if h.service == nil || !h.service.notificationsEnabled() {
		return nil
	}
	return []core.ManagedTable{managedTable(notificationsRootTable, notificationColumns())}
}

func (h notificationControlPlane) ManagedMutationTables() []string {
	if h.service == nil || !h.service.notificationsEnabled() {
		return nil
	}
	return []string{notificationsRootTable}
}

func (h notificationControlPlane) ExecuteManagedQuery(ctx context.Context, req core.ManagedQueryRequest) (json.RawMessage, error) {
	out := make(map[string]any, len(req.Roots))
	for _, root := range req.Roots {
		rows, err := h.service.listNotifications(ctx, false, notificationListMaxLimit, "")
		if err != nil {
			return nil, err
		}
		out[root.FieldName] = filterRows(applyManagedQuery(rows, root), root.Fields)
	}
	return json.Marshal(out)
}

// ExecuteManagedMutation lets a person mark their own notifications seen or
// unseen. Notifications are created only by GraphJin.
func (h notificationControlPlane) ExecuteManagedMutation(ctx context.Context, req core.ManagedMutationRequest) (json.RawMessage, error) {
	ownerID, ok := artifactUserID(ctx)
	if !ok {
		return nil, fmt.Errorf("notifications require user identity")
	}
	out := make(map[string]any, len(req.Roots))
	for _, root := range req.Roots {
		if root.Operation != "update" {
			return nil, fmt.Errorf("gj_notification rows are created by GraphJin; mark one with update: { seen: true }")
		}
		for field := range root.Input {
			if field != "seen" {
				return nil, fmt.Errorf("gj_notification %s is server-managed; send only seen", field)
			}
		}
		id := stringWhere(root.Where, "id")
		if id == "" {
			return nil, fmt.Errorf("gj_notification update needs where: { id: { eq: ... } }")
		}
		if _, err := h.service.setNotificationsSeen(ctx, ownerID, []string{id}, boolInput(root.Input, "seen", true)); err != nil {
			return nil, err
		}
		rows, err := h.service.listNotifications(ctx, false, notificationListMaxLimit, "")
		if err != nil {
			return nil, err
		}
		var found map[string]any
		for _, row := range rows {
			if stringMapValue(row, "id") == id {
				found = row
			}
		}
		if found == nil {
			return nil, errNotificationNotFound
		}
		out[root.FieldName] = filterRow(found, root.Fields)
	}
	return json.Marshal(out)
}

// --- REST ---------------------------------------------------------------------

// Notifications serves the caller's notifications:
//
//	GET  /api/v1/notifications              newest first; ?unseen=1, ?limit=N
//	POST /api/v1/notifications/{id}/seen    mark one seen ({"seen": false} marks it unseen)
//	POST /api/v1/notifications/seen         mark every unseen one seen
//	GET  /api/v1/notifications/stream       server-sent events: notification, count
func (s1 *HttpService) Notifications(ah auth.HandlerFunc, ns *string) http.Handler {
	return apiV1Handler(s1, ns, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := s1.Load().(*graphjinService)
		w.Header().Set("Content-Type", "application/json")
		if !s.notificationsEnabled() {
			writeJSONStatus(w, http.StatusNotFound, map[string]any{"error": "notifications are disabled"})
			return
		}
		if err := s.checkGraphJinInitialized(); err != nil {
			renderErr(w, err)
			return
		}
		ctx := s.applyIdentityContext(r.Context())
		ownerID, ok := artifactUserID(ctx)
		if !ok {
			writeJSONStatus(w, http.StatusUnauthorized, map[string]any{"error": "notifications require a signed-in user"})
			return
		}
		path := strings.Trim(strings.TrimPrefix(r.URL.Path, routeNotifications), "/")
		switch {
		case path == "" && r.Method == http.MethodGet:
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			unseen := r.URL.Query().Get("unseen")
			rows, err := s.listNotifications(ctx, unseen == "1" || unseen == "true", limit, "")
			if err != nil {
				writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
				return
			}
			count, err := s.unseenNotificationCount(ctx)
			if err != nil {
				writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
				return
			}
			writeJSONStatus(w, http.StatusOK, map[string]any{"notifications": rows, "unseen_count": count})
		case path == "stream" && r.Method == http.MethodGet:
			s.streamNotifications(ctx, w, r)
		case path == "seen" && r.Method == http.MethodPost:
			n, err := s.setNotificationsSeen(ctx, ownerID, nil, true)
			if err != nil {
				writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
				return
			}
			writeJSONStatus(w, http.StatusOK, map[string]any{"marked": n})
		case strings.HasSuffix(path, "/seen") && r.Method == http.MethodPost:
			id := strings.TrimSuffix(path, "/seen")
			var body struct {
				Seen *bool `json:"seen"`
			}
			if data, _ := io.ReadAll(io.LimitReader(r.Body, 1<<12)); len(strings.TrimSpace(string(data))) != 0 {
				if err := json.Unmarshal(data, &body); err != nil {
					writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": `body must be {"seen": true|false} or empty`})
					return
				}
			}
			seen := body.Seen == nil || *body.Seen
			n, err := s.setNotificationsSeen(ctx, ownerID, []string{id}, seen)
			if err != nil {
				writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
				return
			}
			if n == 0 {
				writeJSONStatus(w, http.StatusNotFound, map[string]any{"error": errNotificationNotFound.Error()})
				return
			}
			writeJSONStatus(w, http.StatusOK, map[string]any{"id": id, "seen": seen})
		default:
			writeJSONStatus(w, http.StatusMethodNotAllowed, map[string]any{"error": "GET /api/v1/notifications[/stream], POST /api/v1/notifications/seen or /api/v1/notifications/{id}/seen"})
		}
	}), ah)
}

// streamNotifications sends the caller's notifications created after the
// cursor (Last-Event-ID or ?after=, both microseconds since the epoch), then
// follows new ones. Every change to the caller's notifications also sends the
// unseen count, so a badge stays right when a notification is marked seen
// elsewhere. It wakes on the store's revision signal, not a fixed poll.
func (s *graphjinService) streamNotifications(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "streaming unsupported"})
		return
	}
	cursor := ""
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		cursor = notificationCursor(v)
	} else if v := r.URL.Query().Get("after"); v != "" {
		cursor = notificationCursor(v)
	}
	if cursor == "" {
		// A fresh client gets only what arrives from now on; it lists the
		// backlog with GET /api/v1/notifications.
		cursor = notificationTime(time.Now())
	}
	startSSE(w)
	signals := s.revisionSignals(ctx, "notifications", true, notificationStreamRetry)
	ping := time.NewTicker(notificationStreamPing)
	defer ping.Stop()
	lastCount := int64(-1)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ping.C:
			_, _ = fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
			continue
		case _, open := <-signals:
			if !open {
				return
			}
		}
		rows, err := s.listNotifications(ctx, false, notificationListMaxLimit, cursor)
		if err != nil {
			writeSSE(w, flusher, 0, "error", map[string]any{"error": err.Error()})
			return
		}
		for _, row := range rows {
			created := stringFromAny(row["created_at"])
			writeSSE(w, flusher, notificationEventID(created), "notification", row)
			cursor = created
		}
		if count, err := s.unseenNotificationCount(ctx); err == nil && count != lastCount {
			writeSSE(w, flusher, 0, "count", map[string]any{"unseen_count": count})
			lastCount = count
		}
	}
}

// notificationEventID is a notification's SSE id: its creation time in
// microseconds, which is also the stream's resume cursor.
func notificationEventID(created string) int64 {
	t, err := time.Parse(notificationTimeLayout, created)
	if err != nil {
		return 0
	}
	return t.UnixMicro()
}

func notificationCursor(v string) string {
	micros, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil || micros <= 0 {
		return ""
	}
	return notificationTime(time.UnixMicro(micros))
}

// --- storage ------------------------------------------------------------------

func notificationDDL(dbType, schema string) []string {
	table := artifactTableName(dbType, schema, "notifications")
	switch dbType {
	case "postgres", "":
		return []string{
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
id TEXT PRIMARY KEY,
owner_id TEXT NOT NULL DEFAULT '',
account_id TEXT NOT NULL DEFAULT '',
delegated_by TEXT NOT NULL DEFAULT '',
kind TEXT NOT NULL DEFAULT '',
severity TEXT NOT NULL DEFAULT 'info',
title TEXT NOT NULL DEFAULT '',
body TEXT NOT NULL DEFAULT '',
ref_kind TEXT NOT NULL DEFAULT '',
ref_id TEXT NOT NULL DEFAULT '',
link TEXT NOT NULL DEFAULT '',
dedupe_key TEXT NOT NULL DEFAULT '',
seen BOOLEAN NOT NULL DEFAULT FALSE,
seen_at TEXT,
created_at TEXT NOT NULL DEFAULT ''
)`, table),
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (owner_id, seen, created_at)`, quotePGIdent("idx_graphjin_notifications_owner"), table),
		}
	case "mysql", "mariadb":
		return []string{fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
id VARCHAR(191) PRIMARY KEY,
owner_id VARCHAR(191) NOT NULL DEFAULT '',
account_id VARCHAR(191) NOT NULL DEFAULT '',
delegated_by VARCHAR(191) NOT NULL DEFAULT '',
kind VARCHAR(64) NOT NULL DEFAULT '',
severity VARCHAR(16) NOT NULL DEFAULT 'info',
title VARCHAR(255) NOT NULL DEFAULT '',
body TEXT NOT NULL,
ref_kind VARCHAR(64) NOT NULL DEFAULT '',
ref_id VARCHAR(191) NOT NULL DEFAULT '',
link VARCHAR(512) NOT NULL DEFAULT '',
dedupe_key VARCHAR(512) NOT NULL DEFAULT '',
seen BOOLEAN NOT NULL DEFAULT FALSE,
seen_at VARCHAR(64),
created_at VARCHAR(64) NOT NULL DEFAULT '',
KEY idx_graphjin_notifications_owner (owner_id, seen, created_at)
) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`, table)}
	default:
		return []string{
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
id TEXT PRIMARY KEY,
owner_id TEXT NOT NULL DEFAULT '',
account_id TEXT NOT NULL DEFAULT '',
delegated_by TEXT NOT NULL DEFAULT '',
kind TEXT NOT NULL DEFAULT '',
severity TEXT NOT NULL DEFAULT 'info',
title TEXT NOT NULL DEFAULT '',
body TEXT NOT NULL DEFAULT '',
ref_kind TEXT NOT NULL DEFAULT '',
ref_id TEXT NOT NULL DEFAULT '',
link TEXT NOT NULL DEFAULT '',
dedupe_key TEXT NOT NULL DEFAULT '',
seen INTEGER NOT NULL DEFAULT 0,
seen_at TEXT,
created_at TEXT NOT NULL DEFAULT ''
)`, table),
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (owner_id, seen, created_at)`, quoteSQLIdent("idx_graphjin_notifications_owner"), table),
		}
	}
}
