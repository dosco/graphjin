package eval

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	mathrand "math/rand"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
)

type RunOptions struct {
	Mode                 RunMode
	Intent               RunIntent
	Repeats              int
	Seed                 int64
	RunID                string
	Provenance           RunProvenance
	Baseline             *Report
	Store                *Store
	AutoBaseline         bool
	DeliberatePromotion  bool
	ResumePolicy         ResumePolicy
	ResumeRunID          string
	BinaryFingerprint    string
	InvocationArgs       []string
	MaxTransientAttempts int
	// Pool, when set, leases one environment per episode instead of sharing a
	// single instance. Every worker must serve the same dataset, so the pool
	// itself is responsible for refusing to hand out mismatched worlds.
	//
	// With a pool a write no longer has to stop the whole run: it owns the
	// world it was leased and resets only that one. Without a pool the runner
	// keeps its historical behavior, including the exclusive gate.
	Pool InstancePool
	// Concurrency is the number of episode slots executed at once. Zero and
	// one mean the historical serial order, byte-for-byte. Above one,
	// read-only episodes run in parallel while any episode that mutates the
	// demo database (task.Mutation != nil — it resets state before and after)
	// holds the instance exclusively. Per-episode latency is still measured
	// per episode, but percentiles observed under load are not comparable to
	// serial rows, so the value is recorded in run provenance.
	Concurrency int
}

type Runner struct {
	Client     HTTPDoer
	Now        func() time.Time
	RetryDelay time.Duration
}

var ErrRunInterrupted = errors.New("evaluation interrupted")

type PreparedRun struct {
	runner   Runner
	suite    Suite
	instance Instance
	opts     RunOptions
	client   HTTPDoer
	oracles  map[string]OracleResult
	report   *Report
	manifest RunManifest
	preview  TrafficPreview
	existing []Episode
	lock     *RunLock
	invalid  bool
	closed   bool

	// mu guards the shared accounting a slot performs — manifest counters and
	// usage sums, report episode paths, and manifest/episode/attempt persists.
	// It is never held across agent traffic or retry sleeps.
	mu sync.Mutex
	// slotGate serializes database occupancy: read-only episodes hold it
	// shared, and any episode that resets the instance (task.Mutation != nil)
	// holds it exclusively — a concurrent reader would otherwise observe a
	// half-prepared or half-reset world and fail in ways indistinguishable
	// from the model being wrong.
	slotGate sync.RWMutex
}

func (r Runner) Run(ctx context.Context, suite Suite, instance Instance, opts RunOptions) (*Report, error) {
	prepared, err := r.Prepare(ctx, suite, instance, opts)
	if err != nil {
		return nil, err
	}
	defer prepared.Close() //nolint:errcheck
	return prepared.Execute(ctx)
}

// Prepare performs validation, oracle resolution, compatibility checks, and
// resume loading without sending provider-backed agent traffic.
func (r Runner) Prepare(ctx context.Context, suite Suite, instance Instance, opts RunOptions) (*PreparedRun, error) {
	if instance == nil {
		return nil, fmt.Errorf("nil evaluation instance")
	}
	if err := suite.Validate(); err != nil {
		return nil, err
	}
	if suiteNeedsReset(suite) {
		if _, ok := instance.(ResettableInstance); !ok {
			return nil, errors.New("suite contains mutation or reactive tasks but the evaluation instance is not resettable")
		}
	}
	if opts.Repeats <= 0 {
		opts.Repeats = DefaultRepeats
	}
	if opts.Mode == "" {
		opts.Mode = RunModeRun
	}
	if opts.Intent == "" {
		opts.Intent = RunIntentRun
		if opts.Mode == RunModeBenchmark {
			opts.Intent = RunIntentBench
		}
	}
	if opts.ResumePolicy == "" {
		opts.ResumePolicy = ResumeAuto
	}
	if opts.RunID != "" && opts.ResumePolicy == ResumeAuto {
		opts.ResumePolicy = ResumeFresh
	}
	if opts.MaxTransientAttempts <= 0 {
		opts.MaxTransientAttempts = 2
	}
	if opts.Concurrency < 1 {
		opts.Concurrency = 1
	}
	if opts.Concurrency > 16 {
		opts.Concurrency = 16
	}
	if opts.Concurrency > 1 {
		opts.Provenance.Concurrency = opts.Concurrency
	}
	opts.Provenance.Seed = opts.Seed
	opts.Provenance.Repeats = opts.Repeats
	opts.Provenance.Target = instance.Label()
	opts.Provenance.BinaryFingerprint = opts.BinaryFingerprint
	client := r.Client
	if client == nil {
		// One agent request covers a whole multi-step run, so the harness must
		// outlast the agent's own deadline rather than cutting it short. A
		// reasoning-mode model spends far longer per step, and a fixed 90s
		// ceiling turned every DeepSeek thinking episode into provider_timeout
		// — indistinguishable from a model that cannot answer.
		timeout := 90 * time.Second
		if configured := opts.Provenance.TimeoutSeconds; configured > 0 {
			timeout = time.Duration(configured)*time.Second + 30*time.Second
		}
		client = &http.Client{Timeout: timeout}
	}
	requestedRunID := opts.RunID
	if requestedRunID == "" {
		requestedRunID = newRunID(r.now())
	}
	report := &Report{
		SchemaVersion:          ReportSchemaVersion,
		UsageAccountingVersion: UsageAccountingVersion,
		RewardVersion:          RewardVersion,
		RunID:                  requestedRunID,
		RunStatus:              RunStatusRunning,
		Mode:                   opts.Mode,
		GeneratedAt:            r.now(),
		SuiteFingerprint:       SuiteFingerprint(suite),
		CatalogFingerprint:     suite.CatalogFingerprint,
		DatasetFingerprint:     instance.Fingerprint(),
		Provenance:             opts.Provenance,
		ProviderUsage:          ProviderUsage{Complete: true},
		Acceptance:             Acceptance{SuiteValid: true},
	}

	verifier := Verifier{Client: client, Now: r.Now, BaseURL: instance.BaseURL(), Headers: instance.Headers()}
	oracles := make(map[string]OracleResult, len(suite.Tasks))
	invalid := make(map[string]string)
	invalidDetails := make(map[string]string)
	for _, task := range suite.Tasks {
		if task.Oracle == nil || task.Mutation != nil {
			continue
		}
		resolved, err := verifier.Resolve(ctx, *task.Oracle)
		if err != nil {
			invalid[task.ID] = oracleFailureCategory(err)
			invalidDetails[task.ID] = err.Error()
			continue
		}
		oracles[task.ID] = resolved
	}
	if len(invalid) != 0 {
		report.InvalidOracles = invalid
		report.InvalidOracleDetails = invalidDetails
		report.Acceptance.SuiteValid = false
		report.Acceptance.HardPass = false
		report.Acceptance.NoRegression = false
		report.Acceptance.SafetyPass = true
		report.Acceptance.Notices = append(report.Acceptance.Notices, "suite invalid: one or more hidden oracles failed before agent traffic")
		report.RunStatus = RunStatusComplete
		return &PreparedRun{runner: r, suite: suite, instance: instance, opts: opts, client: client, oracles: oracles, report: report, invalid: true}, nil
	}
	report.OracleValueHash = oracleValueHash(report.SuiteFingerprint, oracles)

	baselineRunID := ""
	if opts.Baseline != nil {
		baselineRunID = opts.Baseline.RunID
	}
	now := r.now()
	want := RunManifest{
		SchemaVersion: RunManifestVersion, RunID: requestedRunID, Intent: opts.Intent, Mode: opts.Mode,
		Status: RunStatusRunning, StartedAt: now, UpdatedAt: now,
		SuiteFingerprint: report.SuiteFingerprint, CatalogFingerprint: suite.CatalogFingerprint,
		OracleValueHash: report.OracleValueHash, DatasetFingerprint: instance.Fingerprint(),
		TaskSchemaVersion: TaskSchemaVersion, EpisodeSchemaVersion: EpisodeSchemaVersion,
		ReportSchemaVersion: ReportSchemaVersion, RewardVersion: RewardVersion, GeneratorVersion: suite.Generator.Version,
		BinaryFingerprint: opts.BinaryFingerprint, ServerEvalFingerprint: opts.Provenance.ServerFingerprint,
		Provenance: opts.Provenance, BaselineRunID: baselineRunID, BaselineFingerprint: BaselineFingerprint(opts.Baseline),
		AutoBaseline: opts.AutoBaseline, DeliberatePromotion: opts.DeliberatePromotion,
		Progress: RunProgress{PlannedInitialSlots: len(suite.Tasks) * opts.Repeats}, ProviderUsage: ProviderUsage{Complete: true}, InvocationArgs: append([]string(nil), opts.InvocationArgs...),
	}
	var existing []Episode
	var existingAttempts []Attempt
	var runLock *RunLock
	resuming := false
	previewIgnored := 0
	if opts.Store != nil {
		found, ignored, err := opts.Store.FindRun(want, opts.ResumePolicy, opts.ResumeRunID)
		if err != nil {
			return nil, err
		}
		if found != nil {
			want = *found
			want.ResumeCount++
			want.Status = RunStatusRunning
			want.UpdatedAt = now
			resuming = true
		}
		previewIgnored = len(ignored)
		runLock, err = opts.Store.LockRun(ctx, want.RunID)
		if err != nil {
			return nil, err
		}
		if resuming {
			existing, err = opts.Store.LoadEpisodes(want.RunID)
			if err != nil {
				_ = runLock.Close()
				return nil, err
			}
			if err := validateResumedEpisodes(existing, suite, instance, opts, oracles, want.RunID); err != nil {
				_ = runLock.Close()
				return nil, err
			}
			existingAttempts, err = opts.Store.LoadAttempts(want.RunID)
			if err != nil {
				_ = runLock.Close()
				return nil, err
			}
			if err := validateResumedAttempts(existingAttempts, suite, opts, want.RunID); err != nil {
				_ = runLock.Close()
				return nil, err
			}
			rebuildRunAccounting(&want, existing, existingAttempts)
		}
	}
	opts.RunID = want.RunID
	report.RunID = want.RunID
	if want.Provenance.Concurrency > report.Provenance.Concurrency {
		// A run scheduled concurrently at any point keeps that on its record: the
		// latency percentiles it already collected never become comparable with a
		// serial row, even when the remaining slots finish serially.
		report.Provenance.Concurrency = want.Provenance.Concurrency
	}
	want.Provenance.Concurrency = report.Provenance.Concurrency
	reusedInitial, reusedConfirmation := countEpisodeKinds(existing)
	want.Progress.ReusedEpisodeCount = len(existing)
	want.Progress.CompletedInitialSlots = reusedInitial
	want.Progress.CompletedConfirmation = reusedConfirmation
	possibleConfirmation := possibleConfirmationSlots(suite, opts, existing)
	preview := TrafficPreview{
		RunID: want.RunID, Resuming: resuming, ReusedEpisodes: len(existing),
		RemainingInitialSlots:     len(suite.Tasks)*opts.Repeats - reusedInitial,
		PossibleConfirmationSlots: possibleConfirmation,
		IgnoredIncompatibleRuns:   previewIgnored,
	}
	preview.MaximumProviderAttempts = (preview.RemainingInitialSlots + preview.PossibleConfirmationSlots) * opts.MaxTransientAttempts
	return &PreparedRun{runner: r, suite: suite, instance: instance, opts: opts, client: client, oracles: oracles, report: report, manifest: want, preview: preview, existing: existing, lock: runLock}, nil
}

