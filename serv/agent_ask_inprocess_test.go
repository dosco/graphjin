package serv

import (
	"context"
	"errors"
	"testing"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/core/v3"
	"go.uber.org/zap"
)

func TestAskAgentInProcessCarriesClaimsObserverAndHistory(t *testing.T) {
	runner := &scriptedAgentRunner{
		resp: gjagent.Response{Status: gjagent.StatusAnswered, Answer: "two"},
		emit: []gjagent.ActionEvent{{Tool: "query_catalog"}, {Tool: "execute_graphql"}},
	}
	withScriptedAgentRunner(t, runner)
	logger := zap.NewNop()
	hs := &HttpService{}
	hs.Store(&graphjinService{
		conf: &Config{
			Core: core.Config{Sources: []core.SourceConfig{{Name: "graphjin", Kind: "database", Type: "sqlite"}}},
			Serv: Serv{Agent: AgentConfig{Enabled: true}},
		},
		log:  logger.Sugar(),
		zlog: logger,
	})

	var seen []string
	req := gjagent.Request{
		Instruction: "how many orders",
		History:     []gjagent.Turn{{Role: "user", Content: "earlier"}},
		Observer:    func(e gjagent.ActionEvent) { seen = append(seen, e.Tool) },
	}
	resp, err := hs.AskAgent(context.Background(), map[string]any{"sub": "user-7"}, req)
	if err != nil || resp.Answer != "two" {
		t.Fatalf("AskAgent = %+v, %v", resp, err)
	}
	if got := runner.ctx.Value(core.UserIDKey); got != "user-7" {
		t.Fatalf("user id = %v, want user-7 from claims", got)
	}
	if runner.req.Capabilities == nil || !runner.req.Capabilities.Authenticated {
		t.Fatalf("capability profile not derived from claims: %+v", runner.req.Capabilities)
	}
	if len(runner.req.History) != 1 || len(seen) != 2 {
		t.Fatalf("history %d, observed %v", len(runner.req.History), seen)
	}

	if _, err := hs.AskAgent(context.Background(), map[string]any{}, req); err == nil {
		t.Fatal("claims without a subject must be refused")
	}
	hs.Load().(*graphjinService).conf.Agent.Enabled = false
	if _, err := hs.AskAgent(context.Background(), map[string]any{"sub": "u"}, req); !errors.Is(err, ErrAgentDisabled) {
		t.Fatalf("disabled agent err = %v", err)
	}
}
