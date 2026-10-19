package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type importedBehaviorCase struct {
	ID                    string            `json:"id"`
	Group                 string            `json:"group"`
	Prompt                string            `json:"prompt"`
	CapabilityProfile     CapabilityProfile `json:"capability_profile"`
	ExpectedLoadedSkills  []string          `json:"expected_loaded_skills"`
	ForbiddenLoadedSkills []string          `json:"forbidden_loaded_skills"`
	ExpectedStatus        string            `json:"expected_status"`
	RequiredActions       []string          `json:"required_actions"`
	ForbiddenActions      []string          `json:"forbidden_actions"`
	ExpectedUsedSkills    []string          `json:"expected_used_skills"`
}

type importedDataCase struct {
	ID                string            `json:"id"`
	Group             string            `json:"group"`
	Prompt            string            `json:"prompt"`
	CapabilityProfile CapabilityProfile `json:"capability_profile"`
	ExpectedStatus    string            `json:"expected_status"`
	Oracle            OracleSpec        `json:"oracle"`
	Answer            AnswerRule        `json:"answer"`
	Method            MethodRule        `json:"method"`
	Budget            Budget            `json:"budget"`
}

type ImportOptions struct {
	BehaviorCorpusPath string
	DataCorpusPath     string
	Seed               int64
}

func ImportCorpora(opts ImportOptions) ([]Task, error) {
	var tasks []Task
	if strings.TrimSpace(opts.BehaviorCorpusPath) != "" {
		data, err := os.ReadFile(opts.BehaviorCorpusPath)
		if err != nil {
			return nil, err
		}
		var cases []importedBehaviorCase
		if err := json.Unmarshal(data, &cases); err != nil {
			return nil, fmt.Errorf("parse behavioral corpus: %w", err)
		}
		for _, item := range cases {
			task := Task{
				Slug:              "imported-" + item.ID,
				Category:          importedCategory(item.Group),
				Difficulty:        importedDifficulty(item.Group, item.ExpectedStatus, item.RequiredActions),
				Prompt:            item.Prompt,
				Provenance:        Provenance{Source: "imported", Seed: opts.Seed, SourceID: item.ID},
				CapabilityProfile: item.CapabilityProfile,
				ExpectedStatus:    item.ExpectedStatus,
				Behavior: BehaviorRule{
					RequiredActions:       item.RequiredActions,
					ForbiddenActions:      item.ForbiddenActions,
					ExpectedUsedSkills:    item.ExpectedUsedSkills,
					ExpectedLoadedSkills:  item.ExpectedLoadedSkills,
					ForbiddenLoadedSkills: item.ForbiddenLoadedSkills,
				},
			}
			if err := task.Normalize(); err != nil {
				return nil, fmt.Errorf("behavior case %s: %w", item.ID, err)
			}
			tasks = append(tasks, task)
		}
	}
	if strings.TrimSpace(opts.DataCorpusPath) != "" {
		data, err := os.ReadFile(opts.DataCorpusPath)
		if err != nil {
			return nil, err
		}
		var cases []importedDataCase
		if err := json.Unmarshal(data, &cases); err != nil {
			return nil, fmt.Errorf("parse data corpus: %w", err)
		}
		for _, item := range cases {
			oracle := item.Oracle
			task := Task{
				Slug:              "imported-" + item.ID,
				Category:          importedCategory(item.Group),
				Difficulty:        importedDifficulty(item.Group, item.ExpectedStatus, nil),
				Prompt:            item.Prompt,
				Provenance:        Provenance{Source: "imported", Seed: opts.Seed, SourceID: item.ID},
				CapabilityProfile: item.CapabilityProfile,
				ExpectedStatus:    item.ExpectedStatus,
				Oracle:            &oracle,
				Answer:            item.Answer,
				Method:            item.Method,
				Budget:            item.Budget,
				Behavior: BehaviorRule{
					RequiredActions:  []string{"query_catalog", "execute_graphql"},
					ForbiddenActions: []string{"execute_graphql:mutation"},
				},
			}
			if err := task.Normalize(); err != nil {
				return nil, fmt.Errorf("data case %s: %w", item.ID, err)
			}
			tasks = append(tasks, task)
		}
	}
	return tasks, nil
}

func importedCategory(group string) Category {
	switch strings.ToLower(group) {
	case "aggregate", "data":
		return CategoryAggregate
	case "anchor", "window":
		return CategoryWindow
	case "ranking":
		return CategoryRanking
	case "safety", "refusal", "admin":
		return CategoryRefusal
	case "workflow", "watch", "ambiguous_multi", "code":
		return CategoryTraversal
	default:
		return CategoryDiscovery
	}
}

func importedDifficulty(group, expectedStatus string, actions []string) Difficulty {
	if expectedStatus == "blocked" || group == "safety" || group == "admin" {
		return DifficultyT4
	}
	for _, action := range actions {
		if strings.Contains(action, "mutation") {
			return DifficultyT4
		}
	}
	switch group {
	case "ranking", "workflow", "watch", "ambiguous_multi", "code":
		return DifficultyT3
	case "anchor", "window":
		return DifficultyT2
	default:
		return DifficultyT1
	}
}