func (p *PreparedRun) Preview() TrafficPreview { return p.preview }

func (p *PreparedRun) Close() error {
	if p == nil || p.closed {
		return nil
	}
	p.closed = true
	if p.lock != nil {
		return p.lock.Close()
	}
	return nil
}

func (p *PreparedRun) Execute(ctx context.Context) (*Report, error) {
	if p == nil {
		return nil, errors.New("nil prepared evaluation")
	}
	if p.invalid {
		if p.opts.Store != nil {
			if _, err := p.opts.Store.WriteReport(*p.report); err != nil {
				return nil, err
			}
		}
		return p.report, nil
	}
	if err := p.persistManifest(); err != nil {
		return nil, err
	}
	initial := make(map[string][]Episode, len(p.suite.Tasks))
	confirmation := make(map[string][]Episode, len(p.suite.Tasks))
	allEpisodes := append([]Episode(nil), p.existing...)
	for _, episode := range p.existing {
		if episode.Confirmation {
			confirmation[episode.TaskID] = append(confirmation[episode.TaskID], episode)
		} else {
			initial[episode.TaskID] = append(initial[episode.TaskID], episode)
		}
		p.appendEpisodePath(episode)
	}
	pending := make([]slotRequest, 0, len(p.suite.Tasks)*p.opts.Repeats)
	for _, task := range p.suite.Tasks {
		for rep := 1; rep <= p.opts.Repeats; rep++ {
			if episodeSlotPresent(initial[task.ID], rep) {
				continue
			}
			pending = append(pending, slotRequest{task: task, rep: rep})
		}
	}
	done, status, code, err := p.executeSlots(ctx, pending)
	if err != nil {
		return p.finishIncomplete(status, code, err)
	}
	if code != "" {
		return p.finishIncomplete(status, code, nil)
	}
	for _, episode := range done {
		initial[episode.TaskID] = append(initial[episode.TaskID], episode)
		allEpisodes = append(allEpisodes, episode)
	}

	baselineTasks := map[string]TaskVerdict{}
	if p.opts.Baseline != nil {
		baselineTasks = p.opts.Baseline.TaskMap()
	}
	confirmationTasks := make([]Task, 0)
	for _, task := range p.suite.Tasks {
		verdict := aggregateTask(task, initial[task.ID], nil)
		if prior, ok := baselineTasks[task.ID]; ok && prior.Pass && !verdict.Pass && verdict.SafetyPass {
			confirmationTasks = append(confirmationTasks, task)
		}
	}
	p.manifest.Progress.PlannedConfirmationSlots = len(confirmationTasks) * p.opts.Repeats
	if err := p.persistManifest(); err != nil {
		return nil, err
	}
	pendingConfirmation := make([]slotRequest, 0, len(confirmationTasks)*p.opts.Repeats)
	for _, task := range confirmationTasks {
		for rep := 1; rep <= p.opts.Repeats; rep++ {
			if episodeSlotPresent(confirmation[task.ID], rep) {
				continue
			}
			pendingConfirmation = append(pendingConfirmation, slotRequest{task: task, rep: rep, confirmation: true})
		}
	}
	confirmed, status, code, err := p.executeSlots(ctx, pendingConfirmation)
	if err != nil {
		return p.finishIncomplete(status, code, err)
	}
	if code != "" {
		return p.finishIncomplete(status, code, nil)
	}
	for _, episode := range confirmed {
		confirmation[episode.TaskID] = append(confirmation[episode.TaskID], episode)
		allEpisodes = append(allEpisodes, episode)
	}

	for _, task := range p.suite.Tasks {
		p.report.Tasks = append(p.report.Tasks, aggregateTask(task, initial[task.ID], confirmation[task.ID]))
	}
	p.report.Progress = p.manifest.Progress
	p.report.ProviderUsage = p.manifest.ProviderUsage
	p.report.Metrics = calculateMetrics(p.suite.Tasks, p.report.Tasks, allEpisodes, initial, p.opts.Seed)
	p.report.Acceptance = compareBaseline(*p.report, p.opts.Baseline)
	p.report.UsageComparison = compareUsage(*p.report, p.opts.Baseline)
	p.report.RunStatus = RunStatusComplete
	promote := (p.opts.AutoBaseline && p.opts.Baseline == nil || p.opts.DeliberatePromotion) && p.report.Acceptance.HardPass
	if promote {
		message := fmt.Sprintf("first safety-passing run promoted as baseline at recall %.3f", p.report.Metrics.Recall)
		if p.opts.DeliberatePromotion {
			message = fmt.Sprintf("deliberately promoted as baseline at recall %.3f", p.report.Metrics.Recall)
		}
		p.report.Acceptance.Notices = append(p.report.Acceptance.Notices, message)
	}
	if p.opts.Store != nil {
		if _, err := p.opts.Store.WriteReport(*p.report); err != nil {
			return nil, err
		}
	}
	p.manifest.Status = RunStatusComplete
	p.manifest.UpdatedAt = p.runner.now()
	p.manifest.LastEnvironmentCode = ""
	if err := p.persistManifest(); err != nil {
		return nil, err
	}
	if promote && p.opts.Store != nil {
		if err := p.opts.Store.PromoteBaseline(*p.report); err != nil {
			return nil, err
		}
	}
	return p.report, nil
}

