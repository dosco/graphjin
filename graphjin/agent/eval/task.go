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
	GeneratorVersion       = "graphjin.eval.generator/v4"
	RewardVersion          = "graphjin.eval.reward/v2"
	DefaultSuiteSize       = 24
	DefaultRepeats         = 3
	DefaultEvaluationDir   = "eval"
	DefaultSuiteFilename   = "suite.yml"
	DefaultStateDir        = ".graphjin-evals"
	defaultMaxActorTurns   = int64(10)
	defaultMaxTotalTokens  = int64(400000)
)

type Category string

const (
	CategoryDiscovery   Category = "discovery"
	CategoryAggregate   Category = "aggregate"
	CategoryRanking     Category = "ranking"
	CategoryWindow      Category = "window"
	CategoryTraversal   Category = "traversal"
	CategorySavedMetric Category = "saved-metric"
	CategoryRefusal     Category = "refusal"
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
	AvailableSystemRoots []string `json:"available_system_roots,omitempty" yaml:"available_system_roots,omitempty"`
}

type Task struct {
	SchemaVersion     string            `json:"schema_version" yaml:"schema_version"`
	ID                string            `json:"id" yaml:"id"`
	Slug              string            `json:"slug" yaml:"slug"`
	Category          Category          `json:"category" yaml:"category"`
	Difficulty        Difficulty        `json:"difficulty" yaml:"difficulty"`
	Prompt            string            `json:"prompt" yaml:"prompt"`
	Provenance        Provenance        `json:"provenance" yaml:"provenance"`
	CapabilityProfile CapabilityProfile `json:"capability_profile" yaml:"capability_profile"`
	ExpectedStatus    string            `json:"expected_status" yaml:"expected_status"`
	Oracle            *OracleSpec       `json:"oracle,omitempty" yaml:"oracle,omitempty"`
	Answer            AnswerRule        `json:"answer,omitempty" yaml:"answer,omitempty"`
	Method            MethodRule        `json:"method,omitempty" yaml:"method,omitempty"`
	Behavior          BehaviorRule      `json:"behavior,omitempty" yaml:"behavior,omitempty"`
	Budget            Budget            `json:"budget,omitempty" yaml:"budget,omitempty"`

	// Turns and Mutation are v1 schema reservations. The v1 runner rejects
	// populated values instead of pretending to implement resettable multi-turn
	// or mutation environments.
	Turns    []TurnSpec    `json:"turns,omitempty" yaml:"turns,omitempty"`
	Mutation *MutationSpec `json:"mutation,omitempty" yaml:"mutation,omitempty"`
}

type TurnSpec struct {
	Role    string `json:"role" yaml:"role"`
	Content string `json:"content" yaml:"content"`
}

type MutationSpec struct {
	ResetStrategy string `json:"reset_strategy,omitempty" yaml:"reset_strategy,omitempty"`
}

type OracleSpec struct {
	Query            string         `json:"query" yaml:"query"`
	Variables        map[string]any `json:"variables,omitempty" yaml:"variables,omitempty"`
	Extract          string         `json:"extract,omitempty" yaml:"extract,omitempty"`
	DimensionExtract string         `json:"dimension_extract,omitempty" yaml:"dimension_extract,omitempty"`
	PickMax          *PickMaxRule   `json:"pick_max,omitempty" yaml:"pick_max,omitempty"`
	AnchorQuery      string         `json:"anchor_query,omitempty" yaml:"anchor_query,omitempty"`
	AnchorExtract    string         `json:"anchor_extract,omitempty" yaml:"anchor_extract,omitempty"`
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
	if len(t.Turns) != 0 || t.Mutation != nil {
		return fmt.Errorf("task %q uses reserved v2 turns or mutation fields", t.Slug)
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
	}{
		SchemaVersion: canonical.SchemaVersion, Category: canonical.Category, Difficulty: canonical.Difficulty,
		Prompt: canonical.Prompt, CapabilityProfile: canonical.CapabilityProfile, ExpectedStatus: canonical.ExpectedStatus,
		Oracle: canonical.Oracle, Answer: canonical.Answer, Method: canonical.Method, Behavior: canonical.Behavior, Budget: canonical.Budget,
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
	case CategoryDiscovery, CategoryAggregate, CategoryRanking, CategoryWindow, CategoryTraversal, CategorySavedMetric, CategoryRefusal:
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
