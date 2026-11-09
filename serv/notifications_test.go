package serv

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newNotificationService(t *testing.T) *graphjinService {
	t.Helper()
	db, svc := newSQLiteWatchServiceWithOptions(t, 20, nil)
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatal(err)
	}
	startSQLiteWatchCore(t, svc, db)
	if !svc.notificationsEnabled() {
		t.Fatal("notifications must be on whenever a store feature is")
	}
	return svc
}

func TestNotifyIsIdempotentAndOwnerScoped(t *testing.T) {
	svc := newNotificationService(t)
	ctx := approvalUserCtx("user_1")
	n := notification{OwnerID: "user_1", Kind: notificationRunFailed, Severity: notificationSeverityWarning, Title: "A run stopped", RefKind: "thread", RefID: "th_1", DedupeKey: "run_failed:r1"}
	svc.notify(ctx, n)
	svc.notify(ctx, n) // a retried producer
	svc.notify(ctx, notification{OwnerID: "user_2", Kind: notificationRunFailed, Title: "theirs", DedupeKey: "run_failed:r1"})

	rows, err := svc.listNotifications(ctx, false, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["title"] != "A run stopped" || rows[0]["seen"] != false || rows[0]["link"] != "" {
		t.Fatalf("rows = %v", rows)
	}
	if count, err := svc.unseenNotificationCount(ctx); err != nil || count != 1 {
		t.Fatalf("unseen = %d, %v", count, err)
	}
	if theirs, _ := svc.listNotifications(approvalUserCtx("user_2"), false, 0, ""); len(theirs) != 1 || theirs[0]["title"] != "theirs" {
		t.Fatalf("user_2 rows = %v", theirs)
	}

	// Another person cannot mark someone else's notification.
	if n, err := svc.setNotificationsSeen(ctx, "user_2", []string{stringFromAny(rows[0]["id"])}, true); err != nil || n != 0 {
		t.Fatalf("cross-owner mark = %d, %v", n, err)
	}
	if n, err := svc.setNotificationsSeen(ctx, "user_1", nil, true); err != nil || n != 1 {
		t.Fatalf("mark all = %d, %v", n, err)
	}
	if count, _ := svc.unseenNotificationCount(ctx); count != 0 {
		t.Fatalf("unseen after mark all = %d", count)
	}
	// The same dedupe key is still the same notification once seen.
	svc.notify(ctx, n)
	if count, _ := svc.unseenNotificationCount(ctx); count != 0 {
		t.Fatal("a seen notification came back")
	}
}

func TestNotificationRootMarksSeenAndRefusesInserts(t *testing.T) {
	svc := newNotificationService(t)
	ctx := approvalUserCtx("user_1")
	svc.notify(ctx, notification{OwnerID: "user_1", Kind: notificationRoutineAnswer, Title: "A watch fired", DedupeKey: "routine:e1"})
	res, err := svc.gj.GraphQL(ctx, `query { gj_notification { id kind title seen } }`, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(res.Data), `"kind":"routine_answer"`) {
		t.Fatalf("query = %s", res.Data)
	}
	id := stringFromAny(mapFromAny(anySliceFrom(mapFromAny(parseJSONValue(string(res.Data)))["gj_notification"])[0])["id"])
	if _, err := svc.gj.GraphQL(ctx, `mutation { gj_notification(where: { id: { eq: "`+id+`" } }, update: { seen: true }) { id seen } }`, nil, nil); err != nil {
		t.Fatal(err)
	}
	if count, _ := svc.unseenNotificationCount(ctx); count != 0 {
		t.Fatalf("unseen after GraphQL mark = %d", count)
	}
	if _, err := svc.gj.GraphQL(ctx, `mutation { gj_notification(insert: { kind: "x", title: "forged" }) { id } }`, nil, nil); err == nil {
		t.Fatal("a person must not be able to post a notification")
	}
	if _, err := svc.gj.GraphQL(ctx, `mutation { gj_notification(where: { id: { eq: "`+id+`" } }, update: { title: "edited" }) { id } }`, nil, nil); err == nil {
		t.Fatal("only seen is writable")
	}
}

func TestHeldWriteNotifiesAndTheDecisionSettlesIt(t *testing.T) {
	_, svc, rt := newApprovalService(t)
	ctx := approvalUserCtx("user_1")
	_, approval, err := rt.ProposeWrite(ctx, map[string]any{"query": approvalTestWrite})
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := svc.listNotifications(ctx, true, 0, "")
	if len(rows) != 1 || rows[0]["kind"] != notificationApprovalPending || rows[0]["severity"] != notificationSeverityAction || rows[0]["ref_id"] != approval.ID {
		t.Fatalf("pending notification = %v", rows)
	}
	if _, err := svc.decideApproval(ctx, approval.ID, "reject", approval.Hash); err != nil {
		t.Fatal(err)
	}
	if count, _ := svc.unseenNotificationCount(ctx); count != 0 {
		t.Fatalf("a decided approval still waits: %d unseen", count)
	}
}

// The stream sends what arrives after it connects, and the unseen count
// whenever it changes, woken by the store's revision signal.
func TestNotificationStreamFollowsNewNotifications(t *testing.T) {
	svc := newNotificationService(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := approvalUserCtx("user_1")
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		go func() { <-r.Context().Done(); cancel() }()
		svc.streamNotifications(ctx, w, r)
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	events := make(chan string, 16)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		event := ""
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				events <- event + " " + strings.TrimPrefix(line, "data: ")
			}
		}
		close(events)
	}()
	next := func() string {
		select {
		case e := <-events:
			return e
		case <-time.After(10 * time.Second):
			t.Fatal("no stream event")
			return ""
		}
	}
	if e := next(); !strings.HasPrefix(e, `count {"unseen_count":0}`) {
		t.Fatalf("first event = %q", e)
	}
	svc.notify(approvalUserCtx("user_1"), notification{OwnerID: "user_1", Kind: notificationRunFailed, Title: "live one", DedupeKey: "live:1"})
	if e := next(); !strings.HasPrefix(e, "notification ") || !strings.Contains(e, `"title":"live one"`) {
		t.Fatalf("notification event = %q", e)
	}
	if e := next(); !strings.HasPrefix(e, `count {"unseen_count":1}`) {
		t.Fatalf("count event = %q", e)
	}
}
