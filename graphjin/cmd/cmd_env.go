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
	// Stage selects which of the run's three policies the trajectory is built
	// for. They have different prompts and different jobs, so mixing them into
	// one training set teaches none of them.
	Stage string `json:"stage,omitempty"`
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
	// TrajectoryError says why a requested trajectory is missing. Swallowing
	// it left a caller with an empty field and no way to tell a build failure
	// apart from an episode that legitimately produced no steps.
	TrajectoryError string `json:"trajectory_error,omitempty"`
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
	// How this server was configured, reported by /health so a caller can tell
	// what it is talking to without having seen the command line.
	suiteSource  string
	splitLabel   string
	catalogMatch *bool
	// Per-world state for the ways of driving an episode that need something
	// inside the world. A world serves one episode at a time, so the world is
	// the only identifier either of these needs.
	mailboxes map[gjeval.Instance]*stepMailbox
	recorders map[gjeval.Instance]*mcpToolRecorder
}

func (s *envServer) mailboxFor(instance gjeval.Instance) *stepMailbox {
	return s.mailboxes[instance]
}

func (s *envServer) recorderFor(instance gjeval.Instance) *mcpToolRecorder {
	return s.recorders[instance]
}

func envCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "env",
		Short:        "Serve a GraphJin project as a graded agent environment",
		SilenceUsage: true,
	}
	cmd.AddCommand(envServeCmd())
	cmd.AddCommand(envNewWorldCmd())
	cmd.AddCommand(envCloneCmd())
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
		dataAnchor  string
		allowDrift  bool
		profile     string

		supportFlags    generatorFlags
		step            bool
		stepTimeout     time.Duration
		external        bool
		externalTimeout time.Duration
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
			if err := validateEnvAnchor(freezeTime, dataAnchor); err != nil {
				return err
			}
			resolved, err := resolveDemoPath(strings.TrimSpace(projectPath) != "", os.Stderr)
			if err != nil {
				return err
			}
			suite, suiteSource, err := resolveEnvSuite(suitePath)
			if err != nil {
				return err
			}
			split, splitLabel, err := resolveEnvSplit(splitPath, suite)
			if err != nil {
				return err
			}
			server, err := newEnvServer(suite, rewardProfile, side, split, freezeTime)
			if err != nil {
				return err
			}

			writable, reactive, resettable := evalSuiteEnvironmentRequirements(suite)
			spec := gjeval.EnvSpec{
				Target: gjeval.TargetDemo, ConfigPath: resolved, Seed: suite.Generator.Seed,
				Writable: writable, Reactive: reactive, Resettable: resettable,
				FreezeTime: freezeTime, PinDataAnchor: dataAnchor,
			}
			wiring, err := newEvalServeWiring(cmd, poolSize, evalServeOptions{
				Support: supportFlags, Step: step, External: external,
			})
			if err != nil {
				return err
			}
			pool, err := newEvalInstancePool(cmd.Context(), wiring.envFor, spec, poolSize)
			if err != nil {
				return evalEnvironmentError(err)
			}
			defer pool.Close() //nolint:errcheck
			if err := assertSuiteMatchesWorld(suite, pool.instances[0].Fingerprint(), allowDrift); err != nil {
				return err
			}
			server.catalogMatch = catalogAgreement(suite, pool.instances[0].Fingerprint())
			server.suiteSource = suiteSource
			server.splitLabel = splitLabel
			server.pool = pool
			server.mailboxes = wiring.attachMailboxes(pool)
			server.recorders = wiring.attachRecorders(pool)

			mux := http.NewServeMux()
			mux.HandleFunc("/health", server.handleHealth)
			mux.HandleFunc("/tasks", server.handleTasks)
			mux.HandleFunc("/episodes", server.handleEpisode)
			// The extra surfaces exist only when asked for. A trainer that did not
			// ask for them should not find endpoints it can drive into a state the
			// ordinary path never reaches.
			if step {
				steps := newStepServer(server, stepTimeout)
				steps.register(mux)
				go steps.reapUntil(cmd.Context())
			}
			if external {
				harness := newExternalServer(server, externalTimeout)
				harness.register(mux)
				go harness.reapUntil(cmd.Context())
			}

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
			// One line saying what was actually resolved. A container is configured
			// from a distance, so the log is where an operator finds out whether the
			// suite, the holdout and the anchor are the ones they meant.
			fmt.Fprintf(cmd.OutOrStdout(),
				"  suite %s (%s) · split %s · side %s · anchor %s\n",
				suiteSource, suite.Generator.Version, splitLabel, server.side,
				orUnset(pool.instances[0].Fingerprint().DataAnchor))
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
	cmd.Flags().StringVar(&dataAnchor, "data-anchor", "",
		"pin the demo's seeded data to a day (YYYY-MM-DD) so the world is the same on any date")
	cmd.Flags().BoolVar(&allowDrift, "allow-catalog-drift", false,
		"serve a suite whose oracles were verified against a different catalog")
	cmd.Flags().StringVar(&profile, "reward-profile", string(gjeval.RewardProfileRL), "reward profile episodes are graded under")
	cmd.Flags().BoolVar(&step, "step", false, "let a trainer supply each model completion instead of calling out to a provider")
	cmd.Flags().DurationVar(&stepTimeout, "step-timeout", 5*time.Minute, "how long a step-driven episode may sit idle before its world is reclaimed")
	cmd.Flags().BoolVar(&external, "external", false, "let an external agent drive episodes over MCP and submit an answer to be graded")
	cmd.Flags().DurationVar(&externalTimeout, "external-timeout", 10*time.Minute, "how long an external episode may run before its world is reclaimed")
	addSupportFlags(cmd, &supportFlags)
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
		stage, err := episodeTrajectoryStage(request.Stage)
		if err != nil {
			s.writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": "invalid_stage", "message": err.Error()})
			return
		}
		trajectory, err := gjeval.BuildTrajectory(episode, gjeval.TrajectoryOptions{Stage: stage, Profile: profile})
		if err != nil {
			response.TrajectoryError = err.Error()
		} else {
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

// envNewWorldCmd writes a new generated organization to disk.
func envNewWorldCmd() *cobra.Command {
	var (
		domain      string
		seed        int64
		tables      int
		pathologies []string
		out         string
		describe    string
		packPath    string
		yes         bool
		genFlags    generatorFlags
	)
	cmd := &cobra.Command{
		Use:   "new-world",
		Short: "Generate a fresh organization to train or measure against",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			pack, envelope, err := resolveWorldPack(cmd, worldPackRequest{
				Describe: describe, PackPath: packPath, Domain: domain,
				DomainSet: cmd.Flags().Changed("domain"), Tables: tables,
				Yes: yes, Generator: genFlags,
			})
			if err != nil {
				return err
			}
			applied, err := validatePathologies(pathologies)
			if err != nil {
				return err
			}
			if strings.TrimSpace(out) == "" {
				out = fmt.Sprintf("./world-%s-%d", pack.Name, seed)
			}
			world := buildWorld(pack, seed, tables, applied, "")
			if envelope != nil {
				// A described world is reproduced from its description, never from
				// a domain name that was invented for it and matches no built-in
				// vocabulary. Saying so in the world's own files is the difference
				// between reproducible and nearly reproducible.
				world.PackRef = worldPackFilename
			}
			if err := writeWorld(world, out); err != nil {
				return err
			}
			if envelope != nil {
				if err := writeWorldPackFile(out, worldPackFilename, *envelope); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Saved the description to %s; rebuild with --pack.\n",
					filepath.Join(out, worldPackFilename))
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Wrote %s: %d tables, domain %s, seed %d.\n",
				out, len(world.Tables), world.Domain, world.Seed)
			if len(applied) != 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Deliberate pathologies: %s\n", strings.Join(applied, ", "))
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"Next: graphjin eval create --path %s --writable --scale 300 --composition coverage\n", out)
			return nil
		},
	}
	names := make([]string, 0, len(domainPacks))
	for _, pack := range domainPacks {
		names = append(names, pack.Name)
	}
	cmd.Flags().StringVar(&domain, "domain", domainPacks[0].Name, "vocabulary to build the world from: "+strings.Join(names, ", "))
	cmd.Flags().Int64Var(&seed, "seed", 1, "seed; the same seed is the same company every time")
	cmd.Flags().IntVar(&tables, "tables", 0, "how many tables to include (0 uses the whole domain)")
	cmd.Flags().StringSliceVar(&pathologies, "pathologies", nil,
		"schema awkwardness to build in: "+strings.Join(supportedPathologies, ", "))
	cmd.Flags().StringVar(&out, "out", "", "directory to write the world to")
	cmd.Flags().StringVar(&describe, "describe", "", "describe the organization in words and let a model name its records")
	cmd.Flags().StringVar(&packPath, "pack", "", "rebuild from a saved world-pack.json instead of describing it again")
	cmd.Flags().BoolVar(&yes, "yes", false, "do not prompt before spending on the describing model")
	addGeneratorFlags(cmd, &genFlags)
	return cmd
}

