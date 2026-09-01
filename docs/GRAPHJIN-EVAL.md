# GraphJin Eval And Env — Engine Contract

> **Using** GraphJin's evaluation or training environment? The documentation is
> at <https://graphjin.com/environment/> for training and measurement, and
> <https://graphjin.com/agentic/evaluation/> for gating a release. This file is
> not that. It is the contract a contributor must not break.

`agent/eval` generates verified tasks, runs graded episodes, and scores them.
`graphjin eval` measures; `graphjin env` serves. Both are the same engine, and
the invariants below are what let a number produced by one be compared with a
number produced by the other.

## Engine boundary

`agent/eval` depends on the agent types and the standard library only. It
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
multi-turn episodes, parallel collection and trainer-facing drive modes all
ship.

## Versions, and when to move them

Four version constants gate compatibility. Moving one is a deliberate act with
consequences downstream; not moving one when semantics changed is worse.

| Constant | Move it when | Consequence |
|---|---|---|
| `GeneratorVersion` | Generated task semantics change | Older binaries refuse the suite. Append the previous literal to `SupportedGeneratorVersions` so new binaries still read old suites. |
| `RewardVersion` | Any scoring rule changes | Runs either side are not comparable. Published board rows record which version graded them. |
| `SuiteSchemaVersion` / `SplitSchemaVersion` | The file shape changes | `LoadSuite` uses `DisallowUnknownFields`; a new field is a breaking read for old binaries. |
| `PublicBenchmarkGeneration` | The frozen public suite is regenerated | Earlier generations keep their own cohort and history. Currently `2028.4`. |

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

Regenerating it is the hidden `freeze-suite` command plus a
`PublicBenchmarkGeneration` bump. `import-corpus` is likewise hidden and
converts the frozen skill-eval corpora into tasks.

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

## Deprecated

`agent/cmd/skill-eval` is frozen and prints a deprecation notice. Its corpora
migrate through the hidden `import-corpus` command. New suites use
`graphjin eval`.
