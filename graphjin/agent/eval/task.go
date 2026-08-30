// Package eval provides GraphJin's versioned evaluation and benchmark engine.
//
// The package deliberately talks to GraphJin only through its public HTTP
// surfaces. That keeps task generation, verification, and rollouts on the same
// path used in production and makes the package suitable for a future remote
// rollout service without coupling it to serv internals.
package eval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	gjagent "github.com/dosco/graphjin/agent/v3"
)

const (
	TaskSchemaVersion      = "graphjin.eval.task/v1"
	SuiteSchemaVersion     = "graphjin.eval.suite/v1"
	EpisodeSchemaVersion   = "graphjin.eval.episode/v2"
	ReportSchemaVersion    = "graphjin.eval.report/v3"
	ReportV2Version        = "graphjin.eval.report/v2"
	LegacyReportVersion    = "graphjin.eval.report/v1"
	UsageAccountingVersion = "graphjin.eval.usage/v2"
	RunManifestVersion     = "graphjin.eval.run/v1"
	AttemptSchemaVersion   = "graphjin.eval.attempt/v1"
	// GeneratorVersion is the generated task/scoring contract. Bump it whenever
	// generated task semantics change, including method-rule dialect support.
	//
	// v13 adds the family registry and three read families — filtered
	// aggregates over observed values, relationship traversals with join-count
	// oracles, and windows composed with a value filter. Existing task content
	// is untouched, so content IDs are unchanged; what changes is that a freshly
	// generated suite draws from a wider candidate pool.
	GeneratorVersion = "graphjin.eval.generator/v13"
	// RewardVersion v5: a "which record" answer may name the row by any
	// identifier the oracle selected, not only the one field it projected. No
	// frozen task declares alternates yet, so this changes no existing verdict;
	// it is the scoring half of the fix, landed ahead of the suite regeneration
	// that would let tasks project a second identifier.
	RewardVersion         = "graphjin.eval.reward/v5"
	DefaultSuiteSize      = 24
	DefaultRepeats        = 3
	DefaultEvaluationDir  = "eval"
	DefaultSuiteFilename  = "suite.yml"
	DefaultStateDir       = ".graphjin-evals"
	defaultMaxActorTurns  = int64(10)
	defaultMaxTotalTokens = int64(400000)
)

// SupportedGeneratorVersions are the suite generator contracts this binary can
// run. A published suite is a frozen artifact, so adding a generator version
// must not strand the cohorts already measured against the previous one: the
// binary generates at GeneratorVersion and runs anything in this set.
var SupportedGeneratorVersions = []string{
	"graphjin.eval.generator/v12",
	GeneratorVersion,
}

func IsSupportedGeneratorVersion(version string) bool {
	for _, supported := range SupportedGeneratorVersions {
		if version == supported {
			return true
		}
	}
	return false
}

type Category string

const (
	CategoryDiscovery   Category = "discovery"
	CategoryAggregate   Category = "aggregate"
	CategoryRanking     Category = "ranking"
	CategoryWindow      Category = "window"
	CategoryTraversal   Category = "traversal"
	CategorySavedMetric Category = "saved-metric"
	CategoryRefusal     Category = "refusal"
	CategoryAction      Category = "action"
	CategoryReactive    Category = "reactive"
	CategoryMultiTurn   Category = "multi-turn"
	CategoryCrossSource Category = "cross-source"
)

// Task tiers. An empty tier reads as intent: the read-only families were always
// phrased as business questions.
const (
	TierIntent    = "intent"
	TierExecution = "execution"
)

type Difficulty string

const (
	DifficultyT1 Difficulty = "T1"
	DifficultyT2 Difficulty = "T2"
	DifficultyT3 Difficulty = "T3"
	DifficultyT4 Difficulty = "T4"
)

type Provenance struct {
	GeneratorVersion string `json:"generator_version,omitempty" yaml:"generator_version,omitempty"`
	Source           string `json:"source" yaml:"source"`
	Seed             int64  `json:"generation_seed,omitempty" yaml:"generation_seed,omitempty"`
	SourceID         string `json:"source_id,omitempty" yaml:"source_id,omitempty"`
}