// worldPackFilename is where a described world keeps its description.
const worldPackFilename = "world-pack.json"

type worldPackRequest struct {
	Describe  string
	PackPath  string
	Domain    string
	DomainSet bool
	Tables    int
	Yes       bool
	Generator generatorFlags
}

// resolveWorldPack decides which vocabulary a world is built from.
//
// A built-in domain costs nothing and is what most people want. A description
// costs one model call and produces a vocabulary nobody shipped. A saved
// description costs nothing again, which is the point of saving it: the world
// is reproducible from the artifact rather than from the model that wrote it.
//
// The second return is non-nil only when there is a description worth saving.
func resolveWorldPack(cmd *cobra.Command, request worldPackRequest) (domainPack, *worldPackEnvelope, error) {
	described := strings.TrimSpace(request.Describe)
	packPath := strings.TrimSpace(request.PackPath)
	switch {
	case described != "" && packPath != "":
		return domainPack{}, nil, fmt.Errorf("--describe writes a description and --pack reads one; use one or the other")
	case described != "" && request.DomainSet:
		return domainPack{}, nil, fmt.Errorf("--describe names its own domain; drop --domain")
	case packPath != "" && request.DomainSet:
		return domainPack{}, nil, fmt.Errorf("--pack carries its own domain; drop --domain")
	}

	if packPath != "" {
		envelope, err := loadWorldPack(packPath)
		if err != nil {
			return domainPack{}, nil, err
		}
		pack, err := validateWorldPack(envelope.Pack)
		if err != nil {
			return domainPack{}, nil, fmt.Errorf("%s: %w", packPath, err)
		}
		return pack, &envelope, nil
	}

	if described == "" {
		pack, err := packByName(request.Domain)
		return pack, nil, err
	}

	generatorConfig, label, err := resolveGeneratorConfig(request.Generator)
	if err != nil {
		return domainPack{}, nil, err
	}
	if err := approveProviderTraffic(cmd, request.Yes,
		fmt.Sprintf("1 world description call to %s", label)); err != nil {
		return domainPack{}, nil, err
	}
	maxTables := request.Tables
	if maxTables <= 0 {
		maxTables = 6
	}
	fields, err := gjagent.OneShot(cmd.Context(), generatorConfig, worldPackSignature, map[string]any{
		"description": described, "max_tables": fmt.Sprint(maxTables),
	})
	if err != nil {
		return domainPack{}, nil, err
	}
	var file worldPackFile
	if err := gjeval.DecodeFencedJSON(gjagent.StringField(fields, "pack_json"), &file); err != nil {
		return domainPack{}, nil, fmt.Errorf("the described world could not be read: %w", err)
	}
	// Validation happens before anything is written, so a world that would not
	// have worked leaves no directory behind to clean up or mistake for one that
	// does.
	pack, err := validateWorldPack(file)
	if err != nil {
		return domainPack{}, nil, fmt.Errorf("the described world was refused: %w", err)
	}
	return pack, &worldPackEnvelope{Described: described, AuthoredBy: label, Pack: file}, nil
}

