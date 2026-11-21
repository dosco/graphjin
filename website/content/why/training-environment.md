---
title: "Your organization as a training environment"
headline: "Turn your organization into a training environment."
description: "The machinery that grades DeepORG on its public benchmark also generates checked tasks from your own systems and serves them as graded episodes, with a reward that comes from the database. Measure an agent on your organization, or train one with reinforcement learning."
lede: "The machinery that grades DeepORG in public also generates checked tasks from your own systems and serves them as graded episodes. Measure an agent on your organization, or train one with reinforcement learning, instead of on a toy."
scene: "env"
scene_label: "Animated diagram of one episode: reset, task, the agent works, it answers, a hidden oracle query is resolved in the same world, and the reward comes back. A fluent wrong answer and a right number with no work behind it both score zero."
scene_caption: "reset / act / the database marks it"
actions:
  - label: "Run the environment"
    href: "/environment/quickstart/"
  - label: "Use your own questions"
    href: "/experience/"
landing: true
toc: false
---

{{< band label="The reward" title="The database marks the answer. No model is asked for its opinion." >}}

Most ways of scoring an agent ask a model whether an answer looks right. This one asks the database. Every task carries a hidden oracle: a read-only query resolved against the same world the agent works in. A fluent, confident, wrong answer earns nothing.

The oracle is written when the task is generated, and the agent never sees it. A policy trained against this reward cannot improve its score by becoming more persuasive.

{{< grid >}}

- **Correctness.** The answer is compared with what the oracle returns, now, in this world.
- **Method.** A correct answer reached the wrong way does not score full marks. An answer with no work behind it scores zero.
- **Behavior.** Required actions taken, forbidden ones avoided, and a clear refusal where one was called for.
- **Side effects.** For a change, the end state must be right and nothing outside the task's scope may have moved.
- **Efficiency.** Reported, without turning a slow correct answer into a failure.

{{< endgrid >}}

There are two reward profiles. `benchmark` is the published contract. `rl` is for training, and there safety is a gate rather than a weight: an episode that caused an unsafe effect is worth nothing.

{{< endband >}}

{{< band label="Three ways in" title="Start from our world, your schema, or your people's questions." >}}

### A container that boots ready

```bash
docker run -d -p 8090:8090 --tmpfs /tmp:size=1g dosco/graphjin:env-latest
```

Nothing is mounted and nothing is generated: a demo organization and its verified tasks are inside.

{{< env-figures >}}

### Your schema, without your data

```bash
deeporg env clone --url https://graphjin.internal --out ./clone-acme --seed 7
```

DeepORG learns a running server's shape from its catalog: table and column structure, relationships, saved query names, and the closed value sets the catalog publishes. No rows are read. Synthetic records are generated locally, and every task is verified against a booted copy of the clone before it ships.

### Your people's questions, as the tests

```bash
deeporg eval mine --remote
deeporg eval run --suite eval/mined.yml --yes
```

Every question your people ask is answered by a database query. When that answer is one figure the database computed, the question becomes a test whose answer key is the query, re-run whenever the test runs. Mining spends no model tokens. [Your company is the environment](/experience/) tells that story in full.

{{< note >}}

**What each one is not.** A clone copies your schema, not your data's distribution, and API sources are skipped. Mined tests cover single figures, top rows and looked-up values; lists, changes and dated questions are skipped and counted. A test nobody rated measures agreement with the agent that first answered, not correctness, and the two are reported apart.

{{< endnote >}}

{{< endband >}}

{{< band label="Driving it" title="Three ways to run an episode. One way to grade it." >}}

{{< grid >}}

- **DeepORG calls your endpoint.** The default. Point it at an OpenAI-compatible endpoint and `POST /episodes` runs one graded episode.
- **You supply each completion.** `--step` hands you the prompt and takes back one completion at a time. It is the mode GRPO uses.
- **You bring the whole agent.** `--external` hands you the task, an MCP endpoint and a deadline. Your agent does the rest.

{{< endgrid >}}

Grading is the same in all three: an answer submitted without touching the database scores zero.

Each worker in the pool provisions its own copy of the project, with its own database and its own state, and an episode leases one. The pool refuses to start unless every worker reports the same dataset. A held-out split is derived from each task's content, so two containers agree on it without talking, and the exporter refuses to build a training set from the evaluation side.

{{< endband >}}

{{< band label="Training" title="We ship the environment, not the trainer." >}}

The loop, the optimizer and the GPUs stay on your side. The environment gives you tasks, isolated worlds, a reward, and trajectories you can train on:

```bash
deeporg eval export <run-id> --split eval/suite.split.json --side train \
  --stage executor --out teacher.jsonl
```

The order matters:

1. **Distil from a teacher**, so the policy can complete an episode at all.
2. **Rejection-sample** from the tuned policy for harder, on-policy data.
3. **GRPO**, once attempts differ from each other, because that difference is the whole signal.

{{< note >}}

**Read this before you start.** A small base model dropped into this environment scores approximately zero: every attempt in a group fails, every advantage is zero, and GRPO has no gradient to follow. Starting at step 3 is the most common way to conclude, incorrectly, that the environment does not work. Agent runs are also not deterministic, so compare with repeats, not single runs. This page makes no claim about what a trained policy will score.

{{< endnote >}}

{{< endband >}}

{{< band label="Same machinery" title="The public benchmark is this environment, published." >}}

The DeepORG benchmark is a frozen suite of {{< benchmark-stat "tasks" >}} tasks, each run three times, served and graded by the same code. That is why the scores are measurements, and why the same reward is something a policy can be trained on.

The best published result is {{< benchmark-stat "score" >}} of 100 with {{< benchmark-stat "unsafe" >}} unsafe effects. [See every result](/benchmark/), including the ones that failed.

{{< endband >}}

{{< band label="In the docs" title="Go deeper." >}}

- [Overview](/environment/overview/) and [quick start](/environment/quickstart/).
- [Drive modes](/environment/drive-modes/) and the [HTTP API](/environment/http-api/).
- [The reward](/environment/reward/): profiles, components and rescoring.
- [Your own graph](/environment/your-own-graph/) and [your own work](/environment/your-own-work/).
- [Worlds](/environment/worlds/): generating new organizations to train on.
- [Training](/environment/training/): the recipe, in order.

{{< endband >}}
