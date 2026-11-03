package serv

import (
	"context"
	"strings"
	"testing"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/core/v3"
)

func TestWatchRoutineAnswersInTheWatchThreadOnce(t *testing.T) {
	svc := newSQLiteThreadService(t)
	db := svc.dbs["app"]
	runner := &scriptedAgentRunner{resp: gjagent.Response{Status: "answered", Answer: "3 orders are open; two came in this hour."}}
	withScriptedAgentRunner(t, runner)
	ctx := threadUserCtx("user_1")
	setOpenOrders(t, db, 1)

	row, err := newWatchControlPlane(svc).mutateRow(ctx, core.ManagedMutationRoot{
		Table: watchesRootTable, Operation: "insert",
		Input: map[string]any{
			"name":          "open_orders_routine",
			"schedule":      map[string]any{"cron": "*/5 * * * *"},
			"query":         `query { orders(where: { status: { eq: "open" } }) { count_id } }`,
			"metric":        map[string]any{"path": "orders.0.count_id", "op": "gt", "threshold": 1},
			"delivery_json": map[string]any{"kind": "agent", "prompt": "Which open orders are new?"},
		},
	})
	if err != nil {
		t.Fatalf("create routine watch: %v", err)
	}
	// A read-only routine needs no action review.
	if row["status"] != "active" || row["action_approval"] != watchReviewNotRequired {
		t.Fatalf("routine watch = status %v action_approval %v", row["status"], row["action_approval"])
	}
	id := stringFromAny(row["id"])
	due := time.Now().UTC().Truncate(5 * time.Minute)
	if _, err := db.Exec(`UPDATE "_graphjin_watches" SET next_fire_at = ? WHERE id = ?`, formatWatchFireTime(due), id); err != nil {
		t.Fatal(err)
	}
	setOpenOrders(t, db, 3)
	if sweep, err := svc.sweepWatchSchedules(ctx, due.Add(time.Second)); err != nil || sweep.Fired != 1 {
		t.Fatalf("sweep fired=%d err=%v", sweep.Fired, err)
	}
	if err := svc.processPendingWatchDeliveries(ctx); err != nil {
		t.Fatalf("deliver: %v", err)
	}

	if !runner.conf.ReadOnly {
		t.Fatalf("routine must run read-only")
	}
	if !strings.Contains(runner.req.Instruction, "Which open orders are new?") || !strings.Contains(runner.req.Instruction, "the number it watches is now 3, it was 1; it fires when the number is above 1.") ||
		strings.Contains(runner.req.Instruction, "count_id") {
		t.Fatalf("routine instruction = %q", runner.req.Instruction)
	}
	threadID := watchRoutineThreadID("user_1", id)
	thread, err := svc.threadForCaller(ctx, threadID)
	if err != nil || thread["channel"] != "watch" {
		t.Fatalf("routine thread = %+v err %v", thread, err)
	}
	messages, err := svc.threadMessages(ctx, threadID)
	if err != nil || len(messages) != 2 {
		t.Fatalf("thread messages = %+v err %v", messages, err)
	}
	if shown := stringFromAny(messages[0]["content"]); !strings.Contains(shown, "orders.0.count_id is 3 (was 1), gt 1") {
		t.Fatalf("routine event line = %q", shown)
	}
	if messages[1]["content"] != runner.resp.Answer {
		t.Fatalf("routine answer = %+v", messages[1])
	}
	events, err := newWatchControlPlane(svc).watchEventRows(ctx)
	if err != nil || len(events) != 1 {
		t.Fatalf("events %+v err %v", events, err)
	}
	receipt := mapFromAny(parseJSONValue(jsonMapString(events[0], "receipt_json")))
	if events[0]["delivery_status"] != "delivered" || receipt["thread_id"] != threadID || receipt["run_id"] != watchRoutineRunID(stringFromAny(events[0]["id"])) {
		t.Fatalf("routine delivery = %v receipt %+v", events[0]["delivery_status"], receipt)
	}

	// Delivering the same event again (a replica that lost track of it) must
	// not run the agent a second time.
	runner.req = gjagent.Request{}
	if _, err := db.Exec(`UPDATE "_graphjin_watch_events" SET delivery_status = 'pending'`); err != nil {
		t.Fatal(err)
	}
	if err := svc.processPendingWatchDeliveries(ctx); err != nil {
		t.Fatalf("redeliver: %v", err)
	}
	if runner.req.Instruction != "" {
		t.Fatalf("redelivery ran the agent again")
	}
	if messages, _ := svc.threadMessages(ctx, threadID); len(messages) != 2 {
		t.Fatalf("redelivery changed the thread: %d messages", len(messages))
	}
	if got, _ := svc.threadForCaller(ctx, threadID); got["active_run_id"] != "" {
		t.Fatalf("redelivery left the thread busy: %+v", got)
	}
}

func TestWatchRoutineDeliveryValidation(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want string
	}{
		{`{"kind":"agent","prompt":"` + strings.Repeat("x", watchRoutinePromptMaxBytes+1) + `"}`, "longer than"},
	} {
		if _, _, err := parseWatchDeliveryConfig(tc.raw); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("parseWatchDeliveryConfig err = %v, want %q", err, tc.want)
		}
	}
	cfg, enabled, err := parseWatchDeliveryConfig(`{"kind":"agent"}`)
	if err != nil || !enabled || cfg.Agent.Prompt != watchRoutineDefaultPrompt {
		t.Fatalf("default routine prompt = %+v enabled %v err %v", cfg.Agent, enabled, err)
	}
	normalized, _, _, err := normalizeWatchDeliveryJSON(`{"kind":"agent","prompt":"why?","thread_id":"th_1","profile":"agent_profile:1"}`)
	if err != nil || normalized != `{"kind":"agent","profile":"agent_profile:1","prompt":"why?","thread_id":"th_1"}` {
		t.Fatalf("normalized = %s err %v", normalized, err)
	}
}

