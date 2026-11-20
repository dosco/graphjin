package serv

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	gjeval "github.com/dosco/graphjin/agent/v3/eval"
	"github.com/dosco/graphjin/auth/v3"
)

// Experience is the agent's past work, read back as tests.
//
// Every stored thread turn keeps the questions the lead put to the data agent,
// the answers, and the query text the data agent ran (in the run's result
// event). This turns each such question into a candidate, and the eval miner
// keeps the ones whose query is an answer key the engine can re-run: one
// figure, computed by the database. Nothing new is stored, and the query
// re-runs as the person who asked, so the key sees the rows they saw.

const (
	experienceDefaultRuns = 50
	experienceMaxRuns     = 200
)

type experienceFilter struct {
	Since, Until string
	Origin       string
	Limit        int
	Offset       int
}

// experienceAsker is who a run's queries ran as.
type experienceAsker struct {
	ID, Role, Account string
}

type experienceCandidate struct {
	gjeval.MineCandidate
	asker experienceAsker
}

// experienceRunOrigin names where a turn came from, from its run id.
func experienceRunOrigin(runID, channel string) string {
	switch {
	case strings.HasPrefix(runID, "run_w"):
		return "routine"
	case strings.HasPrefix(runID, "run_c_"):
		return "continuation"
	case strings.HasPrefix(runID, "run_o_"):
		return "outcome"
	case channel == "watch":
		return "routine"
	}
	return "chat"
}

// experienceCandidates reads finished thread runs, newest first, and returns
// one candidate per question each asked the data agent, plus how many runs it
// read.
func (s *graphjinService) experienceCandidates(ctx context.Context, f experienceFilter) ([]experienceCandidate, int, int, error) {
	where := []string{`status: { eq: $status }`}
	vars := map[string]any{"status": runStatusDone}
	if f.Since != "" {
		where, vars["since"] = append(where, `created_at: { gte: $since }`), f.Since
	}
	if f.Until != "" {
		where, vars["until"] = append(where, `created_at: { lte: $until }`), f.Until
	}
	runs, err := s.internalStoreAllRowsOrdered(ctx, "runs", `where: { `+strings.Join(where, ", ")+` }`, runStoreFields, "created_at", vars)
	if err != nil {
		return nil, 0, -1, err
	}
	sort.SliceStable(runs, func(i, j int) bool {
		return stringMapValue(runs[i], "created_at") > stringMapValue(runs[j], "created_at")
	})

	ratings, err := s.experienceRatings(ctx)
	if err != nil {
		return nil, 0, -1, err
	}
	threads := map[string]map[string]any{}
	var out []experienceCandidate
	// Offsets index every finished run, not just the ones a filter keeps, so
	// next is the first run this call did not look at; -1 when none is left.
	read, next := 0, -1
	for i, run := range runs {
		if i < f.Offset {
			continue
		}
		if read >= f.Limit {
			next = i
			break
		}
		runID, threadID := stringMapValue(run, "id"), stringMapValue(run, "thread_id")
		thread, ok := threads[threadID]
		if !ok {
			thread, err = s.threadStoreRow(ctx, threadID)
			if err != nil {
				return nil, 0, -1, err
			}
			threads[threadID] = thread
		}
		if thread == nil {
			continue
		}
		origin := experienceRunOrigin(runID, stringMapValue(thread, "channel"))
		if f.Origin != "" && f.Origin != origin {
			continue
		}
		read++
		resp, err := s.experienceRunResponse(ctx, runID)
		if err != nil || resp == nil {
			continue
		}
		asker := experienceAsker{ID: stringMapValue(thread, "owner_id"), Role: stringMapValue(thread, "owner_role"), Account: stringMapValue(thread, "account_id")}
		// A routine run as the service identity keeps its id but not its
		// role. While the identity is the same, its role is known; once it has
		// been changed, the run's view of the data cannot be rebuilt.
		unknown := ""
		if runAs := stringMapValue(run, "run_as_id"); runAs != "" {
			if runAs == s.conf.Agent.ServiceIdentity.UserID {
				asker.ID, asker.Role = runAs, s.conf.Agent.ServiceIdentity.Role
			} else {
				unknown = "ran as " + runAs + ", no longer the service identity"
			}
		}
		userPrompt, _ := s.experienceUserPrompt(ctx, runID)
		for _, c := range experienceRunCandidates(*resp) {
			c.RunID = runID
			c.ID = runID + "-" + strconv.Itoa(c.Delegate)
			c.AskedAt = stringMapValue(run, "created_at")
			c.Origin = origin
			c.UserPrompt = userPrompt
			c.Rating = ratings[runID]
			c.AskerUnknown = unknown
			out = append(out, experienceCandidate{MineCandidate: c, asker: asker})
		}
	}
	return out, read, next, nil
}

