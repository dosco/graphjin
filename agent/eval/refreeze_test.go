package eval

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func refreezeTask(category Category, name string) Task {
	return Task{
		ID: name, Slug: name, Category: category, Difficulty: DifficultyT1,
		Prompt: "question " + name, Provenance: Provenance{Source: "catalog-entity"},
	}
}

// Retiring a family must leave every other frozen task where it was and hand
// the freed slots out the way the sampler's backfill does: round-robin over the
// backfill categories, each taking the first task of the draw that the suite
// does not already ask.
func TestRefillRetiredSlotsKeepsTheFrozenTasksAndBackfillsInSamplerOrder(t *testing.T) {
	twin := refreezeTask(CategoryAction, "action-twin")
	twin.Tier = TierExecution
	twin.Provenance.Source = "deeporg-reference"
	reference := refreezeTask(CategoryAction, "action-need")
	reference.Provenance.Source = "deeporg-reference"
	kept := []Task{
		reference,
		refreezeTask(CategoryAggregate, "aggregate-frozen"),
		refreezeTask(CategoryWindow, "window-frozen"),
		refreezeTask(CategoryRefusal, "refusal-frozen"),
		twin,
	}

	var draw []Task
	for _, category := range []Category{
		CategoryAggregate, CategoryWindow, CategoryRanking, CategoryDiscovery, CategoryRefusal,
		CategoryTraversal, CategoryMultiTurn,
	} {
		for i := 0; i < 4; i++ {
			draw = append(draw, refreezeTask(category, fmt.Sprintf("%s-new-%d", category, i)))
		}
	}
	// A draw offers the questions the suite already asks, too: the same task,
	// and the same words checked a different way.
	draw = append(draw, kept[1])
	reworded := refreezeTask(CategoryWindow, "window-rechecked")
	reworded.Prompt = strings.ToUpper(kept[2].Prompt)
	reworded.Behavior.RequiredActions = []string{"query_catalog"}
	draw = append(draw, reworded)
	// Reference tasks and execution twins never take a freed slot.
	curated := refreezeTask(CategoryAggregate, "aggregate-reference")
	curated.Provenance.Source = "deeporg-reference"
	extraTwin := refreezeTask(CategoryAggregate, "aggregate-twin")
	extraTwin.Tier = TierExecution
	draw = append(draw, curated, extraTwin)

	retire := map[Category]bool{CategoryMultiTurn: true}
	tasks, added, err := refillRetiredSlots(kept, draw, 7, retire, 23)
	if err != nil {
		t.Fatal(err)
	}

	// Seven slots over five backfill categories: two full rounds would be ten,
	// so the first two categories in quota order get a second task.
	want := map[Category]int{
		CategoryAggregate: 2, CategoryWindow: 2, CategoryRanking: 1, CategoryDiscovery: 1, CategoryRefusal: 1,
	}
	if got := categoryCounts(added); len(added) != 7 || fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("added %d tasks as %v, want %v", len(added), got, want)
	}
	for _, task := range added {
		switch {
		case task.Category == CategoryTraversal || task.Category == CategoryMultiTurn:
			t.Errorf("%s took a slot: traversal never backfills and multi-turn is being retired", task.ID)
		case task.ID == "aggregate-frozen" || task.ID == "window-rechecked":
			t.Errorf("%s is a question the suite already asks", task.ID)
		case task.ID == "aggregate-reference" || task.ID == "aggregate-twin":
			t.Errorf("%s is not part of the sampled pool", task.ID)
		}
	}

	// The kept tasks keep their order, each new task sits with its own
	// category, and the execution twin is still last.
	var keptOrder []string
	for _, task := range tasks {
		for _, original := range kept {
			if task.ID == original.ID {
				keptOrder = append(keptOrder, task.ID)
			}
		}
	}
	if strings.Join(keptOrder, ",") != "action-need,aggregate-frozen,window-frozen,refusal-frozen,action-twin" {
		t.Fatalf("kept tasks moved: %v", keptOrder)
	}
	if len(tasks) != len(kept)+7 || tasks[len(tasks)-1].ID != "action-twin" {
		t.Fatalf("suite has %d tasks ending in %s, want %d ending in the twin", len(tasks), tasks[len(tasks)-1].ID, len(kept)+7)
	}
	if tasks[1].ID != "aggregate-frozen" || tasks[2].Category != CategoryAggregate || tasks[3].Category != CategoryAggregate {
		t.Fatalf("new aggregate tasks do not follow the frozen one: %s %s %s", tasks[1].ID, tasks[2].ID, tasks[3].ID)
	}

	// Same seed, same picks: a refreeze has to be reproducible from its inputs.
	_, again, err := refillRetiredSlots(kept, draw, 7, retire, 23)
	if err != nil {
		t.Fatal(err)
	}
	for i := range added {
		if added[i].ID != again[i].ID {
			t.Fatalf("pick %d changed between identical runs: %s then %s", i, added[i].ID, again[i].ID)
		}
	}

	// A catalog that cannot fill the slots is an error, never a smaller suite.
	if _, _, err := refillRetiredSlots(kept, draw[:3], 7, retire, 23); err == nil || !strings.Contains(err.Error(), "freed slot") {
		t.Fatalf("a short draw must be refused, got %v", err)
	}
	// Whatever order the draw comes in, a question the suite already asks is
	// never the one that fills a slot.
	for _, only := range []Task{kept[1], reworded} {
		if _, picked, err := refillRetiredSlots(kept, []Task{only}, 1, retire, 23); err == nil {
			t.Fatalf("%s is already asked, yet it filled a slot: %+v", only.ID, picked)
		}
	}
}

func TestRefreezeRefusesToRetireNothing(t *testing.T) {
	verifier, _, _ := countingVerifier(t, 0)
	generator := Generator{Source: staticCatalogSource{snapshot: CatalogSnapshot{Fingerprint: "catalog"}}, Verifier: verifier}
	frozen := Suite{Tasks: []Task{refreezeTask(CategoryAggregate, "aggregate-frozen")}}
	if _, _, err := generator.Refreeze(context.Background(), frozen, RefreezeOptions{Seed: 23}); err == nil {
		t.Fatal("a refreeze with nothing to retire must be refused")
	}
	_, _, err := generator.Refreeze(context.Background(), frozen, RefreezeOptions{Seed: 23, Retire: []Category{CategoryMultiTurn}})
	if err == nil || !strings.Contains(err.Error(), "no task in the retired categories") {
		t.Fatalf("retiring a family the suite does not hold must be refused, got %v", err)
	}
}
