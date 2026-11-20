package main

import (
	"os"
	"path/filepath"
	"testing"

	gjeval "github.com/dosco/graphjin/agent/v3/eval"
)

// A saved experience response and a bare list of cases both read, and pages
// merge to one test per question.
func TestEvalMineReadsSavedExperience(t *testing.T) {
	dir := t.TempDir()
	saved := filepath.Join(dir, "experience.json")
	if err := os.WriteFile(saved, []byte(`{"cases":[{"id":"run_1-1","tier":"confirmed","prompt":"How many open orders are there?"}],
		"summary":{"candidates":3,"tests":1,"by_tier":{"confirmed":1},"dropped":{"list":2}},"page":{"runs_read":2}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	doc, err := readExperienceFile(saved)
	if err != nil || len(doc.Cases) != 1 || doc.Summary.Candidates != 3 || doc.Summary.Dropped["list"] != 2 {
		t.Fatalf("doc = %+v err = %v", doc, err)
	}
	bare := filepath.Join(dir, "cases.json")
	if err := os.WriteFile(bare, []byte(`[{"id":"run_2-1","tier":"incumbent","prompt":"What is total MRR?"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	doc, err = readExperienceFile(bare)
	if err != nil || len(doc.Cases) != 1 || doc.Summary.ByTier["incumbent"] != 1 {
		t.Fatalf("bare = %+v err = %v", doc, err)
	}

	all := experienceDocument{Summary: gjeval.MineSummary{ByTier: map[string]int{}, Dropped: map[string]int{}}}
	page := func(prompts ...string) experienceDocument {
		var p experienceDocument
		for _, prompt := range prompts {
			p.Cases = append(p.Cases, gjeval.MinedCase{Prompt: prompt, Tier: "incumbent"})
		}
		p.Summary.Candidates = len(prompts)
		return p
	}
	mergeExperience(&all, page("What is total MRR?", "How many open orders?"))
	mergeExperience(&all, page("what is  total MRR?"))
	if len(all.Cases) != 2 || all.Summary.Candidates != 3 || all.Summary.Dropped[gjeval.MineDropDuplicate] != 1 || all.Summary.Tests != 2 {
		t.Fatalf("merged = %+v", all.Summary)
	}

	// A confirmed copy on a later page replaces the incumbent one already kept.
	confirmed := page("How many open orders?")
	confirmed.Cases[0].Tier, confirmed.Cases[0].ID = gjeval.MineTierConfirmed, "run_old-1"
	mergeExperience(&all, confirmed)
	if len(all.Cases) != 2 || all.Cases[1].ID != "run_old-1" || all.Summary.ByTier[gjeval.MineTierConfirmed] != 1 || all.Summary.ByTier[gjeval.MineTierIncumbent] != 1 {
		t.Fatalf("confirmed copy lost: cases %+v tiers %v", all.Cases, all.Summary.ByTier)
	}
}
