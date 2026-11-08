package serv

import (
	"context"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3"
)

// newFeedbackService is a SQLite service with threads on and one answered
// two-turn conversation owned by user_1.
func newFeedbackService(t *testing.T) (*graphjinService, string) {
	t.Helper()
	db, svc := newSQLiteWatchServiceWithOptions(t, 20, func(c *Config) {
		c.Agent.Enabled = true
		c.Agent.Lead.Enabled = true
		c.Core.Threads.Enabled = true
	})
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatal(err)
	}
	startSQLiteWatchCore(t, svc, db)
	ctx := approvalUserCtx("user_1")
	thread, err := svc.createThread(ctx, threadSpec{Title: "renewals"})
	if err != nil {
		t.Fatal(err)
	}
	threadID := stringFromAny(thread["id"])
	for _, m := range []messageSpec{
		{Role: "user", Content: "Which account renews soonest?"},
		{Role: "assistant", Content: "Meridian Robotics renews on 2026-12-01.", Status: "answered"},
		{Role: "user", Content: "What is the total of their failed invoices?"},
		{Role: "assistant", Content: "Their failed invoices total $4,200.", Status: "answered", ResponseJSON: `{"status":"answered","trace_id":"tr_9"}`},
	} {
		m.ThreadID, m.OwnerID, m.AccountID = threadID, "user_1", "acct_1"
		if _, err := svc.appendMessage(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	return svc, threadID
}

func rateAnswer(svc *graphjinService, ctx context.Context, messageID, rating, correction string) (*core.Result, error) {
	vars := []byte(`{"m":"` + messageID + `","c":"` + correction + `","j":{"rating":"` + rating + `"}}`)
	return svc.gj.GraphQL(ctx, `mutation rate($m: String!, $c: String, $j: JSON) { gj_artifacts(insert: { kind: "feedback", target_ref: $m, content: $c, content_json: $j }) { id kind } }`, vars, nil)
}

func TestFeedbackRecordsTheRealConversation(t *testing.T) {
	svc, threadID := newFeedbackService(t)
	ctx := approvalUserCtx("user_1")
	answerID := threadID + "-00000004"
	if _, err := rateAnswer(svc, ctx, answerID, "down", "It is $1,800: only two invoices failed."); err != nil {
		t.Fatal(err)
	}
	// Rating the same answer again replaces the earlier report.
	if _, err := rateAnswer(svc, ctx, answerID, "down", "It is $1,850."); err != nil {
		t.Fatal(err)
	}
	reports, err := svc.listFeedback(ctx)
	if err != nil || len(reports) != 1 {
		t.Fatalf("reports = %v err = %v", reports, err)
	}
	r := reports[0]
	if r["rating"] != "down" || r["correction"] != "It is $1,850." || r["question"] != "What is the total of their failed invoices?" ||
		r["answer"] != "Their failed invoices total $4,200." || r["trace_id"] != "tr_9" {
		t.Fatalf("report = %v", r)
	}
	if history := anySliceFrom(r["history"]); len(history) != 2 {
		t.Fatalf("history = %v, want the first exchange only", r["history"])
	}
	drafts := feedbackCorpusDrafts(reports)
	if len(drafts) != 1 || drafts[0]["prompt"] != "What is the total of their failed invoices?" || drafts[0]["expected"] != "It is $1,850." {
		t.Fatalf("drafts = %v", drafts)
	}
	if turns, _ := drafts[0]["turns"].([]string); len(turns) != 1 || turns[0] != "Which account renews soonest?" {
		t.Fatalf("draft turns = %v", drafts[0]["turns"])
	}
}

func TestFeedbackOnlyRatesYourOwnAgentAnswers(t *testing.T) {
	svc, threadID := newFeedbackService(t)
	if _, err := rateAnswer(svc, approvalUserCtx("user_2"), threadID+"-00000004", "down", ""); err == nil || !strings.Contains(err.Error(), "your threads") {
		t.Fatalf("another user's answer: err = %v", err)
	}
	ctx := approvalUserCtx("user_1")
	if _, err := rateAnswer(svc, ctx, threadID+"-00000003", "down", ""); err == nil {
		t.Fatal("a user message is not an answer to rate")
	}
	if _, err := rateAnswer(svc, ctx, threadID+"-00000004", "meh", ""); err == nil || !strings.Contains(err.Error(), "up or down") {
		t.Fatalf("bad rating err = %v", err)
	}
	if _, err := rateAnswer(svc, ctx, threadID+"-00000002", "up", ""); err != nil {
		t.Fatal(err)
	}
	if drafts := feedbackCorpusDrafts(mustFeedback(t, svc, ctx)); len(drafts) != 0 {
		t.Fatalf("a helpful rating is not a corpus draft: %v", drafts)
	}
}

func mustFeedback(t *testing.T, svc *graphjinService, ctx context.Context) []map[string]any {
	t.Helper()
	reports, err := svc.listFeedback(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return reports
}
