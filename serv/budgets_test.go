package serv

import (
	"testing"

	gjagent "github.com/dosco/graphjin/agent/v3"
)

func newBudgetService(t *testing.T) *graphjinService {
	t.Helper()
	on := true
	db, svc := newSQLiteWatchServiceWithOptions(t, 10, func(conf *Config) {
		conf.Agent.Enabled = true
		conf.Agent.Budgets = gjagent.BudgetConfig{Enabled: &on, UserDailyTokens: 1000, RoutineDailyTokens: 300}
	})
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatal(err)
	}
	startSQLiteWatchCore(t, svc, db)
	return svc
}

// Runs over a person's daily budget are refused as blocked replies with the
// budget_exceeded refusal, and the person is told once.
func TestBudgetRefusesRunsOverTheDailyCap(t *testing.T) {
	svc := newBudgetService(t)
	runner := &scriptedAgentRunner{resp: gjagent.Response{Status: gjagent.StatusAnswered, Answer: "ok",
		Usage: map[string]any{"prompt_tokens": 500, "completion_tokens": 100, "llm_calls": 2}}}
	withScriptedAgentRunner(t, runner)
	entry, err := svc.agentEntryRunner(agentConfigFromService(svc.conf))
	if err != nil {
		t.Fatal(err)
	}
	ctx := threadUserCtx("user_1")
	for i := 0; i < 2; i++ {
		resp, err := entry.Run(ctx, gjagent.Request{Instruction: "How many orders are open?"})
		if err != nil || resp.Status != gjagent.StatusAnswered {
			t.Fatalf("run %d = %+v err %v", i, resp, err)
		}
	}
	runner.req = gjagent.Request{}
	resp, err := entry.Run(ctx, gjagent.Request{Instruction: "And now?"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != gjagent.StatusBlocked || resp.Refusal == nil || resp.Refusal.Code != refusalBudgetExceeded || runner.req.Instruction != "" {
		t.Fatalf("over the cap: %+v (model ran: %v)", resp, runner.req.Instruction != "")
	}
	resp, _ = entry.Run(ctx, gjagent.Request{Instruction: "Still?"})
	notes, _ := svc.listNotifications(ctx, false, 0, "")
	if resp.Status != gjagent.StatusBlocked || len(notes) != 1 || notes[0]["kind"] != refusalBudgetExceeded {
		t.Fatalf("second refusal %+v notifications %v", resp.Status, notes)
	}
	// Another person has their own budget.
	if resp, _ := entry.Run(threadUserCtx("user_2"), gjagent.Request{Instruction: "Hi"}); resp.Status != gjagent.StatusAnswered {
		t.Fatalf("user_2 = %+v", resp)
	}
}

// A routine has its own, smaller cap, separate from its owner's.
func TestBudgetCapsEachRoutine(t *testing.T) {
	svc := newBudgetService(t)
	runner := &scriptedAgentRunner{resp: gjagent.Response{Status: gjagent.StatusAnswered, Answer: "ok", Usage: map[string]any{"prompt_tokens": 320}}}
	withScriptedAgentRunner(t, runner)
	entry, err := svc.agentEntryRunner(watchRoutineAgentConfig(svc.conf, false))
	if err != nil {
		t.Fatal(err)
	}
	routine := func(watchID string) gjagent.Response {
		ctx := withRunOrigin(threadUserCtx("user_1"), runOrigin{Origin: approvalOriginRoutine, WatchID: watchID})
		resp, err := entry.Run(ctx, gjagent.Request{Instruction: "Look into it."})
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	if resp := routine("w_1"); resp.Status != gjagent.StatusAnswered {
		t.Fatalf("first routine run = %+v", resp)
	}
	if resp := routine("w_1"); resp.Status != gjagent.StatusBlocked || resp.Refusal.Code != refusalBudgetExceeded {
		t.Fatalf("routine over its cap = %+v", resp)
	}
	if resp := routine("w_2"); resp.Status != gjagent.StatusAnswered {
		t.Fatalf("another routine = %+v", resp)
	}
}

func TestBudgetsFollowTheMode(t *testing.T) {
	conf := &Config{}
	conf.Agent.Enabled = true
	conf.Core.Artifacts.Enabled = true
	if configBudgetsEnabled(conf) {
		t.Fatal("budgets are off outside agentic mode unless configured")
	}
	off := false
	conf.Agent.Budgets.Enabled = &off
	if configBudgetsEnabled(conf) {
		t.Fatal("an explicit off wins")
	}
}
