package eval

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Store struct {
	Root string
}

func NewStore(root string) *Store {
	if strings.TrimSpace(root) == "" {
		root = DefaultStateDir
	}
	return &Store{Root: root}
}

func (s *Store) Init() error {
	if s == nil {
		return errors.New("nil eval store")
	}
	for _, dir := range []string{s.Root, filepath.Join(s.Root, "reports"), filepath.Join(s.Root, "episodes")} {
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
	taskFileID := episode.TaskID
	if taskFileID == "" {
		taskFileID = "unknown"
	}
	name := fmt.Sprintf("%s-%s-%03d.json", slugify(episode.TaskSlug), taskFileID, episode.Repeat)
	if episode.Confirmation {
		name = fmt.Sprintf("%s-%s-confirm-%03d.json", slugify(episode.TaskSlug), taskFileID, episode.Repeat)
	}
	path := filepath.Join(dir, name)
	data, err := json.MarshalIndent(episode, "", "  ")
	if err != nil {
		return "", err
	}
	return path, atomicWrite(path, append(data, '\n'), 0o600)
}

func (s *Store) WriteReport(report Report) (string, error) {
	if !safeStoreComponent(report.RunID) {
		return "", fmt.Errorf("invalid report run_id %q", report.RunID)
	}
	if err := s.Init(); err != nil {
		return "", err
	}
	path := filepath.Join(s.Root, "reports", report.RunID+".json")
	data, err := json.MarshalIndent(report, "", "  ")
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
	if report.SchemaVersion != ReportSchemaVersion {
		return nil, fmt.Errorf("unsupported baseline schema_version %q", report.SchemaVersion)
	}
	return &report, nil
}

func (s *Store) PromoteBaseline(report Report) error {
	if !report.Acceptance.HardPass {
		return errors.New("only a passing report can be promoted to baseline")
	}
	if err := s.Init(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
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
