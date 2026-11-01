package roles

import (
	"context"
	"testing"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/open-neko/openneko/apps/ax-harness/serve"
)

type fakeAgent struct {
	readOnly bool
	token    string
	req      gjagent.Request
}

func (f *fakeAgent) AgentReadOnly() bool { return f.readOnly }

func (f *fakeAgent) AskAgentWithToken(_ context.Context, token string, req gjagent.Request) (gjagent.Response, error) {
	f.token, f.req = token, req
	req.Observer(gjagent.ActionEvent{Tool: "query_catalog", Status: "ok"})
	return gjagent.Response{Status: "answered", Answer: "1200", Usage: map[string]any{"total_tokens": 321}}, nil
}

func TestGraphJinAskerRefusesAWritableAgentAndCarriesTheTurn(t *testing.T) {
	agent := &fakeAgent{}
	ask := graphJinAsker(agent)
	if _, err := ask(context.Background(), "jwt", serve.AskRequest{Instruction: "q"}); err == nil {
		t.Fatal("a writable GraphJin agent was asked")
	}
	agent.readOnly = true
	var steps []string
	result, err := ask(context.Background(), "jwt", serve.AskRequest{
		Instruction: "q", MaxSteps: 5,
		History: []serve.HistoryTurn{{Role: "user", Content: "earlier"}},
		OnStep:  func(s string) { steps = append(steps, s) },
	})
	if err != nil || result.Answer != "1200" || result.Tokens != 321 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if agent.token != "jwt" || agent.req.MaxSteps != 5 || len(agent.req.History) != 1 || agent.req.History[0].Content != "earlier" {
		t.Fatalf("request=%+v token=%q", agent.req, agent.token)
	}
	if len(steps) != 1 || steps[0] != "Data agent: query catalog" {
		t.Fatalf("steps=%v", steps)
	}
}

func TestGraphJinPathIsTakenOutOfTheServeArgs(t *testing.T) {
	path, rest := graphJinPath([]string{"--path", "/config", "--socket", "/run/s.sock"})
	if path != "/config" || len(rest) != 2 || rest[0] != "--socket" {
		t.Fatalf("path=%q rest=%v", path, rest)
	}
	if path, rest := graphJinPath([]string{"--path=/c"}); path != "/c" || len(rest) != 0 {
		t.Fatalf("path=%q rest=%v", path, rest)
	}
}
