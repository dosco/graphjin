package main

import (
	"errors"
	"fmt"
	"strings"

	gjeval "github.com/dosco/graphjin/agent/v3/eval"
	"github.com/spf13/cobra"
)

// Keeping held-out tasks out of training corpora.
//
// A split exists so a tuned model can be measured on questions it was not
// trained on. That guarantee is worth exactly as much as the care taken when
// building the corpus, and nothing about an exported file says which side its
// episodes came from — so the mistake is silent, and it shows up much later as
// a score that looks too good and cannot be explained.
//
// So the export refuses. Automatically when the run recorded which side it
// sampled, and on request when a split is named.

var errEvalSideEpisodes = errors.New("export would include held-out episodes")

// selectExportableEpisodes narrows a run's episodes to the ones a corpus may
// contain, and refuses when that would quietly include held-out work.
func selectExportableEpisodes(cmd *cobra.Command, episodes []gjeval.Episode,
	splitPath, side string, allowEvalSide bool) ([]gjeval.Episode, error) {
	if len(episodes) == 0 {
		return episodes, nil
	}
	if strings.TrimSpace(splitPath) == "" {
		return episodesByRecordedSide(cmd, episodes, allowEvalSide)
	}

	side = strings.ToLower(strings.TrimSpace(side))
	if side != "train" && side != "eval" {
		return nil, fmt.Errorf("--side must be train or eval, got %q", side)
	}
	split, err := gjeval.LoadSplit(splitPath)
	if err != nil {
		return nil, err
	}
	// The split has to be the one this run was drawn from. A manifest
	// regenerated at a different ratio names different tasks, so filtering by
	// it would be filtering against a holdout nobody used.
	if fingerprint := strings.TrimSpace(recordedSplitFingerprint(episodes)); fingerprint != "" &&
		fingerprint != split.Fingerprint() {
		return nil, fmt.Errorf(
			"this run was drawn from a different split than %s; export it with the split it recorded, or without --split",
			splitPath)
	}
	train, evalSide, unknown := gjeval.PartitionEpisodesBySide(episodes, *split)
	if len(evalSide) != 0 && side == "train" && !allowEvalSide {
		return nil, fmt.Errorf(
			"%w: %d of %d episode(s) are from the eval side of %s, and a training corpus built from them "+
				"defeats the holdout it was made for; pass --allow-eval-side to export them anyway",
			errEvalSideEpisodes, len(evalSide), len(episodes), splitPath)
	}
	selected := train
	if side == "eval" {
		selected = evalSide
	}
	if allowEvalSide && side == "train" {
		selected = append(append([]gjeval.Episode{}, train...), evalSide...)
	}
	if len(unknown) != 0 && cmd != nil {
		// Not a refusal: a task the split never mentions is not held out. It is
		// not vouched for either, which the caller should hear.
		fmt.Fprintf(cmd.ErrOrStderr(),
			"  %d episode(s) belong to tasks this split does not mention; they were left out.\n", len(unknown))
	}
	return selected, nil
}

// episodesByRecordedSide enforces what the run itself recorded, so a sampling
// run that drew from the eval side cannot be exported into a training corpus
// by simply omitting the flags.
func episodesByRecordedSide(cmd *cobra.Command, episodes []gjeval.Episode, allowEvalSide bool) ([]gjeval.Episode, error) {
	held := 0
	for _, episode := range episodes {
		if strings.EqualFold(episode.Provenance.SplitSide, "eval") {
			held++
		}
	}
	if held == 0 || allowEvalSide {
		if held != 0 && cmd != nil {
			fmt.Fprintf(cmd.ErrOrStderr(),
				"  exporting %d held-out episode(s) because --allow-eval-side was given.\n", held)
		}
		return episodes, nil
	}
	return nil, fmt.Errorf(
		"%w: this run recorded that it drew from the eval side of a split, so every one of its %d episode(s) is "+
			"held out; pass --allow-eval-side if you meant to train on them",
		errEvalSideEpisodes, held)
}

// recordedSplitFingerprint returns the split a run says it came from, if any.
func recordedSplitFingerprint(episodes []gjeval.Episode) string {
	for _, episode := range episodes {
		if fingerprint := strings.TrimSpace(episode.Provenance.SplitFingerprint); fingerprint != "" {
			return fingerprint
		}
	}
	return ""
}
