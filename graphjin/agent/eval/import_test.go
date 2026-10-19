package eval

import "testing"

func TestImportFrozenCorpora(t *testing.T) {
	tasks, err := ImportCorpora(ImportOptions{
		BehaviorCorpusPath: "../testdata/skill_eval_cases.json",
		DataCorpusPath:     "../testdata/data_eval_cases.json",
		Seed:               23,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 74 {
		t.Fatalf("imported %d tasks, want 74", len(tasks))
	}
	suite := Suite{Name: "imported", Generator: GeneratorMeta{Version: GeneratorVersion, Seed: 23, Scale: len(tasks)}, Tasks: tasks}
	if err := suite.Normalize(); err != nil {
		t.Fatalf("imported suite is invalid: %v", err)
	}
}
