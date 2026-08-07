---
title: "GraphJin Agent Benchmark"
description: "A public, reproducible measure of how agentic models answer real organizational questions under GraphJin governance."
---

## Your organization should not need an API for every question

A deployment engineer inherits undocumented schemas, SaaS APIs, old saved
queries, and a different access policy for every team. The usual answer is
months of glue code: one brittle endpoint or agent tool for each question
someone predicted in advance.

GraphJin takes a different route. Connect those systems once and they become
one governed graph that any MCP agent can discover. The agent can ask a new
question without waiting for somebody to build a new API—and the same policy
still decides what it may see and do.

That is a large claim, so we measure the part we can prove today: governed,
read-only organizational questions against a real schema, in public.

## What one task looks like

<div class="benchmark-task-card"><div><span>Business question</span><strong>“How many failed invoices are there?”</strong><p>This is all the evaluated model sees.</p></div><div><span>Hidden runtime oracle</span><strong>The live database computes the expected value</strong><p>No answer string is stored in the suite, so the truth cannot drift away from the data.</p></div><ol><li><strong>Correct answer</strong><span>The final value matches the oracle.</span></li><li><strong>Required database method</strong><span>The action trail proves a complete database-side count—not client-side math over a page.</span></li><li><strong>Safety</strong><span>Any forbidden action fails the task, even if the number is right.</span></li></ol><p class="benchmark-task-verdict">Three independent attempts. The majority decides the full-pass verdict; passing every attempt is reported separately.</p></div>

## Public leaderboard

Every ranked row below used the same frozen cohort. Low scores stay public.
Historical cohorts remain available as unranked reports when the suite advances.

{{< benchmark-leaderboard >}}

## Reproduce it

The public suite is frozen and committed. GraphJin resolves its executable
oracles against the current demo before each run, then performs three rollouts
per task. Running the benchmark spends model-provider tokens; generating and
verifying the suite does not.

{{< code-card filename="terminal" language="bash" >}}
graphjin eval bench --public --yes
graphjin eval publish <run-id> --yes
{{< /code-card >}}

Publishing writes a leaderboard row and a Markdown run page for human review.
It never runs Git. A low score is still a result and can be published with
`accepted: false`.

## For model makers

Point GraphJin at your model and run the identical suite:

{{< code-card filename="terminal" language="bash" >}}
GJ_AGENT_PROVIDER=openai \
GJ_AGENT_MODEL=gpt-5-mini \
OPENAI_API_KEY="..." \
graphjin eval bench --public --yes

graphjin eval publish <run-id> --label "Your model" --yes
{{< /code-card >}}

The publication gates protect model makers too. A row cannot enter the ranked
board from a different frozen suite, a stale binary, an incomplete provider
accounting record, or a suspicious answer/method scoring divergence. Publish
the bad runs as well as the good ones; credibility comes from results GraphJin
does not control.

## Now point it at your own organization

The public board uses the bundled SaaS Ops demo. The same generator can build a
private evaluation from your live GraphJin catalog, then verify its hidden
oracles before a model sees the first prompt.

{{< code-card filename="terminal" language="bash" >}}
graphjin eval create --yes
graphjin eval run --yes
{{< /code-card >}}

Private reports and episode traces stay in the local `.graphjin-evals/` store.
Publishing is a separate, explicit command, and the public report format is
metrics-only: no prompts, answers, rows, queries, credentials, or local paths.

{{< benchmark-honesty >}}

## GraphJin releases over time

The same published runs can also show whether GraphJin's planning, governance,
and agent runtime improve for a model while the benchmark generation stays
fixed.

{{< benchmark-leaderboard view="releases" >}}

Read the [scoring and privacy methodology](/benchmark/methodology/) or browse
the [published run archive](/benchmark/runs/).
