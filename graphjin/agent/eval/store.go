package eval

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Store struct {
	Root    string
	secrets []string
}

func NewStore(root string) *Store {
	if strings.TrimSpace(root) == "" {
		root = DefaultStateDir
	}
	return &Store{Root: root}
}

// WithSecrets configures defense-in-depth redaction for every stored value.
// Secret values are held only in memory and are never written to manifests.
func (s *Store) WithSecrets(values ...string) *Store {
	if s == nil {
		return s
	}
	s.secrets = append([]string(nil), values...)
	return s
}

func (s *Store) Init() error {
	if s == nil {
		return errors.New("nil eval store")
	}
	for _, dir := range []string{s.Root, filepath.Join(s.Root, "reports"), filepath.Join(s.Root, "episodes"), filepath.Join(s.Root, "attempts"), filepath.Join(s.Root, "runs"), filepath.Join(s.Root, "locks")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) WriteEpisode(episode Episode) (string, error) {
	if !safeStoreComponent(episode.RunID) {
		return "", fmt.Errorf("invalid episode run_id %q", episode.RunID)
	}
	if err := s.Init(); err != nil {
		return "", err
	}
	dir := filepath.Join(s.Root, "episodes", episode.RunID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, episodeFilename(episode))
	data, err := sanitizedJSON(episode, s.secrets...)
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	if current, err := os.ReadFile(path); err == nil {
		if string(current) == string(data) {
			return path, nil
		}
		return "", fmt.Errorf("episode slot already exists with different content: %s", path)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	return path, atomicWrite(path, data, 0o600)
}

func episodeFilename(episode Episode) string {
	taskFileID := episode.TaskID
	if taskFileID == "" {
		taskFileID = "unknown"
	}
	name := fmt.Sprintf("%s-%s-%03d.json", slugify(episode.TaskSlug), taskFileID, episode.Repeat)
	if episode.Confirmation {
		name = fmt.Sprintf("%s-%s-confirm-%03d.json", slugify(episode.TaskSlug), taskFileID, episode.Repeat)
	}
	return name
}

func episodeSlotKey(episode Episode) string {
	return fmt.Sprintf("%s/%t/%d", episode.TaskID, episode.Confirmation, episode.Repeat)
}

func (s *Store) LoadEpisodes(runID string) ([]Episode, error) {
	if !safeStoreComponent(runID) {
		return nil, fmt.Errorf("invalid episode run_id %q", runID)
	}
	dir := filepath.Join(s.Root, "episodes", runID)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	episodes := make([]Episode, 0, len(entries))
	seen := map[string]bool{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var episode Episode
		if err := json.Unmarshal(data, &episode); err != nil {
			return nil, fmt.Errorf("parse episode %s: %w", entry.Name(), err)
		}
		if episode.SchemaVersion != EpisodeSchemaVersion || episode.RunID != runID || entry.Name() != episodeFilename(episode) {
			return nil, fmt.Errorf("episode identity mismatch: %s", entry.Name())
		}
		key := episodeSlotKey(episode)
		if seen[key] {
			return nil, fmt.Errorf("duplicate episode slot %s", key)
		}
		seen[key] = true
		episodes = append(episodes, episode)
	}
	sort.Slice(episodes, func(i, j int) bool { return episodeSlotKey(episodes[i]) < episodeSlotKey(episodes[j]) })
	return episodes, nil
}

func (s *Store) WriteReport(report Report) (string, error) {
	if !safeStoreComponent(report.RunID) {
		return "", fmt.Errorf("invalid report run_id %q", report.RunID)
	}
	if err := s.Init(); err != nil {
		return "", err
	}
	path := s.ReportPath(report.RunID)
	data, err := sanitizedJSON(report, s.secrets...)
	if err != nil {
		return "", err
	}
	markdown := []byte(s.redact(RenderFriendlyReportMarkdown(report)))
	technicalMarkdown := []byte(s.redact(RenderTechnicalReportMarkdown(report)))
	if err := atomicWrite(path, append(data, '\n'), 0o600); err != nil {
		return path, err
	}
	if err := atomicWrite(s.ReportMarkdownPath(report.RunID), markdown, 0o600); err != nil {
		return path, err
	}
	return path, atomicWrite(s.ReportTechnicalMarkdownPath(report.RunID), technicalMarkdown, 0o600)
}

func (s *Store) WritePartialReport(report PartialReport) (string, error) {
	if !safeStoreComponent(report.RunID) {
		return "", fmt.Errorf("invalid report run_id %q", report.RunID)
	}
	if err := s.Init(); err != nil {
		return "", err
	}
	path := s.ReportPath(report.RunID)
	data, err := sanitizedJSON(report, s.secrets...)
	if err != nil {
		return "", err
	}
	markdown := []byte(s.redact(RenderFriendlyPartialReportMarkdown(report)))
	technicalMarkdown := []byte(s.redact(RenderTechnicalPartialReportMarkdown(report)))
	if err := atomicWrite(path, append(data, '\n'), 0o600); err != nil {
		return path, err
	}
	if err := atomicWrite(s.ReportMarkdownPath(report.RunID), markdown, 0o600); err != nil {
		return path, err
	}
	return path, atomicWrite(s.ReportTechnicalMarkdownPath(report.RunID), technicalMarkdown, 0o600)
}

func (s *Store) ReportPath(runID string) string {
	return filepath.Join(s.Root, "reports", runID+".json")
}

func (s *Store) ReportMarkdownPath(runID string) string {
	return filepath.Join(s.Root, "reports", runID+".md")
}

func (s *Store) ReportTechnicalMarkdownPath(runID string) string {
	return filepath.Join(s.Root, "reports", runID+".technical.md")
}

type StoredReport struct {
	Report
	EnvironmentCode string `json:"environment_code,omitempty"`
	Notice          string `json:"notice,omitempty"`
}

type ReportSummary struct {
	RunID                 string          `json:"run_id"`
	RunStatus             RunStatus       `json:"run_status"`
	Mode                  RunMode         `json:"mode"`
	GeneratedAt           time.Time       `json:"generated_at"`
	SuiteFingerprint      string          `json:"suite_fingerprint"`
	SuiteIdentity         string          `json:"suite_identity"`
	Provenance            RunProvenance   `json:"provenance"`
	TaskCount             int             `json:"task_count"`
	EpisodeCount          int             `json:"episode_count"`
	Recall                float64         `json:"recall"`
	PassAtK               float64         `json:"pass_at_k"`
	SafetyPrecision       float64         `json:"safety_precision"`
	TotalTokens           int64           `json:"total_tokens"`
	ProviderTotalTokens   int64           `json:"provider_total_tokens"`
	ProviderUsageComplete bool            `json:"provider_usage_complete"`
	Accepted              bool            `json:"accepted"`
	HasMarkdown           bool            `json:"has_markdown"`
	HasTechnicalMarkdown  bool            `json:"has_technical_markdown"`
	Progress              RunProgress     `json:"progress"`
	EnvironmentCode       string          `json:"environment_code,omitempty"`
	FriendlySummary       FriendlySummary `json:"friendly_summary"`
}

func (s *Store) LoadReport(runID string) (*StoredReport, error) {
	if !safeStoreComponent(runID) {
		return nil, fmt.Errorf("invalid report run_id %q", runID)
	}
	data, err := os.ReadFile(s.ReportPath(runID))
	if err != nil {
		return nil, err
	}
	var report StoredReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("parse report %s: %w", runID, err)
	}
	if !supportedReportSchema(report.SchemaVersion) {
		return nil, fmt.Errorf("unsupported report schema_version %q", report.SchemaVersion)
	}
	if report.RunID != runID {
		return nil, fmt.Errorf("report identity mismatch: requested %q, found %q", runID, report.RunID)
	}
	return &report, nil
}

func (s *Store) LoadReportMarkdown(runID string) ([]byte, error) {
	if !safeStoreComponent(runID) {
		return nil, fmt.Errorf("invalid report run_id %q", runID)
	}
	data, err := os.ReadFile(s.ReportMarkdownPath(runID))
	if os.IsNotExist(err) {
		return nil, nil
	}
	return data, err
}

func (s *Store) LoadReportTechnicalMarkdown(runID string) ([]byte, error) {
	if !safeStoreComponent(runID) {
		return nil, fmt.Errorf("invalid report run_id %q", runID)
	}
	data, err := os.ReadFile(s.ReportTechnicalMarkdownPath(runID))
	if os.IsNotExist(err) {
		return nil, nil
	}
	return data, err
}

func (s *Store) ListReports() ([]ReportSummary, error) {
	dir := filepath.Join(s.Root, "reports")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]ReportSummary, 0, len(entries))
	var invalid []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		runID := strings.TrimSuffix(entry.Name(), ".json")
		report, err := s.LoadReport(runID)
		if err != nil {
			invalid = append(invalid, entry.Name())
			continue
		}
		_, markdownErr := os.Stat(s.ReportMarkdownPath(runID))
		hasMarkdown := markdownErr == nil
		if markdownErr != nil && !os.IsNotExist(markdownErr) {
			invalid = append(invalid, entry.Name()+" (markdown)")
		}
		_, technicalMarkdownErr := os.Stat(s.ReportTechnicalMarkdownPath(runID))
		hasTechnicalMarkdown := technicalMarkdownErr == nil
		if technicalMarkdownErr != nil && !os.IsNotExist(technicalMarkdownErr) {
			invalid = append(invalid, entry.Name()+" (technical markdown)")
		}
		out = append(out, ReportSummary{
			RunID: report.RunID, RunStatus: report.RunStatus, Mode: report.Mode, GeneratedAt: report.GeneratedAt,
			SuiteFingerprint: report.SuiteFingerprint, SuiteIdentity: SuiteIdentity(report.Report), Provenance: report.Provenance,
			TaskCount: report.Metrics.TaskCount, EpisodeCount: report.Metrics.EpisodeCount, Recall: report.Metrics.Recall,
			PassAtK: report.Metrics.PassAtK, SafetyPrecision: report.Metrics.SafetyPrecision,
			TotalTokens: report.Metrics.TotalTokens, ProviderTotalTokens: report.ProviderUsage.TotalTokens,
			ProviderUsageComplete: report.ProviderUsage.Complete, Accepted: report.Acceptance.HardPass, HasMarkdown: hasMarkdown,
			HasTechnicalMarkdown: hasTechnicalMarkdown, Progress: report.Progress, EnvironmentCode: report.EnvironmentCode,
			FriendlySummary: SummarizeStoredReport(*report),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].GeneratedAt.Equal(out[j].GeneratedAt) {
			return out[i].RunID > out[j].RunID
		}
		return out[i].GeneratedAt.After(out[j].GeneratedAt)
	})
	if len(invalid) != 0 {
		sort.Strings(invalid)
		return out, fmt.Errorf("ignored unreadable report files: %s", strings.Join(invalid, ", "))
	}
	return out, nil
}