// slotRequest is one planned episode: a task, a repeat number, and which
// phase it belongs to.
type slotRequest struct {
	task         Task
	rep          int
	confirmation bool
}

type slotResult struct {
	episode Episode
	code    string
	err     error
}

// executeSlots runs the pending slots and returns their episodes in pending
// order, so downstream aggregation sees the same sequence a serial run
// produces. At Concurrency 1 it is the historical loop verbatim. Above one it
// fans out over a bounded worker pool; the first failure (environment code or
// interruption) cancels the remaining work, drains in-flight slots, and is
// reported exactly as the serial loop would have reported it.
func (p *PreparedRun) executeSlots(ctx context.Context, pending []slotRequest) ([]Episode, RunStatus, string, error) {
	if len(pending) == 0 {
		return nil, "", "", nil
	}
	workers := p.opts.Concurrency
	if workers <= 1 || len(pending) == 1 {
		episodes := make([]Episode, 0, len(pending))
		for _, request := range pending {
			episode, code, err := p.executeSlot(ctx, request.task, request.rep, request.confirmation)
			if err != nil {
				return nil, RunStatusInterrupted, "interrupted", err
			}
			if code != "" {
				return nil, RunStatusEnvironmentFailed, code, nil
			}
			episodes = append(episodes, episode)
		}
		return episodes, "", "", nil
	}
	if workers > len(pending) {
		workers = len(pending)
	}

	poolCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan int)
	results := make([]slotResult, len(pending))
	var failure slotResult
	var failed bool
	var failureMu sync.Mutex
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				request := pending[index]
				episode, code, err := p.executeSlot(poolCtx, request.task, request.rep, request.confirmation)
				results[index] = slotResult{episode: episode, code: code, err: err}
				if err != nil || code != "" {
					failureMu.Lock()
					if !failed {
						failed = true
						failure = results[index]
					}
					failureMu.Unlock()
					cancel()
				}
			}
		}()
	}
	for index := range pending {
		select {
		case jobs <- index:
		case <-poolCtx.Done():
		}
		if poolCtx.Err() != nil {
			break
		}
	}
	close(jobs)
	wg.Wait()

	if failed {
		if failure.err != nil {
			return nil, RunStatusInterrupted, "interrupted", failure.err
		}
		return nil, RunStatusEnvironmentFailed, failure.code, nil
	}
	episodes := make([]Episode, 0, len(pending))
	for index := range pending {
		episodes = append(episodes, results[index].episode)
	}
	return episodes, "", "", nil
}

// resumeCommand formats the manifest's resume hint under the shared lock:
// concurrent slots keep updating its counters while another slot is building
// an interruption error out of it.
func (p *PreparedRun) resumeCommand() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.manifest.ResumeCommand()
}

func (p *PreparedRun) executeSlot(ctx context.Context, task Task, rep int, confirmation bool) (Episode, string, error) {
	for localAttempt := 1; localAttempt <= p.opts.MaxTransientAttempts; localAttempt++ {
		if err := ctx.Err(); err != nil {
			return Episode{}, "", fmt.Errorf("%w: %s", ErrRunInterrupted, p.resumeCommand())
		}
		episode, gateCode := func() (Episode, string) {
			// A pool hands each episode a world of its own, so a write only has
			// to own the instance it was leased. Against a single shared instance
			// a write must instead exclude every concurrent reader for the whole
			// episode, which is what the exclusive gate is for.
			pooled := p.opts.Pool != nil
			if task.Mutation != nil && !pooled {
				p.slotGate.Lock()
				defer p.slotGate.Unlock()
			} else {
				p.slotGate.RLock()
				defer p.slotGate.RUnlock()
			}
			instance := p.instance
			if pooled {
				leased, err := p.opts.Pool.Acquire(ctx)
				if err != nil {
					return Episode{}, "environment_unavailable"
				}
				defer func() { _ = p.opts.Pool.Release(leased) }()
				instance = leased
			}
			var resettable ResettableInstance
			var collateralBefore []OracleResult
			if task.Mutation != nil {
				var ok bool
				if resettable, ok = instance.(ResettableInstance); !ok {
					return Episode{}, "reset_failed"
				}
				if err := resettable.Reset(ctx); err != nil {
					return Episode{}, "reset_failed"
				}
				if err := prepareMutationEpisode(ctx, p.runner, p.client, instance, task.Mutation); err != nil {
					_ = resettable.Reset(ctx)
					return Episode{}, "setup_failed"
				}
				var err error
				collateralBefore, err = resolveMutationCollateral(ctx, p.runner, p.client, instance, task.Mutation.Collateral)
				if err != nil {
					_ = resettable.Reset(ctx)
					return Episode{}, "oracle_failed"
				}
			}
			episode := p.runner.runEpisode(ctx, p.client, instance, p.opts, task, rep, confirmation, p.oracles, collateralBefore)
			if resettable != nil {
				if err := resettable.Reset(ctx); err != nil {
					return Episode{}, "reset_failed"
				}
			}
			return episode, ""
		}()
		if gateCode != "" {
			return Episode{}, gateCode, nil
		}
		p.mu.Lock()
		p.manifest.Progress.ProviderAttempts++
		// Attempt numbers identify a record run-wide, so each slot keeps the number
		// its own increment produced. Re-reading the counter at persist time hands
		// two concurrent failures the same number, and the second one overwrites
		// the first on disk.
		attemptNumber := p.manifest.Progress.ProviderAttempts
		p.manifest.ProviderUsage.PromptTokens += episode.Score.Tokens.Prompt
		p.manifest.ProviderUsage.CompletionTokens += episode.Score.Tokens.Completion
		p.manifest.ProviderUsage.TotalTokens += episode.Score.Tokens.Total
		p.manifest.ProviderUsage.LLMCalls += episode.Score.Tokens.LLMCalls
		p.manifest.ProviderUsage.LatencyMS += episode.LatencyMS
		p.mu.Unlock()
		code, retryable := episodeEnvironment(episode)
		// Auth errors are classified fatal for callers, and for a dead key that is
		// right — but a benchmark run observes hundreds of successes around a single
		// stray 403, which providers emit while billing state propagates. Two runs
		// died one episode apart tonight on exactly that. Give auth one retry after
		// the long backoff: a genuinely bad key fails both attempts and still aborts.
		if code == gjagent.ErrorCodeProviderAuth {
			retryable = true
		}
		if ctx.Err() != nil {
			code, retryable = "interrupted", false
		}
		if providerUsageUnknown(code) {
			p.mu.Lock()
			p.manifest.ProviderUsage.Complete = false
			p.manifest.ProviderUsage.UnknownAttempts++
			p.mu.Unlock()
		}
		if code == "" {
			p.mu.Lock()
			if err := p.runner.persistEpisode(p.report, p.opts.Store, episode); err != nil {
				p.mu.Unlock()
				return Episode{}, "", err
			}
			if confirmation {
				p.manifest.Progress.CompletedConfirmation++
			} else {
				p.manifest.Progress.CompletedInitialSlots++
			}
			p.manifest.UpdatedAt = p.runner.now()
			if err := p.persistManifest(); err != nil {
				p.mu.Unlock()
				return Episode{}, "", err
			}
			p.mu.Unlock()
			return episode, "", nil
		}
		p.mu.Lock()
		if err := p.persistAttempt(episode, code, retryable, attemptNumber); err != nil {
			p.mu.Unlock()
			return Episode{}, "", err
		}
		p.manifest.LastEnvironmentCode = code
		p.manifest.UpdatedAt = p.runner.now()
		if err := p.persistManifest(); err != nil {
			p.mu.Unlock()
			return Episode{}, "", err
		}
		p.mu.Unlock()
		if code == "interrupted" {
			return Episode{}, "", fmt.Errorf("%w: %s", ErrRunInterrupted, p.resumeCommand())
		}
		if !retryable || localAttempt == p.opts.MaxTransientAttempts {
			return Episode{}, code, nil
		}
		p.mu.Lock()
		p.manifest.Progress.RetryCount++
		if err := p.persistManifest(); err != nil {
			p.mu.Unlock()
			return Episode{}, "", err
		}
		p.mu.Unlock()
		delay := retryDelayForCode(p.runner.RetryDelay, code)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Episode{}, "", fmt.Errorf("%w: %s", ErrRunInterrupted, p.resumeCommand())
		case <-timer.C:
		}
	}
	return Episode{}, gjagent.ErrorCodeProviderTransport, nil
}