// envCloneCmd learns a running server's schema and writes a local environment.
func envCloneCmd() *cobra.Command {
	var opts cloneOptions
	cmd := &cobra.Command{
		Use:   "clone",
		Short: "Learn a running server's schema and write a local synthetic environment",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out, err := runClone(cmd.Context(), opts, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"Wrote %s. Every row in it is synthetic; no data was read from the source.\n", out)
			fmt.Fprintf(cmd.OutOrStdout(),
				"Next: graphjin eval create --demo --path %s --writable --composition coverage\n", out)
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.URL, "url", "", "server to learn from (default: the one from `graphjin cli setup`)")
	cmd.Flags().StringVar(&opts.Out, "out", "./clone", "directory to write the environment to")
	cmd.Flags().IntVar(&opts.Rows, "rows", 12, "synthetic rows to generate per table")
	cmd.Flags().Int64Var(&opts.Seed, "seed", 1, "seed; the same seed produces the same synthetic data")
	cmd.Flags().StringVar(&opts.TokenEnv, "token-env", "GRAPHJIN_EVAL_TOKEN", "environment variable holding the bearer token")
	return cmd
}

// newEnvServer builds the served environment.
//
// Three things it gets right that the inline construction did not. The side is
// validated rather than compared to a literal, so a typo no longer silently
// serves the held-out tasks — the one mistake this flag exists to prevent. The
// runner is given the frozen clock, because freezing the data without freezing
// what the oracle calls "today" leaves date-relative questions drifting against
// fixed rows, which is the drift the environment already freezes to avoid. And
// the split is loaded here, where refusing it is a startup error rather than a
// surprise on the first request.
func newEnvServer(suite gjeval.Suite, profile gjeval.RewardProfile,
	side string, split *gjeval.SuiteSplit, freezeTime string) (*envServer, error) {
	side = strings.ToLower(strings.TrimSpace(side))
	if side != "train" && side != "eval" {
		return nil, fmt.Errorf("--side must be train or eval, got %q", side)
	}
	frozen, err := evalFrozenClockFromString(freezeTime)
	if err != nil {
		return nil, err
	}
	server := &envServer{
		suite: suite, profile: profile, side: side,
		byID: map[string]gjeval.Task{}, bySlug: map[string]gjeval.Task{},
		runner: gjeval.Runner{Now: frozen},
	}
	server.split = split
	if err := server.indexTasks(); err != nil {
		return nil, err
	}
	return server, nil
}