type CapabilityProfile struct {
	RoleClass            string   `json:"role_class,omitempty" yaml:"role_class,omitempty"`
	ReadOnly             bool     `json:"read_only,omitempty" yaml:"read_only,omitempty"`
	AllowedActions       []string `json:"allowed_actions,omitempty" yaml:"allowed_actions,omitempty"`
	AvailableSystemRoots []string `json:"available_system_roots,omitempty" yaml:"available_system_roots,omitempty"`
}

type Task struct {
	SchemaVersion string     `json:"schema_version" yaml:"schema_version"`
	ID            string     `json:"id" yaml:"id"`
	Slug          string     `json:"slug" yaml:"slug"`
	Category      Category   `json:"category" yaml:"category"`
	Difficulty    Difficulty `json:"difficulty" yaml:"difficulty"`
	// Tier separates what the benchmark actually claims to measure from its
	// instrumentation. TierIntent tasks phrase a business need the way a caller
	// really states it and require the agent to plan the translation; TierExecution
	// twins hand it the finished operation so a failure can be attributed to
	// execution rather than planning. Anyone who could speak operationally learned
	// the vocabulary from GraphJin's catalog in-session, so intent is the only
	// phrasing that crosses the natural-language boundary in practice.
	Tier string `json:"tier,omitempty" yaml:"tier,omitempty"`
	// NeedID pairs an intent task with its execution twin over one underlying
	// need, so "twin passes, intent fails" reads as a planning gap.
	NeedID            string            `json:"need_id,omitempty" yaml:"need_id,omitempty"`
	Prompt            string            `json:"prompt" yaml:"prompt"`
	Provenance        Provenance        `json:"provenance" yaml:"provenance"`
	CapabilityProfile CapabilityProfile `json:"capability_profile" yaml:"capability_profile"`
	ExpectedStatus    string            `json:"expected_status" yaml:"expected_status"`
	Oracle            *OracleSpec       `json:"oracle,omitempty" yaml:"oracle,omitempty"`
	Answer            AnswerRule        `json:"answer,omitempty" yaml:"answer,omitempty"`
	Method            MethodRule        `json:"method,omitempty" yaml:"method,omitempty"`
	Behavior          BehaviorRule      `json:"behavior,omitempty" yaml:"behavior,omitempty"`
	Budget            Budget            `json:"budget,omitempty" yaml:"budget,omitempty"`

	Turns    []TurnSpec    `json:"turns,omitempty" yaml:"turns,omitempty"`
	Mutation *MutationSpec `json:"mutation,omitempty" yaml:"mutation,omitempty"`
}

type TurnSpec struct {
	Role    string `json:"role" yaml:"role"`
	Content string `json:"content" yaml:"content"`
}

type MutationSpec struct {
	ResetStrategy     string        `json:"reset_strategy" yaml:"reset_strategy"`
	Setup             []GraphQLStep `json:"setup,omitempty" yaml:"setup,omitempty"`
	ReadyState        *OracleSpec   `json:"ready_state,omitempty" yaml:"ready_state,omitempty"`
	ReadyValue        string        `json:"ready_value,omitempty" yaml:"ready_value,omitempty"`
	ReadyTimeoutMS    int64         `json:"ready_timeout_ms,omitempty" yaml:"ready_timeout_ms,omitempty"`
	PostState         OracleSpec    `json:"post_state" yaml:"post_state"`
	ExpectedValue     string        `json:"expected_value" yaml:"expected_value"`
	ExpectedDimension string        `json:"expected_dimension,omitempty" yaml:"expected_dimension,omitempty"`
	// AcceptedValues and AcceptedDimensions widen post-state acceptance for intent
	// tasks, where several results are equally correct. Prefer encoding the
	// requirement in the post-state WHERE clause and expecting exactly one match;
	// reach for these only where a filter cannot express the choice, such as a
	// digest window the caller bounded rather than dictated.
	AcceptedValues     []string     `json:"accepted_values,omitempty" yaml:"accepted_values,omitempty"`
	AcceptedDimensions []string     `json:"accepted_dimensions,omitempty" yaml:"accepted_dimensions,omitempty"`
	Collateral         []OracleSpec `json:"collateral,omitempty" yaml:"collateral,omitempty"`
}

