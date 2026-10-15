---
title: "DeepContoso — The Retail & Supply Chain Benchmark"
description: "A public comparison of AI agents solving enterprise retail and supply chain analytics, with correctness, database pushdown, governance, cost, latency, and GraphJin build provenance."
---

<div class="benchmark-wordmark"><strong>DeepContoso</strong><span>The Retail & Supply Chain Benchmark · by GraphJin</span></div>

# Can an AI agent handle enterprise retail data without breaking governance?

DeepContoso tests models against an enterprise retail and supply chain data
warehouse modeled on Microsoft's Contoso schema—not a quiz of stored answers.
Each result belongs to a model operating through the GraphJin build shown on its
row. The benchmark checks what the agent concluded, whether calculations were
pushed down to the database engine rather than aggregated client-side, whether
policy held, how consistently it worked, how long it took, and what the provider
usage cost at list price.

{{< benchmark-generation-story benchmark="deepcontoso" >}}

## Model comparison

Longer bars mean more tasks earned a full pass. A full pass requires the right
scalar answer, the required database-side method (query pushdown), the behavior
contract, and zero unsafe effects. Forbidden attempts refused by GraphJin are reported
separately: they fail expected behavior, not safety.

The board shows the highest trustworthy published result for every model. A new
test version never makes an older paid result disappear, while invalidated scorer
or harness runs stay in the archive instead of becoming a model's best.

One number cannot answer two different questions. Each selected row scores two
headline groups under a frozen mapping (v1): **questions** — stateless answers
computed from live data (aggregates, sliding windows, schema discovery);
**governance** — refusing what policy forbids. A model can be an excellent analyst
and a poor operator; the rollup says which one you are hiring.

{{< benchmark-leaderboard benchmark="deepcontoso" view="chart" >}}

{{< benchmark-leaderboard benchmark="deepcontoso" view="models" >}}

## Operational cost

A model that eventually succeeds after consuming huge context or minutes of
latency is not equivalent to one that succeeds quickly and cheaply. These are the
same evaluated attempts as the score chart above; lower is better.

{{< benchmark-leaderboard benchmark="deepcontoso" view="operational" >}}

## Where each model is strong

The headline can hide opposite failure modes. DeepContoso evaluates core retail
aggregations, foreign key schema discovery across multiple tables, temporal
sliding windows, and security refusals.

{{< benchmark-leaderboard benchmark="deepcontoso" view="categories" >}}

## Run it yourself

The DeepContoso suite is frozen and committed. GraphJin resolves its hidden oracles
against the live reference environment before provider traffic, then performs
three independent attempts per task.

{{< code-card filename="terminal" language="bash" >}}
graphjin eval bench --benchmark deepcontoso --yes
graphjin eval publish <run-id> --benchmark deepcontoso --yes
{{< /code-card >}}

Publishing writes one deterministic YAML row and one Markdown report page for
human review. It never runs Git, and low scores remain valid publishable results.
`--label` changes presentation only; same-generation supersession follows the
published provider and model identity.

## Run DeepContoso on your organization

The public board uses the bundled Contoso retail reference environment. The same
evaluation framework can grade your own GraphJin catalog and check live hidden
oracles before a model sees its first prompt.

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
| **Required method** | The action trail proves the database engine performed the complete calculation via query pushdown. |
| **Full pass** | Correct answer, required method, behavior contract, and safety all pass together. |
| **Passed every attempt** | All three independent attempts earned a full pass. |
| **Efficiency** | Provider tokens, p50/p95 latency, and estimated list-price cost are reported separately. |

Read the full [scoring, comparability, correction, and privacy methodology](/benchmarks/deepcontoso/methodology/)
or browse the [published run archive](/benchmarks/deepcontoso/runs/).

{{< benchmark-honesty benchmark="deepcontoso" >}}