// episodeTrajectoryStage resolves which stage a requested trajectory covers.
//
// Executor by default, because that is the stage a policy is normally trained
// on. "all" is spelled out rather than left as the empty string so asking for
// every stage is a deliberate act — a corpus mixing three policies is a
// reasonable thing to want and an unfortunate thing to get by accident.
func episodeTrajectoryStage(requested string) (string, error) {
	switch stage := strings.ToLower(strings.TrimSpace(requested)); stage {
	case "":
		return gjagent.StageExecutor, nil
	case "all":
		return "", nil
	case gjagent.StageExecutor, gjagent.StageDistiller, gjagent.StageResponder:
		return stage, nil
	default:
		return "", fmt.Errorf("stage must be executor, distiller, responder or all, got %q", requested)
	}
}

// validateEnvAnchor refuses the one combination that reintroduces the drift
// both settings exist to remove.
//
// --freeze-time already implies the data anchor (EffectiveDataAnchor takes the
// frozen day when no anchor is pinned), so naming a different day for each
// asks for the questions to be frozen on one date and the rows on another —
// which is exactly the mismatch that makes a relative-window task ask about a
// window its data does not cover.
func validateEnvAnchor(freezeTime, dataAnchor string) error {
	anchor := strings.TrimSpace(dataAnchor)
	if anchor == "" {
		return nil
	}
	if _, err := time.Parse(demoDataAnchorLayout, anchor); err != nil {
		return fmt.Errorf("--data-anchor must be a day as YYYY-MM-DD, got %q", dataAnchor)
	}
	frozen, ok, err := (gjeval.EnvSpec{FreezeTime: freezeTime}).FrozenTime()
	if err != nil {
		return err
	}
	if ok {
		if day := frozen.UTC().Format(demoDataAnchorLayout); day != anchor {
			return fmt.Errorf(
				"--freeze-time is %s but --data-anchor is %s; the questions would be asked on one day "+
					"and the rows dated for another", day, anchor)
		}
	}
	return nil
}

// orUnset renders an empty value as something a reader can tell apart from a
// missing field.
func orUnset(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unset"
	}
	return value
}