// GraphQLStep is trusted environment setup performed after an episode reset
// and before the model sees the task. It is deliberately kept out of the
// action trail so only the model's own method is scored.
type GraphQLStep struct {
	Query       string         `json:"query" yaml:"query"`
	Variables   map[string]any `json:"variables,omitempty" yaml:"variables,omitempty"`
	WaitAfterMS int64          `json:"wait_after_ms,omitempty" yaml:"wait_after_ms,omitempty"`
}

type OracleSpec struct {
	Query            string         `json:"query" yaml:"query"`
	Variables        map[string]any `json:"variables,omitempty" yaml:"variables,omitempty"`
	Extract          string         `json:"extract,omitempty" yaml:"extract,omitempty"`
	DimensionExtract string         `json:"dimension_extract,omitempty" yaml:"dimension_extract,omitempty"`
	// DimensionAlternateExtracts name the same row by another field the query
	// selects — typically its primary key. Any one of them satisfies the
	// dimension check, because "which record" has more than one right answer.
	DimensionAlternateExtracts []string     `json:"dimension_alternate_extracts,omitempty" yaml:"dimension_alternate_extracts,omitempty"`
	DimensionLiteral           string       `json:"dimension_literal,omitempty" yaml:"dimension_literal,omitempty"`
	PickMax                    *PickMaxRule `json:"pick_max,omitempty" yaml:"pick_max,omitempty"`
	AnchorQuery                string       `json:"anchor_query,omitempty" yaml:"anchor_query,omitempty"`
	AnchorExtract              string       `json:"anchor_extract,omitempty" yaml:"anchor_extract,omitempty"`
	AllowMissing               bool         `json:"allow_missing,omitempty" yaml:"allow_missing,omitempty"`
}

type PickMaxRule struct {
	List      string `json:"list" yaml:"list"`
	Value     string `json:"value" yaml:"value"`
	Dimension string `json:"dimension" yaml:"dimension"`
}

type AnswerRule struct {
	Kind             string    `json:"kind,omitempty" yaml:"kind,omitempty"`
	ExtractRegex     string    `json:"extract_regex,omitempty" yaml:"extract_regex,omitempty"`
	FromData         string    `json:"from_data,omitempty" yaml:"from_data,omitempty"`
	TolerancePct     float64   `json:"tolerance_pct,omitempty" yaml:"tolerance_pct,omitempty"`
	AcceptScales     []float64 `json:"accept_scales,omitempty" yaml:"accept_scales,omitempty"`
	ForbiddenPhrases []string  `json:"forbidden_phrases,omitempty" yaml:"forbidden_phrases,omitempty"`
}

type MethodRule struct {
	RequireQueryMatch          []string `json:"require_query_match,omitempty" yaml:"require_query_match,omitempty"`
	ForbidQueryMatch           []string `json:"forbid_query_match,omitempty" yaml:"forbid_query_match,omitempty"`
	ForbidFinalizeFromListOnly bool     `json:"forbid_finalize_from_list_only,omitempty" yaml:"forbid_finalize_from_list_only,omitempty"`
	RequireTools               []string `json:"require_tools,omitempty" yaml:"require_tools,omitempty"`
	ForbidTools                []string `json:"forbid_tools,omitempty" yaml:"forbid_tools,omitempty"`
}

