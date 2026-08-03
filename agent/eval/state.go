package eval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/gofrs/flock"
)

type RunIntent string

const (
	RunIntentRun      RunIntent = "run"
	RunIntentBaseline RunIntent = "baseline"
	RunIntentBench    RunIntent = "bench"
)

type ResumePolicy string

const (
	ResumeAuto  ResumePolicy = "auto"
	ResumeFresh ResumePolicy = "fresh"
	ResumeExact ResumePolicy = "exact"
)

type RunStatus string

const (
	RunStatusRunning           RunStatus = "running"
	RunStatusInterrupted       RunStatus = "interrupted"
	RunStatusEnvironmentFailed RunStatus = "environment_failed"
	RunStatusComplete          RunStatus = "complete"
)

type RunManifest struct {
	SchemaVersion         string             `json:"schema_version"`
	RunID                 string             `json:"run_id"`
	Intent                RunIntent          `json:"intent"`
	Mode                  RunMode            `json:"mode"`
	Status                RunStatus          `json:"status"`
	StartedAt             time.Time          `json:"started_at"`
	UpdatedAt             time.Time          `json:"updated_at"`
	SuiteFingerprint      string             `json:"suite_fingerprint"`
	CatalogFingerprint    string             `json:"catalog_fingerprint"`
	OracleValueHash       string             `json:"oracle_value_hash,omitempty"`
	DatasetFingerprint    DatasetFingerprint `json:"dataset_fingerprint"`
	TaskSchemaVersion     string             `json:"task_schema_version"`
	EpisodeSchemaVersion  string             `json:"episode_schema_version"`
	ReportSchemaVersion   string             `json:"report_schema_version"`
	RewardVersion         string             `json:"reward_version"`
	GeneratorVersion      string             `json:"generator_version"`
	BinaryFingerprint     string             `json:"binary_fingerprint"`
	ServerEvalFingerprint string             `json:"server_eval_fingerprint,omitempty"`
	Provenance            RunProvenance      `json:"provenance"`
	BaselineRunID         string             `json:"baseline_run_id,omitempty"`
	BaselineFingerprint   string             `json:"baseline_fingerprint,omitempty"`
	AutoBaseline          bool               `json:"auto_baseline,omitempty"`
	DeliberatePromotion   bool               `json:"deliberate_promotion,omitempty"`
	Progress              RunProgress        `json:"progress"`
	ProviderUsage         ProviderUsage      `json:"provider_usage"`
	ResumeCount           int                `json:"resume_count,omitempty"`
	InvocationArgs        []string           `json:"invocation_args,omitempty"`
	LastEnvironmentCode   string             `json:"last_environment_code,omitempty"`
}

func (m RunManifest) Complete() bool { return m.Status == RunStatusComplete }

func (m RunManifest) ResumeCommand() string {
	args := []string{"graphjin", "eval", string(m.Intent)}
	args = append(args, m.InvocationArgs...)
	args = append(args, "--resume", m.RunID, "--yes")
	return strings.Join(args, " ")
}

func (m RunManifest) RestartCommand() string {
	args := []string{"graphjin", "eval", string(m.Intent)}
	args = append(args, m.InvocationArgs...)
	args = append(args, "--restart", "--yes")
	return strings.Join(args, " ")
}

type TrafficPreview struct {
	RunID                     string `json:"run_id"`
	Resuming                  bool   `json:"resuming"`
	ReusedEpisodes            int    `json:"reused_episodes"`
	RemainingInitialSlots     int    `json:"remaining_initial_slots"`
	PossibleConfirmationSlots int    `json:"possible_confirmation_slots"`
	MaximumProviderAttempts   int    `json:"maximum_provider_attempts"`
	IgnoredIncompatibleRuns   int    `json:"ignored_incompatible_runs,omitempty"`
}

func (p TrafficPreview) String() string {
	action := "fresh run"
	if p.Resuming {
		action = fmt.Sprintf("resume %s with %d reusable episodes", p.RunID, p.ReusedEpisodes)
	}
	if p.IgnoredIncompatibleRuns != 0 {
		action += fmt.Sprintf("; ignored %d incompatible incomplete run(s)", p.IgnoredIncompatibleRuns)
	}
	return fmt.Sprintf("%s; %d initial slots remain, up to %d confirmation slots, and at most %d provider attempts including one transient retry per pending slot", action, p.RemainingInitialSlots, p.PossibleConfirmationSlots, p.MaximumProviderAttempts)
}

