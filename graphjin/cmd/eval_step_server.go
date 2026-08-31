package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	gjeval "github.com/dosco/graphjin/agent/v3/eval"
)

// The HTTP side of the step bridge.
//
// Three calls: start an episode and get the first thing the model is asked,
// send a completion and get the next thing, and abandon one. The episode itself
// runs in a goroutine driving the ordinary graded path, so nothing here decides
// anything about scoring.

type stepEpisode struct {
	id       string
	task     gjeval.Task
	instance gjeval.Instance
	mailbox  *stepMailbox
	cancel   context.CancelFunc
	done     chan struct{}

	mu       sync.Mutex
	parked   *mailboxRequest
	episode  gjeval.Episode
	runErr   error
	released bool
	// deadline is when an episode nobody is driving gives its world back. A
	// parked episode holds a world open, and a trainer that crashed mid-episode
	// would otherwise take that world out of the pool for good.
	deadline time.Time
}

type stepServer struct {
	env     *envServer
	timeout time.Duration

	mu       sync.Mutex
	episodes map[string]*stepEpisode
	next     int64
}

type stepResetRequest struct {
	TaskID  string `json:"task_id,omitempty"`
	Slug    string `json:"slug,omitempty"`
	Profile string `json:"reward_profile,omitempty"`
}

type stepRequest struct {
	EpisodeID        string `json:"episode_id"`
	Completion       string `json:"completion"`
	PromptTokens     int64  `json:"prompt_tokens,omitempty"`
	CompletionTokens int64  `json:"completion_tokens,omitempty"`
}

// stepObservation is everything the model was given: which stage asked, the
// rendered conversation, the tools it may call and the shape its answer must
// take. A trainer that received only the messages would produce completions the
// pipeline then failed to parse.
type stepObservation struct {
	Stage          string        `json:"stage"`
	Messages       []stepMessage `json:"messages"`
	Functions      any           `json:"functions,omitempty"`
	ResponseFormat any           `json:"response_format,omitempty"`
}

type stepResponse struct {
	EpisodeID   string              `json:"episode_id"`
	TaskID      string              `json:"task_id,omitempty"`
	Slug        string              `json:"slug,omitempty"`
	Done        bool                `json:"done"`
	Observation *stepObservation    `json:"observation,omitempty"`
	Status      string              `json:"status,omitempty"`
	Answer      string              `json:"answer,omitempty"`
	Pass        bool                `json:"pass,omitempty"`
	Reward      float64             `json:"reward,omitempty"`
	Score       *gjeval.ScoreDetail `json:"score,omitempty"`
}

func newStepServer(env *envServer, timeout time.Duration) *stepServer {
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	return &stepServer{env: env, timeout: timeout, episodes: map[string]*stepEpisode{}}
}

func (s *stepServer) register(mux *http.ServeMux) {
	mux.HandleFunc("/step/reset", s.handleReset)
	mux.HandleFunc("/step", s.handleStep)
	mux.HandleFunc("/step/", s.handleAbandon)
}

func (s *stepServer) handleReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.env.writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		return
	}
	var request stepResetRequest
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
	mailbox := s.env.mailboxFor(instance)
	if mailbox == nil {
		_ = s.env.pool.Release(instance)
		s.env.writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "no_mailbox", "message": "this world was not started for step-driven episodes"})
		return
	}
	mailbox.reset()

	// The episode context is deliberately not the request's: the request ends as
	// soon as the first observation is returned, and the episode continues.
	ctx, cancel := context.WithCancel(context.Background())
	episode := &stepEpisode{
		id: s.mint(), task: task, instance: instance, mailbox: mailbox,
		cancel: cancel, done: make(chan struct{}), deadline: time.Now().Add(s.timeout),
	}
	s.mu.Lock()
	s.episodes[episode.id] = episode
	s.mu.Unlock()

	go func() {
		defer close(episode.done)
		result, err := s.env.runner.RunEpisode(ctx, instance, task, gjeval.EpisodeOptions{Profile: profile})
		episode.mu.Lock()
		episode.episode, episode.runErr = result, err
		episode.mu.Unlock()
	}()

	s.respondWithNext(w, episode)
}

func (s *stepServer) handleStep(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.env.writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		return
	}
	var request stepRequest
	if !decodeStepBody(s.env, w, r, &request) {
		return
	}
	episode, ok := s.lookup(strings.TrimSpace(request.EpisodeID))
	if !ok {
		// Gone rather than not-found: the usual reason is that the episode timed
		// out and gave its world back, which a trainer needs to tell apart from a
		// typo in the id.
		s.env.writeJSON(w, http.StatusGone, map[string]any{
			"error": "episode_gone", "message": "That episode has ended or timed out."})
		return
	}
	episode.mu.Lock()
	parked := episode.parked
	episode.parked = nil
	episode.deadline = time.Now().Add(s.timeout)
	episode.mu.Unlock()
	if parked == nil {
		s.env.writeJSON(w, http.StatusConflict, map[string]any{
			"error": "nothing_awaiting", "message": "This episode is not waiting for a completion."})
		return
	}
	parked.Reply <- mailboxReply{
		Completion:       request.Completion,
		PromptTokens:     request.PromptTokens,
		CompletionTokens: request.CompletionTokens,
	}
	s.respondWithNext(w, episode)
}