type BehaviorRule struct {
	RequiredActions       []string `json:"required_actions,omitempty" yaml:"required_actions,omitempty"`
	ForbiddenActions      []string `json:"forbidden_actions,omitempty" yaml:"forbidden_actions,omitempty"`
	ExpectedUsedSkills    []string `json:"expected_used_skills,omitempty" yaml:"expected_used_skills,omitempty"`
	ForbiddenUsedSkills   []string `json:"forbidden_used_skills,omitempty" yaml:"forbidden_used_skills,omitempty"`
	ExpectedLoadedSkills  []string `json:"expected_loaded_skills,omitempty" yaml:"expected_loaded_skills,omitempty"`
	ForbiddenLoadedSkills []string `json:"forbidden_loaded_skills,omitempty" yaml:"forbidden_loaded_skills,omitempty"`
}

type Budget struct {
	MaxActorTurns  int64 `json:"max_actor_turns,omitempty" yaml:"max_actor_turns,omitempty"`
	MaxTotalTokens int64 `json:"max_total_tokens,omitempty" yaml:"max_total_tokens,omitempty"`
	MaxLatencyMS   int64 `json:"max_latency_ms,omitempty" yaml:"max_latency_ms,omitempty"`
}

func (t *Task) Normalize() error {
	if t == nil {
		return errors.New("nil task")
	}
	t.SchemaVersion = TaskSchemaVersion
	t.Prompt = strings.TrimSpace(t.Prompt)
	t.ExpectedStatus = strings.TrimSpace(t.ExpectedStatus)
	t.Slug = slugify(t.Slug)
	if t.Slug == "" {
		t.Slug = slugify(string(t.Category) + "-" + t.Prompt)
	}
	if t.Budget.MaxActorTurns == 0 {
		t.Budget.MaxActorTurns = defaultMaxActorTurns
	}
	if t.Budget.MaxTotalTokens == 0 {
		t.Budget.MaxTotalTokens = defaultMaxTotalTokens
	}
	if t.Provenance.GeneratorVersion == "" && t.Provenance.Source != "user-added" && t.Provenance.Source != "imported" {
		t.Provenance.GeneratorVersion = GeneratorVersion
	}
	t.CapabilityProfile.AvailableSystemRoots = sortedUnique(t.CapabilityProfile.AvailableSystemRoots)
	t.CapabilityProfile.AllowedActions = sortedUnique(t.CapabilityProfile.AllowedActions)
	t.Method.RequireQueryMatch = sortedUnique(t.Method.RequireQueryMatch)
	t.Method.ForbidQueryMatch = sortedUnique(t.Method.ForbidQueryMatch)
	t.Method.RequireTools = sortedUnique(t.Method.RequireTools)
	t.Method.ForbidTools = sortedUnique(t.Method.ForbidTools)
	t.Answer.ForbiddenPhrases = sortedUnique(t.Answer.ForbiddenPhrases)
	sort.Float64s(t.Answer.AcceptScales)
	t.Behavior.RequiredActions = sortedUnique(t.Behavior.RequiredActions)
	t.Behavior.ForbiddenActions = sortedUnique(t.Behavior.ForbiddenActions)
	t.Behavior.ExpectedUsedSkills = sortedUnique(t.Behavior.ExpectedUsedSkills)
	t.Behavior.ForbiddenUsedSkills = sortedUnique(t.Behavior.ForbiddenUsedSkills)
	t.Behavior.ExpectedLoadedSkills = sortedUnique(t.Behavior.ExpectedLoadedSkills)
	t.Behavior.ForbiddenLoadedSkills = sortedUnique(t.Behavior.ForbiddenLoadedSkills)
	for i := range t.Turns {
		t.Turns[i].Role = strings.ToLower(strings.TrimSpace(t.Turns[i].Role))
		t.Turns[i].Content = strings.TrimSpace(t.Turns[i].Content)
	}
	if t.Mutation != nil {
		t.Mutation.ResetStrategy = strings.ToLower(strings.TrimSpace(t.Mutation.ResetStrategy))
		t.Mutation.ExpectedValue = strings.TrimSpace(t.Mutation.ExpectedValue)
		t.Mutation.ExpectedDimension = strings.TrimSpace(t.Mutation.ExpectedDimension)
	}
	if err := t.validateShape(); err != nil {
		return err
	}
	id, err := t.ContentID()
	if err != nil {
		return err
	}
	t.ID = id
	return nil
}

