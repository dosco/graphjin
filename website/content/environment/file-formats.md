---
title: "Suites, Splits, Packs, And Trajectories"
nav_title: "File Formats"
description: "What each artifact is, who writes it, and what breaks if you edit it by hand."
nav_group: "environment"
doc_kind: "reference"
weight: 90
---

Every file here is written by a command and read by another. **None of them are
hand-editable**, and the reason is the same in each case: each carries a
fingerprint that something downstream compares against, so an edit that looks
harmless silently detaches the file from the thing that validates it.

## eval/suite.yml

Written by `eval create`, `eval add`, `eval rm`, `eval author`.

The tasks: prompts, categories, difficulties, answer rules, behaviour rules, and
the hidden oracle for each. Also `generator.version`, which decides whether a
binary is allowed to run the suite at all, and `catalog_fingerprint`, which
records the schema each oracle was verified against.

**Hand-editing breaks two things.** Task identifiers are content hashes, so
changing a prompt without regenerating detaches the task from every stored
episode that referenced it. And an oracle edited by hand has not been verified
against a booted instance, which is the one property that makes a suite a
measurement.

Use `eval add` and `eval rm`, which go through the validated writer.

## eval/suite.split.json

Written by `eval create --split <ratio>`. Read by `eval sample`, `eval export`
and `env serve`.

Which task identifiers are on the training side and which are held out, plus
`suite_fingerprint` — the suite it was cut from. Every consumer checks that
fingerprint, because a split naming identifiers from another suite would hold
out nothing at all while appearing to work.

`--split auto[:ratio]` derives the same division without a file, from each
task's content identifier. Two processes reach the same answer with no
coordination, which is what lets one image serve a train container and an eval
container that agree.

## eval/authored.yml

Written by `eval author`. Read by the next `eval create`.

Tasks a capable model phrased and the engine assembled and verified — watches,
confirmations, follow-ups, cross-source questions. Carries `authored_by`
recording the model and the hash of the authoring prompts, so a task's origin is
part of its record.

Authored tasks are injected as candidates and go through the identical
verification bar as generated ones. They are not exempt.

## world-pack.json

Written by `env new-world --describe`. Read by `env new-world --pack`.

The entity vocabulary a model produced: tables, labels, metrics, dates,
statuses, and which entity follows which. Once written, **the pack is the source
of truth, not the model** — the same pack and seed rebuild identical bytes with
no provider traffic.

This is the artifact that makes a described world reproducible. Keep it with the
world.

## clone-manifest.json

Written by `env clone`.

The source catalog's fingerprint, the seed, per-table type mapping notes, and
every element that could not be carried over with the reason why — an API source
skipped, a column type with no exact equivalent. It is the record of what the
clone does and does not claim to represent.

## Trajectory JSONL

Written by `eval export`. One JSON object per line.

Each record carries the task, the reward vector under the requested profile, and
the steps: the rendered prompt the model actually saw, the program it produced,
the observation that came back, and which stage authored it.

Two fields decide whether a record is usable for training. `prompts_recorded`
says the rendered prompts were captured; `authorship_resolved` says each step
can be attributed to the model rather than to the runtime. The exporter refuses
to emit records failing either, rather than emitting one that would train a
model on text it never wrote.

Steps the runtime authored on the model's behalf are marked and dropped by
default; `--include-environment-steps` keeps them, which is useful for debugging
and wrong for training.

`--stage` selects which of the three policies the trajectory is built for and
defaults to `executor`. Mixing stages into one corpus teaches none of them.

## Episode records

Written to `.graphjin-evals/episodes/<run>/` by every run.

One file per episode, with the full response including the trace. This is what
`eval rescore` recomputes from and what `eval export` reads, so a run can be
re-graded under a different reward profile or a corrected contract **without any
provider traffic**. Secrets are redacted on the way in.