type Attempt struct {
	SchemaVersion string     `json:"schema_version"`
	RunID         string     `json:"run_id"`
	TaskID        string     `json:"task_id"`
	TaskSlug      string     `json:"task_slug"`
	Repeat        int        `json:"repeat"`
	Confirmation  bool       `json:"confirmation,omitempty"`
	Attempt       int        `json:"attempt"`
	StartedAt     time.Time  `json:"started_at"`
	CompletedAt   time.Time  `json:"completed_at"`
	HTTPStatus    int        `json:"http_status,omitempty"`
	LatencyMS     int64      `json:"latency_ms"`
	ErrorCode     string     `json:"error_code"`
	Retryable     bool       `json:"retryable"`
	Error         string     `json:"error,omitempty"`
	Response      any        `json:"response,omitempty"`
	Tokens        TokenUsage `json:"tokens"`
}

type RunLock struct {
	path string
	file *flock.Flock
}

func (l *RunLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	return l.file.Unlock()
}

func (s *Store) LockRun(_ context.Context, runID string) (*RunLock, error) {
	if !safeStoreComponent(runID) {
		return nil, fmt.Errorf("invalid run_id %q", runID)
	}
	if err := s.Init(); err != nil {
		return nil, err
	}
	path := filepath.Join(s.Root, "locks", runID+".lock")
	lock := flock.New(path)
	ok, err := lock.TryLock()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("evaluation run %s is active in another process", runID)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = lock.Unlock()
		return nil, err
	}
	return &RunLock{path: path, file: lock}, nil
}

func (s *Store) ManifestPath(runID string) string {
	return filepath.Join(s.Root, "runs", runID+".json")
}

func (s *Store) WriteManifest(manifest RunManifest) (string, error) {
	if !safeStoreComponent(manifest.RunID) {
		return "", fmt.Errorf("invalid run manifest run_id %q", manifest.RunID)
	}
	manifest.SchemaVersion = RunManifestVersion
	if err := s.Init(); err != nil {
		return "", err
	}
	path := s.ManifestPath(manifest.RunID)
	data, err := sanitizedJSON(manifest, s.secrets...)
	if err != nil {
		return "", err
	}
	return path, atomicWrite(path, append(data, '\n'), 0o600)
}

func (s *Store) LoadManifest(runID string) (*RunManifest, error) {
	if !safeStoreComponent(runID) {
		return nil, fmt.Errorf("invalid run_id %q", runID)
	}
	data, err := os.ReadFile(s.ManifestPath(runID))
	if err != nil {
		return nil, err
	}
	var manifest RunManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse run manifest %s: %w", runID, err)
	}
	if manifest.SchemaVersion != RunManifestVersion || manifest.RunID != runID {
		return nil, fmt.Errorf("invalid run manifest %s", runID)
	}
	return &manifest, nil
}

func (s *Store) ListRuns() ([]RunManifest, error) {
	runs, ignored, err := s.listRunsForAutoResume()
	if err != nil {
		return nil, err
	}
	if len(ignored) != 0 {
		return runs, fmt.Errorf("ignored invalid run manifests: %s", strings.Join(ignored, ", "))
	}
	return runs, nil
}

func (s *Store) FindRun(want RunManifest, policy ResumePolicy, exactID string) (*RunManifest, []string, error) {
	if policy == "" {
		policy = ResumeAuto
	}
	if policy == ResumeFresh {
		return nil, nil, nil
	}
	if policy == ResumeExact {
		manifest, err := s.LoadManifest(exactID)
		if err != nil {
			return nil, nil, err
		}
		if manifest.Complete() {
			return nil, nil, fmt.Errorf("evaluation run %s is already complete", exactID)
		}
		mismatches := manifestCompatibilityMismatches(*manifest, want)
		if len(mismatches) != 0 {
			return nil, mismatches, fmt.Errorf("evaluation run %s is incompatible: %s", exactID, strings.Join(mismatches, ", "))
		}
		return manifest, nil, nil
	}
	runs, ignored, err := s.listRunsForAutoResume()
	if err != nil {
		return nil, nil, err
	}
	for i := range runs {
		if runs[i].Complete() {
			continue
		}
		if len(manifestCompatibilityMismatches(runs[i], want)) == 0 {
			return &runs[i], ignored, nil
		}
		ignored = append(ignored, runs[i].RunID)
	}
	return nil, ignored, nil
}

func (s *Store) listRunsForAutoResume() ([]RunManifest, []string, error) {
	dir := filepath.Join(s.Root, "runs")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	runs := make([]RunManifest, 0, len(entries))
	ignored := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		runID := strings.TrimSuffix(entry.Name(), ".json")
		manifest, err := s.LoadManifest(runID)
		if err != nil {
			ignored = append(ignored, runID)
			continue
		}
		runs = append(runs, *manifest)
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].UpdatedAt.After(runs[j].UpdatedAt) })
	return runs, ignored, nil
}

