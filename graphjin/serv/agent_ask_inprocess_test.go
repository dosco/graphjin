package serv

import (
	"context"
	"errors"
	"testing"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/auth/v3"
	"github.com/golang-jwt/jwt/v5"
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

func TestAskAgentWithTokenVerifiesTheJWT(t *testing.T) {
	runner := &scriptedAgentRunner{resp: gjagent.Response{Status: gjagent.StatusAnswered, Answer: "ok"}}
	withScriptedAgentRunner(t, runner)
	const secret = "a-secret-that-is-long-enough-for-hs256"
	logger := zap.NewNop()
	hs := &HttpService{}
	conf := &Config{
		Core: core.Config{Sources: []core.SourceConfig{{Name: "graphjin", Kind: "database", Type: "sqlite"}}},
		Serv: Serv{Agent: AgentConfig{Enabled: true}},
	}
	conf.Auth = auth.Auth{Type: "jwt"}
	conf.Auth.JWT.Secret = secret
	hs.Store(&graphjinService{conf: conf, log: logger.Sugar(), zlog: logger})

	sign := func(key string) string {
		tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "user-9", "role": "member"}).SignedString([]byte(key))
		if err != nil {
			t.Fatal(err)
		}
		return tok
	}
	if _, err := hs.AskAgentWithToken(context.Background(), sign(secret), gjagent.Request{Instruction: "q"}); err != nil {
		t.Fatal(err)
	}
	if got := runner.ctx.Value(core.UserIDKey); got != "user-9" {
		t.Fatalf("user id = %v, want user-9 from the verified token", got)
	}
	if _, err := hs.AskAgentWithToken(context.Background(), sign("a-different-secret-of-sufficient-length"), gjagent.Request{Instruction: "q"}); err == nil {
		t.Fatal("a token signed with another key was accepted")
	}
}
