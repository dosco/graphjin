package eval

import (
	"context"
	"fmt"
	mathrand "math/rand"
	"strings"
	"time"
)

// Retiring a family from a frozen suite.
//
// Regenerating a frozen suite outright is not a small change: the demo's data
// is anchored to the calendar, and a same-day regeneration was measured to swap
// a third of the questions with nothing else different. So when a family has to
// leave, every other task stays exactly as it was frozen, and only the slots
// the family held are refilled. They go where the sampler's own backfill would
// send them, round-robin over the backfill categories, each taking the first
// task of a fresh draw that the suite does not already ask.

// RefreezeOptions says what leaves a frozen suite and what the result is called.
type RefreezeOptions struct {
	Seed int64
	Name string
	// Retire names the categories whose tasks leave the suite.
	Retire []Category
	// VerifyConcurrency bounds how many oracles are resolved at once.
	VerifyConcurrency int
}

// RefreezeReport lists what a refreeze took out and what it put in.
type RefreezeReport struct {
	Retired []Task
	Added   []Task
	Kept    int
}

// Refreeze returns the frozen suite without the retired categories, at the
// same intent-tier size, stamped with this binary's generator version and the
// live catalog's fingerprint. Kept tasks are carried over untouched and must
// still verify against the live instance: a refreeze that quietly dropped one
// would shrink the suite it claims to preserve.
func (g Generator) Refreeze(ctx context.Context, frozen Suite, opts RefreezeOptions) (*Suite, RefreezeReport, error) {
	var report RefreezeReport
	if g.Source == nil || g.Verifier == nil {
		return nil, report, fmt.Errorf("refreeze needs a catalog source and an oracle verifier")
	}
	if len(opts.Retire) == 0 {
		return nil, report, fmt.Errorf("refreeze needs at least one category to retire")
	}
	retire := make(map[Category]bool, len(opts.Retire))
	for _, category := range opts.Retire {
		retire[category] = true
	}
	var kept []Task
	freed := 0
	for _, task := range frozen.Tasks {
		if !retire[task.Category] {
			kept = append(kept, task)
			continue
		}
		report.Retired = append(report.Retired, task)
		if task.Tier != TierExecution {
			freed++
		}
	}
	if len(report.Retired) == 0 {
		return nil, report, fmt.Errorf("the frozen suite has no task in the retired categories")
	}
	report.Kept = len(kept)

	snapshot, err := g.Source.Snapshot(ctx)
	if err != nil {
		return nil, report, err
	}
	stillValid, err := g.VerifyTasks(ctx, append([]Task(nil), kept...), opts.Seed, opts.VerifyConcurrency)
	if err != nil {
		return nil, report, err
	}
	if len(stillValid) != len(kept) {
		valid := make(map[string]bool, len(stillValid))
		for _, task := range stillValid {
			valid[task.ID] = true
		}
		var lost []string
		for _, task := range kept {
			if !valid[task.ID] {
				lost = append(lost, firstNonEmpty(task.Slug, task.ID))
			}
		}
		return nil, report, fmt.Errorf("%d kept task(s) no longer verify against the live instance: %s",
			len(lost), strings.Join(lost, ", "))
	}

	var candidates []Task
	for _, family := range candidateFamilies {
		candidates = append(candidates, family.Generate(snapshot, opts.Seed)...)
	}
	draw, err := g.VerifyTasks(ctx, candidates, opts.Seed, opts.VerifyConcurrency)
	if err != nil {
		return nil, report, err
	}
	tasks, added, err := refillRetiredSlots(kept, draw, freed, retire, opts.Seed)
	if err != nil {
		return nil, report, err
	}
	report.Added = added

	suite := frozen
	suite.Name = firstNonEmpty(strings.TrimSpace(opts.Name), frozen.Name)
	suite.CreatedAt = time.Time{}
	suite.CatalogFingerprint = snapshot.Fingerprint
	suite.Generator = GeneratorMeta{Version: GeneratorVersion, Seed: frozen.Generator.Seed, Scale: frozen.Generator.Scale}
	suite.Tasks = tasks
	if err := suite.Normalize(); err != nil {
		return nil, report, err
	}
	return &suite, report, nil
}

// refillRetiredSlots returns the kept tasks with the freed slots refilled from
// a verified draw, and the tasks it added.
func refillRetiredSlots(kept, draw []Task, freed int, retire map[Category]bool, seed int64) ([]Task, []Task, error) {
	intent, _ := partitionExecutionTwins(draw)
	_, generated := partitionCuratedTasks(intent)

	// The draw is ordered exactly as the sampler orders it, with one generator
	// stepping through the categories in quota order, so "the first task the
	// suite does not already ask" means the same thing here as in a generation.
	rng := mathrand.New(mathrand.NewSource(seed))
	byCategory := make(map[Category][]Task, len(categoryQuotaSpecs))
	for _, spec := range categoryQuotaSpecs {
		var items []Task
		for _, task := range generated {
			if task.Category == spec.Category && !retire[task.Category] {
				items = append(items, task)
			}
		}
		byCategory[spec.Category] = orderedCategoryTasks(items, rng)
	}
	asked := make(map[string]bool, 2*len(kept))
	for _, task := range kept {
		asked[taskStructureKey(task)] = true
		asked[refreezePromptKey(task)] = true
	}
	var added []Task
	for len(added) < freed {
		progress := false
		for _, spec := range categoryQuotaSpecs {
			if !spec.Backfill || len(added) == freed {
				continue
			}
			items := byCategory[spec.Category]
			for len(items) != 0 && (asked[taskStructureKey(items[0])] || asked[refreezePromptKey(items[0])]) {
				items = items[1:]
			}
			if len(items) == 0 {
				byCategory[spec.Category] = nil
				continue
			}
			pick := items[0]
			byCategory[spec.Category] = items[1:]
			asked[taskStructureKey(pick)] = true
			asked[refreezePromptKey(pick)] = true
			added = append(added, pick)
			progress = true
		}
		if !progress {
			return nil, added, fmt.Errorf("the live catalog offers only %d new task(s) for the %d freed slot(s)",
				len(added), freed)
		}
	}

	// Each new task follows the last intent-tier task of its own category, so
	// execution twins stay grouped at the end and the kept tasks keep their
	// relative order.
	tasks := append([]Task(nil), kept...)
	for _, task := range added {
		at := len(tasks)
		for index := len(tasks) - 1; index >= 0; index-- {
			if tasks[index].Tier == TierExecution {
				at = index
				continue
			}
			if tasks[index].Category == task.Category {
				at = index + 1
				break
			}
		}
		tasks = append(tasks[:at], append([]Task{task}, tasks[at:]...)...)
	}
	return tasks, added, nil
}

// refreezePromptKey catches a question the suite already asks in the same
// words. The structural key alone would let one through when the generator has
// since changed how it checks that question.
func refreezePromptKey(task Task) string {
	return "prompt\x00" + string(task.Category) + "\x00" + strings.ToLower(strings.TrimSpace(task.Prompt))
}
