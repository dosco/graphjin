package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	gjeval "github.com/dosco/graphjin/agent/v3/eval"
	"github.com/spf13/cobra"
)

// The environment server turns a GraphJin project into something a training
// loop can drive: a fixed set of graded tasks, a pool of identical worlds, and
// one request per episode.
//
// It deliberately serves episodes rather than steps. GraphJin's agent owns its
// own loop — discovery, guards, repairs, the evidence ledger — and that loop is
// the thing being learned against. A step-level protocol would hand the policy
// a different environment than the one it will actually run in.
//
// The policy is whatever the project's agent configuration points at, so
// pointing this at a trainer's own inference server is GJ_AGENT_BASE_URL and
// GJ_AGENT_MODEL rather than anything specific to training.

type envEpisodeRequest struct {
	TaskID  string `json:"task_id,omitempty"`
	Slug    string `json:"slug,omitempty"`
	Repeat  int    `json:"repeat,omitempty"`
	Profile string `json:"reward_profile,omitempty"`
	// IncludeTrajectory returns the episode rewritten as training steps.
	IncludeTrajectory bool `json:"include_trajectory,omitempty"`
	IncludeResponse   bool `json:"include_response,omitempty"`
}

type envEpisodeResponse struct {
	TaskID     string             `json:"task_id"`
	Slug       string             `json:"slug"`
	Status     string             `json:"status"`
	Answer     string             `json:"answer"`
	Pass       bool               `json:"pass"`
	Reward     float64            `json:"reward"`
	Score      gjeval.ScoreDetail `json:"score"`
	LatencyMS  int64              `json:"latency_ms"`
	Trajectory *gjeval.Trajectory `json:"trajectory,omitempty"`
	Response   any                `json:"response,omitempty"`
}

type envHealthResponse struct {
	Status        string                    `json:"status"`
	Workers       int                       `json:"workers"`
	Tasks         int                       `json:"tasks"`
	Dataset       gjeval.DatasetFingerprint `json:"dataset"`
	RewardVersion string                    `json:"reward_version"`
	RewardProfile string                    `json:"reward_profile"`
	Suite         gjeval.GeneratorMeta      `json:"suite"`
}

type envTaskSummary struct {
	TaskID     string `json:"task_id"`
	Slug       string `json:"slug"`
	Category   string `json:"category"`
	Difficulty string `json:"difficulty"`
	Prompt     string `json:"prompt"`
	Writes     bool   `json:"writes"`
	Family     string `json:"family,omitempty"`
}

type envServer struct {
	pool    *evalInstancePool
	suite   gjeval.Suite
	byID    map[string]gjeval.Task
	bySlug  map[string]gjeval.Task
	profile gjeval.RewardProfile
	runner  gjeval.Runner
	split   *gjeval.SuiteSplit
	side    string
}

func envCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "env",
		Short:        "Serve a GraphJin project as a graded agent environment",
		SilenceUsage: true,
	}
	cmd.AddCommand(envServeCmd())
	return cmd
}

