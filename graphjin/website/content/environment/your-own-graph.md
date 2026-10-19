---
title: "Measure Your Agent On Your Own Graph"
nav_title: "Your Own Graph"
description: "Clone the shape of a running GraphJin server into a local synthetic world, generate a verified suite, and grade against that."
nav_group: "environment"
doc_kind: "guide"
weight: 40
---

For anyone who already runs GraphJin and wants to know how an agent performs on
their data — not on a demo. If you want a ready environment to train against,
start at the [quickstart](/environment/quickstart/) instead.

The problem this solves: you cannot point a training loop at production, and a
benchmark built on somebody else's schema tells you very little about yours.

## Clone the shape, not the data

```bash
graphjin env clone --url https://graphjin.internal --out ./clone-acme --seed 7
```

This reads the **catalog** of a running server and writes a local SQLite project
you can boot. What crosses over is what any connected agent can already see:
table and column structure, relationships, saved query names, and the closed
value sets the catalog publishes — the fact that `status` is one of
`active`/`past_due`/`cancelled`.

**No rows are read.** Synthetic records are generated locally from that
structure, honouring foreign keys, not-null constraints and the published value
sets. A file source crosses over by name only, with synthetic documents; API
sources are listed and skipped with a reason, because mocking an arbitrary API
would mean grading against invented answers.

The same snapshot and seed produce the same clone every time, and
`clone-manifest.json` records the source fingerprint, the seed, and every type
that could not be mapped exactly.

## Generate a suite from it

```bash
graphjin eval create --demo --path ./clone-acme --writable \
  --scale 300 --composition coverage --split 0.8
```

Every task is generated from your catalog and then **verified against a booted
copy of the clone** before it ships. A task whose oracle does not resolve is
dropped rather than shipped, so a suite cannot contain a question with no
checkable answer.

`--split 0.8` also writes `eval/suite.split.json`, dividing tasks into a
training side and a held-out side by content identifier.

## Let a bigger model write the harder tasks

Generated tasks cover what can be derived mechanically. The richer ones —
standing questions, multi-turn confirmations, questions spanning a database and
a document — are phrased by a capable model and then assembled and verified by
the engine:

```bash
export GJ_GENERATOR_MODEL=<a-capable-model>
graphjin eval author --demo --path ./clone-acme --kinds watch,confirmation,file --yes
```

The authoring model chooses and phrases; the engine constructs, gates
fail-closed, and verifies against a booted instance. A pick naming a table or
value that does not exist is rejected with a reason rather than becoming a task
nobody can pass. `--yes` is required to spend provider tokens; the call count
and model are printed first.

The result lands in `eval/authored.yml` and is picked up by the next
`eval create`.

## Serve it

```bash
graphjin env serve --path ./clone-acme --suite eval/suite.yml \
  --split eval/suite.split.json --side train --pool 4 \
  --freeze-time 2026-08-01T12:00:00Z
```

From here it is the same environment the [quickstart](/environment/quickstart/)
describes: the same routes, the same [drive modes](/environment/drive-modes/),
the same reward contract.

`--freeze-time` matters more than it looks. Without it, a run that crosses
midnight asks a different question of the same rows, and the episodes on either
side stop being comparable.

## Gating a release rather than training

If your goal is to know that an agent still works after a model, prompt or
GraphJin upgrade — rather than to train one — that is a different workflow with
its own baseline and exit codes:

[Evaluate the GraphJin agent](/agentic/evaluation/)

## Bake it into an image

A cloned world can be baked into a derived image so a team shares one
environment rather than one recipe. See
[Building and baking worlds](/environment/worlds/), including the SQLite path
trap that catches everyone once.
