package eval

import (
	"bytes"
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
)

type Suite struct {
	SchemaVersion      string        `json:"schema_version" yaml:"schema_version"`
	Name               string        `json:"name" yaml:"name"`
	Description        string        `json:"description,omitempty" yaml:"description,omitempty"`
	CreatedAt          time.Time     `json:"-" yaml:"-"`
	CatalogFingerprint string        `json:"target_catalog_fingerprint" yaml:"target_catalog_fingerprint"`
	Generator          GeneratorMeta `json:"generator" yaml:"generator"`
	Tasks              []Task        `json:"tasks" yaml:"tasks"`
}

type GeneratorMeta struct {
	Version string `json:"version" yaml:"version"`
	Seed    int64  `json:"seed" yaml:"seed"`
	Scale   int    `json:"scale" yaml:"scale"`
}

func (s *Suite) Normalize() error {
	if s == nil {
		return errors.New("nil suite")
	}
	s.SchemaVersion = SuiteSchemaVersion
	if s.Name == "" {
		s.Name = "GraphJin Eval"
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now().UTC()
	}
	if s.Generator.Version == "" {
		s.Generator.Version = GeneratorVersion
	}
	if s.Generator.Scale <= 0 {
		s.Generator.Scale = len(s.Tasks)
	}
	for i := range s.Tasks {
		if err := s.Tasks[i].Normalize(); err != nil {
			return fmt.Errorf("task %d: %w", i+1, err)
		}
	}
	return s.Validate()
}

func (s Suite) Validate() error {
	if s.SchemaVersion != SuiteSchemaVersion {
		return fmt.Errorf("unsupported suite schema_version %q", s.SchemaVersion)
	}
	if !IsSupportedGeneratorVersion(s.Generator.Version) {
		return fmt.Errorf("unsupported suite generator version %q; this binary supports %s; regenerate the suite",
			s.Generator.Version, strings.Join(SupportedGeneratorVersions, ", "))
	}
	if s.Name == "" || len(s.Tasks) == 0 {
		return errors.New("suite needs a name and at least one task")
	}
	seenID := make(map[string]struct{}, len(s.Tasks))
	seenStructure := make(map[string]string, len(s.Tasks))
	for i, task := range s.Tasks {
		if err := task.Validate(); err != nil {
			return fmt.Errorf("task %d: %w", i+1, err)
		}
		if _, ok := seenID[task.ID]; ok {
			return fmt.Errorf("duplicate task id %q", task.ID)
		}
		seenID[task.ID] = struct{}{}
		key := taskStructureKey(task)
		if prior, ok := seenStructure[key]; ok {
			return fmt.Errorf("tasks %q and %q are structural duplicates", prior, task.Slug)
		}
		seenStructure[key] = task.Slug
	}
	return nil
}

func taskStructureKey(task Task) string {
	// Turns and Mutation are part of what makes a task distinct. Confirmation
	// flows share one approval prompt ("Yes, go ahead and set that up") and differ
	// only in the history that precedes them and the post-state they verify, so
	// omitting either collapsed them into a single task.
	value := struct {
		Category          Category
		Difficulty        Difficulty
		Prompt            string
		CapabilityProfile CapabilityProfile
		Oracle            *OracleSpec
		Behavior          BehaviorRule
		Turns             []TurnSpec
		Mutation          *MutationSpec
	}{task.Category, task.Difficulty, strings.ToLower(strings.TrimSpace(task.Prompt)), task.CapabilityProfile, task.Oracle, task.Behavior, task.Turns, task.Mutation}
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (s Suite) CatalogTaskIDs() []string {
	ids := make([]string, 0, len(s.Tasks))
	for _, task := range s.Tasks {
		ids = append(ids, task.ID)
	}
	sort.Strings(ids)
	return ids
}

// MarshalSuite emits deterministic indented JSON. JSON is a strict subset of
// YAML 1.2, so suite.yml remains readable by YAML tooling while this package
// retains its agent+stdlib-only dependency boundary.
func MarshalSuite(s Suite) ([]byte, error) {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func LoadSuite(path string) (*Suite, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var suite Suite
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&suite); err != nil {
		return nil, fmt.Errorf("parse %s (suite.yml uses YAML-compatible JSON): %w", path, err)
	}
	if err := suite.Validate(); err != nil {
		return nil, fmt.Errorf("validate %s: %w", path, err)
	}
	return &suite, nil
}

func SaveSuite(path string, suite Suite) error {
	if err := suite.Normalize(); err != nil {
		return err
	}
	data, err := MarshalSuite(suite)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return atomicWrite(path, data, 0o600)
}

func SuiteFingerprint(s Suite) string {
	copySuite := s
	copySuite.CreatedAt = time.Time{}
	data, _ := json.Marshal(copySuite)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:16])
}
