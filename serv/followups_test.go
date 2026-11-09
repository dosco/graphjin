package serv

import (
	"strings"
	"testing"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
)

// A follow-up the lead schedules runs once in its conversation, with the
// lead's note as the instruction, and then leaves nothing behind.
func TestFollowUpRunsOnceInItsThread(t *testing.T) {
	svc := newSQLiteThreadService(t)
	runner := &scriptedAgentRunner{resp: gjagent.Response{Status: gjagent.StatusAnswered, Answer: "Invoice 42 has been paid."}}
	withScriptedAgentRunner(t, runner)
	ctx := threadUserCtx("user_1")
	thread, err := svc.createThread(ctx, threadSpec{Title: "billing"})
	if err != nil {
		t.Fatal(err)
	}
	threadID := stringFromAny(thread["id"])
	scheduler := followUpScheduler{service: svc}

	if _, err := scheduler.ScheduleFollowUp(ctx, time.Now().Add(time.Hour), "Check invoice 42."); err == nil || !strings.Contains(err.Error(), "conversation") {
		t.Fatalf("no thread err = %v", err)
	}
	runCtx := withRunThread(ctx, threadID, "run_1")
	if _, err := scheduler.ScheduleFollowUp(runCtx, time.Now().Add(40*24*time.Hour), "Too far."); err == nil {
		t.Fatal("a follow-up more than 30 days ahead must be refused")
	}
	followUp, err := scheduler.ScheduleFollowUp(runCtx, time.Now().Add(2*time.Hour), "Check whether invoice 42 was paid.")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := scheduler.ScheduleFollowUp(runCtx, time.Now().Add(time.Duration(3+i)*time.Hour), "Another check."+string(rune('a'+i))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := scheduler.ScheduleFollowUp(runCtx, time.Now().Add(6*time.Hour), "A fourth."); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("fourth follow-up err = %v", err)
	}

	db := svc.dbs["app"]
	due := time.Now().UTC().Truncate(time.Minute)
	if _, err := db.Exec(`UPDATE "_graphjin_watches" SET next_fire_at = ? WHERE id = ?`, formatWatchFireTime(due), followUp.ID); err != nil {
		t.Fatal(err)
	}
	if sweep, err := svc.sweepWatchSchedules(ctx, due.Add(time.Second)); err != nil || sweep.Fired != 1 {
		t.Fatalf("sweep fired=%d err=%v", sweep.Fired, err)
	}
	if err := svc.processPendingWatchDeliveries(ctx); err != nil {
		t.Fatal(err)
	}
	if runner.req.Instruction != "Check whether invoice 42 was paid." {
		t.Fatalf("follow-up instruction = %q", runner.req.Instruction)
	}
	messages, _ := svc.threadMessages(ctx, threadID)
	if len(messages) != 2 || messages[0]["content"] != "Follow-up: Check whether invoice 42 was paid." || messages[1]["content"] != "No change was proposed.\n\nInvoice 42 has been paid." {
		t.Fatalf("thread = %v", messages)
	}
	if row, _ := svc.internalWatchStoreRow(ctx, followUp.ID); row != nil {
		t.Fatal("a delivered follow-up must be deleted")
	}
	if _, err := scheduler.ScheduleFollowUp(runCtx, time.Now().Add(6*time.Hour), "A fourth."); err != nil {
		t.Fatalf("a delivered follow-up should free its slot: %v", err)
	}
}