func TestWatchRoutineAnswersInItsAgentsHomeThread(t *testing.T) {
	svc := newSQLiteThreadService(t)
	db := svc.dbs["app"]
	runner := &scriptedAgentRunner{resp: gjagent.Response{Status: "answered", Answer: "3 orders are open."}}
	withScriptedAgentRunner(t, runner)
	alice := profileUserCtx("alice", "acct_1")
	bob := profileUserCtx("bob", "acct_1")
	cp := newArtifactControlPlane(svc)
	ops := addProfile(t, cp, alice, "Ops", "Focus on late shipments.")["id"].(string)
	bobs := addProfile(t, cp, bob, "Bob's", "")["id"].(string)
	setOpenOrders(t, db, 1)

	create := func(ctx context.Context, name string, delivery map[string]any) (map[string]any, error) {
		return newWatchControlPlane(svc).mutateRow(ctx, core.ManagedMutationRoot{
			Table: watchesRootTable, Operation: "insert",
			Input: map[string]any{
				"name":          name,
				"schedule":      map[string]any{"cron": "*/5 * * * *"},
				"query":         `query { orders(where: { status: { eq: "open" } }) { count_id } }`,
				"metric":        map[string]any{"path": "orders.0.count_id", "op": "gt", "threshold": 1},
				"delivery_json": delivery,
			},
		})
	}
	// Created from a turn in the Ops agent's chat, with no thread named: the
	// routine is pinned to that agent.
	row, err := create(withRunThreadProfile(alice, ops), "ops_open_orders", map[string]any{"kind": "agent", "prompt": "Which are late?"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := mapFromAny(parseJSONValue(jsonMapString(row, "delivery_json"))); got["profile"] != ops {
		t.Fatalf("delivery = %+v, want profile %s", got, ops)
	}
	home := profileHomeThreadID("alice", ops)
	if row["routine_thread_id"] != home {
		t.Fatalf("routine_thread_id = %v, want the agent's home thread %s", row["routine_thread_id"], home)
	}
	// An agent the owner cannot see is refused at create time.
	if _, err := create(alice, "not_mine", map[string]any{"kind": "agent", "profile": bobs}); err == nil || !strings.Contains(err.Error(), "profile") {
		t.Fatalf("other user's agent err = %v", err)
	}

	id := stringFromAny(row["id"])
	due := time.Now().UTC().Truncate(5 * time.Minute)
	if _, err := db.Exec(`UPDATE "_graphjin_watches" SET next_fire_at = ? WHERE id = ?`, formatWatchFireTime(due), id); err != nil {
		t.Fatal(err)
	}
	setOpenOrders(t, db, 3)
	if sweep, err := svc.sweepWatchSchedules(alice, due.Add(time.Second)); err != nil || sweep.Fired != 1 {
		t.Fatalf("sweep fired=%d err=%v", sweep.Fired, err)
	}
	if err := svc.processPendingWatchDeliveries(alice); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if runner.req.Profile == nil || runner.req.Profile.Instructions != "Focus on late shipments." {
		t.Fatalf("routine ran without the agent's instructions: %+v", runner.req.Profile)
	}
	if got := runThreadProfile(runner.ctx); got != ops {
		t.Fatalf("run context profile = %q, want %s", got, ops)
	}
	messages, err := svc.threadMessages(alice, home)
	if err != nil || len(messages) != 2 || messages[1]["content"] != runner.resp.Answer {
		t.Fatalf("home thread messages = %+v err %v", messages, err)
	}
	if thread, _ := svc.threadForCaller(alice, home); thread["unread"] != true {
		t.Fatalf("home thread should be unread after the routine answered: %+v", thread)
	}

	// With the agent deleted, the next firing answers in the watch's own thread.
	if _, err := cp.deleteArtifact(alice, core.ManagedMutationRoot{Operation: "delete", Input: map[string]any{"id": ops}}); err != nil {
		t.Fatal(err)
	}
	setOpenOrders(t, db, 1)
	next := due.Add(5 * time.Minute)
	if _, err := db.Exec(`UPDATE "_graphjin_watches" SET next_fire_at = ? WHERE id = ?`, formatWatchFireTime(due.Add(time.Minute)), id); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.sweepWatchSchedules(alice, due.Add(time.Minute+time.Second)); err != nil {
		t.Fatal(err)
	}
	setOpenOrders(t, db, 4)
	if _, err := db.Exec(`UPDATE "_graphjin_watches" SET next_fire_at = ? WHERE id = ?`, formatWatchFireTime(next), id); err != nil {
		t.Fatal(err)
	}
	if sweep, err := svc.sweepWatchSchedules(alice, next.Add(time.Second)); err != nil || sweep.Fired != 1 {
		t.Fatalf("second crossing fired=%d err=%v", sweep.Fired, err)
	}
	if err := svc.processPendingWatchDeliveries(alice); err != nil {
		t.Fatalf("deliver after delete: %v", err)
	}
	if messages, err := svc.threadMessages(alice, watchRoutineThreadID("alice", id)); err != nil || len(messages) != 2 {
		t.Fatalf("fallback thread messages = %+v err %v", messages, err)
	}
}