func envServeCmd() *cobra.Command {
	var (
		projectPath string
		suitePath   string
		splitPath   string
		side        string
		poolSize    int
		listen      string
		freezeTime  string
		profile     string
	)
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve graded episodes over HTTP for a training or evaluation loop",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rewardProfile := gjeval.RewardProfile(strings.TrimSpace(profile))
			if err := rewardProfile.Validate(); err != nil {
				return err
			}
			if trimmed := strings.TrimSpace(projectPath); trimmed != "" {
				absolute, err := filepath.Abs(trimmed)
				if err != nil {
					return err
				}
				cpath = absolute
			}
			resolved, err := resolveDemoPath(strings.TrimSpace(projectPath) != "", os.Stderr)
			if err != nil {
				return err
			}
			suite, err := gjeval.LoadSuite(suitePath)
			if err != nil {
				return fmt.Errorf("load suite: %w", err)
			}
			server := &envServer{
				suite: *suite, profile: rewardProfile, side: strings.TrimSpace(side),
				byID: map[string]gjeval.Task{}, bySlug: map[string]gjeval.Task{},
			}
			if strings.TrimSpace(splitPath) != "" {
				split, err := gjeval.LoadSplit(splitPath)
				if err != nil {
					return fmt.Errorf("load split: %w", err)
				}
				server.split = split
			}
			if err := server.indexTasks(); err != nil {
				return err
			}

			writable, reactive, resettable := evalSuiteEnvironmentRequirements(*suite)
			spec := gjeval.EnvSpec{
				Target: gjeval.TargetDemo, ConfigPath: resolved, Seed: suite.Generator.Seed,
				Writable: writable, Reactive: reactive, Resettable: resettable,
				FreezeTime: freezeTime,
			}
			pool, err := newEvalInstancePool(cmd.Context(), evalEnvironment{StatusOut: os.Stderr}, spec, poolSize)
			if err != nil {
				return evalEnvironmentError(err)
			}
			defer pool.Close() //nolint:errcheck
			server.pool = pool

			mux := http.NewServeMux()
			mux.HandleFunc("/health", server.handleHealth)
			mux.HandleFunc("/tasks", server.handleTasks)
			mux.HandleFunc("/episodes", server.handleEpisode)

			httpServer := &http.Server{Addr: listen, Handler: mux, ReadHeaderTimeout: 15 * time.Second}
			go func() {
				<-cmd.Context().Done()
				shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				_ = httpServer.Shutdown(shutdown)
			}()
			fmt.Fprintf(cmd.OutOrStdout(),
				"GraphJin environment on %s — %d worlds, %d tasks, reward %s/%s\n",
				listen, pool.Size(), len(server.byID), rewardProfile, gjeval.RewardVersion)
			if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&projectPath, "path", "", "project to serve (defaults to the built-in demo)")
	cmd.Flags().StringVar(&suitePath, "suite", "eval/suite.yml", "task suite to serve")
	cmd.Flags().StringVar(&splitPath, "split", "", "split manifest restricting which tasks are served")
	cmd.Flags().StringVar(&side, "side", "train", "which side of the split to serve: train or eval")
	cmd.Flags().IntVar(&poolSize, "pool", 2, "isolated worlds to run episodes against")
	cmd.Flags().StringVar(&listen, "listen", "127.0.0.1:8090", "address to serve on")
	cmd.Flags().StringVar(&freezeTime, "freeze-time", "", "run every episode against a fixed clock (RFC3339)")
	cmd.Flags().StringVar(&profile, "reward-profile", string(gjeval.RewardProfileRL), "reward profile episodes are graded under")
	return cmd
}

// indexTasks selects the tasks this server will serve.
//
// Serving a split's training side and measuring on its eval side is the whole
// point of recording one, so the side is chosen here rather than left to the
// caller to filter correctly.
func (s *envServer) indexTasks() error {
	for _, task := range s.suite.Tasks {
		if s.split != nil {
			inTrain := s.split.Contains(s.split.Train, task.ID)
			if (s.side == "train") != inTrain {
				continue
			}
		}
		s.byID[task.ID] = task
		if task.Slug != "" {
			s.bySlug[task.Slug] = task
		}
	}
	if len(s.byID) == 0 {
		return fmt.Errorf("no tasks to serve (suite has %d, side %q)", len(s.suite.Tasks), s.side)
	}
	return nil
}

func (s *envServer) writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (s *envServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		return
	}
	dataset := gjeval.DatasetFingerprint{}
	if len(s.pool.instances) != 0 {
		dataset = s.pool.instances[0].Fingerprint()
	}
	s.writeJSON(w, http.StatusOK, envHealthResponse{
		Status: "ready", Workers: s.pool.Size(), Tasks: len(s.byID), Dataset: dataset,
		RewardVersion: gjeval.RewardVersion, RewardProfile: string(s.profile), Suite: s.suite.Generator,
	})
}

