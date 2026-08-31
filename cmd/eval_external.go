package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	gjeval "github.com/dosco/graphjin/agent/v3/eval"
	"github.com/dosco/graphjin/serv/v3"
)

// Grading an agent this harness does not host.
//
// A lab with its own scaffold does not want GraphJin's agent loop; it wants its
// own policy connected to a real governed database, doing real work, and graded
// by the same contract everyone else is graded by. So it gets the task, an MCP
// endpoint and a deadline, does the work itself, and posts an answer.
//
// The part that makes this a measurement rather than a self-report is that the
// server watched. Every tool call is recorded here and assembled into the same
// account of what happened that GraphJin's own agent returns, so the method and
// behavior rules apply unchanged — an answer submitted without doing the work
// scores zero for the same reason it always did.

// mcpToolRecorder collects the calls made against one world.
type mcpToolRecorder struct {
	mu        sync.Mutex
	recording bool
	events    []serv.MCPToolEvent
}

func newMCPToolRecorder() *mcpToolRecorder { return &mcpToolRecorder{} }

func (r *mcpToolRecorder) record(event serv.MCPToolEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.recording {
		return
	}
	// A runaway client must not be able to grow this without bound; the method
	// rules only ever read a handful of calls.
	if len(r.events) < 2000 {
		r.events = append(r.events, event)
	}
}

func (r *mcpToolRecorder) start() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recording, r.events = true, nil
}

func (r *mcpToolRecorder) stop() []serv.MCPToolEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recording = false
	events := r.events
	r.events = nil
	return events
}

// responseFromMCPEvents turns recorded calls into the account of an episode the
// scorer already knows how to read.
//
// The shape matters exactly: a call with no summary counts as having succeeded,
// so a failed call must say so or a run of errors would grade as competent work.
func responseFromMCPEvents(status, answer string, events []serv.MCPToolEvent) gjagent.Response {
	response := gjagent.Response{Status: status, Answer: answer}
	actions := make([]map[string]any, 0, len(events))
	for _, event := range events {
		errorCount := 0
		if event.IsError {
			errorCount = 1
		}
		action := map[string]any{
			"tool":    event.Tool,
			"status":  "ok",
			"summary": map[string]any{"error_count": errorCount},
		}
		if len(event.Arguments) != 0 {
			action["args"] = event.Arguments
		}
		actions = append(actions, action)
	}
	response.Actions = actions
	return response
}

type externalEpisode struct {
	id       string
	task     gjeval.Task
	profile  gjeval.RewardProfile
	instance gjeval.Instance
	recorder *mcpToolRecorder
	prep     gjeval.EpisodePrep
	started  time.Time
	deadline time.Time

	mu       sync.Mutex
	released bool
}

type externalServer struct {
	env     *envServer
	timeout time.Duration

	mu       sync.Mutex
	episodes map[string]*externalEpisode
	next     int64
}

func newExternalServer(env *envServer, timeout time.Duration) *externalServer {
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	return &externalServer{env: env, timeout: timeout, episodes: map[string]*externalEpisode{}}
}

func (s *externalServer) register(mux *http.ServeMux) {
	mux.HandleFunc("/external/episodes", s.handleStart)
	mux.HandleFunc("/external/episodes/", s.handleEpisodePath)
}

type externalStartRequest struct {
	TaskID  string `json:"task_id,omitempty"`
	Slug    string `json:"slug,omitempty"`
	Profile string `json:"reward_profile,omitempty"`
}

type externalStartResponse struct {
	EpisodeID  string            `json:"episode_id"`
	TaskID     string            `json:"task_id"`
	Slug       string            `json:"slug"`
	Prompt     string            `json:"prompt"`
	Turns      []gjeval.TurnSpec `json:"turns,omitempty"`
	Headers    map[string]string `json:"headers"`
	MCPURL     string            `json:"mcp_url"`
	GraphQLURL string            `json:"graphql_url"`
	Deadline   string            `json:"deadline"`
	Note       string            `json:"note"`
}

