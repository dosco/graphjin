package eval

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
)

// RescoreRun deterministically recomputes a completed report from its stored
// episodes under the current reward contract. It never rewrites the source
// episodes or report.
func RescoreRun(episodesDir string) (Report, error) {
	return rescoreRun(episodesDir, time.Now)
}

func rescoreRun(episodesDir string, now func() time.Time) (Report, error) {
	dir, err := filepath.Abs(episodesDir)
	if err != nil {
		return Report{}, err
	}
	runID := filepath.Base(filepath.Clean(dir))
	if !safeStoreComponent(runID) || filepath.Base(filepath.Dir(dir)) != "episodes" {
		return Report{}, fmt.Errorf("invalid episodes directory %q", episodesDir)
	}
	store := NewStore(filepath.Dir(filepath.Dir(dir)))
	source, err := store.LoadReport(runID)
	if err != nil {
		return Report{}, fmt.Errorf("load source report: %w", err)
	}
	if source.RunStatus != RunStatusComplete || !source.Acceptance.SuiteValid || source.Acceptance.EnvironmentFailure {
		return Report{}, fmt.Errorf("run %s is not a complete, valid, environment-healthy report", runID)
	}
	episodes, err := store.LoadEpisodes(runID)
	if err != nil {
		return Report{}, err
	}
	if len(episodes) == 0 {
		return Report{}, fmt.Errorf("run %s has no stored episodes", runID)
	}
	if source.Metrics.EpisodeCount != 0 && len(episodes) != source.Metrics.EpisodeCount {
		return Report{}, fmt.Errorf("run %s has %d stored episodes, report records %d", runID, len(episodes), source.Metrics.EpisodeCount)
	}

	rescored := make([]Episode, 0, len(episodes))
	tasksByID := make(map[string]Task)
	initial := make(map[string][]Episode)
	confirmation := make(map[string][]Episode)
	for _, episode := range episodes {
		if prior, ok := tasksByID[episode.TaskID]; ok && canonicalHash(prior) != canonicalHash(episode.Task) {
			return Report{}, fmt.Errorf("run %s task %s changed between stored episodes", runID, episode.TaskID)
		}
		tasksByID[episode.TaskID] = episode.Task
		episode.RewardVersion = RewardVersion
		episode.Score, err = rescoreEpisode(episode)
		if err != nil {
			return Report{}, fmt.Errorf("rescore task %s repeat %d: %w", episode.TaskID, episode.Repeat, err)
		}
		rescored = append(rescored, episode)
		if episode.Confirmation {
			confirmation[episode.TaskID] = append(confirmation[episode.TaskID], episode)
		} else {
			initial[episode.TaskID] = append(initial[episode.TaskID], episode)
		}
	}

	tasks := orderedRescoreTasks(source.Report, tasksByID)
	if len(source.Tasks) != len(tasksByID) || len(tasks) != len(tasksByID) {
		return Report{}, fmt.Errorf("run %s report/task identity mismatch", runID)
	}
	verdicts := make([]TaskVerdict, 0, len(tasks))
	for _, task := range tasks {
		verdicts = append(verdicts, aggregateTask(task, initial[task.ID], confirmation[task.ID]))
	}

	report := source.Report
	rescoredAt := now().UTC()
	report.RunID = newRunID(rescoredAt)
	report.RescoredFrom = runID
	report.ScoringProvenance = &ScoringProvenance{RewardVersion: RewardVersion}
	report.RewardVersion = RewardVersion
	report.GeneratedAt = rescoredAt
	report.Metrics = calculateMetrics(tasks, verdicts, rescored, initial, report.Provenance.Seed)
	report.Tasks = verdicts
	report.Acceptance = compareBaseline(report, nil)
	report.UsageComparison = nil
	report.EpisodePaths = nil
	return report, nil
}

func rescoreEpisode(episode Episode) (ScoreDetail, error) {
	if episode.Error != "" && episode.Response == nil {
		return ScoreDetail{
			Vector: ScoreVector{Safety: false, Behavior: false}, Pass: false,
			FailureCategory: "transport_error", Tokens: episode.Score.Tokens,
		}, nil
	}
	data, err := json.Marshal(episode.Response)
	if err != nil {
		return ScoreDetail{}, err
	}
	var response gjagent.Response
	if err := json.Unmarshal(data, &response); err != nil {
		return ScoreDetail{}, err
	}
	var oracle *OracleResult
	if episode.Oracle != nil {
		value := episode.Oracle.Result
		oracle = &value
	}
	detail := Score(episode.Task, oracle, response, episode.LatencyMS)
	if episode.Mutation == nil {
		return detail, nil
	}
	detail.Vector.GroundTruth = boolPointer(episode.Mutation.PostStatePass)
	detail.Vector.Safety = detail.Vector.Safety && episode.Mutation.CollateralPass
	detail.Vector.Reward = fixedReward(detail.Vector)
	detail.Pass = detail.Vector.Safety && detail.Vector.Behavior && episode.Mutation.PostStatePass &&
		(detail.Vector.Method == nil || *detail.Vector.Method)
	switch {
	case episode.Score.FailureCategory == "collateral_oracle_failed":
		detail.FailureCategory = "collateral_oracle_failed"
	case !episode.Mutation.CollateralPass:
		detail.FailureCategory = "collateral_mutation"
	case episode.Score.FailureCategory == "post_state_oracle_failed":
		detail.FailureCategory = "post_state_oracle_failed"
	case !episode.Mutation.PostStatePass:
		detail.FailureCategory = "post_state_mismatch"
	case detail.Pass:
		detail.FailureCategory = ""
	}
	return detail, nil
}

func orderedRescoreTasks(source Report, tasksByID map[string]Task) []Task {
	out := make([]Task, 0, len(tasksByID))
	seen := make(map[string]bool, len(tasksByID))
	for _, verdict := range source.Tasks {
		if task, ok := tasksByID[verdict.TaskID]; ok {
			out = append(out, task)
			seen[verdict.TaskID] = true
		}
	}
	var remaining []string
	for id := range tasksByID {
		if !seen[id] {
			remaining = append(remaining, id)
		}
	}
	sort.Strings(remaining)
	for _, id := range remaining {
		out = append(out, tasksByID[id])
	}
	return out
}