func (s *envServer) handleTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		return
	}
	out := make([]envTaskSummary, 0, len(s.byID))
	for _, task := range s.suite.Tasks {
		if _, ok := s.byID[task.ID]; !ok {
			continue
		}
		out = append(out, envTaskSummary{
			TaskID: task.ID, Slug: task.Slug, Category: string(task.Category),
			Difficulty: string(task.Difficulty), Prompt: task.Prompt,
			Writes: task.Mutation != nil, Family: task.Provenance.Source,
		})
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"tasks": out, "count": len(out)})
}

func (s *envServer) handleEpisode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		s.writeJSON(w, http.StatusBadRequest, map[string]any{"error": "unreadable_body", "message": err.Error()})
		return
	}
	var request envEpisodeRequest
	if len(body) != 0 {
		if err := json.Unmarshal(body, &request); err != nil {
			s.writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_body", "message": err.Error()})
			return
		}
	}
	task, ok := s.resolveTask(request)
	if !ok {
		s.writeJSON(w, http.StatusNotFound, map[string]any{
			"error": "unknown_task", "message": "No served task matches that id or slug.",
		})
		return
	}
	profile := s.profile
	if requested := strings.TrimSpace(request.Profile); requested != "" {
		profile = gjeval.RewardProfile(requested)
		if err := profile.Validate(); err != nil {
			s.writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_reward_profile", "message": err.Error()})
			return
		}
	}

	instance, err := s.pool.Acquire(r.Context())
	if err != nil {
		s.writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "no_world_available", "message": err.Error()})
		return
	}
	defer func() { _ = s.pool.Release(instance) }()

	episode, err := s.runner.RunEpisode(r.Context(), instance, task, gjeval.EpisodeOptions{
		Profile: profile, Repeat: request.Repeat,
	})
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "episode_failed", "message": err.Error()})
		return
	}

	response := envEpisodeResponse{
		TaskID: task.ID, Slug: task.Slug, Pass: episode.Score.Pass,
		Reward: episode.Score.Vector.Reward, Score: episode.Score, LatencyMS: episode.LatencyMS,
	}
	response.Status, response.Answer = episodeStatusAndAnswer(episode.Response)
	if request.IncludeResponse {
		response.Response = episode.Response
	}
	if request.IncludeTrajectory {
		trajectory, err := gjeval.BuildTrajectory(episode, gjeval.TrajectoryOptions{Stage: "executor", Profile: profile})
		if err == nil {
			response.Trajectory = &trajectory
		}
	}
	s.writeJSON(w, http.StatusOK, response)
}

// episodeStatusAndAnswer reads the agent's verdict from an episode.
//
// A live episode carries the typed response; one loaded back from disk carries
// the same thing decoded into a map. Both have to work, or the server silently
// returns empty answers for runs it just executed.
func episodeStatusAndAnswer(raw any) (string, string) {
	switch response := raw.(type) {
	case gjagent.Response:
		return response.Status, response.Answer
	case *gjagent.Response:
		if response != nil {
			return response.Status, response.Answer
		}
	case map[string]any:
		status, _ := response["status"].(string)
		answer, _ := response["answer"].(string)
		return status, answer
	}
	return "", ""
}

func (s *envServer) resolveTask(request envEpisodeRequest) (gjeval.Task, bool) {
	if id := strings.TrimSpace(request.TaskID); id != "" {
		task, ok := s.byID[id]
		return task, ok
	}
	if slug := strings.TrimSpace(request.Slug); slug != "" {
		task, ok := s.bySlug[slug]
		return task, ok
	}
	// With nothing named, serve the first task in suite order. That keeps a
	// smoke check to one request without inventing a sampling policy the caller
	// did not ask for.
	for _, task := range s.suite.Tasks {
		if _, ok := s.byID[task.ID]; ok {
			return task, true
		}
	}
	return gjeval.Task{}, false
}
