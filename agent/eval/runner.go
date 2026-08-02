package eval

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	mathrand "math/rand"
	"net/http"
	"sort"
	"strings"
	"time"
)

type RunOptions struct {
	Mode         RunMode
	Repeats      int
	Seed         int64
	RunID        string
	Provenance   RunProvenance
	Baseline     *Report
	Store        *Store
	AutoBaseline bool
}

type Runner struct {
	Client HTTPDoer
	Now    func() time.Time
}

func (r Runner) Run(ctx context.Context, suite Suite, instance Instance, opts RunOptions) (*Report, error) {
	if instance == nil {
		return nil, fmt.Errorf("nil evaluation instance")
	}
	if err := suite.Validate(); err != nil {
		return nil, err
	}
	if opts.Repeats <= 0 {
		opts.Repeats = DefaultRepeats
	}
	if opts.Mode == "" {
		opts.Mode = RunModeRun
	}
	if opts.RunID == "" {
		opts.RunID = newRunID(r.now())
	}
	opts.Provenance.Seed = opts.Seed
	opts.Provenance.Repeats = opts.Repeats
	opts.Provenance.Target = instance.Label()
	client := r.Client
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}
	report := &Report{
		SchemaVersion:      ReportSchemaVersion,
		RewardVersion:      RewardVersion,
		RunID:              opts.RunID,
		Mode:               opts.Mode,
		GeneratedAt:        r.now(),
		SuiteFingerprint:   SuiteFingerprint(suite),
		CatalogFingerprint: suite.CatalogFingerprint,
		DatasetFingerprint: instance.Fingerprint(),
		Provenance:         opts.Provenance,
		Acceptance:         Acceptance{SuiteValid: true},
	}

	verifier := Verifier{Client: client, Now: r.Now, BaseURL: instance.BaseURL(), Headers: instance.Headers()}
	oracles := make(map[string]OracleResult, len(suite.Tasks))
	invalid := make(map[string]string)
	invalidDetails := make(map[string]string)
	for _, task := range suite.Tasks {
		if task.Oracle == nil {
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
		if opts.Store != nil {
			if _, err := opts.Store.WriteReport(*report); err != nil {
				return nil, err
			}
		}
		return report, nil
	}
	report.OracleValueHash = oracleValueHash(report.SuiteFingerprint, oracles)

	initial := make(map[string][]Episode, len(suite.Tasks))
	allEpisodes := make([]Episode, 0, len(suite.Tasks)*opts.Repeats)
	for _, task := range suite.Tasks {
		for rep := 1; rep <= opts.Repeats; rep++ {
			episode := r.runEpisode(ctx, client, instance, opts, task, rep, false, oracles)
			initial[task.ID] = append(initial[task.ID], episode)
			allEpisodes = append(allEpisodes, episode)
			if err := r.persistEpisode(report, opts.Store, episode); err != nil {
				return nil, err
			}
		}
	}

	baselineTasks := map[string]TaskVerdict{}
	if opts.Baseline != nil {
		baselineTasks = opts.Baseline.TaskMap()
	}
	for _, task := range suite.Tasks {
		verdict := aggregateTask(task, initial[task.ID], nil)
		if prior, ok := baselineTasks[task.ID]; ok && prior.Pass && !verdict.Pass && verdict.SafetyPass {
			confirmation := make([]Episode, 0, opts.Repeats)
			for rep := 1; rep <= opts.Repeats; rep++ {
				episode := r.runEpisode(ctx, client, instance, opts, task, rep, true, oracles)
				confirmation = append(confirmation, episode)
				allEpisodes = append(allEpisodes, episode)
				if err := r.persistEpisode(report, opts.Store, episode); err != nil {
					return nil, err
				}
			}
			verdict = aggregateTask(task, initial[task.ID], confirmation)
		}
		report.Tasks = append(report.Tasks, verdict)
	}
	report.Metrics = calculateMetrics(suite.Tasks, report.Tasks, allEpisodes, initial, opts.Seed)
	report.Acceptance = compareBaseline(*report, opts.Baseline)
	if opts.Store != nil {
		if _, err := opts.Store.WriteReport(*report); err != nil {
			return nil, err
		}
		if opts.AutoBaseline && opts.Baseline == nil && report.Acceptance.HardPass {
			if err := opts.Store.PromoteBaseline(*report); err != nil {
				return nil, err
			}
			report.Acceptance.Notices = append(report.Acceptance.Notices, fmt.Sprintf("first safety-passing run promoted as baseline at recall %.3f", report.Metrics.Recall))
			if _, err := opts.Store.WriteReport(*report); err != nil {
				return nil, err
			}
		}
	}
	return report, nil
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

func (r Runner) runEpisode(ctx context.Context, client HTTPDoer, instance Instance, opts RunOptions, task Task, rep int, confirmation bool, oracles map[string]OracleResult) Episode {
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
	response, status, latency, err := postAgent(ctx, client, instance.BaseURL(), instance.Headers(), task.Prompt)
	episode.HTTPStatus = status
	episode.LatencyMS = latency
	if err != nil {
		episode.Error = err.Error()
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
	return episode
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
	verdict.BehaviorPass = behavior*2 > len(selected)
	if groundTruthRuns != 0 {
		verdict.GroundTruthPass = boolPointer(groundTruth*2 > groundTruthRuns)
	}
	if methodRuns != 0 {
		verdict.MethodPass = boolPointer(method*2 > methodRuns)
	}
	verdict.FailureCategory = dominantBucket(buckets)
	// Safety is a hard gate even if two of three episodes passed overall.
	verdict.Pass = verdict.Pass && verdict.SafetyPass
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
	out := Metrics{TaskCount: len(verdicts), EpisodeCount: len(episodes), SafetyPrecision: 1, ByTier: map[Difficulty]TierMetrics{}}
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
		out.PromptTokens += episode.Score.Tokens.Prompt
		out.CompletionTokens += episode.Score.Tokens.Completion
		out.TotalTokens += episode.Score.Tokens.Total
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
