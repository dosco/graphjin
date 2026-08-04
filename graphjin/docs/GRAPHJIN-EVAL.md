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

Commands that can invoke a model show reused episodes, remaining initial slots,
possible confirmation slots, and the maximum provider attempts including one
retry for each pending slot. A non-interactive caller must pass `--yes`.

Google uses `GOOGLE_API_KEY` by convention:

```sh
GJ_AGENT_PROVIDER=google-gemini
GJ_AGENT_API_KEY_ENV=GOOGLE_API_KEY
```

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
  receives one fresh three-repetition confirmation run. It automatically
  resumes the newest strictly compatible incomplete run.
- `graphjin eval baseline`: run the suite and deliberately promote it only if it
  passes.
- `graphjin eval bench --scale N --seed S`: sample the extended distribution and
  report pass@k, pass^k, bootstrap recall intervals, and per-tier metrics.

`run`, `baseline`, and `bench` accept `--resume <run-id>` to select one
compatible incomplete run and `--restart` to intentionally create a fresh run.
Those flags are mutually exclusive. Resumption is strict: suite/oracle/dataset,
binary and server fingerprints, schemas, reward, provider/model, seed/repeats,
baseline identity, target, provenance, and promotion intent must match. Model
quality failures are completed slots and are reused; provider/environment
attempts are retried rather than scored.

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
  attempts/<run-id>/<task>-attempt-<number>.json
  runs/<run-id>.json
  locks/<run-id>.lock
```

Reports are shareable and contain metrics, task IDs, tiers, failure categories,
provenance, fingerprints, and acceptance state. Report provenance includes the
exact CLI executable SHA-256 as `binary_fingerprint`; use it to prove that two
runs really used the same build before attributing a score change. They do not
contain prompts, answers, database rows, executed queries, request headers,
authentication tokens, token contents, or secrets. Aggregate token-usage counts
remain in the report as efficiency metrics. GraphJin obtains them from Ax's
`GetUsage()` state and the merged stage chat logs, including successful model
calls made before an agent
error. A complete report separates finalized-slot usage from actual provider
usage, which includes finalized errors, failed attempts, and retries.
`provider_usage.complete` says whether every attempt returned usage;
`unknown_attempts` counts timeouts or transport failures where the provider
returned none. When usage is incomplete, recorded tokens are a lower bound.

Reports use `graphjin.eval.report/v3` and identify their accounting rules with
`usage_accounting_version`. Token percentages are comparable only when the
suite shape, accounting version, provider, model, configured `max_steps`, and
finalized episode count match and both runs have complete provider usage.
Other runs retain absolute counts but do not present an apples-to-oranges token
percentage. Quality and safety comparisons remain valid.
Episode files are private trajectories containing the request, response, action
trail, oracle query/result, usage, timing, and seeds. `--debug` prints their
paths; it does not move private data into the report.
Failed execution action summaries retain stable `error_codes`,
`recovery_codes`, and `recovery_tool` fields without copying provider or
compiler error prose, so common repair loops can be triaged without guessing
from `error_count` alone.
Attempt files contain sanitized interrupted or failed provider attempts and are
also private. Manifests checkpoint progress and contain only allowlisted,
non-secret provenance. Even private files never contain credentials: provider
URL keys, authorization values, configured secrets, and recognizable provider
key patterns are redacted before persistence.

An interrupted run checkpoints after every attempt and finalized slot. Rerun
the same command to resume automatically, or use the exact command printed by
`graphjin eval` status. A compatible run is protected by an OS-held advisory
lock so two processes cannot duplicate provider traffic. Old episode folders
without a run manifest remain inspectable but are never imported.

## Gates and exit codes

- `0`: accepted.
- `1`: confirmed regression or another hard gate failure.
- `2`: invalid suite. One or more oracles failed before any evaluated-agent
  traffic; this is not counted as a model regression.
- `3`: target/environment failure. Transient timeout/rate-limit/transport/5xx
  failures retry once, then write a metric-free partial report. Authentication,
  quota, or unavailable-model failures stop immediately. No baseline is
  promoted.
- `130`: interrupted by `SIGINT` or `SIGTERM`; progress is checkpointed and the
  CLI prints the exact resume command.

Safety is always a hard gate. Efficiency budgets are advisory. The first safe,
valid run can establish a baseline at its observed recall; recall below `0.90`
produces a quality warning instead of blocking adoption. Later runs compare the
aggregate recall over the task-ID intersection, and new tasks remain advisory
until promoted.

GraphJin deliberately keeps the global agent limit at eight actor steps. Eval
counts actor turns from executor `stage_request` trace events and classifies
`agent_actor_steps_exhausted` as `runaway`. More steps are not the remedy for a
model repeating the same successful work: GraphJin suppresses the repeated
database call, returns cached governed evidence with a completion directive,
and gives the model one grace turn to finish. A future task-specific increase
should require traces showing distinct productive progress on every turn.

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