func (t Task) Validate() error {
	if err := t.validateShape(); err != nil {
		return err
	}
	want, err := t.ContentID()
	if err != nil {
		return err
	}
	if t.ID != want {
		return fmt.Errorf("task %q id is not its content hash (want %s)", t.Slug, want)
	}
	return nil
}

func (t Task) validateShape() error {
	if t.SchemaVersion != TaskSchemaVersion {
		return fmt.Errorf("unsupported task schema_version %q", t.SchemaVersion)
	}
	if t.Prompt == "" || t.Slug == "" || t.ExpectedStatus == "" {
		return errors.New("task needs slug, prompt, and expected_status")
	}
	if !validCategory(t.Category) {
		return fmt.Errorf("task %q has invalid category %q", t.Slug, t.Category)
	}
	if !validDifficulty(t.Difficulty) {
		return fmt.Errorf("task %q has invalid difficulty %q", t.Slug, t.Difficulty)
	}
	// A percentage over 100 is a unit mistake, not an intent: it would accept
	// numbers further from the answer than the answer itself. Catching it at
	// load keeps a mis-typed tolerance from silently certifying wrong answers
	// for a whole run.
	if t.Answer.TolerancePct < 0 || t.Answer.TolerancePct > 100 {
		return fmt.Errorf("task %q has tolerance_pct %g; it is a percentage and must be between 0 and 100", t.Slug, t.Answer.TolerancePct)
	}
	for index, turn := range t.Turns {
		if turn.Role != "user" && turn.Role != "assistant" {
			return fmt.Errorf("task %q turn %d has invalid role %q", t.Slug, index+1, turn.Role)
		}
		if strings.TrimSpace(turn.Content) == "" {
			return fmt.Errorf("task %q turn %d has empty content", t.Slug, index+1)
		}
	}
	if t.Mutation != nil {
		if t.Mutation.ResetStrategy != "sqlite-copy" {
			return fmt.Errorf("task %q mutation reset_strategy must be sqlite-copy", t.Slug)
		}
		if strings.TrimSpace(t.Mutation.ExpectedValue) == "" {
			return fmt.Errorf("task %q mutation needs expected_value", t.Slug)
		}
		if err := t.Mutation.PostState.Validate(); err != nil {
			return fmt.Errorf("task %q mutation post_state: %w", t.Slug, err)
		}
		if t.Mutation.ExpectedDimension != "" && t.Mutation.PostState.DimensionExtract == "" {
			return fmt.Errorf("task %q mutation expected_dimension needs post_state dimension_extract", t.Slug)
		}
		for index, step := range t.Mutation.Setup {
			if err := step.Validate(); err != nil {
				return fmt.Errorf("task %q mutation setup %d: %w", t.Slug, index+1, err)
			}
		}
		if t.Mutation.ReadyState != nil {
			if err := t.Mutation.ReadyState.Validate(); err != nil {
				return fmt.Errorf("task %q mutation ready_state: %w", t.Slug, err)
			}
			if strings.TrimSpace(t.Mutation.ReadyValue) == "" {
				return fmt.Errorf("task %q mutation ready_state needs ready_value", t.Slug)
			}
		}
		for index, collateral := range t.Mutation.Collateral {
			if err := collateral.Validate(); err != nil {
				return fmt.Errorf("task %q mutation collateral %d: %w", t.Slug, index+1, err)
			}
		}
	}
	if t.Oracle != nil {
		if err := t.Oracle.Validate(); err != nil {
			return fmt.Errorf("task %q: %w", t.Slug, err)
		}
	}
	for _, pattern := range append(append([]string(nil), t.Method.RequireQueryMatch...), t.Method.ForbidQueryMatch...) {
		if _, err := regexp.Compile(pattern); err != nil {
			return fmt.Errorf("task %q has invalid method regex %q: %w", t.Slug, pattern, err)
		}
	}
	if t.Answer.ExtractRegex != "" {
		if _, err := regexp.Compile(t.Answer.ExtractRegex); err != nil {
			return fmt.Errorf("task %q has invalid answer regex: %w", t.Slug, err)
		}
	}
	if t.Provenance.Source == "" {
		return fmt.Errorf("task %q needs provenance.source", t.Slug)
	}
	return nil
}

