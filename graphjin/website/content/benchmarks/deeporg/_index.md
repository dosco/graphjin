---
title: "DeepORG — The Organizational Agent Benchmark"
description: "A public comparison of AI agents doing real organizational work, with correctness, consistency, safety, cost, latency, and GraphJin build provenance."
aliases:
  - /benchmarks/organizational-agent/
---

<div class="benchmark-wordmark"><strong>DeepORG</strong><span>The Organizational Agent Benchmark · by GraphJin</span></div>

# Can an AI agent handle the questions an organization actually asks?

DeepORG tests models against a live organizational system—not a quiz of stored
answers. Each result belongs to a model operating through the GraphJin build
shown on its row. The benchmark checks what the agent concluded, what really
ran, whether policy held, how consistently it worked, how long it took, and
what the provider usage cost at list price. The current generation measures
read-only, watchable, and modifiable work alongside multi-turn and cross-source
questions.

**Generation scope:** {{< benchmark-scope benchmark="deeporg" >}}.

{{< benchmark-generation-story benchmark="deeporg" >}}

## Model comparison

Longer bars mean more tasks earned a full pass. A full pass requires the right
answer, the required database-side method, the behavior contract, and zero
unsafe effects. Forbidden attempts refused by GraphJin are reported separately:
they fail expected behavior, not safety.

One number cannot answer two different questions. Each ranked row also scores
three headline groups under a frozen mapping (v1): **questions** — stateless
answers computed from live data (aggregates, windows, rankings, discovery,
saved metrics); **operations** — work that carries state (writes, watches,
follow-ups, multi-source); **governance** — refusing what policy forbids. A
model can be an excellent analyst and a poor operator; the rollup says which
one you are hiring.

{{< benchmark-leaderboard benchmark="deeporg" view="chart" >}}

{{< benchmark-leaderboard benchmark="deeporg" view="models" >}}

## Operational cost

A model that eventually succeeds after consuming huge context or minutes of
latency is not equivalent to one that succeeds quickly and cheaply. These are
the same evaluated attempts as the score chart above; lower is better.

{{< benchmark-leaderboard benchmark="deeporg" view="operational" >}}

## Where each model is strong

The headline can hide opposite failure modes. This chart breaks the same full
pass apart by task family so a model that excels at aggregation but struggles
with refusals does not look identical to one with the reverse profile.

{{< benchmark-leaderboard benchmark="deeporg" view="categories" >}}

## Run it yourself

The public suite is frozen and committed. GraphJin resolves its hidden oracles
against the live demo before provider traffic, then performs three independent
attempts per task. Generating and verifying the suite is free; running it spends
provider tokens.

{{< code-card filename="terminal" language="bash" >}}
graphjin eval bench --public --yes
graphjin eval publish <run-id> --benchmark deeporg --yes
{{< /code-card >}}

Publishing writes one deterministic YAML row and one Markdown report page for
human review. It never runs Git, and low scores remain valid publishable results.
`--label` changes presentation only; same-generation supersession follows the
published provider and model identity.

## Run DeepORG on your organization

The public board uses the bundled SaaS Ops reference environment. The same
generator can build a private evaluation from your own GraphJin catalog and
check live hidden oracles before a model sees its first prompt.

{{< code-card filename="terminal" language="bash" >}}
graphjin eval create --yes
graphjin eval run --yes
{{< /code-card >}}

Private prompts, answers, rows, queries, credentials, and local paths stay in
the local `.graphjin-evals/` store. Publication is separate and metrics-only.

## How scoring works

| Dimension | Plain-language contract |
| --- | --- |
| **Correct answer** | The final value matches a hidden oracle computed from the live system. |
| **Required method** | The action trail proves the database or governed system performed the complete operation. |
| **Full pass** | Correct answer, required method, behavior contract, and safety all pass together. |
| **Passed every attempt** | All three independent attempts earned a full pass. |
| **Efficiency** | Provider tokens, p50/p95 latency, and estimated list-price cost are reported separately. |

Read the full [scoring, cohort, correction, and privacy methodology](/benchmarks/deeporg/methodology/)
or browse the [published run archive](/benchmarks/deeporg/runs/).

{{< benchmark-honesty benchmark="deeporg" >}}
