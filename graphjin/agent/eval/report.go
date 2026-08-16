package eval

import "time"

type RunMode string

const (
	RunModeRun       RunMode = "run"
	RunModeBenchmark RunMode = "bench"
)

type RunProvenance struct {
	Provider           string  `json:"provider,omitempty"`
	Model              string  `json:"model,omitempty"`
	APIKeyEnv          string  `json:"api_key_env,omitempty"`
	ServerFingerprint  string  `json:"server_eval_fingerprint,omitempty"`
	AxVersion          string  `json:"ax_version,omitempty"`
	GraphJinCommit     string  `json:"graphjin_commit,omitempty"`
	BinaryFingerprint  string  `json:"binary_fingerprint,omitempty"`
	PromptRegistryHash string  `json:"prompt_registry_hash,omitempty"`
	Temperature        float64 `json:"temperature"`
	Seed               int64   `json:"seed"`
	Repeats            int     `json:"repeats"`
	MaxSteps           int     `json:"max_steps,omitempty"`
	// Reasoning records the provider thinking effort the run used. Absent
	// means the provider default, which for some adapters is thinking off —
	// runs are not comparable across different values.
	Reasoning          string  `json:"reasoning,omitempty"`
	// TimeoutSeconds is the agent's per-run deadline. The harness sizes its
	// own HTTP timeout from it: one request covers a whole multi-step run.
	TimeoutSeconds     int     `json:"timeout_seconds,omitempty"`
	Target             string  `json:"target"`
}

// ScoringProvenance identifies the build that deterministically rescored a
// stored run. RunProvenance continues to identify the model/runtime build that
// produced the original episodes.
type ScoringProvenance struct {
	GraphJinCommit    string `json:"graphjin_commit,omitempty"`
	BinaryFingerprint string `json:"binary_fingerprint,omitempty"`
	RewardVersion     string `json:"reward_version"`
}

type RunProgress struct {
	PlannedInitialSlots      int `json:"planned_initial_slots"`
	PlannedConfirmationSlots int `json:"planned_confirmation_slots,omitempty"`
	CompletedInitialSlots    int `json:"completed_initial_slots"`
	CompletedConfirmation    int `json:"completed_confirmation_slots,omitempty"`
	ProviderAttempts         int `json:"provider_attempts"`
	RetryCount               int `json:"retry_count,omitempty"`
	ReusedEpisodeCount       int `json:"reused_episode_count,omitempty"`
}

type ProviderUsage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
	LLMCalls         int64 `json:"llm_calls"`
	LatencyMS        int64 `json:"latency_ms"`
	Complete         bool  `json:"complete"`
	UnknownAttempts  int   `json:"unknown_attempts,omitempty"`
}

// UsageComparison separates finalized-slot efficiency from actual provider
// traffic. Provider totals include failed attempts and retries; finalized
// totals are the stable quality-run denominator used for agent regressions.
type UsageComparison struct {
	BaselineRunID                 string   `json:"baseline_run_id"`
	Comparable                    bool     `json:"comparable"`
	Reason                        string   `json:"reason,omitempty"`
	BaselineFinalizedTokens       int64    `json:"baseline_finalized_tokens"`
	CandidateFinalizedTokens      int64    `json:"candidate_finalized_tokens"`
	FinalizedTokensDelta          int64    `json:"finalized_tokens_delta"`
	FinalizedTokensChangePercent  *float64 `json:"finalized_tokens_change_percent,omitempty"`
	BaselineProviderTokens        int64    `json:"baseline_provider_tokens"`
	CandidateProviderTokens       int64    `json:"candidate_provider_tokens"`
	ProviderTokensDelta           int64    `json:"provider_tokens_delta"`
	ProviderTokensChangePercent   *float64 `json:"provider_tokens_change_percent,omitempty"`
	BaselineTokensPerEpisode      float64  `json:"baseline_tokens_per_episode"`
	CandidateTokensPerEpisode     float64  `json:"candidate_tokens_per_episode"`
	TokensPerEpisodeDelta         float64  `json:"tokens_per_episode_delta"`
	TokensPerEpisodeChangePercent *float64 `json:"tokens_per_episode_change_percent,omitempty"`
}

type Episode struct {
	SchemaVersion string             `json:"schema_version"`
	RewardVersion string             `json:"reward_version"`
	RunID         string             `json:"run_id"`
	TaskID        string             `json:"task_id"`
	TaskSlug      string             `json:"task_slug"`
	Repeat        int                `json:"repeat"`
	Confirmation  bool               `json:"confirmation,omitempty"`
	Seed          int64              `json:"seed"`
	StartedAt     time.Time          `json:"started_at"`
	Task          Task               `json:"task"`
	Request       EpisodeRequest     `json:"request"`
	Dataset       DatasetFingerprint `json:"dataset_fingerprint"`
	Provenance    RunProvenance      `json:"provenance"`
	Response      any                `json:"response,omitempty"`
	Oracle        *EpisodeOracle     `json:"oracle,omitempty"`
	Mutation      *MutationEvidence  `json:"mutation,omitempty"`
	Score         ScoreDetail        `json:"score"`
	HTTPStatus    int                `json:"http_status,omitempty"`
	LatencyMS     int64              `json:"latency_ms"`
	Error         string             `json:"error,omitempty"`
}

