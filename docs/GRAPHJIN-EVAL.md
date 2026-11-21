# GraphJin Eval And Env — Engine Contract

> **Using** GraphJin's evaluation or training environment? The documentation is
> at <https://deeporg.io/environment/> for training and measurement, and
> <https://deeporg.io/agentic/evaluation/> for gating a release. This file is
> not that. It is the contract a contributor must not break.

`agent/eval` generates verified tasks, runs graded episodes, and scores them.
`graphjin eval` measures; `graphjin env` serves. Both are the same engine, and
the invariants below are what let a number produced by one be compared with a
number produced by the other.

## Engine boundary

`agent/eval` depends on the agent types, shared pure query-syntax checks, and
the standard library. The syntax checks reuse the GraphJin parser without
instantiating an engine, connecting to a database, or adding server calls. It
reaches GraphJin through exactly four surfaces:

- `POST /api/v1/agent`
- `POST /api/v1/graphql`
- the `gj_catalog` root
- `GET /api/v1/agent/status`

Anything that would widen that list is a design change, not an implementation
detail. The package must stay usable against a GraphJin it does not link.

`Env`, `Instance`, `InstancePool`, `ResettableInstance`, versioned `Episode`,
`RewardProfile` and `RewardVersion` were once described here as future seams.
They are implemented. Instance pools, reset primitives, mutation tasks,
parallel collection and trainer-facing drive modes all ship.

## What an episode grades

The environment grades the data agent answering one self-contained question.
`configureEvalInstance` pins `agent.lead.enabled` off, because with the lead on
the agent endpoint would run the lead and every number would measure something
else. The data agent holds no conversation, and a server exposing it alone
refuses a request that carries history: HTTP 400, before any agent runs.

So a task with earlier turns (`turns`) cannot run here. The task shape still
loads, because older suites and stored episodes carry it, and it still runs
against a server whose lead is enabled. Four things keep it away from an
instance that would refuse it:

- No generated family writes one (generator v20 on).
- `eval author` skips its `confirmation` and `history` kinds when the instance
  reports `lead_enabled: false`, before the model call that would write them.
- `Generate` leaves out any curated or already-authored candidate that sends
  earlier turns on such an instance, and says how many.
- `Runner.Prepare` and `env serve` ask the instance's status and refuse to
  start, naming the tasks, rather than let each one be rejected and scored as
  the model failing. A server that does not report `lead_enabled` predates the
  field and is let through.

Conversations are scored on the lead, in `agent/testdata/lead_eval_cases.json`
(`scripts/agent-data-eval.sh --corpus lead`).

## Versions, and when to move them

Four version constants gate compatibility. Moving one is a deliberate act with
consequences downstream; not moving one when semantics changed is worse.

| Constant | Move it when | Consequence |
|---|---|---|
| `GeneratorVersion` | Generated task semantics change | Older binaries refuse the suite. Append the previous literal to `SupportedGeneratorVersions` so new binaries still read old suites. |
| `RewardVersion` | Any scoring rule changes | Runs either side are not comparable. Published board rows record which version graded them. |
| `SuiteSchemaVersion` / `SplitSchemaVersion` | The file shape changes | `LoadSuite` uses `DisallowUnknownFields`; a new field is a breaking read for old binaries. |
| `PublicBenchmarkGeneration` | The frozen public suite is regenerated | Earlier generations keep their own cohort and history. Currently `2028.6`. |

A suite whose generator this binary does not know is refused at load and at
serve, rather than run under rules never written for it.

## Ranked suite identity

Run identity is the hash of exactly these fields, and nothing else:

- `mode` and `suite_fingerprint`
- `dataset_fingerprint.catalog_hash` and `dataset_fingerprint.seed_manifest_hash`
- `provenance.seed`, `provenance.repeats`, `provenance.max_steps`,
  `provenance.temperature`, `provenance.top_p`
- `reward_version`

**Model and provider are excluded** so different models can be compared on one
suite. **GraphJin commit and binary fingerprint are excluded** so release
changes can be shown over time. `oracle_value_hash` and
`dataset_fingerprint.data_anchor` are audit columns, excluded from identity
because the demo shifts relative dates forward and both can change with the
calendar while the frozen task stays the same.

Adding a field to this projection changes every existing run's identity. Add it
with `omitempty` and a zero default, so existing hashes are byte-identical and
only a run that actually uses the new field gets a new identity — the mechanism
`top_p` uses.

## Prompt registry

`PromptRegistryHash()` covers every skill's id, name and content, plus the
runtime instruction blobs. **Editing any string in `agent/skills.go` moves it**,
and it is recorded in run provenance, so an edit invalidates baseline
comparability whether or not it changed behaviour. Authoring prompts are
deliberately excluded and ride on tasks as `Provenance.AuthoredBy` instead:
generator prose must not move the evaluated agent's provenance.

## Skill payload budgets

Enforced as test literals in `agent/skills_test.go`, not at runtime: 3,200 bytes
for a caller with no `gj_*` roots, 9,344 for a full admin. They ratchet both
ways deliberately. A guide gated behind a root costs the ordinary caller
nothing; universal guidance costs every caller. Growth should be a decision that
shows up in a diff.

## The frozen public suite

`graphjin eval bench --public` loads the embedded suite, starts the bundled
demo, and **re-verifies every hidden oracle against the live instance** before
any evaluated-agent traffic. `graphjin env serve --suite public` serves the same
artifact.

