package serv

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
	gjagent "github.com/dosco/graphjin/agent/v3"
	gjeval "github.com/dosco/graphjin/agent/v3/eval"
	"github.com/dosco/graphjin/core/v3"
)

// queryingWorker answers each question with a fixed answer and reports the
// query it ran in the shape the data agent's action trail has.
type queryingWorker struct {
	turns map[string]struct{ answer, query string }
}

func (w *queryingWorker) Run(_ context.Context, req gjagent.Request) (gjagent.Response, error) {
	turn := w.turns[req.Instruction]
	return gjagent.Response{
		Status: gjagent.StatusAnswered,
		Answer: turn.answer,
		Actions: []any{map[string]any{
			"step": 1, "source": "model", "tool": "execute_graphql",
			"args": map[string]any{"query": turn.query}, "status": "ok",
			"summary": map[string]any{"has_data": true},
		}},
	}, nil
}

// Past questions become tests: the query is re-run by the real engine as the
// person who asked, the answer they were given must pass it, and only an
// admin can read the result.
func TestExperienceMinesPastQuestionsAsTheirAsker(t *testing.T) {
	_, svc := newSQLiteWatchServiceWithOptions(t, 10, func(conf *Config) {
		conf.Agent.Enabled = true
		conf.Core.Threads = core.ThreadsConfig{Enabled: true, HistoryTurns: 12, RunLeaseSeconds: 3, MaxPerOwner: 3}
		conf.Agent.Lead.Enabled = true
		// The asker sees only open orders; an admin sees them all.
		conf.Core.Sources[0].Access.Grants = []core.SourceAccessGrant{{Role: "analyst", Tables: []core.SourceAccessGrantTable{{
			Name: "orders", Columns: []string{"id", "status", "updated_at"}, Filter: `{ status: { eq: "open" } }`,
		}}}}
	})
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatal(err)
	}
	db := svc.dbs["app"]
	startSQLiteWatchCore(t, svc, db)
	for _, status := range []string{"open", "open", "open", "closed", "closed"} {
		if _, err := db.Exec(`INSERT INTO orders (status, updated_at) VALUES (?, '2026-11-01')`, status); err != nil {
			t.Fatal(err)
		}
	}

	hs := &HttpService{}
	hs.Store(svc)
	worker := &queryingWorker{turns: map[string]struct{ answer, query string }{
		"How many open orders are there right now?": {"There are 3 open orders.", `query { orders { count_id } }`},
		"Which orders are open at the moment?":      {"Orders 1, 2 and 3 are open.", `query { orders(where: { status: { eq: "open" } }) { id status } }`},
	}}
	prev := newGraphJinAgentRunner
	newGraphJinAgentRunner = func(*graphjinService, gjagent.Config, ...gjagent.Option) (graphjinAgentRunner, error) {
		return worker, nil
	}
	t.Cleanup(func() { newGraphJinAgentRunner = prev })
	model := &leadModelClient{responses: []ax.Value{
		leadAsks("How many open orders are there right now?"),
		leadAnswers("There are 3 open orders."),
		leadAsks("Which orders are open at the moment?"),
		leadAnswers("Orders 1, 2 and 3 are open."),
	}}
	svc.agentClientFactory = func(gjagent.Config) (ax.AIClient, error) { return model, nil }

	user := threadUserCtx("user_1")
	thread, err := svc.createThread(user, threadSpec{})
	if err != nil {
		t.Fatal(err)
	}
	threadID := thread["id"].(string)
	for _, instruction := range []string{"How many orders are open?", "Which ones?"} {
		if rec := postThreadTurn(t, hs, user, `{"instruction":"`+instruction+`","thread_id":"`+threadID+`"}`, false); rec.Code != http.StatusOK {
			t.Fatalf("turn %q = %d: %s", instruction, rec.Code, rec.Body.String())
		}
	}
	// The person marks the first answer right.
	if _, err := rateAnswer(svc, user, threadID+"-00000002", "up", ""); err != nil {
		t.Fatal(err)
	}

	// The rating's report carries what the data agent ran, for whoever writes
	// a draft's oracle.
	reports := mustFeedback(t, svc, user)
	if len(reports) != 1 || fmt.Sprint(reports[0]["queries"]) != "[query { orders { count_id } }]" {
		t.Fatalf("feedback queries = %v", reports)
	}

	get := func(ctx context.Context, query string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, routeAgentExperience+query, nil).WithContext(ctx)
		rec := httptest.NewRecorder()
		hs.AgentExperience(nil, nil).ServeHTTP(rec, req)
		return rec
	}
	if rec := get(user, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("a non-admin read experience: %d %s", rec.Code, rec.Body.String())
	}

	admin := contextWithUserRole(artifactUserCtx("admin_1"), "admin")
	rec := get(admin, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("experience = %d: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Cases   []gjeval.MinedCase `json:"cases"`
		Summary gjeval.MineSummary `json:"summary"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Summary.Candidates != 2 || out.Summary.Tests != 1 || out.Summary.Dropped[gjeval.MineDropList] != 1 {
		t.Fatalf("summary = %+v", out.Summary)
	}
	// Run as the admin, the count would be 5 and the answer "3" would fail its
	// own key: a case here means the query ran as the person who asked.
	c := out.Cases[0]
	if c.Prompt != "How many open orders are there right now?" || c.Value != "3" || c.Tier != gjeval.MineTierConfirmed {
		t.Fatalf("case = %+v", c)
	}
	if c.Source.UserPrompt != "How many orders are open?" || c.Source.Origin != "chat" || c.Oracle.Extract != "orders.0.count_id" {
		t.Fatalf("case source %+v oracle %+v", c.Source, c.Oracle)
	}
	task, err := c.Task()
	if err != nil || task.Provenance.Source != "mined-confirmed" {
		t.Fatalf("task %+v err %v", task.Provenance, err)
	}

	// The data agent's answers stay on the server; only the tests leave.
	if strings.Contains(rec.Body.String(), "There are 3 open orders") {
		t.Fatalf("the response carries an answer: %s", rec.Body.String())
	}

	// One run per page: the offset of the next page is a run index, and the
	// last page says nothing is left.
	type paged struct {
		Summary gjeval.MineSummary `json:"summary"`
		Page    experiencePage     `json:"page"`
	}
	readPage := func(query string) paged {
		t.Helper()
		var p paged
		if rec := get(admin, query); rec.Code != http.StatusOK {
			t.Fatalf("%s = %d: %s", query, rec.Code, rec.Body.String())
		} else if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	first := readPage("?limit=1")
	if first.Page.RunsRead != 1 || first.Page.NextOffset == nil || *first.Page.NextOffset != 1 {
		t.Fatalf("first page = %+v", first.Page)
	}
	if first.Page.Until == "" {
		t.Fatalf("the first page names no cutoff: %+v", first.Page)
	}
	second := readPage("?limit=1&offset=1&until=" + url.QueryEscape(first.Page.Until))
	if second.Page.Until != first.Page.Until {
		t.Fatalf("cutoff moved: %q then %q", first.Page.Until, second.Page.Until)
	}
	// A cutoff before every run lists nothing.
	if empty := readPage("?until=2000-01-01T00:00:00Z"); empty.Page.RunsRead != 0 || empty.Summary.Candidates != 0 {
		t.Fatalf("cutoff ignored: %+v", empty)
	}
	if second.Page.RunsRead != 1 || second.Page.NextOffset != nil || first.Summary.Candidates+second.Summary.Candidates != 2 {
		t.Fatalf("second page = %+v, candidates %d + %d", second.Page, first.Summary.Candidates, second.Summary.Candidates)
	}

	rec = get(admin, "?format=summary")
	var summaryOnly map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &summaryOnly); err != nil || summaryOnly["cases"] != nil || summaryOnly["summary"] == nil {
		t.Fatalf("summary format = %s", rec.Body.String())
	}
}