// handleAbandon ends an episode early and gives its world back.
func (s *stepServer) handleAbandon(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/step/")
	if r.Method != http.MethodDelete || id == "" || strings.Contains(id, "/") {
		s.env.writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		return
	}
	episode, ok := s.lookup(id)
	if !ok {
		s.env.writeJSON(w, http.StatusGone, map[string]any{"error": "episode_gone"})
		return
	}
	s.finish(episode)
	s.env.writeJSON(w, http.StatusOK, stepResponse{EpisodeID: id, Done: true, Status: "abandoned"})
}

// respondWithNext waits for whichever comes first: the next thing the model is
// asked, or the episode ending.
func (s *stepServer) respondWithNext(w http.ResponseWriter, episode *stepEpisode) {
	select {
	case parked := <-episode.mailbox.requests:
		episode.mu.Lock()
		episode.parked = parked
		episode.deadline = time.Now().Add(s.timeout)
		episode.mu.Unlock()
		s.env.writeJSON(w, http.StatusOK, stepResponse{
			EpisodeID: episode.id, TaskID: episode.task.ID, Slug: episode.task.Slug,
			Observation: &stepObservation{
				Stage:          parked.Stage,
				Messages:       stepMessagesFromRequest(parked.Values),
				Functions:      parked.Values["functions"],
				ResponseFormat: parked.Values["response_format"],
			},
		})
	case <-episode.done:
		s.completed(w, episode)
	case <-time.After(s.timeout):
		// The agent is neither asking anything nor finishing. Ending it here is
		// what keeps a stuck episode from holding a world for the rest of the run.
		s.finish(episode)
		s.env.writeJSON(w, http.StatusGatewayTimeout, map[string]any{
			"error": "episode_timeout", "message": "The episode neither asked for a completion nor finished in time."})
	}
}

func (s *stepServer) completed(w http.ResponseWriter, episode *stepEpisode) {
	episode.mu.Lock()
	result, runErr := episode.episode, episode.runErr
	episode.mu.Unlock()
	s.finish(episode)
	if runErr != nil {
		s.env.writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "episode_failed", "message": runErr.Error()})
		return
	}
	status, answer := episodeStatusAndAnswer(result.Response)
	score := result.Score
	s.env.writeJSON(w, http.StatusOK, stepResponse{
		EpisodeID: episode.id, TaskID: episode.task.ID, Slug: episode.task.Slug, Done: true,
		Status: status, Answer: answer, Pass: score.Pass, Reward: score.Vector.Reward, Score: &score,
	})
}

// finish ends an episode once: cancels it, waits for the goroutine to stop
// touching the world, and gives the world back.
func (s *stepServer) finish(episode *stepEpisode) {
	s.mu.Lock()
	delete(s.episodes, episode.id)
	s.mu.Unlock()

	episode.mu.Lock()
	if episode.released {
		episode.mu.Unlock()
		return
	}
	episode.released = true
	parked := episode.parked
	episode.parked = nil
	episode.mu.Unlock()

	if parked != nil {
		// Unblock the agent so the run can unwind rather than sitting on a world.
		close(parked.Reply)
	}
	episode.cancel()
	<-episode.done
	episode.mailbox.reset()
	_ = s.env.pool.Release(episode.instance)
}

// reap ends episodes nobody has driven for longer than the timeout.
func (s *stepServer) reap() {
	now := time.Now()
	s.mu.Lock()
	var stale []*stepEpisode
	for _, episode := range s.episodes {
		episode.mu.Lock()
		expired := now.After(episode.deadline)
		episode.mu.Unlock()
		if expired {
			stale = append(stale, episode)
		}
	}
	s.mu.Unlock()
	for _, episode := range stale {
		s.finish(episode)
	}
}

func (s *stepServer) reapUntil(ctx context.Context) {
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

func (s *stepServer) lookup(id string) (*stepEpisode, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	episode, ok := s.episodes[id]
	return episode, ok
}

func (s *stepServer) mint() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	return fmt.Sprintf("step-%d", s.next)
}

// readLimitedBody reads a request body with a cap, so a malformed or hostile
// client cannot make the server allocate without bound.
func readLimitedBody(r *http.Request) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r.Body, 1<<20))
}

func decodeStepBody(env *envServer, w http.ResponseWriter, r *http.Request, out any) bool {
	body, err := readLimitedBody(r)
	if err != nil {
		env.writeJSON(w, http.StatusBadRequest, map[string]any{"error": "unreadable_body", "message": err.Error()})
		return false
	}
	if len(body) == 0 {
		return true
	}
	if err := json.Unmarshal(body, out); err != nil {
		env.writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_body", "message": err.Error()})
		return false
	}
	return true
}