// retryDelayForCode sizes the single transient retry to the failure it follows.
// Rate and quota limits are windows, not blips: they refill on the order of a
// minute, and the 2-second delay meant the one retry fired into the same closed
// window every time — one slot then failed twice within seconds and took the
// whole run down. Three flash runs on the frozen suite died exactly this way at
// 85, 131 and 60 episodes, each abandoning a half-finished, fully paid run that a
// one-minute wait would have carried through.
// An explicitly configured RetryDelay wins outright — tests and operators set it
// deliberately. The window-scale default applies only when nothing is configured.
// TransientEnvironmentCode reports whether a run-level halt is worth resuming.
// The transient set is the one docs/GRAPHJIN-EVAL.md already promises callers:
// timeouts, rate limits, transport faults, and provider 5xx are weather, and a
// run that waits them out finishes with every completed episode intact.
//
// Everything else is terminal by design. Quota and an unavailable model need a
// human. So does auth — the slot layer already grants it one extra attempt for
// the stray 403 providers emit while billing state propagates
// (see executeSlot), so a code that survives to a run-level halt is a real
// credential problem. reset/setup/oracle failures are local, not weather, and
// an interrupt is a deliberate stop that must never be undone by a retry loop.
//
// Deliberately not providerUsageUnknown: that set answers "were tokens
// counted", excludes rate limits, and would refuse to resume the single most
// common reason a long run stops.
func TransientEnvironmentCode(code string) bool {
	switch strings.TrimSpace(code) {
	case gjagent.ErrorCodeProviderTimeout,
		gjagent.ErrorCodeProviderRateLimit,
		gjagent.ErrorCodeProviderTransport,
		gjagent.ErrorCodeProviderServer:
		return true
	default:
		return false
	}
}

func retryDelayForCode(configured time.Duration, code string) time.Duration {
	if configured > 0 {
		return configured
	}
	switch code {
	case gjagent.ErrorCodeProviderRateLimit, gjagent.ErrorCodeProviderQuota, gjagent.ErrorCodeProviderAuth:
		return 75 * time.Second
	}
	return 2 * time.Second
}

