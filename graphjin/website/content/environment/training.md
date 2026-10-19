---
title: "Training A Policy Against It"
nav_title: "Training"
description: "Teacher distillation, then rejection sampling, then GRPO — and why that order is forced rather than chosen."
nav_group: "environment"
doc_kind: "guide"
weight: 60
---

GraphJin ships the environment, not the trainer. The loop, the optimizer and the
GPUs stay on your side. What follows is what the environment gives you and the
order in which it is usable.

## Why the workflows have an order

A small base model dropped into this environment scores approximately zero, and
it scores zero for a structural reason: it skips discovery, writes a query
against guessed identifiers, and gets nothing back. Every attempt in a group
fails, every advantage is zero, and GRPO has no gradient to follow.

So the order is not a preference:

1. **Distil from a teacher** so the policy can complete an episode at all.
2. **Rejection-sample** from the tuned policy to get harder, on-policy data.
3. **GRPO** once attempts differ from each other, because that difference is the
   entire signal.

Starting at step 3 is the most common way to conclude, incorrectly, that the
environment does not work.

## 1. Distil from a teacher

Run a capable model over the training side and keep what passed.

```bash
export GJ_AGENT_PROVIDER=... GJ_AGENT_MODEL=<a-capable-model> GJ_AGENT_API_KEY_ENV=...

graphjin eval sample --demo --repeats 2 \
  --split eval/suite.split.json --side train --yes

graphjin eval export <run-id> --split eval/suite.split.json --side train \
  --stage executor --out teacher.jsonl

python3 training/sft_from_export.py teacher.jsonl --out sft.jsonl --min-reward 1.0
```

`--stage executor` is not optional in spirit. An agent run is three model calls
with different jobs — a distiller that condenses context, an executor that
writes the program, a responder that phrases the answer. Mixing all three into
one corpus teaches none of them.

The exporter refuses to emit a trajectory whose prompts were not recorded or
whose authorship cannot be resolved, rather than emitting a record that trains
the model on text it never produced.

## 2. Rejection-sample from the tuned policy

Point the environment at your checkpoint and collect several attempts per task
at a temperature above zero.

```bash
export GJ_AGENT_BASE_URL=http://your-inference-server:8000 GJ_AGENT_MODEL=<checkpoint>

graphjin eval sample --demo --repeats 8 --temperature 0.8 \
  --split eval/suite.split.json --side train --yes
```

**Without a temperature you get n identical answers.** The stack pins sampling to
zero by default, so a sampling run that forgets `--temperature` produces a group
with nothing to select from. What actually reaches the provider varies — some
models manage sampling server-side, some clamp low values upward, some drop it
when a reasoning effort is set — so the run records what was configured, and a
group of identical rewards is the symptom to check first.

## 3. GRPO

Serve the training side with `--step` and let your loop supply completions.

```bash
graphjin env serve --suite public --split auto:0.8 --side train --pool 8 \
  --step --support-model <a-small-fast-model>
```

```python
from graphjin_env import group_rollout, group_advantages

rewards = group_rollout(env_url, task, n=8, complete_fn=policy, concurrency=8)
advantages = group_advantages(rewards)
```

A group is n attempts at one task. With no baseline model, the other attempts
are the baseline — which is why a group whose rewards are all equal contributes
nothing, and why step 1 exists.

Keep `concurrency` at or below `--pool`: a world serves one episode at a time,
and the server blocks rather than interleaving.

### Train one stage, not three

`--support-model` routes the distiller and responder to a fixed cheap model
while the policy serves only the executor. This is the default posture rather
than an optimization: the executor prompt is the largest of the three, it is the
one that writes the program, and training the other two teaches your policy to
imitate a summarizer.

The support model deliberately does **not** inherit `GJ_AGENT_*`. Falling back
would serve the support stages with the very model under evaluation, which is
the thing the flag exists to prevent.

## Measuring between epochs

Serve the held-out side separately and measure:

```bash
graphjin env serve --suite public --split auto:0.8 --side eval --pool 4 --listen :8091
python3 training/measure.py --env http://127.0.0.1:8091 --repeats 2
```

`measure.py` prints a pass rate with a confidence interval and, always, the
resolution floor. **The same binary run twice against the same suite flips a
meaningful number of tasks.** Treat small differences as noise; the tool says so
every time rather than letting you forget.

## Held-out work stays held out

`eval export` refuses to build a training corpus from eval-side episodes, and
says how many it found. `--allow-eval-side` overrides it if you genuinely mean
to. The refusal exists because the alternative is a number that looks like
generalization and is not.

## What the model actually emits

The action space is a program, not a tool call. A completion carries JavaScript
that calls governed GraphJin functions, so one step can query, branch, and
aggregate rather than making a round trip per thought. Steps the runtime
authored on the model's behalf are marked and dropped from exports by default —
training on them would teach the model to imitate the harness.

## Next

- [Reward and comparability](/environment/reward/) — what to record with a number.
- [File formats](/environment/file-formats/) — what `eval export` actually writes.
