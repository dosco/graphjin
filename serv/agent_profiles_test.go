package serv

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/core/v3"
)

func profileUserCtx(userID, accountID string) context.Context {
	return contextWithUserRole(annotationAccountCtx(userID, accountID, "analyst"), "analyst")
}

func addProfile(t *testing.T, cp artifactControlPlane, ctx context.Context, name, instructions string) map[string]any {
	t.Helper()
	row, err := cp.upsertAgentProfile(ctx, core.ManagedMutationRoot{Operation: "insert",
		Input: map[string]any{"name": name, "instructions": instructions, "icon": "chart"}}, nil)
	if err != nil {
		t.Fatalf("add profile %q: %v", name, err)
	}
	return row
}

func TestAgentProfileScopingOwnerTeammateOtherAccount(t *testing.T) {
	svc := newSQLiteThreadService(t)
	cp := newArtifactControlPlane(svc)
	alice := profileUserCtx("alice", "acct_1")
	bob := profileUserCtx("bob", "acct_1")
	carol := profileUserCtx("carol", "acct_2")

	profile := addProfile(t, cp, alice, "Finance", "Report revenue in EUR.")
	id := profile["id"].(string)
	if profile["tier"] != "observed" || profile["icon"] != "chart" || profile["content"] != "Report revenue in EUR." {
		t.Fatalf("new profile = %+v", profile)
	}
	visible := func(ctx context.Context) bool {
		t.Helper()
		row, err := svc.visibleProfileRow(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		rows, err := svc.visibleProfileRows(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if (row != nil) != (len(rows) == 1) {
			t.Fatalf("row and list visibility disagree: row=%v list=%d", row != nil, len(rows))
		}
		return row != nil
	}
	if !visible(alice) || visible(bob) || visible(carol) {
		t.Fatal("a personal profile must be visible to its owner only")
	}
	if _, err := svc.profileHomeThread(bob, id); !errors.Is(err, errProfileNotFound) {
		t.Fatalf("teammate opened a personal profile's home thread: %v", err)
	}

	existing, _ := svc.internalArtifactStoreRow(alice, id)
	shared, err := cp.upsertAgentProfile(alice, core.ManagedMutationRoot{Operation: "update", Input: map[string]any{"tier": "approved"}}, existing)
	if err != nil || shared["tier"] != "approved" {
		t.Fatalf("share = %+v err=%v", shared, err)
	}
	if !visible(alice) || !visible(bob) || visible(carol) {
		t.Fatal("a shared profile must be visible to its account only")
	}
	if _, err := svc.profileHomeThread(carol, id); !errors.Is(err, errProfileNotFound) {
		t.Fatalf("another account opened the profile's home thread: %v", err)
	}
	aliceHome, err := svc.profileHomeThread(alice, id)
	if err != nil {
		t.Fatal(err)
	}
	bobHome, err := svc.profileHomeThread(bob, id)
	if err != nil {
		t.Fatal(err)
	}
	if aliceHome["id"] == bobHome["id"] || bobHome["profile"] != id || bobHome["title"] != "Finance" {
		t.Fatalf("each user needs their own home thread: alice=%+v bob=%+v", aliceHome, bobHome)
	}

	existing, _ = svc.internalArtifactStoreRow(alice, id)
	if _, err := cp.upsertAgentProfile(bob, core.ManagedMutationRoot{Operation: "update", Input: map[string]any{"instructions": "hijack"}}, existing); !errors.Is(err, errProfileNotFound) {
		t.Fatalf("teammate edited a shared profile: %v", err)
	}
	edited, err := cp.upsertAgentProfile(alice, core.ManagedMutationRoot{Operation: "update", Input: map[string]any{"instructions": "Report revenue in USD."}}, existing)
	if err != nil || edited["tier"] != "observed" || edited["icon"] != "chart" {
		t.Fatalf("editing shared instructions must take the profile back to personal: %+v err=%v", edited, err)
	}
	if _, err := cp.upsertAgentProfile(alice, core.ManagedMutationRoot{Operation: "insert",
		Input: map[string]any{"name": "Big", "instructions": strings.Repeat("x", maxProfileInstructionsBytes+1)}}, nil); err == nil {
		t.Fatal("instructions over 2KB were accepted")
	}
	if _, err := cp.upsertAgentProfile(alice, core.ManagedMutationRoot{Operation: "insert",
		Input: map[string]any{"name": "X", "visibility": "account"}}, nil); err == nil {
		t.Fatal("profile accepted a server-managed field")
	}
	if _, err := cp.upsertAgentProfile(alice, core.ManagedMutationRoot{Operation: "insert", Input: map[string]any{"instructions": "no name"}}, nil); err == nil {
		t.Fatal("profile without a name was accepted")
	}
}

// Profiles are managed entirely through gj_artifacts; only finding a
// profile's home thread is a REST action.
func TestAgentProfilesThroughGraphQL(t *testing.T) {
	svc := newSQLiteThreadService(t)
	hs := &HttpService{}
	hs.Store(svc)
	alice := profileUserCtx("alice", "acct_1")
	gql := func(ctx context.Context, query string) (map[string]any, string) {
		t.Helper()
		raw, errText := skillGraphQL(t, svc, ctx, query, nil)
		var row map[string]any
		_ = json.Unmarshal(raw, &row)
		return row, errText
	}

	row, errText := gql(alice, `mutation { gj_artifacts(insert: { kind: "agent_profile", name: "Ops", content: "Focus on late shipments.", icon: "truck" }) { id name content icon tier } }`)
	if errText != "" || row["icon"] != "truck" || row["tier"] != "observed" {
		t.Fatalf("create = %+v %s", row, errText)
	}
	id := row["id"].(string)
	row, errText = gql(alice, `mutation { gj_artifacts(where: { id: { eq: "`+id+`" } }, update: { name: "Operations" }) { id name content } }`)
	if errText != "" || row["name"] != "Operations" || row["content"] != "Focus on late shipments." {
		t.Fatalf("rename = %+v %s", row, errText)
	}

	home := func(ctx context.Context) (int, map[string]any) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, routeAgentProfiles+"/"+id+"/thread", nil).WithContext(ctx)
		rec := httptest.NewRecorder()
		hs.AgentProfiles(nil).ServeHTTP(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	if code, out := home(alice); code != http.StatusOK || out["data"].(map[string]any)["id"] != profileHomeThreadID("alice", id) {
		t.Fatalf("home thread = %d %+v", code, out)
	}
	if code, _ := home(profileUserCtx("carol", "acct_2")); code != http.StatusNotFound {
		t.Fatalf("other account home thread = %d, want 404", code)
	}
	req := httptest.NewRequest(http.MethodGet, routeAgentProfiles+"/"+id, nil).WithContext(alice)
	rec := httptest.NewRecorder()
	hs.AgentProfiles(nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("profile REST read = %d, want 404 (profiles are GraphQL rows)", rec.Code)
	}

	// A non-owner's delete is a no-op that does not reveal the row exists.
	gql(profileUserCtx("bob", "acct_1"), `mutation { gj_artifacts(where: { id: { eq: "`+id+`" } }, delete: true) { id } }`)
	if row, _ := svc.internalArtifactStoreRow(alice, id); row == nil {
		t.Fatal("a teammate deleted alice's profile")
	}
	if _, errText := gql(alice, `mutation { gj_artifacts(where: { id: { eq: "`+id+`" } }, delete: true) { id } }`); errText != "" {
		t.Fatalf("owner delete: %s", errText)
	}
	if row, _ := svc.internalArtifactStoreRow(alice, id); row != nil {
		t.Fatalf("profile survived delete: %+v", row)
	}
}

func TestAgentProfileHomeThreadConcurrentFindOrCreate(t *testing.T) {
	svc := newSQLiteThreadService(t)
	ctx := profileUserCtx("alice", "acct_1")
	id := addProfile(t, newArtifactControlPlane(svc), ctx, "Finance", "Report revenue in EUR.")["id"].(string)

	const callers = 8
	ids := make([]string, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			row, err := svc.profileHomeThread(ctx, id)
			errs[i] = err
			if row != nil {
				ids[i], _ = row["id"].(string)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	for i := range ids {
		if errs[i] != nil || ids[i] != profileHomeThreadID("alice", id) {
			t.Fatalf("caller %d = %q err=%v", i, ids[i], errs[i])
		}
	}
	threads, err := svc.listThreads(ctx, true)
	if err != nil || len(threads) != 1 {
		t.Fatalf("threads after concurrent first use = %+v err=%v", threads, err)
	}
	// The dedupe is the primary key: a second insert of the derived id fails.
	if _, err := svc.createThread(ctx, threadSpec{ID: profileHomeThreadID("alice", id), Profile: id}); err == nil {
		t.Fatal("a duplicate home thread insert was accepted")
	}
}

func TestAgentProfileInstructionsReachOnlyItsRuns(t *testing.T) {
	svc := newSQLiteThreadService(t)
	hs := &HttpService{}
	hs.Store(svc)
	runner := &scriptedAgentRunner{resp: gjagent.Response{Status: "answered", Answer: "ok"}}
	withScriptedAgentRunner(t, runner)
	cp := newArtifactControlPlane(svc)
	alice := profileUserCtx("alice", "acct_1")
	bob := profileUserCtx("bob", "acct_1")

	finance := addProfile(t, cp, alice, "Finance", "Report revenue in EUR.")["id"].(string)
	ops := addProfile(t, cp, alice, "Ops", "Focus on late shipments.")["id"].(string)
	turn := func(ctx context.Context, threadID string) *gjagent.ProfileInstructions {
		t.Helper()
		runner.req = gjagent.Request{}
		rec := postThreadTurn(t, hs, ctx, `{"instruction":"How are we doing?","thread_id":"`+threadID+`"}`, false)
		if rec.Code != http.StatusOK {
			t.Fatalf("turn = %d: %s", rec.Code, rec.Body.String())
		}
		return runner.req.Profile
	}

	home, err := svc.profileHomeThread(alice, finance)
	if err != nil {
		t.Fatal(err)
	}
	if got := turn(alice, home["id"].(string)); got == nil || got.Name != "Finance" || got.Instructions != "Report revenue in EUR." {
		t.Fatalf("finance home run profile = %+v", got)
	}
	opsHome, err := svc.profileHomeThread(alice, ops)
	if err != nil {
		t.Fatal(err)
	}
	if got := turn(alice, opsHome["id"].(string)); got == nil || got.Instructions != "Focus on late shipments." {
		t.Fatalf("ops home run profile = %+v", got)
	}
	plain, err := svc.createThread(alice, threadSpec{})
	if err != nil {
		t.Fatal(err)
	}
	if got := turn(alice, plain["id"].(string)); got != nil {
		t.Fatalf("a thread without a profile carried one: %+v", got)
	}
	// A thread pointed at a profile the caller cannot see runs as the default agent.
	stolen, err := svc.createThread(bob, threadSpec{Profile: finance})
	if err != nil {
		t.Fatal(err)
	}
	if got := turn(bob, stolen["id"].(string)); got != nil {
		t.Fatalf("teammate's run used a personal profile: %+v", got)
	}
}

func TestThreadUnreadFlipsAndSeen(t *testing.T) {
	svc := newSQLiteThreadService(t)
	hs := &HttpService{}
	hs.Store(svc)
	ctx := threadUserCtx("user_1")
	thread, err := svc.createThread(ctx, threadSpec{})
	if err != nil {
		t.Fatal(err)
	}
	id := thread["id"].(string)
	unread := func() bool {
		t.Helper()
		row, err := svc.threadForCaller(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return row["unread"] == true
	}
	appendMsg := func(role string) {
		t.Helper()
		time.Sleep(2 * time.Millisecond)
		if _, err := svc.appendMessage(ctx, messageSpec{ThreadID: id, Role: role, Content: role + " says hi", OwnerID: "user_1"}); err != nil {
			t.Fatal(err)
		}
	}
	if unread() {
		t.Fatal("a new thread must not be unread")
	}
	appendMsg("user")
	if unread() {
		t.Fatal("the owner's own message must not make the thread unread")
	}
	appendMsg("assistant")
	if !unread() {
		t.Fatal("an assistant message after the last visit must make the thread unread")
	}
	time.Sleep(2 * time.Millisecond)
	if _, errText := threadGraphQL(t, svc, ctx, `mutation { gj_thread(where: { id: { eq: "`+id+`" } }, update: { last_seen_at: "now" }) { id unread } }`); errText != "" {
		t.Fatalf("seen: %s", errText)
	}
	if unread() {
		t.Fatal("marking the thread seen must clear unread")
	}
	appendMsg("assistant")
	if rows, _ := svc.listThreads(ctx, false); len(rows) != 1 || rows[0]["unread"] != true {
		t.Fatalf("list must report the unread thread: %+v", rows)
	}
}

func TestThreadMigrationAddsLastSeenAt(t *testing.T) {
	dsn := "file:" + t.TempDir() + "/threads.db"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	_, svc := newSQLiteWatchServiceWithDBAndOptions(t, db, dsn, 10, func(conf *Config) {
		conf.Agent.Enabled = true
		conf.Core.Threads = core.ThreadsConfig{Enabled: true, HistoryTurns: 12, RunLeaseSeconds: 3, MaxPerOwner: 3}
		conf.Agent.Lead.Enabled = true
	})
	schema := svc.conf.Core.EffectiveArtifactsConfig().Schema
	// The thread table as an older build created it, with one answered thread.
	for _, stmt := range threadDDL("sqlite", schema) {
		if _, err := db.Exec(strings.Replace(stmt, "last_seen_at TEXT,\n", "", 1)); err != nil {
			t.Fatal(err)
		}
	}
	threads := artifactTableName("sqlite", schema, "threads")
	if exists, err := sqliteColumnExists(context.Background(), db, threads, "last_seen_at"); err != nil || exists {
		t.Fatalf("old table already has last_seen_at (exists=%v err=%v)", exists, err)
	}
	if _, err := db.Exec(`INSERT INTO ` + threads + ` (id, title, owner_id, last_message_at, created_at, updated_at)
VALUES ('th_old', 'Old', 'user_1', '2026-01-02T03:04:05Z', '2026-01-01T00:00:00Z', '2026-01-02T03:04:05Z')`); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 2; i++ { // the migration must be safe to rerun
		if err := svc.initArtifactsBeforeCore(); err != nil {
			t.Fatalf("init %d: %v", i, err)
		}
	}
	var seen sql.NullString
	if err := db.QueryRow(`SELECT last_seen_at FROM ` + threads + ` WHERE id = 'th_old'`).Scan(&seen); err != nil {
		t.Fatal(err)
	}
	if seen.String != "2026-01-02T03:04:05Z" {
		t.Fatalf("old thread last_seen_at = %+v, want its last message time", seen)
	}
	startSQLiteWatchCore(t, svc, svc.dbs["app"])
	ctx := threadUserCtx("user_1")
	row, err := svc.threadForCaller(ctx, "th_old")
	if err != nil || row["unread"] != false {
		t.Fatalf("migrated thread = %+v err=%v", row, err)
	}
	if _, err := svc.appendMessage(ctx, messageSpec{ThreadID: "th_old", Role: "assistant", Content: "new", OwnerID: "user_1"}); err != nil {
		t.Fatal(err)
	}
	if row, _ := svc.threadForCaller(ctx, "th_old"); row["unread"] != true {
		t.Fatalf("migrated thread did not turn unread: %+v", row)
	}
}
