---
title: "graphjin env And graphjin eval Reference"
nav_title: "CLI Reference"
description: "Every subcommand and flag of both namespaces, plus the GJ_ENV_, GJ_SUPPORT_ and GJ_GENERATOR_ variable families."
nav_group: "environment"
doc_kind: "reference"
weight: 70
---

Two namespaces. `graphjin eval` builds and runs suites; `graphjin env` serves
worlds and generates them.

## graphjin env

| Command | Purpose |
|---|---|
| `env serve` | Serve graded episodes over HTTP for a training or evaluation loop |
| `env health` | Check that a served environment is ready, for use as a container health check |
| `env new-world` | Generate a fresh organization to train or measure against |
| `env clone` | Learn a running server's schema and write a local synthetic environment |

### env serve

| Flag | Default | Purpose |
|---|---|---|
| `--path` | built-in demo | Project to serve |
| `--suite` | `eval/suite.yml` | Task suite. The reserved word `public` selects the embedded suite; a file named `public` is `./public` |
| `--split` | — | Split manifest, or `auto[:ratio]` to derive one from the suite |
| `--side` | `train` | Which side of the split to serve: `train` or `eval` |
| `--pool` | `2` | Isolated worlds to run episodes against |
| `--listen` | `127.0.0.1:8090` | Address to serve on. The environment image defaults to `0.0.0.0:8090` |
| `--work-dir` | — | Directory to run in; the demo and each world's state are written below it |
| `--freeze-time` | — | Run every episode against a fixed clock (RFC3339) |
| `--data-anchor` | — | Pin the seeded data to a day (`YYYY-MM-DD`) |
| `--allow-catalog-drift` | off | Serve a suite verified against a different catalog |
| `--reward-profile` | `rl` | Profile episodes are graded under |
| `--step` | off | Let a trainer supply each model completion |
| `--step-timeout` | `5m` | How long a step episode may sit idle before its world is reclaimed |
| `--external` | off | Let an external agent drive episodes over MCP |
| `--external-timeout` | `10m` | How long an external episode may run |
| `--advertise-url` | request `Host` | Base URL external agents should use |
| `--support-model` and friends | — | Route the distiller and responder to a different model |

### env clone

| Flag | Default |
|---|---|
| `--url` | the server from `graphjin cli setup` |
| `--out` | `./clone` |
| `--rows` | `12` synthetic rows per table |
| `--seed` | `1` |
| `--token-env` | `GRAPHJIN_EVAL_TOKEN` |

### env new-world

| Flag | Default |
|---|---|
| `--domain` | `logistics` (also `clinic`, `retail`) |
| `--describe` | —, let a model name the records |
| `--pack` | —, rebuild from a saved `world-pack.json` |
| `--tables` | `0`, the whole domain |
| `--pathologies` | — `distractor-columns`, `synonym-collision`, `legacy-columns`, `nullable-gaps` |
| `--seed` | `1` |
| `--out` | — |

### env health

`--url` (defaults to the port `GJ_ENV_LISTEN` names), `--timeout` (`5s`),
`--json`. Exits zero only on a `200` whose status is `ready`.

## graphjin eval

| Command | Purpose |
|---|---|
| `create` | Generate a verified catalog-derived suite |
| `add` | Add one model-assisted question with a verified hidden oracle |
| `rm` | Remove a task through the validated suite writer |
| `run` | Run the current suite |
| `baseline` | Run and deliberately promote a passing baseline |
| `bench` | Generate and run the extended stratified benchmark distribution |
| `sample` | Collect several graded attempts at each task, for a training corpus |
| `rescore` | Recompute a completed run from stored episodes, with no provider traffic |
| `export` | Export a run's episodes as CodeAct trajectories |
| `author` | Author watch, confirmation, follow-up and scenario tasks with a capable model |
| `publish` | Publish one shareable report to the benchmark website |

Persistent flags: `--demo`, `--remote`, `--path`, `--yes`, `--json`, `--debug`,
`--freeze-time`.

`sample` adds `--repeats`, `--temperature`, `--top-p`, `--split`, `--side`.
`export` adds `--stage` (default `executor`), `--reward-profile`,
`--include-environment-steps`, `--split`, `--side`, `--allow-eval-side`, `--out`.
`run`, `baseline` and `bench` share `--resume`, `--restart`, `--concurrency`.

## Environment variables

### GJ_ENV_ — the served environment

Every `env serve` flag has one variable. **A flag wins when it is passed**, and
**a `GJ_ENV_` variable the server does not read is a startup error** rather than
a silent default.

`GJ_ENV_PATH`, `GJ_ENV_WORK_DIR`, `GJ_ENV_SUITE`, `GJ_ENV_SPLIT`, `GJ_ENV_SIDE`,
`GJ_ENV_POOL`, `GJ_ENV_LISTEN`, `GJ_ENV_FREEZE_TIME`, `GJ_ENV_DATA_ANCHOR`,
`GJ_ENV_REWARD_PROFILE`, `GJ_ENV_ALLOW_CATALOG_DRIFT`, `GJ_ENV_STEP`,
`GJ_ENV_STEP_TIMEOUT`, `GJ_ENV_EXTERNAL`, `GJ_ENV_EXTERNAL_TIMEOUT`,
`GJ_ENV_ADVERTISE_URL`.

### GJ_AGENT_ — the model being evaluated

`GJ_AGENT_PROVIDER`, `GJ_AGENT_MODEL`, `GJ_AGENT_BASE_URL`,
`GJ_AGENT_API_KEY_ENV`, plus `GJ_AGENT_TEMPERATURE` and `GJ_AGENT_TOP_P` for
sampling. See [Agent Configuration](/reference/config-reference/).

### GJ_SUPPORT_ — the distiller and responder

`GJ_SUPPORT_PROVIDER`, `GJ_SUPPORT_MODEL`, `GJ_SUPPORT_BASE_URL`,
`GJ_SUPPORT_API_KEY_ENV`, `GJ_SUPPORT_REASONING`.

These deliberately **do not** fall back to `GJ_AGENT_*`. Falling back would
serve the support stages with the very model under evaluation, which is what the
separation exists to prevent.

### GJ_GENERATOR_ — the model that authors tasks

`GJ_GENERATOR_PROVIDER`, `GJ_GENERATOR_MODEL`, `GJ_GENERATOR_BASE_URL`,
`GJ_GENERATOR_API_KEY_ENV`, `GJ_GENERATOR_REASONING`.

These **do** fall back to `GJ_AGENT_*`, because authoring with the evaluated
model is a reasonable default when you have only configured one.