type MutationEvidence struct {
	PostState            OracleResult `json:"post_state"`
	ExpectedValue        string       `json:"expected_value"`
	ExpectedDimension    string       `json:"expected_dimension,omitempty"`
	PostStatePass        bool         `json:"post_state_pass"`
	CollateralBeforeHash string       `json:"collateral_before_hash,omitempty"`
	CollateralAfterHash  string       `json:"collateral_after_hash,omitempty"`
	CollateralPass       bool         `json:"collateral_pass"`
}

type EpisodeRequest struct {
	Instruction string `json:"instruction"`
	Target      string `json:"target"`
}

type EpisodeOracle struct {
	Spec   OracleSpec   `json:"spec"`
	Result OracleResult `json:"result"`
}

type TaskVerdict struct {
	TaskID   string   `json:"task_id"`
	Slug     string   `json:"-"`
	Category Category `json:"category"`
	// Tier and NeedID carry the intent/execution split into the report so a
	// planning gap can be computed from verdicts alone.
	Tier                string     `json:"tier,omitempty"`
	NeedID              string     `json:"need_id,omitempty"`
	Difficulty          Difficulty `json:"difficulty"`
	Pass                bool       `json:"pass"`
	InitialPass         bool       `json:"initial_pass"`
	ConfirmedRegression bool       `json:"confirmed_regression,omitempty"`
	GroundTruthPass     *bool      `json:"ground_truth_pass,omitempty"`
	MethodPass          *bool      `json:"method_pass,omitempty"`
	SafetyPass          bool       `json:"safety_pass"`
	BehaviorPass        bool       `json:"behavior_pass"`
	Consistency         float64    `json:"consistency"`
	MeanReward          float64    `json:"mean_reward"`
	FailureCategory     string     `json:"failure_category,omitempty"`
	EpisodeCount        int        `json:"episode_count"`
	ConfirmationCount   int        `json:"confirmation_count,omitempty"`
	GuardInterventions  int        `json:"guard_interventions,omitempty"`
	ForbiddenAttempts   int        `json:"forbidden_attempts,omitempty"`
	ForbiddenEffects    int        `json:"forbidden_effects,omitempty"`
}

type ConfidenceInterval struct {
	Low  float64 `json:"low"`
	High float64 `json:"high"`
}

type TierMetrics struct {
	TaskCount  int                `json:"task_count"`
	Recall     float64            `json:"recall"`
	PassAtK    float64            `json:"pass_at_k"`
	PassPowerK float64            `json:"pass_power_k"`
	RecallCI   ConfidenceInterval `json:"recall_ci"`
}

type Metrics struct {
	TaskCount         int     `json:"task_count"`
	EpisodeCount      int     `json:"episode_count"`
	Recall            float64 `json:"recall"`
	GroundTruthRecall float64 `json:"ground_truth_recall"`
	MethodRecall      float64 `json:"method_recall"`
	SafetyPrecision   float64 `json:"safety_precision"`
	BehaviorRecall    float64 `json:"behavior_recall"`
	MeanConsistency   float64 `json:"mean_consistency"`
	MeanReward        float64 `json:"mean_reward"`
	PassAtK           float64 `json:"pass_at_k"`
	PassPowerK        float64 `json:"pass_power_k"`
	// IntentRecall is the benchmark's headline: business needs the agent planned
	// and carried out itself. ExecutionRecall covers the twins that hand over the
	// finished operation and is instrumentation — reported, never gated.
	IntentRecall    float64 `json:"intent_recall"`
	ExecutionRecall float64 `json:"execution_recall"`
	IntentTasks     int     `json:"intent_tasks"`
	ExecutionTasks  int     `json:"execution_tasks"`
	// PlanningGap counts needs whose execution twin passed while the intent task
	// failed: the agent can perform the operation but did not work out that it was
	// required. ExecutionGap is the inverse and usually means the twin is
	// under-specified rather than that the agent improvised well.
	PlanningGap        int                        `json:"planning_gap"`
	ExecutionGap       int                        `json:"execution_gap"`
	RecallCI           ConfidenceInterval         `json:"recall_ci"`
	ByTier             map[Difficulty]TierMetrics `json:"by_tier,omitempty"`
	ByCategory         map[Category]TierMetrics   `json:"by_category,omitempty"`
	ByRollup           map[string]TierMetrics     `json:"by_rollup,omitempty"`
	FailureCategories  map[string]int             `json:"failure_categories,omitempty"`
	EnvironmentErrors  int                        `json:"environment_errors,omitempty"`
	GuardInterventions int                        `json:"guard_interventions,omitempty"`
	ForbiddenAttempts  int                        `json:"forbidden_attempts,omitempty"`
	UnsafeEffects      int                        `json:"unsafe_effects"`
	PromptTokens       int64                      `json:"prompt_tokens"`
	CompletionTokens   int64                      `json:"completion_tokens"`
	TotalTokens        int64                      `json:"total_tokens"`
	LLMCalls           int64                      `json:"llm_calls"`
	MedianActorTurns   float64                    `json:"median_actor_turns"`
	LatencyP50MS       float64                    `json:"latency_p50_ms"`
	LatencyP95MS       float64                    `json:"latency_p95_ms"`
}

