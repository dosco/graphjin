---
title: "GraphJin Agent Benchmark"
description: "A public, reproducible measure of how agentic models answer real organizational questions under GraphJin governance."
---

## How well can an agent work against a real organization?

Enterprise data work is harder than translating a clean question into SQL. An
agent has to discover an unfamiliar catalog, choose a complete database-side
method, respect policy, and still give the right answer. The GraphJin Agent
Benchmark measures all of those outcomes independently against a live demo.

Every task is verified before agent traffic begins. Each answer is checked
against a hidden oracle executed at run time, while the action trail proves
whether the database—not a truncated client-side page—did the work.

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

## What makes two runs comparable?

Ranked runs share the same mode, frozen suite fingerprint, catalog hash, seed
manifest hash, rollout seed, repeat count, step limit, temperature, and reward
version. The model and provider are deliberately excluded so models can be
compared on the same cohort. The GraphJin commit is deliberately excluded so
the same model can show release-to-release progress.

`oracle_value_hash` and `data_anchor` remain on every report as audit columns,
but they are not cohort identity. The demo shifts calendar-relative seed data
forward over time; those values can change without changing the task or method
being measured.

## GraphJin releases over time

The same published runs can also be read as a release timeline. This view asks
whether GraphJin's planning, governance, and agent runtime improve for a given
model while the benchmark generation stays fixed.

{{< benchmark-leaderboard view="releases" >}}

Read the [scoring and privacy methodology](/benchmark/methodology/) or browse
the [published run archive](/benchmark/runs/).