func manifestCompatibilityMismatches(have, want RunManifest) []string {
	fields := make([]string, 0, 12)
	check := func(equal bool, name string) {
		if !equal {
			fields = append(fields, name)
		}
	}
	check(have.Intent == want.Intent, "intent")
	check(have.Mode == want.Mode, "mode")
	check(have.SuiteFingerprint == want.SuiteFingerprint, "suite_fingerprint")
	check(have.OracleValueHash == want.OracleValueHash, "oracle_value_hash")
	check(canonicalHash(have.DatasetFingerprint) == canonicalHash(want.DatasetFingerprint), "dataset_fingerprint")
	check(have.TaskSchemaVersion == want.TaskSchemaVersion, "task_schema_version")
	check(have.EpisodeSchemaVersion == want.EpisodeSchemaVersion, "episode_schema_version")
	check(have.ReportSchemaVersion == want.ReportSchemaVersion, "report_schema_version")
	check(have.RewardVersion == want.RewardVersion, "reward_version")
	check(have.GeneratorVersion == want.GeneratorVersion, "generator_version")
	check(have.BinaryFingerprint != "" && have.BinaryFingerprint == want.BinaryFingerprint, "binary_fingerprint")
	check(canonicalHash(have.Provenance) == canonicalHash(want.Provenance), "provenance")
	check(have.BaselineRunID == want.BaselineRunID && have.BaselineFingerprint == want.BaselineFingerprint, "baseline")
	check(have.AutoBaseline == want.AutoBaseline, "auto_baseline")
	check(have.DeliberatePromotion == want.DeliberatePromotion, "deliberate_promotion")
	serverRequired := strings.EqualFold(want.Provenance.Target, string(TargetRemote))
	check((!serverRequired || have.ServerEvalFingerprint != "") && have.ServerEvalFingerprint == want.ServerEvalFingerprint, "server_eval_fingerprint")
	sort.Strings(fields)
	return fields
}

func canonicalHash(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func BaselineFingerprint(report *Report) string {
	if report == nil {
		return ""
	}
	return canonicalHash(report)
}

func (s *Store) WriteAttempt(attempt Attempt) (string, error) {
	if !safeStoreComponent(attempt.RunID) || !safeStoreComponent(attempt.TaskID) {
		return "", errors.New("invalid attempt identity")
	}
	if err := s.Init(); err != nil {
		return "", err
	}
	attempt.SchemaVersion = AttemptSchemaVersion
	dir := filepath.Join(s.Root, "attempts", attempt.RunID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", err
	}
	name := attemptFilename(attempt)
	path := filepath.Join(dir, name)
	data, err := sanitizedJSON(attempt, s.secrets...)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("attempt already exists: %s", path)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	return path, atomicWrite(path, append(data, '\n'), 0o600)
}

func attemptFilename(attempt Attempt) string {
	kind := "initial"
	if attempt.Confirmation {
		kind = "confirm"
	}
	return fmt.Sprintf("%s-%s-%s-%03d-attempt-%03d.json", slugify(attempt.TaskSlug), attempt.TaskID, kind, attempt.Repeat, attempt.Attempt)
}

func attemptSlotKey(attempt Attempt) string {
	return fmt.Sprintf("%s/%t/%d", attempt.TaskID, attempt.Confirmation, attempt.Repeat)
}

func (s *Store) LoadAttempts(runID string) ([]Attempt, error) {
	if !safeStoreComponent(runID) {
		return nil, fmt.Errorf("invalid attempt run_id %q", runID)
	}
	dir := filepath.Join(s.Root, "attempts", runID)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	attempts := make([]Attempt, 0, len(entries))
	seenNumbers := map[int]bool{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var attempt Attempt
		if err := json.Unmarshal(data, &attempt); err != nil {
			return nil, fmt.Errorf("parse attempt %s: %w", entry.Name(), err)
		}
		if attempt.SchemaVersion != AttemptSchemaVersion || attempt.RunID != runID || attempt.Attempt < 1 || entry.Name() != attemptFilename(attempt) {
			return nil, fmt.Errorf("attempt identity mismatch: %s", entry.Name())
		}
		if seenNumbers[attempt.Attempt] {
			return nil, fmt.Errorf("duplicate provider attempt number %d", attempt.Attempt)
		}
		seenNumbers[attempt.Attempt] = true
		attempts = append(attempts, attempt)
	}
	sort.Slice(attempts, func(i, j int) bool { return attempts[i].Attempt < attempts[j].Attempt })
	return attempts, nil
}

func sanitizedJSON(value any, secrets ...string) ([]byte, error) {
	return json.MarshalIndent(gjagent.SanitizeValue(value, secrets...), "", "  ")
}