func supportedReportSchema(version string) bool {
	return version == ReportSchemaVersion || version == ReportV2Version || version == LegacyReportVersion
}

func (s *Store) redact(value string) string {
	for _, secret := range s.secrets {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
	}
	return value
}

func safeStoreComponent(value string) bool {
	return value != "" && filepath.Base(value) == value && !strings.ContainsAny(value, `/\`)
}

func (s *Store) BaselinePath() string { return filepath.Join(s.Root, "baseline.json") }

func (s *Store) LoadBaseline() (*Report, error) {
	data, err := os.ReadFile(s.BaselinePath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var report Report
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("parse baseline: %w", err)
	}
	if !supportedReportSchema(report.SchemaVersion) {
		return nil, fmt.Errorf("unsupported baseline schema_version %q", report.SchemaVersion)
	}
	if report.RunStatus != "" && report.RunStatus != RunStatusComplete {
		return nil, fmt.Errorf("baseline %s is incomplete", report.RunID)
	}
	return &report, nil
}

func (s *Store) PromoteBaseline(report Report) error {
	if report.RunStatus != RunStatusComplete || !report.Acceptance.HardPass || !report.Acceptance.SafetyPass {
		return errors.New("only a complete, accepted, safety-passing report can be promoted to baseline")
	}
	if err := s.Init(); err != nil {
		return err
	}
	data, err := sanitizedJSON(report, s.secrets...)
	if err != nil {
		return err
	}
	return atomicWrite(s.BaselinePath(), append(data, '\n'), 0o600)
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".graphjin-eval-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}
