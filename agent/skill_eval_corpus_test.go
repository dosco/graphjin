package agent

import (
	"encoding/json"
	"os"
	"testing"
)

type skillEvalCapabilityProfile struct {
	RoleClass            string   `json:"role_class"`
	ReadOnly             bool     `json:"read_only"`
	AvailableSystemRoots []string `json:"available_system_roots"`
}

type skillEvalCase struct {
	ID                    string                     `json:"id"`
	Group                 string                     `json:"group"`
	Representative        bool                       `json:"representative"`
	Prompt                string                     `json:"prompt"`
	CapabilityProfile     skillEvalCapabilityProfile `json:"capability_profile"`
	ExpectedLoadedSkills  []string                   `json:"expected_loaded_skills"`
	ForbiddenLoadedSkills []string                   `json:"forbidden_loaded_skills"`
	ExpectedStatus        string                     `json:"expected_status"`
	RequiredActions       []string                   `json:"required_actions"`
	ForbiddenActions      []string                   `json:"forbidden_actions"`
	ExpectedUsedSkills    []string                   `json:"expected_used_skills"`
}

func loadSkillEvalCases(t testing.TB) []skillEvalCase {
	t.Helper()
	data, err := os.ReadFile("testdata/skill_eval_cases.json")
	if err != nil {
		t.Fatalf("read skill evaluation corpus: %v", err)
	}
	var cases []skillEvalCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("parse skill evaluation corpus: %v", err)
	}
	return cases
}

func TestSkillEvaluationCorpusShape(t *testing.T) {
	cases := loadSkillEvalCases(t)
	if len(cases) != 60 {
		t.Fatalf("corpus cases = %d, want 60", len(cases))
	}
	wantGroups := map[string]int{
		"data":            8,
		"code":            8,
		"workflow":        8,
		"watch":           8,
		"admin":           8,
		"ambiguous_multi": 10,
		"safety":          10,
	}
	groups := map[string]int{}
	ids := map[string]bool{}
	representative := 0
	for _, testCase := range cases {
		groups[testCase.Group]++
		if testCase.Representative {
			representative++
		}
		if testCase.ID == "" || testCase.Prompt == "" || testCase.ExpectedStatus == "" {
			t.Fatalf("case missing identity, prompt, or status: %+v", testCase)
		}
		if ids[testCase.ID] {
			t.Fatalf("duplicate case id %q", testCase.ID)
		}
		ids[testCase.ID] = true
		if testCase.ExpectedLoadedSkills == nil ||
			testCase.ForbiddenLoadedSkills == nil ||
			testCase.RequiredActions == nil ||
			testCase.ForbiddenActions == nil ||
			testCase.ExpectedUsedSkills == nil {
			t.Fatalf("case %s must record every expected/forbidden field", testCase.ID)
		}
		if testCase.ExpectedStatus != StatusAnswered && testCase.ExpectedStatus != StatusBlocked {
			t.Fatalf("case %s has unsupported expected status %q", testCase.ID, testCase.ExpectedStatus)
		}
	}
	for group, want := range wantGroups {
		if groups[group] != want {
			t.Fatalf("group %s cases = %d, want %d", group, groups[group], want)
		}
		delete(groups, group)
	}
	if len(groups) != 0 {
		t.Fatalf("unexpected corpus groups: %v", groups)
	}
	if representative != 24 {
		t.Fatalf("representative cases = %d, want 24", representative)
	}
}

func TestSkillEvaluationCorpusAvailabilityAndExclusion(t *testing.T) {
	cases := loadSkillEvalCases(t)
	expectedChecks := 0
	expectedHits := 0
	forbiddenChecks := 0
	forbiddenMisses := 0

	for _, testCase := range cases {
		profile := &CapabilityProfile{
			RoleClass:            testCase.CapabilityProfile.RoleClass,
			AvailableSystemRoots: append([]string(nil), testCase.CapabilityProfile.AvailableSystemRoots...),
		}
		loaded := skillIDs(allowedSkills(testCase.CapabilityProfile.ReadOnly, profile))
		for _, expected := range testCase.ExpectedLoadedSkills {
			expectedChecks++
			if containsString(loaded, expected) {
				expectedHits++
				continue
			}
			t.Errorf("%s: expected loaded skill %q; got %v", testCase.ID, expected, loaded)
		}
		for _, forbidden := range testCase.ForbiddenLoadedSkills {
			forbiddenChecks++
			if !containsString(loaded, forbidden) {
				forbiddenMisses++
				continue
			}
			t.Errorf("%s: forbidden skill %q was loaded; got %v", testCase.ID, forbidden, loaded)
		}
		for _, used := range testCase.ExpectedUsedSkills {
			if !containsString(loaded, used) {
				t.Errorf("%s: expected used skill %q is unavailable; loaded %v", testCase.ID, used, loaded)
			}
		}
	}

	if expectedHits != expectedChecks {
		t.Fatalf("availability recall = %d/%d, want 100%%", expectedHits, expectedChecks)
	}
	if forbiddenMisses != forbiddenChecks {
		t.Fatalf("forbidden-skill exclusion = %d/%d, want 100%%", forbiddenMisses, forbiddenChecks)
	}
	t.Logf("availability recall: %d/%d; forbidden-skill exclusion: %d/%d", expectedHits, expectedChecks, forbiddenMisses, forbiddenChecks)
}
