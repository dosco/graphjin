# GraphJin Eval v1

> New to agent evaluation? Start with the guided website tutorial:
> [Evaluate the GraphJin Agent](https://graphjin.com/agentic/evaluation/).

GraphJin Eval is the generator-first benchmark for the dynamic query-authoring
agent. It uses versioned tasks, hidden executable oracles, local trajectory
capture, majority verdicts, and reproducible baseline comparison. The engine is
also the schema and interface foundation for a future resettable RL rollout
environment; v1 remains sequential and read-only.

## Quick start

```sh
graphjin eval create
graphjin eval add "Which customers are at churn risk?"
graphjin eval run --yes
graphjin eval bench --scale 100 --seed 23 --yes
```

Use `--demo` for the bundled SQLite demo or `--remote` for the identity created
by `graphjin cli setup`. Local and demo instances are embedded loopback services
with the agent forced read-only and background watch workers disabled. Remote
CI can override the saved token with `GRAPHJIN_EVAL_TOKEN`.

Commands that can invoke a model show their expected request count before
traffic starts. A non-interactive caller must pass `--yes`.

## Commands

- `graphjin eval`: show suite, state, and baseline status.
- `graphjin eval create`: sample at most 24 deterministic, deduplicated tasks
  from the public catalog and retain only tasks whose hidden oracle compiles,
  executes, and extracts successfully.
- `graphjin eval add "<question>"`: use the configured agent model for catalog
  discovery and ambiguity detection, execute the proposed hidden oracle, show
  its value with a plain-language interpretation, and save only after approval.
- `graphjin eval rm <task-id>`: remove a task through the same normalized,
  validated suite writer. It confirms interactively unless `--yes` is set.
- `graphjin eval run`: execute three repetitions per task. A baseline regression
  receives one fresh three-repetition confirmation run.
- `graphjin eval baseline`: run the suite and deliberately promote it only if it
  passes.
- `graphjin eval bench --scale N --seed S`: sample the extended distribution and
  report pass@k, pass^k, bootstrap recall intervals, and per-tier metrics.

The hidden `import-corpus` command converts the frozen behavioral and data
corpora to the v1 schema. `agent/cmd/skill-eval` remains unchanged for one
release apart from a deprecation notice; its Make targets continue to work.

## Files and privacy

The authored suite is `eval/suite.yml`. It is deterministic JSON, which is also
valid YAML 1.2, and must be changed through the CLI so content-hash IDs and
oracle invariants remain valid.

Local state is stored with owner-only permissions:

```text
.graphjin-evals/
  baseline.json
  reports/<run-id>.json
  episodes/<run-id>/<task>-<rep>.json
```

Reports are shareable and contain metrics, task IDs, tiers, failure categories,
provenance, fingerprints, and acceptance state. They do not contain prompts,
answers, database rows, executed queries, request headers, authentication tokens,
token contents, or secrets. Aggregate token-usage counts remain in the report as
efficiency metrics.
Episode files are private trajectories containing the request, response, action
trail, oracle query/result, usage, timing, and seeds. `--debug` prints their
paths; it does not move private data into the report.

## Gates and exit codes

- `0`: accepted.
- `1`: confirmed regression or another hard gate failure.
- `2`: invalid suite. One or more oracles failed before any evaluated-agent
  traffic; this is not counted as a model regression.
- `3`: target/environment failure.

Safety is always a hard gate. Efficiency budgets are advisory. The first safe,
valid run can establish a baseline at its observed recall; recall below `0.90`
produces a quality warning instead of blocking adoption. Later runs compare the
aggregate recall over the task-ID intersection, and new tasks remain advisory
until promoted.

Value correctness is compared when dataset fingerprints match or when both
reports have the same aggregate `oracle_value_hash`. The latter hashes the suite
fingerprint plus all resolved oracle values and dimensions in task-ID order, so
stable local and remote targets can retain ground-truth regression gates without
putting individual values in the shareable report. When neither proof matches,
GraphJin records the mismatch and compares method correctness instead.

## Engine boundary

`agent/eval` depends on the agent types and standard library only. It reaches
GraphJin through `/api/v1/agent`, `/api/v1/graphql`, `gj_catalog`, and the agent
status endpoint. `Env`, `Instance`, `InstancePool`, versioned `Episode`, reserved
`turns`/`mutation` task fields, per-step action trails, and `reward_version` are
the deliberate v2 seams for instance pools, reset primitives, mutation tasks,
parallel collection, and trainer-facing rollout APIs.

The next RL milestone is environment diversity first: synthesize many distinct
schemas and deterministic seed datasets, then run the existing generator over
each catalog. The throughput milestone follows with per-worker SQLite file
copies behind `InstancePool` and file restoration behind
`ResettableInstance.Reset`. Mutation tasks, multi-turn curricula, a headless
batch rollout API, and additional process rewards remain later work and are not
implemented by v1.
