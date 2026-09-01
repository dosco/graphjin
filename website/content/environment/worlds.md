---
title: "Building And Baking Worlds"
nav_title: "Worlds"
description: "Generate new organizations to train against, and bake one into an image your team shares."
nav_group: "environment"
doc_kind: "guide"
weight: 50
---

A policy trained against one schema learns that schema. Environment diversity is
the difference between an agent that can query your demo and an agent that can
query a database it has never seen.

## Generate an organization

```bash
graphjin env new-world --domain logistics --seed 7 --out ./world-logistics
```

Three vocabularies ship — `logistics`, `clinic`, `retail`. The same domain and
seed produce the same company every time, so a world is reproducible from two
values rather than from a directory somebody has to keep.

`--tables` bounds the size. `--pathologies` builds in the schema awkwardness
real databases have and demos do not:

| Pathology | What it adds |
|---|---|
| `distractor-columns` | Columns that look relevant and are not |
| `synonym-collision` | Two plausible names for different things |
| `legacy-columns` | Superseded columns still carrying data |
| `nullable-gaps` | Nullable columns with meaningful absence |

An agent that only ever sees a tidy schema has not been tested on the thing that
makes real schemas hard.

## Describe one instead

```bash
graphjin env new-world --describe "genome sequencing lab" --seed 7 --yes
```

A model names the records and the vocabulary; the engine validates every
identifier, rejects anything malformed with a reason, and renders the world
deterministically. The pack it produced is written to `world-pack.json`, and
that artifact — not the model — is the source of truth from then on:

```bash
graphjin env new-world --pack world-pack.json --seed 7 --out ./rebuild
```

Rebuilding from the pack spends nothing and produces identical bytes. The model
runs once.

## Bake a world into an image

The reference image serves the built-in demo. Your own world is a derived image:

```dockerfile
FROM dosco/graphjin:env-latest
COPY clone-acme /world
ENV GJ_ENV_PATH=/world
ENV GJ_ENV_SUITE=/world/eval/suite.yml
ENV GJ_ENV_SPLIT=/world/eval/suite.split.json
ENV GJ_ENV_DATA_ANCHOR=2026-08-01
```

A derived image can set `ENTRYPOINT`, `ENV` and `WORKDIR`, which is exactly why
this customization belongs here rather than in the base image.

### The one trap

Everything in a GraphJin config resolves against the **config directory** —
`root:` on a file source, `specs_dir:` on an API source, `path:` on a code
source. There is one exception: a SQLite `path:` is handed to the driver exactly
as written, and so stays relative to the **working directory**.

In the image the working directory is `/tmp/graphjin-env`, not `/world`. Make it
absolute:

```yaml
database:
  type: sqlite
  path: /world/demo/app.sqlite3
```

This catches everyone once. It is the only path in a GraphJin config that
behaves this way.

### Pin the day

Any world with date-relative data needs `GJ_ENV_DATA_ANCHOR` or
`GJ_ENV_FREEZE_TIME`. The base image pins its clock to its own build date, which
is right for the built-in demo and almost certainly wrong for your rows.
`/health` reports `dataset.data_anchor` and `capabilities.freeze_time_source`, so
you can check rather than assume.

## Verifying a world before you trust it

A world is only useful if a suite generated from it is verified against it. Boot
it and generate:

```bash
graphjin eval create --demo --path ./world-logistics --writable --split 0.8
```

If `catalog_match` on `/health` is `false` when you later serve them together,
the suite and the world have drifted apart and every episode is being graded
against answers computed elsewhere. The server refuses that pairing at startup
unless you override it.
