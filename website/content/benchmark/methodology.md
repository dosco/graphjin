---
title: "Benchmark Methodology"
nav_title: "Methodology"
description: "How GraphJin scores answers, methods, safety, behavior, efficiency, and benchmark comparability."
nav_group: "benchmark"
weight: 1
---

The GraphJin Agent Benchmark asks models to work against a real, governed
organizational schema. It measures the final answer and the path used to reach
it instead of treating text-to-SQL as a single exact-match problem.

## Frozen suite, live verification

Generation `2026.1` contains a committed, deterministic 100-task suite built
from the bundled SaaS Ops demo at seed `23`. Publishing the suite makes results
reproducible. Future generations can rotate the questions without rewriting
the history of earlier cohorts.

Before any evaluated-agent traffic, GraphJin resolves every hidden oracle
against the live instance. A broken oracle invalidates the suite and stops the
run. Calendar-relative windows use a live anchor, so the expected answer stays
aligned with the demo as its dates move forward.

## What is scored

Each task runs three times. The report keeps the dimensions separate:

| Dimension | Passing contract |
| --- | --- |
| **Answer** | The final value matches the live hidden oracle within the task's declared tolerance. |
| **Method** | The action trail proves GraphJin or the database performed the complete operation. Client-side aggregation over a limited row page fails even when its number happens to match. |
| **Safety** | No forbidden action occurs. One unsafe rollout is a hard task failure. |
| **Behavior** | Required discovery, validation, skill, or governed-action behavior occurs. |
| **Efficiency** | Actor turns, model tokens, calls, and latency are reported without turning a slow correct answer into a false quality failure. |

Recall is the fraction of tasks whose majority verdict passes. `pass@3` asks
whether at least one of the three rollouts passed; `pass³` asks whether all
three passed. The report also publishes a bootstrap confidence interval and
per-tier results from T1 through T4.

## Safety and acceptance

Safety is always a hard gate. Suite validity and environment health are also
reported independently from answer quality. `accepted` records GraphJin's
local regression-gate result; it is not an admission threshold for this public
board. Publish refuses incomplete, environment-failed, invalid-suite, and empty
runs. It does not refuse a low score—a low score is a result.

## Comparable cohorts

Ranked identity includes:

- mode and suite fingerprint;
- catalog and seed-manifest hashes;
- rollout seed, repeats, maximum steps, and temperature; and
- reward version.

Model, provider, GraphJin commit, and binary fingerprint are presentation axes,
not cohort keys. Oracle value hash and data anchor are audit-only because the
demo's relative dates shift with the calendar.

A run outside the pinned cohort can be published only with an explicit
`--allow-off-suite`; it appears in a separate unranked table with the mismatch
reason.

## Privacy

Published reports contain metrics, public task IDs and categories,
fingerprints, provenance, and acceptance state. They never contain prompts,
answers, database rows, executed queries, headers, credentials, task slugs, raw
oracle errors, or local episode paths. The private JSON trajectories remain in
the local `.graphjin-evals/` store.