type externalAnswerRequest struct {
	Status string `json:"status,omitempty"`
	Answer string `json:"answer"`
}

type externalAnswerResponse struct {
	EpisodeID string                   `json:"episode_id"`
	TaskID    string                   `json:"task_id"`
	Pass      bool                     `json:"pass"`
	Reward    float64                  `json:"reward"`
	Score     gjeval.ScoreDetail       `json:"score"`
	Mutation  *gjeval.MutationEvidence `json:"mutation,omitempty"`
	ToolCalls int                      `json:"tool_calls"`
	Note      string                   `json:"note"`
}

// externalRewardNote is returned with every graded external episode.
//
// An external agent's token use is its own and never reaches this server, so
// the efficiency term has nothing to price. Saying so beside the number is the
// difference between a caller comparing the right things and a caller putting
// an external score next to a benchmark row as though they meant the same.
const externalRewardNote = "Token usage is not observable for an external agent, so the efficiency term is " +
	"not measured here. Rewards from external episodes are comparable with each other, not with hosted runs."

func (s *externalServer) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.env.writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		return
	}
	var request externalStartRequest
	if !decodeStepBody(s.env, w, r, &request) {
		return
	}
	task, ok := s.env.resolveTask(envEpisodeRequest{TaskID: request.TaskID, Slug: request.Slug})
	if !ok {
		s.env.writeJSON(w, http.StatusNotFound, map[string]any{
			"error": "unknown_task", "message": "No served task matches that id or slug."})
		return
	}
	profile := s.env.profile
	if requested := strings.TrimSpace(request.Profile); requested != "" {
		profile = gjeval.RewardProfile(requested)
		if err := profile.Validate(); err != nil {
			s.env.writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": "invalid_reward_profile", "message": err.Error()})
			return
		}
	}

	instance, err := s.env.pool.Acquire(r.Context())
	if err != nil {
		s.env.writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": "no_world_available", "message": err.Error()})
		return
	}
	recorder := s.env.recorderFor(instance)
	if recorder == nil {
		_ = s.env.pool.Release(instance)
		s.env.writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "no_recorder", "message": "this world was not started for external episodes"})
		return
	}

	// The same preparation a hosted episode gets: reset, setup steps, the state
	// the task waits for, and a record of everything it must leave alone.
	prep, err := s.env.runner.BeginEpisodeEnvironment(r.Context(), instance, task)
	if err != nil {
		_ = s.env.pool.Release(instance)
		s.env.writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "prepare_failed", "message": err.Error()})
		return
	}
	recorder.start()

	episode := &externalEpisode{
		id: s.mint(), task: task, profile: profile, instance: instance, recorder: recorder,
		prep: prep, started: time.Now(), deadline: time.Now().Add(s.timeout),
	}
	s.mu.Lock()
	s.episodes[episode.id] = episode
	s.mu.Unlock()

	s.env.writeJSON(w, http.StatusOK, externalStartResponse{
		EpisodeID: episode.id, TaskID: task.ID, Slug: task.Slug,
		Prompt: task.Prompt, Turns: task.Turns,
		Headers:    episodeHeaders(instance, task),
		MCPURL:     strings.TrimSuffix(instance.BaseURL(), "/") + "/api/v1/mcp",
		GraphQLURL: strings.TrimSuffix(instance.BaseURL(), "/") + "/api/v1/graphql",
		Deadline:   episode.deadline.UTC().Format(time.RFC3339),
		Note:       externalRewardNote,
	})
}

func (s *externalServer) handleEpisodePath(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/external/episodes/")
	id, action, _ := strings.Cut(path, "/")
	switch {
	case id == "":
		s.env.writeJSON(w, http.StatusNotFound, map[string]any{"error": "unknown_episode"})
	case action == "answer" && r.Method == http.MethodPost:
		s.handleAnswer(w, r, id)
	case action == "" && r.Method == http.MethodDelete:
		s.handleAbandon(w, id)
	default:
		s.env.writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
	}
}