type Acceptance struct {
	SuiteValid             bool     `json:"suite_valid"`
	SafetyPass             bool     `json:"safety_pass"`
	NoRegression           bool     `json:"no_regression"`
	HardPass               bool     `json:"hard_pass"`
	ScoringSuspect         bool     `json:"scoring_suspect,omitempty"`
	BaselineCompared       bool     `json:"baseline_compared"`
	ValueComparisonEnabled bool     `json:"value_comparison_enabled"`
	EnvironmentFailure     bool     `json:"environment_failure,omitempty"`
	IntersectionTaskCount  int      `json:"intersection_task_count,omitempty"`
	Notices                []string `json:"notices,omitempty"`
}

const ScoringDivergenceThreshold = 0.30

// ScoringDivergence returns the gap between reliable answer correctness and
// reliable method correctness. A large positive gap is evidence that the
// generated method contract or scorer may not match the runtime dialect.
func ScoringDivergence(metrics Metrics) float64 {
	return metrics.GroundTruthRecall - metrics.MethodRecall
}

func IsScoringDivergenceSuspect(metrics Metrics) bool {
	return ScoringDivergence(metrics) > ScoringDivergenceThreshold+1e-9
}

type Report struct {
	SchemaVersion          string             `json:"schema_version"`
	UsageAccountingVersion string             `json:"usage_accounting_version,omitempty"`
	RewardVersion          string             `json:"reward_version"`
	RunID                  string             `json:"run_id"`
	RescoredFrom           string             `json:"rescored_from,omitempty"`
	ScoringProvenance      *ScoringProvenance `json:"scoring_provenance,omitempty"`
	RunStatus              RunStatus          `json:"run_status"`
	Mode                   RunMode            `json:"mode"`
	GeneratedAt            time.Time          `json:"generated_at"`
	SuiteFingerprint       string             `json:"suite_fingerprint"`
	CatalogFingerprint     string             `json:"catalog_fingerprint"`
	DatasetFingerprint     DatasetFingerprint `json:"dataset_fingerprint"`
	OracleValueHash        string             `json:"oracle_value_hash,omitempty"`
	Provenance             RunProvenance      `json:"provenance"`
	Progress               RunProgress        `json:"progress"`
	ProviderUsage          ProviderUsage      `json:"provider_usage"`
	UsageComparison        *UsageComparison   `json:"usage_comparison,omitempty"`
	Metrics                Metrics            `json:"metrics"`
	Tasks                  []TaskVerdict      `json:"tasks"`
	Acceptance             Acceptance         `json:"acceptance"`
	InvalidOracles         map[string]string  `json:"invalid_oracles,omitempty"`
	InvalidOracleDetails   map[string]string  `json:"-"`
	EpisodePaths           []string           `json:"-"`
}

// PartialReport is written when a run is interrupted or the provider
// environment fails. It intentionally has no metrics, task verdicts, or
// baseline acceptance fields.
type PartialReport struct {
	SchemaVersion          string             `json:"schema_version"`
	UsageAccountingVersion string             `json:"usage_accounting_version,omitempty"`
	RewardVersion          string             `json:"reward_version"`
	RunID                  string             `json:"run_id"`
	RunStatus              RunStatus          `json:"run_status"`
	Mode                   RunMode            `json:"mode"`
	GeneratedAt            time.Time          `json:"generated_at"`
	SuiteFingerprint       string             `json:"suite_fingerprint"`
	CatalogFingerprint     string             `json:"catalog_fingerprint"`
	DatasetFingerprint     DatasetFingerprint `json:"dataset_fingerprint"`
	OracleValueHash        string             `json:"oracle_value_hash,omitempty"`
	Provenance             RunProvenance      `json:"provenance"`
	Progress               RunProgress        `json:"progress"`
	ProviderUsage          ProviderUsage      `json:"provider_usage"`
	EnvironmentCode        string             `json:"environment_code,omitempty"`
	Notice                 string             `json:"notice,omitempty"`
}

func (r Report) TaskMap() map[string]TaskVerdict {
	out := make(map[string]TaskVerdict, len(r.Tasks))
	for _, task := range r.Tasks {
		out[task.TaskID] = task
	}
	return out
}