Changing it is the hidden `freeze-suite` command plus a
`PublicBenchmarkGeneration` bump and the new `publicBenchmarkSuiteFingerprint`.
Run bare, the command regenerates every task, which is rarely what a change
needs: the demo's data follows the calendar, and a same-day regeneration was
measured to swap a third of the questions. To take a family out, use
`freeze-suite --retire <category>`. It keeps every other committed task
byte-identical, re-verifies them against the live demo, and refills only the
freed intent-tier slots, round-robin over the sampler's backfill categories,
each taking the first task of a fresh draw the suite does not already ask. The
same day and catalog give the same file. Generations 2028.5 (saved-metric) and
2028.6 (multi-turn) were both retirements of a family the data agent could no
longer be asked about.

`import-corpus` is likewise hidden and converts the frozen skill-eval corpora
into tasks.

## Mined tests

`graphjin eval mine` writes `eval/mined.yml`: questions people asked the agent
on a running server, each with the query that answered it as its oracle. The
server half is `GET /api/v1/agent/experience` (admin only, `serv/experience.go`);
the rules are in `agent/eval/mine.go`.

- **The engine decides what is a test, not a model.** A question qualifies when
  the data agent's last successful read is one aggregate figure, or one row
  (`limit: 1`, a ranking when it also orders), with no variables. A literal date
  in the query becomes an offset from the day it was asked (`{{today-N}}`),
  unless the question itself names a date, which drops it as `dated`. A date
  more than 400 days from that day is a fixed bound and stays as written. The query is parsed by the engine's parser (`core/queryshape`), and the
  answer path is read from the result the query actually returned.
- **The query re-runs as the person who asked**, never as the admin reading
  experience, so the oracle sees the rows they saw. The answer they were given
  must pass it, or the candidate is dropped (`answer_mismatch`).
- **Two tiers, never summed.** `mined-confirmed` (a thumbs-up on a
  one-question turn) measures correctness; `mined-incumbent` (never rated)
  measures agreement with the agent that first answered. A run of mined tests
  prints one line per tier, and its friendly and technical reports have no
  combined headline. In the JSON, read `metrics.by_family`: the overall
  `metrics.recall` still counts every test together.
- **Mined tests are a suite of their own.** They are not candidates for
  `eval create`, whose sampling could drop a question somebody really asked.
  `eval run --suite eval/mined.yml` runs them; that run is compared with no
  baseline and promotes none.
- Every drop is counted by reason in the summary. The counts say which kinds of
  real questions cannot be tested yet.

## Publishing

`graphjin eval publish <run-id>` is the **only** sanctioned writer of
`website/data/benchmarks/<slug>.yaml` and
`website/content/benchmarks/<slug>/runs/`. The slug defaults to `deeporg`; the
flag selects publication metadata and paths, never engine behaviour.

Publish asks for confirmation, rejects empty runs, writes exactly those two
files, and prints their paths for human review. It refuses incomplete,
environment-failed and invalid-suite runs. **It does not refuse a low score** —
a low score is a result.

## Reports and privacy

Each finished run writes canonical `reports/<run-id>.json`, a plain-language
`reports/<run-id>.md`, and `reports/<run-id>.technical.md`, at owner-only
permissions. Both Markdown views are generated from the same shareable
projection as the JSON: **neither can include task slugs, raw oracle errors, or
local episode paths.** That projection is the privacy boundary; anything added
to a report must go through it.

Episodes are stored one file per episode under
`.graphjin-evals/episodes/<run>/`, with the full response including the trace,
so `eval rescore` and `eval export` can re-read a run without provider traffic.
Set `eval_state_dir` to relocate the directory, or to `off` to disable the API.

## Method evidence

Method scoring uses successful execution receipts, excluding structured
execution errors. Bare native aggregate requirements such as `sum_price` or
`avg_price|sum_price` compare the parsed operation and exact column. The
equivalent Hasura-compatible and `column:` forms receive the same credit;
aliases, comments, argument strings, and a different column do not. Structural
requirements such as `order_by` retain their explicit regex semantics. An
implicit compatibility `count` proves a row count, not a particular count
column whose schema is unknown to the scorer.

Saved-query execution is resolved only from an earlier, successful same-run
catalog detail receipt. Its complete definition must match the catalog source
hash and qualified name. The catalog publishes `query_hash` for the exact
normalized definition, separately from the original file's `source_hash`.
Older records accept only source encodings that reproduce their recorded hash,
including the service's raw file hash with a final LF or CRLF. New detail receipts retain bounded complete query
source; older prepared recordings can use the matching host catalog snapshot.
A saved-query name, model-generated source, prefetch without a consumed detail,
or a changed/incomplete definition cannot earn method credit. Rescoring never
fetches a current definition to stand in for historical evidence.

## Invariants a change must preserve

1. **An oracle is verified against a booted instance before it ships.** A task
   whose oracle does not resolve is dropped, not shipped.
2. **A pool refuses to serve unless every worker reports the same dataset.**
   Oracles are resolved once per run; a divergent worker would mark correct
   answers wrong and look like a model regression.
3. **A suite is refused against a catalog it was not verified on**, unless the
   operator overrides it explicitly.
4. **Safety is a gate in the `rl` profile, not a weighted term.** A weighted
   term is a trade a policy can learn to make.
5. **A trajectory whose prompts were not recorded is not exported.** The
   alternative trains a model on text it never produced.
6. **Held-out episodes do not become training corpora** without an explicit
   override that names how many were found.
7. **A request the instance would refuse is never sent and scored.** A run does
   not start on a suite whose tasks the instance has said it will reject
   unanswered. `TestEmbeddedSuitesSendNoHistoryToAnInstanceThatRefusesIt` holds
   the embedded suites to it.

## Deprecated

`agent/cmd/skill-eval` is frozen and prints a deprecation notice. Scoring
correctness fixes use the shared method checker and record its version; the
command gains no new suite-generation surface. Its corpora
migrate through the hidden `import-corpus` command. New suites use
`graphjin eval`.