func (s GraphQLStep) Validate() error {
	if strings.TrimSpace(s.Query) == "" {
		return errors.New("setup step needs query")
	}
	if !gjagent.ContainsMutationOperation(s.Query) {
		return errors.New("setup step must be a GraphQL mutation")
	}
	if s.WaitAfterMS < 0 || s.WaitAfterMS > 30000 {
		return errors.New("setup step wait_after_ms must be between 0 and 30000")
	}
	return nil
}

func (o OracleSpec) Validate() error {
	if strings.TrimSpace(o.Query) == "" {
		return errors.New("oracle needs query")
	}
	if !readOnlyGraphQL(o.Query) || (o.AnchorQuery != "" && !readOnlyGraphQL(o.AnchorQuery)) {
		return errors.New("oracle must be a read-only GraphQL query")
	}
	if strings.TrimSpace(o.Extract) == "" && o.PickMax == nil {
		return errors.New("oracle needs extract or pick_max")
	}
	if o.AnchorQuery != "" && strings.TrimSpace(o.AnchorExtract) == "" {
		return errors.New("oracle anchor_query needs anchor_extract")
	}
	if o.DimensionExtract != "" && o.DimensionLiteral != "" {
		return errors.New("oracle cannot use both dimension_extract and dimension_literal")
	}
	if o.PickMax != nil && (o.PickMax.List == "" || o.PickMax.Value == "" || o.PickMax.Dimension == "") {
		return errors.New("oracle pick_max needs list, value, and dimension")
	}
	return nil
}

func readOnlyGraphQL(query string) bool {
	if gjagent.ContainsMutationOperation(query) {
		return false
	}
	q := strings.TrimSpace(stripGraphQLComments(query))
	q = strings.TrimPrefix(q, "\ufeff")
	fields := strings.Fields(q)
	if len(fields) == 0 {
		return false
	}
	word := strings.ToLower(fields[0])
	return word == "query" || strings.HasPrefix(q, "{")
}

func stripGraphQLComments(query string) string {
	lines := strings.Split(query, "\n")
	for i, line := range lines {
		if at := strings.IndexByte(line, '#'); at >= 0 {
			lines[i] = line[:at]
		}
	}
	return strings.Join(lines, "\n")
}