func (s *externalServer) handleAnswer(w http.ResponseWriter, r *http.Request, id string) {
	episode, ok := s.take(id)
	if !ok {
		s.env.writeJSON(w, http.StatusGone, map[string]any{
			"error": "episode_gone", "message": "That episode has ended or timed out."})
		return
	}
	var request externalAnswerRequest
	if !decodeStepBody(s.env, w, r, &request) {
		s.abandon(episode)
		return
	}
	status := strings.TrimSpace(request.Status)
	if status == "" {
		status = gjagent.StatusAnswered
	}
	events := episode.recorder.stop()
	response := responseFromMCPEvents(status, request.Answer, events)

	// Scoring runs on its own deadline rather than the request's. Once an answer
	// has been submitted the episode has to be graded and the world put back —
	// a client that hangs up mid-grade would otherwise abort the reset and hand
	// a mutated world to whatever ran next.
	scoreCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Minute)
	defer cancel()
	detail, evidence, err := s.env.runner.FinishEpisodeScoring(scoreCtx, episode.instance, episode.task,
		episode.prep, response, time.Since(episode.started).Milliseconds(), episode.profile)
	s.release(episode)
	if err != nil {
		s.env.writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "scoring_failed", "message": err.Error()})
		return
	}
	s.env.writeJSON(w, http.StatusOK, externalAnswerResponse{
		EpisodeID: episode.id, TaskID: episode.task.ID, Pass: detail.Pass,
		Reward: detail.Vector.Reward, Score: detail, Mutation: evidence,
		ToolCalls: len(events), Note: externalRewardNote,
	})
}

func (s *externalServer) handleAbandon(w http.ResponseWriter, id string) {
	episode, ok := s.take(id)
	if !ok {
		s.env.writeJSON(w, http.StatusGone, map[string]any{"error": "episode_gone"})
		return
	}
	s.abandon(episode)
	s.env.writeJSON(w, http.StatusOK, map[string]any{"episode_id": id, "status": "abandoned"})
}

// abandon ends an episode without grading it, putting the world back the way a
// graded episode would.
func (s *externalServer) abandon(episode *externalEpisode) {
	episode.recorder.stop()
	if episode.prep.Resettable != nil {
		// A prepared write left in place would silently change every episode that
		// followed it in this world.
		_ = episode.prep.Resettable.Reset(context.Background())
	}
	s.release(episode)
}

func (s *externalServer) release(episode *externalEpisode) {
	episode.mu.Lock()
	if episode.released {
		episode.mu.Unlock()
		return
	}
	episode.released = true
	episode.mu.Unlock()
	_ = s.env.pool.Release(episode.instance)
}

func (s *externalServer) reap() {
	now := time.Now()
	s.mu.Lock()
	var stale []*externalEpisode
	for id, episode := range s.episodes {
		if now.After(episode.deadline) {
			stale = append(stale, episode)
			delete(s.episodes, id)
		}
	}
	s.mu.Unlock()
	for _, episode := range stale {
		s.abandon(episode)
	}
}

func (s *externalServer) reapUntil(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.reap()
		}
	}
}

// take removes an episode so two requests cannot grade or abandon the same one.
func (s *externalServer) take(id string) (*externalEpisode, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	episode, ok := s.episodes[id]
	if ok {
		delete(s.episodes, id)
	}
	return episode, ok
}

func (s *externalServer) mint() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	return fmt.Sprintf("external-%d", s.next)
}

// episodeHeaders are the headers an external agent must send, so it acts under
// the same role the task was written for.
func episodeHeaders(instance gjeval.Instance, task gjeval.Task) map[string]string {
	headers := map[string]string{}
	for key, value := range instance.Headers() {
		headers[key] = value
	}
	if role := strings.TrimSpace(task.CapabilityProfile.RoleClass); role != "" {
		headers["X-User-Role"] = role
		if role == "anon" {
			delete(headers, "X-User-ID")
			delete(headers, "X-Account-ID")
		}
	}
	return headers
}
