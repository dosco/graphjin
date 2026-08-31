package eval

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const SplitSchemaVersion = "graphjin.eval.split/v1"

// SuiteSplit records which tasks may be trained on and which are held back for
// measurement.
//
// Tuning a model on an organization's own graph and then measuring it on that
// graph is the obvious way to produce a number that means nothing. The split is
// the record that keeps the two apart, and it is written next to the suite so a
// later run cannot quietly disagree about which side a task was on.
type SuiteSplit struct {
	SchemaVersion    string   `json:"schema_version"`
	SuiteFingerprint string   `json:"suite_fingerprint"`
	TrainRatio       float64  `json:"train_ratio"`
	HoldoutFamilies  []string `json:"holdout_families,omitempty"`
	Train            []string `json:"train"`
	Eval             []string `json:"eval"`
}

// SplitSuite assigns every task to exactly one side, deterministically from the
// task's own content id.
//
// Assignment deliberately does not depend on the suite: not on its size, its
// order, its seed, or which other tasks came along. A task therefore keeps its
// side when the suite is regenerated at a different scale, which is what stops
// yesterday's training task from becoming today's measurement. Raising the
// ratio only ever moves tasks from eval to train, so widening a training set
// cannot silently promote a task that was already measured against.
//
// A family named in holdoutFamilies is never trained on, whatever the ratio.
// That is how a whole capability is kept unseen — the sharper question is not
// whether a model learned these questions but whether it learned this kind.
func SplitSuite(suite Suite, trainRatio float64, holdoutFamilies []string) (SuiteSplit, error) {
	if trainRatio < 0 || trainRatio > 1 {
		return SuiteSplit{}, fmt.Errorf("train ratio %v is not between 0 and 1", trainRatio)
	}
	holdout := map[string]bool{}
	for _, family := range holdoutFamilies {
		if family = strings.TrimSpace(family); family != "" {
			holdout[family] = true
		}
	}
	split := SuiteSplit{
		SchemaVersion:    SplitSchemaVersion,
		SuiteFingerprint: SuiteFingerprint(suite),
		TrainRatio:       trainRatio,
		HoldoutFamilies:  sortedUnique(holdoutFamilies),
	}
	for _, task := range suite.Tasks {
		if holdout[task.Provenance.Source] || taskSplitFraction(task.ID) >= trainRatio {
			split.Eval = append(split.Eval, task.ID)
			continue
		}
		split.Train = append(split.Train, task.ID)
	}
	sort.Strings(split.Train)
	sort.Strings(split.Eval)
	return split, nil
}

// taskSplitFraction maps a task id onto [0,1). The id is already a content
// hash, but it is hashed again with a fixed label so the split cannot be read
// off an id by eye and so it stays independent of any other use of that hash.
func taskSplitFraction(taskID string) float64 {
	sum := sha256.Sum256([]byte("graphjin.eval.split:" + taskID))
	return float64(binary.BigEndian.Uint64(sum[:8])>>11) / float64(uint64(1)<<53)
}

// Contains reports which side a task is on.
func (s SuiteSplit) Contains(side []string, taskID string) bool {
	index := sort.SearchStrings(side, taskID)
	return index < len(side) && side[index] == taskID
}

func (s SuiteSplit) Validate() error {
	if s.SchemaVersion != SplitSchemaVersion {
		return fmt.Errorf("unsupported split schema_version %q", s.SchemaVersion)
	}
	seen := make(map[string]struct{}, len(s.Train)+len(s.Eval))
	for _, side := range [][]string{s.Train, s.Eval} {
		for _, id := range side {
			if _, ok := seen[id]; ok {
				return fmt.Errorf("task %q appears on both sides of the split", id)
			}
			seen[id] = struct{}{}
		}
	}
	return nil
}

func SaveSplit(path string, split SuiteSplit) error {
	if err := split.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(split, "", "  ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func LoadSplit(path string) (*SuiteSplit, error) {
	data, err := os.ReadFile(path) //nolint:gosec
	if err != nil {
		return nil, err
	}
	var split SuiteSplit
	if err := json.Unmarshal(data, &split); err != nil {
		return nil, err
	}
	if err := split.Validate(); err != nil {
		return nil, err
	}
	return &split, nil
}

// Fingerprint identifies which split this is.
//
// A suite regenerated at a different ratio, or with different holdout
// families, is a different holdout even when the tasks look familiar. Runs
// record this so a corpus can be checked against the split it claims to come
// from rather than against one that merely has the same filename.
func (s SuiteSplit) Fingerprint() string {
	canonical := struct {
		SchemaVersion    string   `json:"schema_version"`
		SuiteFingerprint string   `json:"suite_fingerprint"`
		TrainRatio       float64  `json:"train_ratio"`
		HoldoutFamilies  []string `json:"holdout_families,omitempty"`
		Train            []string `json:"train"`
		Eval             []string `json:"eval"`
	}{
		SchemaVersion: s.SchemaVersion, SuiteFingerprint: s.SuiteFingerprint,
		TrainRatio: s.TrainRatio, HoldoutFamilies: s.HoldoutFamilies,
		Train: s.Train, Eval: s.Eval,
	}
	data, err := json.Marshal(canonical)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:16])
}

// SideOf names which side a task was assigned to, or empty when the split does
// not mention it at all — which is itself worth distinguishing, since a task
// the split never saw is not the same as one it held out.
func (s SuiteSplit) SideOf(taskID string) string {
	switch {
	case s.Contains(s.Train, taskID):
		return "train"
	case s.Contains(s.Eval, taskID):
		return "eval"
	default:
		return ""
	}
}

// PartitionEpisodesBySide sorts a run's episodes by which side of the split
// their task belongs to.
//
// Unknown is kept separate rather than folded into either side: a task the
// split never mentions is not held out, but it is not vouched for either, and
// a caller deciding whether a corpus is safe to train on should be told the
// difference.
func PartitionEpisodesBySide(episodes []Episode, split SuiteSplit) (train, eval, unknown []Episode) {
	for _, episode := range episodes {
		switch split.SideOf(episode.TaskID) {
		case "train":
			train = append(train, episode)
		case "eval":
			eval = append(eval, episode)
		default:
			unknown = append(unknown, episode)
		}
	}
	return train, eval, unknown
}