func (t Task) ContentID() (string, error) {
	canonical := t
	canonical.SchemaVersion = TaskSchemaVersion
	canonical.CapabilityProfile.AvailableSystemRoots = sortedUnique(canonical.CapabilityProfile.AvailableSystemRoots)
	canonical.CapabilityProfile.AllowedActions = sortedUnique(canonical.CapabilityProfile.AllowedActions)
	canonical.Method.RequireQueryMatch = sortedUnique(canonical.Method.RequireQueryMatch)
	canonical.Method.ForbidQueryMatch = sortedUnique(canonical.Method.ForbidQueryMatch)
	canonical.Method.RequireTools = sortedUnique(canonical.Method.RequireTools)
	canonical.Method.ForbidTools = sortedUnique(canonical.Method.ForbidTools)
	canonical.Answer.ForbiddenPhrases = sortedUnique(canonical.Answer.ForbiddenPhrases)
	canonical.Answer.AcceptScales = append([]float64(nil), canonical.Answer.AcceptScales...)
	sort.Float64s(canonical.Answer.AcceptScales)
	canonical.Behavior.RequiredActions = sortedUnique(canonical.Behavior.RequiredActions)
	canonical.Behavior.ForbiddenActions = sortedUnique(canonical.Behavior.ForbiddenActions)
	canonical.Behavior.ExpectedUsedSkills = sortedUnique(canonical.Behavior.ExpectedUsedSkills)
	canonical.Behavior.ForbiddenUsedSkills = sortedUnique(canonical.Behavior.ForbiddenUsedSkills)
	canonical.Behavior.ExpectedLoadedSkills = sortedUnique(canonical.Behavior.ExpectedLoadedSkills)
	canonical.Behavior.ForbiddenLoadedSkills = sortedUnique(canonical.Behavior.ForbiddenLoadedSkills)
	// IDs identify the behavior being measured, not how or when the task was
	// generated. Slugs and provenance remain useful metadata but must not reset
	// baseline intersections when a seed or generator version changes.
	content := struct {
		SchemaVersion     string            `json:"schema_version"`
		Category          Category          `json:"category"`
		Difficulty        Difficulty        `json:"difficulty"`
		Prompt            string            `json:"prompt"`
		CapabilityProfile CapabilityProfile `json:"capability_profile"`
		ExpectedStatus    string            `json:"expected_status"`
		Oracle            *OracleSpec       `json:"oracle,omitempty"`
		Answer            AnswerRule        `json:"answer,omitempty"`
		Method            MethodRule        `json:"method,omitempty"`
		Behavior          BehaviorRule      `json:"behavior,omitempty"`
		Budget            Budget            `json:"budget,omitempty"`
		Turns             []TurnSpec        `json:"turns,omitempty"`
		Mutation          *MutationSpec     `json:"mutation,omitempty"`
	}{
		SchemaVersion: canonical.SchemaVersion, Category: canonical.Category, Difficulty: canonical.Difficulty,
		Prompt: canonical.Prompt, CapabilityProfile: canonical.CapabilityProfile, ExpectedStatus: canonical.ExpectedStatus,
		Oracle: canonical.Oracle, Answer: canonical.Answer, Method: canonical.Method, Behavior: canonical.Behavior, Budget: canonical.Budget,
		Turns: canonical.Turns, Mutation: canonical.Mutation,
	}
	data, err := json.Marshal(content)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "gjv1_" + hex.EncodeToString(sum[:12]), nil
}

func validCategory(value Category) bool {
	switch value {
	case CategoryDiscovery, CategoryAggregate, CategoryRanking, CategoryWindow, CategoryTraversal, CategorySavedMetric, CategoryRefusal,
		CategoryAction, CategoryReactive, CategoryMultiTurn, CategoryCrossSource:
		return true
	default:
		return false
	}
}

func validDifficulty(value Difficulty) bool {
	switch value {
	case DifficultyT1, DifficultyT2, DifficultyT3, DifficultyT4:
		return true
	default:
		return false
	}
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = nonSlug.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	if len(value) > 72 {
		value = strings.Trim(value[:72], "-")
	}
	return value
}

func sortedUnique(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	write := 0
	for _, value := range out {
		value = strings.TrimSpace(value)
		if value == "" || (write > 0 && out[write-1] == value) {
			continue
		}
		out[write] = value
		write++
	}
	return out[:write]
}

// IsExecutionTwin reports whether a task hands the agent a finished operation.
// Execution twins are instrumentation: they are reported, never gated.
func (t Task) IsExecutionTwin() bool { return t.Tier == TierExecution }

// AcceptsValue reports whether a post-state value satisfies the spec. Exact
// ExpectedValue remains the contract; AcceptedValues only widens it.
func (m MutationSpec) AcceptsValue(value string) bool {
	if value == m.ExpectedValue {
		return true
	}
	return containsExactString(m.AcceptedValues, value)
}

// AcceptsDimension reports whether a post-state dimension satisfies the spec. An
// empty expectation with no accepted set means the dimension is not checked.
func (m MutationSpec) AcceptsDimension(dimension string) bool {
	if m.ExpectedDimension == "" && len(m.AcceptedDimensions) == 0 {
		return true
	}
	if m.ExpectedDimension != "" && dimension == m.ExpectedDimension {
		return true
	}
	return containsExactString(m.AcceptedDimensions, dimension)
}

func containsExactString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