func prepareMutationEpisode(ctx context.Context, runner Runner, client HTTPDoer, instance Instance, mutation *MutationSpec) error {
	if mutation == nil {
		return nil
	}
	for _, step := range mutation.Setup {
		if _, err := postGraphQL(ctx, client, instance.BaseURL(), instance.Headers(), step.Query, step.Variables); err != nil {
			return fmt.Errorf("mutation setup: %w", err)
		}
		if step.WaitAfterMS > 0 {
			timer := time.NewTimer(time.Duration(step.WaitAfterMS) * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	if mutation.ReadyState == nil {
		return nil
	}
	timeout := time.Duration(mutation.ReadyTimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	deadline := time.Now().Add(timeout)
	verifier := Verifier{Client: client, Now: runner.Now, BaseURL: instance.BaseURL(), Headers: instance.Headers()}
	for {
		result, err := verifier.Resolve(ctx, *mutation.ReadyState)
		if err == nil && result.Value == mutation.ReadyValue {
			return nil
		}
		if !time.Now().Before(deadline) {
			if err != nil {
				return fmt.Errorf("mutation readiness: %w", err)
			}
			return fmt.Errorf("mutation readiness was %q, want %q", result.Value, mutation.ReadyValue)
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (p *PreparedRun) persistAttempt(episode Episode, code string, retryable bool, number int) error {
	if p.opts.Store == nil {
		return nil
	}
	attempt := Attempt{
		RunID: episode.RunID, TaskID: episode.TaskID, TaskSlug: episode.TaskSlug,
		Repeat: episode.Repeat, Confirmation: episode.Confirmation,
		Attempt: number, StartedAt: episode.StartedAt, CompletedAt: p.runner.now(),
		HTTPStatus: episode.HTTPStatus, LatencyMS: episode.LatencyMS,
		ErrorCode: code, Retryable: retryable, Error: episode.Error, Response: episode.Response, Tokens: episode.Score.Tokens,
	}
	_, err := p.opts.Store.WriteAttempt(attempt)
	return err
}

func (p *PreparedRun) finishIncomplete(status RunStatus, code string, cause error) (*Report, error) {
	p.manifest.Status = status
	p.manifest.LastEnvironmentCode = code
	p.manifest.UpdatedAt = p.runner.now()
	p.report.RunStatus = status
	p.report.Progress = p.manifest.Progress
	p.report.ProviderUsage = p.manifest.ProviderUsage
	p.report.Acceptance = Acceptance{SuiteValid: true, SafetyPass: true, NoRegression: false, HardPass: false, EnvironmentFailure: status == RunStatusEnvironmentFailed}
	if status == RunStatusEnvironmentFailed {
		p.report.Metrics.EnvironmentErrors = 1
	}
	if err := p.persistManifest(); err != nil {
		return nil, err
	}
	if p.opts.Store != nil {
		notice := "evaluation interrupted; finalized quality metrics are unavailable"
		if status == RunStatusEnvironmentFailed {
			notice = "evaluation environment failed; finalized quality metrics are unavailable"
		}
		_, err := p.opts.Store.WritePartialReport(PartialReport{
			SchemaVersion: p.report.SchemaVersion, UsageAccountingVersion: p.report.UsageAccountingVersion, RewardVersion: p.report.RewardVersion,
			RunID: p.report.RunID, RunStatus: status, Mode: p.report.Mode, GeneratedAt: p.runner.now(),
			SuiteFingerprint: p.report.SuiteFingerprint, CatalogFingerprint: p.report.CatalogFingerprint,
			DatasetFingerprint: p.report.DatasetFingerprint, OracleValueHash: p.report.OracleValueHash,
			Provenance: p.report.Provenance, Progress: p.report.Progress, ProviderUsage: p.report.ProviderUsage,
			EnvironmentCode: code, Notice: notice,
		})
		if err != nil {
			return nil, err
		}
	}
	if cause != nil {
		return p.report, cause
	}
	return p.report, nil
}

func (p *PreparedRun) persistManifest() error {
	if p.opts.Store == nil {
		return nil
	}
	_, err := p.opts.Store.WriteManifest(p.manifest)
	return err
}

func (p *PreparedRun) appendEpisodePath(episode Episode) {
	if p.opts.Store == nil {
		return
	}
	p.report.EpisodePaths = append(p.report.EpisodePaths, filepath.Join(p.opts.Store.Root, "episodes", episode.RunID, episodeFilename(episode)))
}

func episodeSlotPresent(episodes []Episode, repeat int) bool {
	for _, episode := range episodes {
		if episode.Repeat == repeat {
			return true
		}
	}
	return false
}

func countEpisodeKinds(episodes []Episode) (initial, confirmation int) {
	for _, episode := range episodes {
		if episode.Confirmation {
			confirmation++
		} else {
			initial++
		}
	}
	return initial, confirmation
}

func possibleConfirmationSlots(suite Suite, opts RunOptions, existing []Episode) int {
	if opts.Baseline == nil {
		return 0
	}
	initial := map[string][]Episode{}
	confirmation := map[string][]Episode{}
	for _, episode := range existing {
		if episode.Confirmation {
			confirmation[episode.TaskID] = append(confirmation[episode.TaskID], episode)
		} else {
			initial[episode.TaskID] = append(initial[episode.TaskID], episode)
		}
	}
	baseline := opts.Baseline.TaskMap()
	total := 0
	for _, task := range suite.Tasks {
		prior, ok := baseline[task.ID]
		if !ok || !prior.Pass {
			continue
		}
		if len(initial[task.ID]) < opts.Repeats {
			total += opts.Repeats - len(confirmation[task.ID])
			continue
		}
		verdict := aggregateTask(task, initial[task.ID], nil)
		if !verdict.Pass && verdict.SafetyPass {
			total += opts.Repeats - len(confirmation[task.ID])
		}
	}
	return total
}

func validateResumedEpisodes(episodes []Episode, suite Suite, instance Instance, opts RunOptions, oracles map[string]OracleResult, runID string) error {
	tasks := make(map[string]Task, len(suite.Tasks))
	for _, task := range suite.Tasks {
		tasks[task.ID] = task
	}
	for _, episode := range episodes {
		task, ok := tasks[episode.TaskID]
		if !ok || episode.RunID != runID || episode.TaskSlug != task.Slug || episode.Repeat < 1 || episode.Repeat > opts.Repeats {
			return fmt.Errorf("resumed episode identity mismatch for slot %s", episodeSlotKey(episode))
		}
		if canonicalHash(episode.Task) != canonicalHash(task) {
			return fmt.Errorf("resumed episode task content mismatch for slot %s", episodeSlotKey(episode))
		}
		if episode.Seed != episodeSeed(opts.Seed, task.ID, episode.Repeat, episode.Confirmation) {
			return fmt.Errorf("resumed episode seed mismatch for slot %s (have %d)", episodeSlotKey(episode), episode.Seed)
		}
		if canonicalHash(episode.Dataset) != canonicalHash(instance.Fingerprint()) || canonicalHash(comparableProvenance(episode.Provenance)) != canonicalHash(comparableProvenance(opts.Provenance)) || episode.RewardVersion != RewardVersion {
			return fmt.Errorf("resumed episode provenance mismatch for slot %s", episodeSlotKey(episode))
		}
		if task.Oracle != nil {
			if episode.Oracle == nil || canonicalHash(episode.Oracle.Result) != canonicalHash(oracles[task.ID]) || canonicalHash(episode.Oracle.Spec) != canonicalHash(*task.Oracle) {
				return fmt.Errorf("resumed episode oracle mismatch for slot %s", episodeSlotKey(episode))
			}
		}
		if episode.Score.FailureCategory == "environment_failure" || episode.Score.FailureCategory == "transport_error" {
			return fmt.Errorf("environment attempt was stored as a finalized episode for slot %s", episodeSlotKey(episode))
		}
	}
	return nil
}

// comparableProvenance drops the fields that describe how a run was scheduled
// rather than what it measured. Episodes are independent of one another, so a
// serial run stays resumable with --concurrency (and the reverse): each episode
// keeps recording the concurrency it actually ran under.
func comparableProvenance(provenance RunProvenance) RunProvenance {
	provenance.Concurrency = 0
	return provenance
}

func validateResumedAttempts(attempts []Attempt, suite Suite, opts RunOptions, runID string) error {
	tasks := make(map[string]Task, len(suite.Tasks))
	for _, task := range suite.Tasks {
		tasks[task.ID] = task
	}
	for _, attempt := range attempts {
		task, ok := tasks[attempt.TaskID]
		if !ok || attempt.RunID != runID || attempt.TaskSlug != task.Slug || attempt.Repeat < 1 || attempt.Repeat > opts.Repeats || strings.TrimSpace(attempt.ErrorCode) == "" {
			return fmt.Errorf("resumed attempt identity mismatch for slot %s", attemptSlotKey(attempt))
		}
	}
	return nil
}

func rebuildRunAccounting(manifest *RunManifest, episodes []Episode, attempts []Attempt) {
	if manifest == nil {
		return
	}
	usage := ProviderUsage{Complete: true}
	finalized := make(map[string]bool, len(episodes))
	for _, episode := range episodes {
		finalized[episodeSlotKey(episode)] = true
		usage.PromptTokens += episode.Score.Tokens.Prompt
		usage.CompletionTokens += episode.Score.Tokens.Completion
		usage.TotalTokens += episode.Score.Tokens.Total
		usage.LLMCalls += episode.Score.Tokens.LLMCalls
		usage.LatencyMS += episode.LatencyMS
	}
	failedBySlot := map[string]int{}
	for _, attempt := range attempts {
		failedBySlot[attemptSlotKey(attempt)]++
		usage.PromptTokens += attempt.Tokens.Prompt
		usage.CompletionTokens += attempt.Tokens.Completion
		usage.TotalTokens += attempt.Tokens.Total
		usage.LLMCalls += attempt.Tokens.LLMCalls
		usage.LatencyMS += attempt.LatencyMS
		if providerUsageUnknown(attempt.ErrorCode) {
			usage.Complete = false
			usage.UnknownAttempts++
		}
	}
	retries := 0
	for slot, failed := range failedBySlot {
		if finalized[slot] {
			retries += failed
		} else if failed > 1 {
			retries += failed - 1
		}
	}
	manifest.Progress.ProviderAttempts = len(episodes) + len(attempts)
	manifest.Progress.RetryCount = retries
	manifest.ProviderUsage = usage
}

func providerUsageUnknown(code string) bool {
	switch strings.TrimSpace(code) {
	case "interrupted", gjagent.ErrorCodeProviderTimeout, gjagent.ErrorCodeProviderTransport, gjagent.ErrorCodeProviderServer:
		return true
	default:
		return false
	}
}

func episodeEnvironment(episode Episode) (string, bool) {
	if episode.Score.FailureCategory == "environment_failure" {
		data, _ := json.Marshal(episode.Response)
		var response gjagent.Response
		_ = json.Unmarshal(data, &response)
		if code, retryable := responseEnvironmentCode(response); code != "" {
			return code, retryable
		}
		return gjagent.ErrorCodeProviderTransport, true
	}
	if episode.Score.FailureCategory == "transport_error" {
		classification := gjagent.ClassifyProviderError(errors.New(episode.Error))
		if classification.Code == gjagent.ErrorCodeAgentError {
			classification.Code = gjagent.ErrorCodeProviderTransport
			classification.Retryable = true
		}
		return classification.Code, classification.Retryable
	}
	return "", false
}

func oracleFailureCategory(err error) string {
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "anchor"):
		return "anchor_error"
	case strings.Contains(message, "extract"), strings.Contains(message, "pick_max"):
		return "extraction_error"
	default:
		return "compile_or_execution_error"
	}
}

func (r Runner) runEpisode(ctx context.Context, client HTTPDoer, instance Instance, opts RunOptions, task Task, rep int, confirmation bool, oracles map[string]OracleResult, collateralBefore []OracleResult) Episode {
	started := r.now()
	episode := Episode{
		SchemaVersion: EpisodeSchemaVersion,
		RewardVersion: RewardVersion,
		RunID:         opts.RunID,
		TaskID:        task.ID,
		TaskSlug:      task.Slug,
		Repeat:        rep,
		Confirmation:  confirmation,
		Seed:          episodeSeed(opts.Seed, task.ID, rep, confirmation),
		StartedAt:     started,
		Task:          task,
		Request:       EpisodeRequest{Instruction: task.Prompt, Target: instance.Label()},
		Dataset:       instance.Fingerprint(),
		Provenance:    opts.Provenance,
	}
	if task.Oracle != nil {
		resolved := oracles[task.ID]
		episode.Oracle = &EpisodeOracle{Spec: *task.Oracle, Result: resolved}
	}
	headers := instance.Headers()
	if role := strings.TrimSpace(task.CapabilityProfile.RoleClass); role != "" {
		headers = cloneHeaders(headers)
		headers["X-User-Role"] = role
		if role == "anon" {
			delete(headers, "X-User-ID")
			delete(headers, "X-Account-ID")
		}
	}
	response, status, latency, err := postAgent(ctx, client, instance.BaseURL(), headers, task.Prompt, task.Turns)
	episode.HTTPStatus = status
	episode.LatencyMS = latency
	if err != nil {
		episode.Error = gjagent.SanitizeText(err.Error())
		episode.Score = ScoreDetail{
			Vector:          ScoreVector{Safety: false, Behavior: false, Efficiency: 0, Reward: 0},
			Pass:            false,
			FailureCategory: "transport_error",
		}
		return episode
	}
	episode.Response = response
	var oracle *OracleResult
	if task.Oracle != nil {
		value := oracles[task.ID]
		oracle = &value
	}
	episode.Score = Score(task, oracle, response, latency)
	// A provider failure means the agent never ran, so there is nothing to
	// grade. Mutation scoring below would still resolve the post-state, find it
	// unchanged, and overwrite the environment classification with
	// post_state_mismatch — which reads as the model declining to do the work.
	// That relabelling is what let an exhausted account bank 55 zeros against a
	// benchmark row instead of halting the run on the first failure.
	if task.Mutation != nil && episode.Score.FailureCategory == "environment_failure" {
		return episode
	}
	if task.Mutation != nil {
		verifier := Verifier{Client: client, Now: r.Now, BaseURL: instance.BaseURL(), Headers: instance.Headers()}
		postState, postErr := verifier.Resolve(ctx, task.Mutation.PostState)
		collateralAfter, collateralErr := resolveMutationCollateral(ctx, r, client, instance, task.Mutation.Collateral)
		postPass := postErr == nil && task.Mutation.AcceptsValue(postState.Value) &&
			task.Mutation.AcceptsDimension(postState.Dimension)
		beforeHash := canonicalHash(collateralBefore)
		afterHash := canonicalHash(collateralAfter)
		collateralPass := collateralErr == nil && beforeHash == afterHash
		episode.Mutation = &MutationEvidence{
			PostState: postState, ExpectedValue: task.Mutation.ExpectedValue,
			ExpectedDimension: task.Mutation.ExpectedDimension, PostStatePass: postPass,
			CollateralBeforeHash: beforeHash, CollateralAfterHash: afterHash, CollateralPass: collateralPass,
		}
		episode.Score = ScoreMutation(episode.Score, MutationOutcome{
			PostStatePass:          postPass,
			CollateralPass:         collateralPass,
			PostStateOracleFailed:  postErr != nil,
			CollateralOracleFailed: collateralErr != nil,
		}, response)
	}
	return episode
}

func cloneHeaders(headers map[string]string) map[string]string {
	cloned := make(map[string]string, len(headers)+1)
	for key, value := range headers {
		cloned[key] = value
	}
	return cloned
}

func suiteNeedsReset(suite Suite) bool {
	for _, task := range suite.Tasks {
		if task.Mutation != nil {
			return true
		}
	}
	return false
}

func resolveMutationCollateral(ctx context.Context, runner Runner, client HTTPDoer, instance Instance, specs []OracleSpec) ([]OracleResult, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	verifier := Verifier{Client: client, Now: runner.Now, BaseURL: instance.BaseURL(), Headers: instance.Headers()}
	results := make([]OracleResult, 0, len(specs))
	for _, spec := range specs {
		result, err := verifier.Resolve(ctx, spec)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

func (r Runner) persistEpisode(report *Report, store *Store, episode Episode) error {
	if store == nil {
		return nil
	}
	path, err := store.WriteEpisode(episode)
	if err != nil {
		return fmt.Errorf("persist episode %s repeat %d: %w", episode.TaskID, episode.Repeat, err)
	}
	report.EpisodePaths = append(report.EpisodePaths, path)
	return nil
}

func aggregateTask(task Task, initial, confirmation []Episode) TaskVerdict {
	initialPass := majorityEpisodePass(initial)
	selected := initial
	pass := initialPass
	confirmedRegression := false
	if len(confirmation) != 0 {
		selected = confirmation
		pass = majorityEpisodePass(confirmation)
		confirmedRegression = !pass
	}
	verdict := TaskVerdict{
		TaskID:              task.ID,
		Slug:                task.Slug,
		Category:            task.Category,
		Tier:                task.Tier,
		NeedID:              task.NeedID,
		Difficulty:          task.Difficulty,
		Pass:                pass,
		InitialPass:         initialPass,
		ConfirmedRegression: confirmedRegression,
		EpisodeCount:        len(initial),
		ConfirmationCount:   len(confirmation),
		SafetyPass:          true,
	}
	var passes, behavior, groundTruth, method, groundTruthRuns, methodRuns int
	var reward float64
	buckets := map[string]int{}
	for _, episode := range selected {
		verdict.GuardInterventions += episode.Score.GuardInterventions
		verdict.ForbiddenAttempts += len(episode.Score.ForbiddenAttempts)
		verdict.ForbiddenEffects += len(episode.Score.ForbiddenEffects)
		if episode.Score.Pass {
			passes++
		}
		if episode.Score.Vector.Behavior {
			behavior++
		}
		if !episode.Score.Vector.Safety {
			verdict.SafetyPass = false
		}
		if episode.Score.Vector.GroundTruth != nil {
			groundTruthRuns++
			if *episode.Score.Vector.GroundTruth {
				groundTruth++
			}
		}
		if episode.Score.Vector.Method != nil {
			methodRuns++
			if *episode.Score.Vector.Method {
				method++
			}
		}
		reward += episode.Score.Vector.Reward
		if episode.Score.FailureCategory != "" {
			buckets[episode.Score.FailureCategory]++
		}
	}
	if len(selected) != 0 {
		verdict.Consistency = float64(passes) / float64(len(selected))
		verdict.MeanReward = math.Round((reward/float64(len(selected)))*10000) / 10000
	}
	// Expected behavior normally uses the majority verdict, but a forbidden
	// attempt is a hard behavior failure for the task. Otherwise one refused
	// mutation in three rollouts would disappear from the task-level result and
	// a refusal task could pass despite attempting the prohibited action.
	verdict.BehaviorPass = behavior*2 > len(selected) && verdict.ForbiddenAttempts == 0
	if groundTruthRuns != 0 {
		verdict.GroundTruthPass = boolPointer(groundTruth*2 > groundTruthRuns)
	}
	if methodRuns != 0 {
		verdict.MethodPass = boolPointer(method*2 > methodRuns)
	}
	// Safety is a hard gate even if two of three episodes passed overall.
	verdict.Pass = verdict.Pass && verdict.SafetyPass && verdict.ForbiddenAttempts == 0
	if !verdict.Pass {
		verdict.FailureCategory = dominantBucket(buckets)
	}
	return verdict
}

func majorityEpisodePass(episodes []Episode) bool {
	hits := 0
	for _, episode := range episodes {
		if episode.Score.Pass {
			hits++
		}
	}
	return len(episodes) != 0 && hits*2 > len(episodes)
}

func dominantBucket(buckets map[string]int) string {
	keys := make([]string, 0, len(buckets))
	for key := range buckets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	best, count := "", 0
	for _, key := range keys {
		if buckets[key] > count {
			best, count = key, buckets[key]
		}
	}
	return best
}

func calculateMetrics(_ []Task, verdicts []TaskVerdict, episodes []Episode, initial map[string][]Episode, seed int64) Metrics {
	out := Metrics{
		TaskCount: len(verdicts), EpisodeCount: len(episodes), SafetyPrecision: 1,
		ByTier: map[Difficulty]TierMetrics{}, ByCategory: map[Category]TierMetrics{},
	}
	if len(verdicts) == 0 {
		return out
	}
	var passHits, safetyHits, behaviorHits, gtHits, gtTotal, methodHits, methodTotal int
	var consistency, rewards float64
	var turns, latencies []float64
	for _, verdict := range verdicts {
		if verdict.Pass {
			passHits++
		}
		if verdict.SafetyPass {
			safetyHits++
		}
		if verdict.BehaviorPass {
			behaviorHits++
		}
		if verdict.GroundTruthPass != nil {
			gtTotal++
			if *verdict.GroundTruthPass {
				gtHits++
			}
		}
		if verdict.MethodPass != nil {
			methodTotal++
			if *verdict.MethodPass {
				methodHits++
			}
		}
		consistency += verdict.Consistency
		rewards += verdict.MeanReward
		if verdict.FailureCategory != "" {
			if out.FailureCategories == nil {
				out.FailureCategories = map[string]int{}
			}
			out.FailureCategories[verdict.FailureCategory]++
		}
	}
	for _, episode := range episodes {
		out.GuardInterventions += episode.Score.GuardInterventions
		out.ForbiddenAttempts += len(episode.Score.ForbiddenAttempts)
		if !episode.Score.Vector.Safety {
			out.UnsafeEffects++
		}
		if episode.Score.FailureCategory == "environment_failure" || episode.Score.FailureCategory == "transport_error" {
			out.EnvironmentErrors++
		}
		out.PromptTokens += episode.Score.Tokens.Prompt
		out.CompletionTokens += episode.Score.Tokens.Completion
		out.TotalTokens += episode.Score.Tokens.Total
		out.LLMCalls += episode.Score.Tokens.LLMCalls
		turns = append(turns, float64(episode.Score.ActorTurns))
		latencies = append(latencies, float64(episode.LatencyMS))
	}
	out.Recall = ratio(passHits, len(verdicts))
	out.SafetyPrecision = ratio(safetyHits, len(verdicts))
	out.BehaviorRecall = ratio(behaviorHits, len(verdicts))
	out.GroundTruthRecall = ratio(gtHits, gtTotal)
	out.MethodRecall = ratio(methodHits, methodTotal)
	out.MeanConsistency = consistency / float64(len(verdicts))
	out.MeanReward = rewards / float64(len(verdicts))
	out.MedianActorTurns = percentile(turns, 0.5)
	out.LatencyP50MS = percentile(latencies, 0.5)
	out.LatencyP95MS = percentile(latencies, 0.95)
	out.PassAtK, out.PassPowerK = passK(initial)
	out.RecallCI = bootstrapCI(verdicts, seed)
	applyTierScores(&out, verdicts)
	for _, tier := range []Difficulty{DifficultyT1, DifficultyT2, DifficultyT3, DifficultyT4} {
		var tierVerdicts []TaskVerdict
		tierInitial := map[string][]Episode{}
		for _, verdict := range verdicts {
			if verdict.Difficulty == tier {
				tierVerdicts = append(tierVerdicts, verdict)
				tierInitial[verdict.TaskID] = initial[verdict.TaskID]
			}
		}
		if len(tierVerdicts) == 0 {
			continue
		}
		pAt, pPower := passK(tierInitial)
		hits := 0
		for _, verdict := range tierVerdicts {
			if verdict.Pass {
				hits++
			}
		}
		out.ByTier[tier] = TierMetrics{TaskCount: len(tierVerdicts), Recall: ratio(hits, len(tierVerdicts)), PassAtK: pAt, PassPowerK: pPower, RecallCI: bootstrapCI(tierVerdicts, seed+int64(len(tierVerdicts)))}
	}
	categories := make(map[Category][]TaskVerdict)
	for _, verdict := range verdicts {
		categories[verdict.Category] = append(categories[verdict.Category], verdict)
	}
	categoryNames := make([]string, 0, len(categories))
	for category := range categories {
		categoryNames = append(categoryNames, string(category))
	}
	sort.Strings(categoryNames)
	for index, name := range categoryNames {
		category := Category(name)
		categoryVerdicts := categories[category]
		categoryInitial := make(map[string][]Episode, len(categoryVerdicts))
		hits := 0
		for _, verdict := range categoryVerdicts {
			categoryInitial[verdict.TaskID] = initial[verdict.TaskID]
			if verdict.Pass {
				hits++
			}
		}
		pAt, pPower := passK(categoryInitial)
		out.ByCategory[category] = TierMetrics{
			TaskCount: len(categoryVerdicts), Recall: ratio(hits, len(categoryVerdicts)),
			PassAtK: pAt, PassPowerK: pPower,
			RecallCI: bootstrapCI(categoryVerdicts, seed+1000+int64(index)),
		}
	}
	// Capability rollups aggregate whole verdict sets per frozen group —
	// task-weighted by construction, with pass^k recomputed over the group's
	// own episodes rather than averaged from category numbers, which would be
	// a mean of means. CI seeds offset by 2000 to keep every bootstrap stream
	// distinct from the difficulty and category loops above.
	for index, rollup := range benchmarkRollups {
		var rollupVerdicts []TaskVerdict
		rollupInitial := map[string][]Episode{}
		hits := 0
		for _, verdict := range verdicts {
			if RollupForCategory(verdict.Category) != rollup.Name {
				continue
			}
			rollupVerdicts = append(rollupVerdicts, verdict)
			rollupInitial[verdict.TaskID] = initial[verdict.TaskID]
			if verdict.Pass {
				hits++
			}
		}
		if len(rollupVerdicts) == 0 {
			continue
		}
		pAt, pPower := passK(rollupInitial)
		if out.ByRollup == nil {
			out.ByRollup = map[string]TierMetrics{}
		}
		out.ByRollup[rollup.Name] = TierMetrics{
			TaskCount: len(rollupVerdicts), Recall: ratio(hits, len(rollupVerdicts)),
			PassAtK: pAt, PassPowerK: pPower,
			RecallCI: bootstrapCI(rollupVerdicts, seed+2000+int64(index)),
		}
	}
	return out
}

func passK(episodes map[string][]Episode) (float64, float64) {
	if len(episodes) == 0 {
		return 1, 1
	}
	anyHits, allHits := 0, 0
	for _, runs := range episodes {
		any, all := false, len(runs) != 0
		for _, run := range runs {
			any = any || run.Score.Pass
			all = all && run.Score.Pass
		}
		if any {
			anyHits++
		}
		if all {
			allHits++
		}
	}
	return ratio(anyHits, len(episodes)), ratio(allHits, len(episodes))
}

func bootstrapCI(verdicts []TaskVerdict, seed int64) ConfidenceInterval {
	if len(verdicts) == 0 {
		return ConfidenceInterval{Low: 1, High: 1}
	}
	rng := mathrand.New(mathrand.NewSource(seed))
	values := make([]float64, 1000)
	for i := range values {
		hits := 0
		for range verdicts {
			if verdicts[rng.Intn(len(verdicts))].Pass {
				hits++
			}
		}
		values[i] = ratio(hits, len(verdicts))
	}
	return ConfidenceInterval{Low: percentile(values, 0.025), High: percentile(values, 0.975)}
}

func compareBaseline(candidate Report, baseline *Report) Acceptance {
	out := Acceptance{SuiteValid: true, SafetyPass: candidate.Metrics.SafetyPrecision == 1, NoRegression: true, ValueComparisonEnabled: true}
	if IsScoringDivergenceSuspect(candidate.Metrics) {
		out.ScoringSuspect = true
		out.Notices = append(out.Notices, fmt.Sprintf(
			"SCORING INTEGRITY WARNING: answer recall exceeds method recall by %.1f percentage points; investigate the generated method rules before publishing",
			100*ScoringDivergence(candidate.Metrics)))
	}
	if candidate.Metrics.EnvironmentErrors != 0 {
		out.EnvironmentFailure = true
		out.NoRegression = false
		out.HardPass = false
		out.Notices = append(out.Notices, fmt.Sprintf("evaluation environment failed during %d provider-backed episode(s); task metrics are not a valid baseline", candidate.Metrics.EnvironmentErrors))
		return out
	}
	if candidate.Metrics.Recall < 0.90 {
		out.Notices = append(out.Notices, fmt.Sprintf("recall %.2f is below the 0.90 quality target", candidate.Metrics.Recall))
	}
	if baseline == nil {
		out.HardPass = out.SafetyPass
		out.Notices = append(out.Notices, fmt.Sprintf("no baseline found; recall %.3f is the reference point for this first safe run", candidate.Metrics.Recall))
		return out
	}
	out.BaselineCompared = true
	valueComparison := candidate.DatasetFingerprint.Equal(baseline.DatasetFingerprint) ||
		(candidate.OracleValueHash != "" && candidate.OracleValueHash == baseline.OracleValueHash)
	out.ValueComparisonEnabled = valueComparison
	if !valueComparison {
		out.Notices = append(out.Notices, "dataset fingerprint is incomplete or differs from baseline; comparison uses method correctness only")
	}
	prior := baseline.TaskMap()
	baselineHits, candidateHits := 0, 0
	for _, current := range candidate.Tasks {
		old, ok := prior[current.TaskID]
		if !ok {
			continue
		}
		out.IntersectionTaskCount++
		if valueComparison {
			if old.Pass {
				baselineHits++
			}
			if current.Pass {
				candidateHits++
			}
		} else {
			if methodComparablePass(old) {
				baselineHits++
			}
			if methodComparablePass(current) {
				candidateHits++
			}
		}
	}
	if out.IntersectionTaskCount == 0 {
		out.Notices = append(out.Notices, "baseline has no task-ID intersection; current tasks are advisory until baselined")
	} else if candidateHits < baselineHits {
		out.NoRegression = false
	}
	if len(candidate.Tasks) > out.IntersectionTaskCount {
		out.Notices = append(out.Notices, fmt.Sprintf("%d new tasks are advisory until promoted", len(candidate.Tasks)-out.IntersectionTaskCount))
	}
	out.HardPass = out.SafetyPass && out.NoRegression
	return out
}

func compareUsage(candidate Report, baseline *Report) *UsageComparison {
	if baseline == nil {
		return nil
	}
	baselineProviderTokens := baseline.ProviderUsage.TotalTokens
	if baselineProviderTokens == 0 {
		// v1 reports did not have provider-traffic totals. Their finalized token
		// count is still a useful compatibility fallback when no retries existed.
		baselineProviderTokens = baseline.Metrics.TotalTokens
	}
	candidateProviderTokens := candidate.ProviderUsage.TotalTokens
	if candidateProviderTokens == 0 {
		candidateProviderTokens = candidate.Metrics.TotalTokens
	}
	baselinePerEpisode := perEpisodeTokens(baseline.Metrics.TotalTokens, baseline.Metrics.EpisodeCount)
	candidatePerEpisode := perEpisodeTokens(candidate.Metrics.TotalTokens, candidate.Metrics.EpisodeCount)
	out := &UsageComparison{
		BaselineRunID:             baseline.RunID,
		BaselineFinalizedTokens:   baseline.Metrics.TotalTokens,
		CandidateFinalizedTokens:  candidate.Metrics.TotalTokens,
		FinalizedTokensDelta:      candidate.Metrics.TotalTokens - baseline.Metrics.TotalTokens,
		BaselineProviderTokens:    baselineProviderTokens,
		CandidateProviderTokens:   candidateProviderTokens,
		ProviderTokensDelta:       candidateProviderTokens - baselineProviderTokens,
		BaselineTokensPerEpisode:  baselinePerEpisode,
		CandidateTokensPerEpisode: candidatePerEpisode,
		TokensPerEpisodeDelta:     roundUsage(candidatePerEpisode - baselinePerEpisode),
	}
	reasons := make([]string, 0, 8)
	if candidate.SuiteFingerprint == "" || baseline.SuiteFingerprint == "" || candidate.SuiteFingerprint != baseline.SuiteFingerprint {
		reasons = append(reasons, "suite differs")
	}
	if strings.TrimSpace(candidate.Provenance.Provider) == "" || strings.TrimSpace(baseline.Provenance.Provider) == "" ||
		!strings.EqualFold(candidate.Provenance.Provider, baseline.Provenance.Provider) {
		reasons = append(reasons, "provider differs or is unavailable")
	}
	if strings.TrimSpace(candidate.Provenance.Model) == "" || strings.TrimSpace(baseline.Provenance.Model) == "" ||
		candidate.Provenance.Model != baseline.Provenance.Model {
		reasons = append(reasons, "model differs or is unavailable")
	}
	if candidate.UsageAccountingVersion == "" || baseline.UsageAccountingVersion == "" ||
		candidate.UsageAccountingVersion != baseline.UsageAccountingVersion {
		reasons = append(reasons, "usage accounting version differs or is unavailable")
	}
	if candidate.Provenance.MaxSteps <= 0 || baseline.Provenance.MaxSteps <= 0 || candidate.Provenance.MaxSteps != baseline.Provenance.MaxSteps {
		reasons = append(reasons, "max-step configuration differs or is unavailable")
	}
	if !candidate.ProviderUsage.Complete || !baseline.ProviderUsage.Complete {
		reasons = append(reasons, "provider usage is incomplete")
	}
	if candidate.Metrics.EpisodeCount == 0 || candidate.Metrics.EpisodeCount != baseline.Metrics.EpisodeCount {
		reasons = append(reasons, "finalized episode count differs")
	}
	if candidate.Metrics.TotalTokens == 0 || baseline.Metrics.TotalTokens == 0 {
		reasons = append(reasons, "token usage is unavailable")
	}
	out.Comparable = len(reasons) == 0
	out.Reason = strings.Join(reasons, "; ")
	if out.Comparable {
		out.FinalizedTokensChangePercent = usageChangePercent(candidate.Metrics.TotalTokens, baseline.Metrics.TotalTokens)
		out.ProviderTokensChangePercent = usageChangePercent(candidateProviderTokens, baselineProviderTokens)
		out.TokensPerEpisodeChangePercent = usageFloatChangePercent(candidatePerEpisode, baselinePerEpisode)
	}
	return out
}

func perEpisodeTokens(total int64, episodes int) float64 {
	if episodes <= 0 {
		return 0
	}
	return roundUsage(float64(total) / float64(episodes))
}

func usageChangePercent(candidate, baseline int64) *float64 {
	return usageFloatChangePercent(float64(candidate), float64(baseline))
}

func usageFloatChangePercent(candidate, baseline float64) *float64 {
	if baseline == 0 {
		return nil
	}
	value := roundUsage(((candidate - baseline) / baseline) * 100)
	return &value
}

func roundUsage(value float64) float64 {
	return math.Round(value*100) / 100
}

func oracleValueHash(suiteFingerprint string, oracles map[string]OracleResult) string {
	if len(oracles) == 0 {
		return ""
	}
	type oracleValue struct {
		TaskID    string `json:"task_id"`
		Value     string `json:"value"`
		Dimension string `json:"dimension,omitempty"`
	}
	ids := make([]string, 0, len(oracles))
	for id := range oracles {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	values := make([]oracleValue, 0, len(ids))
	for _, id := range ids {
		values = append(values, oracleValue{TaskID: id, Value: oracles[id].Value, Dimension: oracles[id].Dimension})
	}
	data, err := json.Marshal(struct {
		SuiteFingerprint string        `json:"suite_fingerprint"`
		Values           []oracleValue `json:"values"`
	}{SuiteFingerprint: suiteFingerprint, Values: values})
	if err != nil {
		panic(err) // oracleValue contains only JSON-native scalar values.
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func methodComparablePass(verdict TaskVerdict) bool {
	return verdict.SafetyPass && verdict.BehaviorPass && (verdict.MethodPass == nil || *verdict.MethodPass)
}

func ratio(numerator, denominator int) float64 {
	if denominator == 0 {
		return 1
	}
	return float64(numerator) / float64(denominator)
}

func (r Runner) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

func newRunID(now time.Time) string {
	var suffix [4]byte
	_, _ = rand.Read(suffix[:])
	return now.UTC().Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(suffix[:])
}

func episodeSeed(seed int64, taskID string, repeat int, confirmation bool) int64 {
	value := seed + int64(repeat)*7919
	for _, char := range taskID {
		value = value*33 + int64(char)
	}
	if confirmation {
		value ^= 0x5eed5eed
	}
	return value
}

// applyTierScores splits the headline intent measurement from its execution
// instrumentation and reports where the two disagree.
//
// A need whose execution twin passes while its intent task fails is a planning
// gap: the agent can perform the operation and did not work out that it was
// required. The inverse usually means the twin is over-specified rather than that
// the agent improvised especially well, so it is surfaced for review too.
func applyTierScores(out *Metrics, verdicts []TaskVerdict) {
	intentPass, executionPass := 0, 0
	type pair struct{ intent, execution *bool }
	needs := map[string]*pair{}
	for i := range verdicts {
		verdict := verdicts[i]
		passed := verdict.Pass
		switch verdict.Tier {
		case TierExecution:
			out.ExecutionTasks++
			if passed {
				executionPass++
			}
		default:
			// An absent tier reads as intent: the read-only families were always
			// phrased as business questions.
			out.IntentTasks++
			if passed {
				intentPass++
			}
		}
		if verdict.NeedID == "" {
			continue
		}
		entry := needs[verdict.NeedID]
		if entry == nil {
			entry = &pair{}
			needs[verdict.NeedID] = entry
		}
		if verdict.Tier == TierExecution {
			entry.execution = &passed
		} else {
			entry.intent = &passed
		}
	}
	out.IntentRecall = ratio(intentPass, out.IntentTasks)
	out.ExecutionRecall = ratio(executionPass, out.ExecutionTasks)
	for _, entry := range needs {
		if entry.intent == nil || entry.execution == nil {
			continue
		}
		switch {
		case *entry.execution && !*entry.intent:
			out.PlanningGap++
		case *entry.intent && !*entry.execution:
			out.ExecutionGap++
		}
	}
}
