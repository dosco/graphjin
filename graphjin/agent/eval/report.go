package eval

import "time"

type RunMode string

const (
	RunModeRun       RunMode = "run"
	RunModeBenchmark RunMode = "bench"
)

type RunProvenance struct {
	Model              string  `json:"model,omitempty"`
	AxVersion          string  `json:"ax_version,omitempty"`
	GraphJinCommit     string  `json:"graphjin_commit,omitempty"`
	PromptRegistryHash string  `json:"prompt_registry_hash,omitempty"`
	Temperature        float64 `json:"temperature"`
	Seed               int64   `json:"seed"`
	Repeats            int     `json:"repeats"`
	Target             string  `json:"target"`
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
	Score         ScoreDetail        `json:"score"`
	HTTPStatus    int                `json:"http_status,omitempty"`
	LatencyMS     int64              `json:"latency_ms"`
	Error         string             `json:"error,omitempty"`
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
	TaskID              string     `json:"task_id"`
	Slug                string     `json:"-"`
	Category            Category   `json:"category"`
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
	TaskCount         int                        `json:"task_count"`
	EpisodeCount      int                        `json:"episode_count"`
	Recall            float64                    `json:"recall"`
	GroundTruthRecall float64                    `json:"ground_truth_recall"`
	MethodRecall      float64                    `json:"method_recall"`
	SafetyPrecision   float64                    `json:"safety_precision"`
	BehaviorRecall    float64                    `json:"behavior_recall"`
	MeanConsistency   float64                    `json:"mean_consistency"`
	MeanReward        float64                    `json:"mean_reward"`
	PassAtK           float64                    `json:"pass_at_k"`
	PassPowerK        float64                    `json:"pass_power_k"`
	RecallCI          ConfidenceInterval         `json:"recall_ci"`
	ByTier            map[Difficulty]TierMetrics `json:"by_tier,omitempty"`
	FailureCategories map[string]int             `json:"failure_categories,omitempty"`
	PromptTokens      int64                      `json:"prompt_tokens"`
	CompletionTokens  int64                      `json:"completion_tokens"`
	TotalTokens       int64                      `json:"total_tokens"`
	MedianActorTurns  float64                    `json:"median_actor_turns"`
	LatencyP50MS      float64                    `json:"latency_p50_ms"`
	LatencyP95MS      float64                    `json:"latency_p95_ms"`
}

type Acceptance struct {
	SuiteValid             bool     `json:"suite_valid"`
	SafetyPass             bool     `json:"safety_pass"`
	NoRegression           bool     `json:"no_regression"`
	HardPass               bool     `json:"hard_pass"`
	BaselineCompared       bool     `json:"baseline_compared"`
	ValueComparisonEnabled bool     `json:"value_comparison_enabled"`
	IntersectionTaskCount  int      `json:"intersection_task_count,omitempty"`
	Notices                []string `json:"notices,omitempty"`
}

type Report struct {
	SchemaVersion        string             `json:"schema_version"`
	RewardVersion        string             `json:"reward_version"`
	RunID                string             `json:"run_id"`
	Mode                 RunMode            `json:"mode"`
	GeneratedAt          time.Time          `json:"generated_at"`
	SuiteFingerprint     string             `json:"suite_fingerprint"`
	CatalogFingerprint   string             `json:"catalog_fingerprint"`
	DatasetFingerprint   DatasetFingerprint `json:"dataset_fingerprint"`
	OracleValueHash      string             `json:"oracle_value_hash,omitempty"`
	Provenance           RunProvenance      `json:"provenance"`
	Metrics              Metrics            `json:"metrics"`
	Tasks                []TaskVerdict      `json:"tasks"`
	Acceptance           Acceptance         `json:"acceptance"`
	InvalidOracles       map[string]string  `json:"invalid_oracles,omitempty"`
	InvalidOracleDetails map[string]string  `json:"-"`
	EpisodePaths         []string           `json:"-"`
}

func (r Report) TaskMap() map[string]TaskVerdict {
	out := make(map[string]TaskVerdict, len(r.Tasks))
	for _, task := range r.Tasks {
		out[task.TaskID] = task
	}
	return out
}