// experienceRunResponse is the run's settled response, from its result event.
// The event is stored whole, so unlike the message's response_json it is not
// trimmed when large.
func (s *graphjinService) experienceRunResponse(ctx context.Context, runID string) (*gjagent.Response, error) {
	rows, err := s.internalStoreRows(ctx, "run_events", `where: { run_id: { eq: $run_id }, kind: { eq: "result" } }, limit: 1`,
		runEventStoreFields, map[string]any{"run_id": runID})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	var resp gjagent.Response
	if err := json.Unmarshal([]byte(jsonMapString(rows[0], "payload")), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (s *graphjinService) experienceUserPrompt(ctx context.Context, runID string) (string, error) {
	rows, err := s.internalStoreRows(ctx, "messages", `where: { run_id: { eq: $run_id }, role: { eq: "user" } }, limit: 1`,
		messageStoreFields, map[string]any{"run_id": runID})
	if err != nil || len(rows) == 0 {
		return "", err
	}
	return stringMapValue(rows[0], "content"), nil
}

// experienceRatings maps each rated run to its latest rating.
func (s *graphjinService) experienceRatings(ctx context.Context) (map[string]string, error) {
	out := map[string]string{}
	if !s.feedbackEnabled() {
		return out, nil
	}
	reports, err := s.listFeedback(ctx)
	if err != nil {
		return nil, err
	}
	// listFeedback is newest first: the first rating seen for a run is its
	// latest.
	for _, report := range reports {
		runID := stringFromAny(report["run_id"])
		if _, seen := out[runID]; runID != "" && !seen {
			out[runID] = stringFromAny(report["rating"])
		}
	}
	return out, nil
}

// experienceRunCandidates splits a lead turn into its questions to the data
// agent. The lead's response lists each question's answer and data in order
// (Data.delegations), its evidence with the question's index
// (Evidence.delegations), and every data-agent action tagged with that index.
func experienceRunCandidates(resp gjagent.Response) []gjeval.MineCandidate {
	data := anySliceFrom(mapFromAny(normalizeExperienceValue(resp.Data))["delegations"])
	evidence := anySliceFrom(mapFromAny(normalizeExperienceValue(resp.Evidence))["delegations"])
	actions := anySliceFrom(normalizeExperienceValue(resp.Actions))
	out := make([]gjeval.MineCandidate, 0, len(data))
	for i, item := range data {
		d := mapFromAny(item)
		var ev map[string]any
		if i < len(evidence) {
			ev = mapFromAny(evidence[i])
		}
		index := int(int64MapValue(ev, "delegate"))
		if index == 0 {
			index = i + 1
		}
		c := gjeval.MineCandidate{
			Delegate:      index,
			Question:      stringFromAny(d["question"]),
			Status:        stringFromAny(d["status"]),
			Answer:        stringFromAny(d["answer"]),
			Data:          d["data"],
			TurnQuestions: len(data),
		}
		if ev != nil {
			c.ViolationCodes = gjagent.ProtocolViolationCodes(gjagent.Response{Evidence: ev["evidence"]})
		}
		for _, raw := range actions {
			a := mapFromAny(raw)
			if int(int64MapValue(a, "delegate")) != index || stringFromAny(a["tool"]) != "execute_graphql" {
				continue
			}
			query := stringFromAny(mapFromAny(a["args"])["query"])
			if strings.TrimSpace(query) == "" {
				continue
			}
			summary := mapFromAny(a["summary"])
			status := stringFromAny(a["status"])
			c.Queries = append(c.Queries, gjeval.MineQuery{
				Query:   query,
				OK:      (status == "" || status == "ok") && int64MapValue(summary, "error_count") == 0,
				HasData: summary["has_data"] == true,
			})
		}
		out = append(out, c)
	}
	return out
}

func normalizeExperienceValue(v any) any {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var out any
	if json.Unmarshal(raw, &out) != nil {
		return nil
	}
	return out
}

// mineExperience mines candidates, re-running each query as the person whose
// turn it was. The admin asking only reads the result.
func (s *graphjinService) mineExperience(ctx context.Context, candidates []experienceCandidate) gjeval.MineReport {
	plain := make([]gjeval.MineCandidate, len(candidates))
	askers := make(map[string]experienceAsker, len(candidates))
	for i, c := range candidates {
		plain[i] = c.MineCandidate
		askers[c.ID] = c.asker
	}
	return gjeval.Mine(ctx, plain, func(ctx context.Context, c gjeval.MineCandidate, query string) (any, error) {
		asker := askers[c.ID]
		// A fresh context: nothing of the admin's identity may reach the query.
		queryCtx, cancel := context.WithTimeout(s.ownerContext(context.Background(), asker.ID, asker.Role, asker.Account), 30*time.Second)
		defer cancel()
		stop := context.AfterFunc(ctx, cancel)
		defer stop()
		return s.runWatchMetricQuery(queryCtx, query, "", time.Now().UTC(), time.UTC)
	})
}

// experiencePage says which runs a call read.
type experiencePage struct {
	// RunsRead is how many finished runs this call read.
	RunsRead int `json:"runs_read"`
	Offset   int `json:"offset"`
	Limit    int `json:"limit"`
	// Until is the newest run time this listing covers; pass it with
	// next_offset so later pages list the same runs.
	Until string `json:"until"`
	// NextOffset continues the listing; absent when no runs are left.
	NextOffset *int `json:"next_offset,omitempty"`
}

// experienceResponse is GET /api/v1/agent/experience: the tests mined from
// past questions to the data agent.
type experienceResponse struct {
	Cases   []gjeval.MinedCase `json:"cases"`
	Summary gjeval.MineSummary `json:"summary"`
	// Drops lists the questions that did not become tests, with the reason:
	// what to look at when deciding which kind of question to support next.
	Drops []gjeval.MineDrop `json:"drops"`
	Page  experiencePage    `json:"page"`
}

// experienceSummaryResponse is the same call with ?format=summary: the counts
// alone.
type experienceSummaryResponse struct {
	Summary gjeval.MineSummary `json:"summary"`
	Page    experiencePage     `json:"page"`
}

// AgentExperience serves GET /api/v1/agent/experience: past questions to the
// data agent, mined into tests. Admin only.
func (s1 *HttpService) AgentExperience(ah auth.HandlerFunc, ns *string) http.Handler {
	return apiV1Handler(s1, ns, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := s1.Load().(*graphjinService)
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			writeJSONStatus(w, http.StatusMethodNotAllowed, map[string]any{"error": "read experience with GET"})
			return
		}
		ctx := s.applyIdentityContext(r.Context())
		if _, ok := artifactUserID(ctx); !ok {
			writeJSONStatus(w, http.StatusUnauthorized, map[string]any{"error": "experience requires a signed-in admin"})
			return
		}
		if !s.identityRoleIsAdmin(ctx) {
			writeJSONStatus(w, http.StatusForbidden, map[string]any{"error": "experience is for admins: it reads every person's questions"})
			return
		}
		q := r.URL.Query()
		f := experienceFilter{Since: q.Get("since"), Until: q.Get("until"), Origin: q.Get("origin"), Limit: experienceDefaultRuns}
		// Offsets index runs newest first, so a run that finishes between two
		// pages would shift them all. The first page pins the listing to now,
		// by the server's clock, and the next pages pass that cutoff back.
		if f.Until == "" {
			f.Until = storeNow()
		}
		if v, err := strconv.Atoi(q.Get("limit")); err == nil && v > 0 {
			f.Limit = min(v, experienceMaxRuns)
		}
		if v, err := strconv.Atoi(q.Get("offset")); err == nil && v > 0 {
			f.Offset = v
		}
		candidates, read, next, err := s.experienceCandidates(ctx, f)
		if err != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		report := s.mineExperience(r.Context(), candidates)
		page := experiencePage{RunsRead: read, Offset: f.Offset, Limit: f.Limit, Until: f.Until}
		if next >= 0 {
			page.NextOffset = &next
		}
		if q.Get("format") == "summary" {
			writeJSONStatus(w, http.StatusOK, experienceSummaryResponse{Summary: report.Summary, Page: page})
			return
		}
		cases := report.Cases
		if cases == nil {
			cases = []gjeval.MinedCase{}
		}
		drops := report.Drops
		if drops == nil {
			drops = []gjeval.MineDrop{}
		}
		writeJSONStatus(w, http.StatusOK, experienceResponse{Cases: cases, Summary: report.Summary, Drops: drops, Page: page})
	}), ah)
}
