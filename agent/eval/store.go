package eval

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	path := filepath.Join(s.Root, "reports", report.RunID+".json")
	data, err := sanitizedJSON(report, s.secrets...)
	if err != nil {
		return "", err
	}
	return path, atomicWrite(path, append(data, '\n'), 0o600)
}

func (s *Store) WritePartialReport(report PartialReport) (string, error) {
	if !safeStoreComponent(report.RunID) {
		return "", fmt.Errorf("invalid report run_id %q", report.RunID)
	}
	if err := s.Init(); err != nil {
		return "", err
	}
	path := filepath.Join(s.Root, "reports", report.RunID+".json")
	data, err := sanitizedJSON(report, s.secrets...)
	if err != nil {
		return "", err
	}
	return path, atomicWrite(path, append(data, '\n'), 0o600)
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
	if report.SchemaVersion != ReportSchemaVersion && report.SchemaVersion != LegacyReportVersion {
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
