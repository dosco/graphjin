package eval

import (
	"math"
	"path/filepath"
	"testing"
)

func splitSuite(t *testing.T, n int) Suite {
	t.Helper()
	suite := Suite{SchemaVersion: SuiteSchemaVersion, Name: "split", Generator: GeneratorMeta{Version: GeneratorVersion, Seed: 23, Scale: n}}
	for i := 0; i < n; i++ {
		task := categoryTask("split-"+padSlug(i), CategoryAggregate)
		task.Prompt = "How many orders are there? " + padSlug(i)
		if err := task.Normalize(); err != nil {
			t.Fatal(err)
		}
		suite.Tasks = append(suite.Tasks, task)
	}
	return suite
}

func padSlug(i int) string {
	digits := "abcdefghijklmnopqrstuvwxyz"
	return string(digits[i%26]) + string(digits[(i/26)%26]) + string(digits[(i/676)%26])
}

func TestSplitIsDeterministicAndDisjoint(t *testing.T) {
	suite := splitSuite(t, 200)
	first, err := SplitSuite(suite, 0.8, nil)
	if err != nil {
		t.Fatal(err)
	}
	again, err := SplitSuite(suite, 0.8, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Train) != len(again.Train) || len(first.Eval) != len(again.Eval) {
		t.Fatal("split sizes differ between runs")
	}
	for i := range first.Train {
		if first.Train[i] != again.Train[i] {
			t.Fatalf("train side differs at %d", i)
		}
	}
	if err := first.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(first.Train)+len(first.Eval) != len(suite.Tasks) {
		t.Fatalf("split lost tasks: %d + %d != %d", len(first.Train), len(first.Eval), len(suite.Tasks))
	}
}

func TestSplitApproximatesTheRequestedRatio(t *testing.T) {
	suite := splitSuite(t, 400)
	split, err := SplitSuite(suite, 0.75, nil)
	if err != nil {
		t.Fatal(err)
	}
	ratio := float64(len(split.Train)) / float64(len(suite.Tasks))
	if math.Abs(ratio-0.75) > 0.06 {
		t.Fatalf("train share %.3f is too far from the requested 0.75", ratio)
	}
}

// A task must keep its side when the suite is regenerated at another scale.
// Otherwise yesterday's training task becomes today's measurement and the
// number stops meaning anything.
func TestSplitAssignmentSurvivesRegenerationAtAnotherScale(t *testing.T) {
	small := splitSuite(t, 50)
	large := splitSuite(t, 300)
	smallSplit, err := SplitSuite(small, 0.8, nil)
	if err != nil {
		t.Fatal(err)
	}
	largeSplit, err := SplitSuite(large, 0.8, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range small.Tasks {
		inSmallTrain := smallSplit.Contains(smallSplit.Train, task.ID)
		inLargeTrain := largeSplit.Contains(largeSplit.Train, task.ID)
		if inSmallTrain != inLargeTrain {
			t.Fatalf("task %s changed sides between suite sizes", task.ID)
		}
	}
}

// Widening the training set must only ever take tasks from eval. If it
// reshuffled, a task already measured against could be promoted into training
// without anyone noticing.
func TestRaisingTheRatioOnlyMovesTasksIntoTraining(t *testing.T) {
	suite := splitSuite(t, 300)
	narrow, err := SplitSuite(suite, 0.5, nil)
	if err != nil {
		t.Fatal(err)
	}
	wide, err := SplitSuite(suite, 0.9, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range narrow.Train {
		if !wide.Contains(wide.Train, id) {
			t.Fatalf("task %s left the training set when the ratio was raised", id)
		}
	}
	if len(wide.Train) <= len(narrow.Train) {
		t.Fatal("raising the ratio did not widen the training set")
	}
}

// Holding out a family keeps a whole capability unseen, which answers a sharper
// question than whether the model learned these particular rows.
func TestHoldoutFamilyIsNeverTrainedOn(t *testing.T) {
	suite := splitSuite(t, 100)
	for i := range suite.Tasks {
		if i%3 == 0 {
			suite.Tasks[i].Provenance.Source = "rel-traversal"
		}
	}
	split, err := SplitSuite(suite, 0.95, []string{"rel-traversal"})
	if err != nil {
		t.Fatal(err)
	}
	held := map[string]bool{}
	for _, task := range suite.Tasks {
		if task.Provenance.Source == "rel-traversal" {
			held[task.ID] = true
		}
	}
	if len(held) == 0 {
		t.Fatal("fixture produced no held-out tasks")
	}
	for _, id := range split.Train {
		if held[id] {
			t.Fatalf("held-out task %s was placed in training", id)
		}
	}
}

func TestSplitRejectsImpossibleRatio(t *testing.T) {
	suite := splitSuite(t, 10)
	for _, ratio := range []float64{-0.1, 1.5} {
		if _, err := SplitSuite(suite, ratio, nil); err == nil {
			t.Fatalf("expected ratio %v to be rejected", ratio)
		}
	}
}

func TestSplitRoundTripsThroughDisk(t *testing.T) {
	suite := splitSuite(t, 40)
	split, err := SplitSuite(suite, 0.7, []string{"rel-traversal"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "eval", "suite.split.json")
	if err := SaveSplit(path, split); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSplit(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SuiteFingerprint != split.SuiteFingerprint || len(loaded.Train) != len(split.Train) {
		t.Fatalf("round trip changed the split: %+v", loaded)
	}
}

func TestSplitValidateCatchesATaskOnBothSides(t *testing.T) {
	split := SuiteSplit{SchemaVersion: SplitSchemaVersion, Train: []string{"gjv1_a"}, Eval: []string{"gjv1_a"}}
	if err := split.Validate(); err == nil {
		t.Fatal("expected a task on both sides to be rejected")
	}
}
