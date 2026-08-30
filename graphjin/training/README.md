# GraphJin as an agent environment

GraphJin can serve one of your projects as a graded environment: a pool of
identical worlds, a set of tasks with hidden oracles, and a reward computed by
the same code the public benchmark uses.

**GraphJin ships the environment, not the trainer.** Nothing here runs an
optimizer or holds a GPU. The loop, the algorithm and the weights stay on your
side; this is the client and two example scripts.

## Why the reward is the environment's job

Every task carries an oracle — a read-only query that computes the answer in the
database — and it is never shown to the agent. Grading compares the agent's
answer to what the database says, so being plausible earns nothing. A task that
writes is graded by the state the database ended in, and by every other row
staying put: reaching the asked-for state while rewriting its neighbours fails
on safety rather than passing.

Because the reward comes from the environment, a number collected here and a
number on the public leaderboard mean the same thing.

## Using your own schema

You do not have to point this at a demo. `graphjin env clone` reads a running
GraphJin server's catalog — the same description of the schema any connected
agent already sees — and writes a local SQLite project with the same tables,
columns, keys and relationships, filled with synthetic rows:

```bash
graphjin env clone --url https://graphjin.internal --out ./clone-acme --seed 7
```

**No rows are read.** The only real values that cross over are the closed sets
the catalog publishes for a column — the handful of statuses it is known to
hold — because a task filtering on a state the business does not have is a task
about nothing. The clone is writable and resettable, which is what makes write
tasks and training possible against a schema whose real database is neither.

## The authoring model is not the model being trained

Counting, filtering and following a relationship can be derived from a schema.
Knowing that *failed invoices are worth alerting on*, and phrasing that the way
the person who wants it would, cannot. `graphjin eval author` asks a capable
model for those judgements and builds the tasks itself:

```bash
export GJ_GENERATOR_PROVIDER=anthropic
export GJ_GENERATOR_MODEL=<a-strong-model>
graphjin eval author --demo --path ./clone-acme --kinds watch,confirmation,history,scenario --yes
```

`GJ_GENERATOR_*` is deliberately separate from `GJ_AGENT_*`: the model being
trained is often small, and the model deciding what is worth asking should not
be. With nothing set it falls back to the agent's own configuration, so one
configured model is enough to try it.

Nothing the model returns is taken on trust. Every table, column and value it
names must exist in the schema census it was given; prose that names GraphJin's
own vocabulary is refused, since an intent task that says "create a watch" no
longer measures whether the agent can plan; and every task that survives is
resolved against the live database before it can enter a suite. Refusals are
printed with reasons. The result is written to `eval/authored.yml`, and
`eval create` picks it up as ordinary candidates.

## Running one

Generate a suite for your project, then serve it:

```bash
graphjin eval create --demo --writable --scale 500 --composition coverage \
  --verify-concurrency 8 --split 0.8

graphjin env serve --path ./graphjin-demo --suite eval/suite.yml \
  --pool 4 --split eval/suite.split.json --side train \
  --freeze-time 2026-08-01T12:00:00Z
```

`--freeze-time` fixes what the environment calls "now". Without it a run that
crosses midnight asks a different question of the same rows, and the episodes on
either side stop being comparable.

`--split` and `--side` keep training and measurement apart. Train on `train`,
measure on `eval`, and the number at the end means something.

## Pointing it at your policy

The agent talks to whatever OpenAI-compatible endpoint its configuration names,
so plugging in your own inference server is configuration rather than code:

```bash
export GJ_AGENT_PROVIDER=openai-compatible
export GJ_AGENT_BASE_URL=http://127.0.0.1:8099
export GJ_AGENT_MODEL=your-policy
export GJ_AGENT_API_KEY_ENV=YOUR_KEY_ENV
```

`policy_server.py` is a stand-in for that server. It answers every call with one
fixed program, which is enough to exercise the whole loop without a GPU or a
provider account:

```bash
python3 policy_server.py --listen 127.0.0.1:8099
python3 rollout_smoke.py --limit 5 --trajectories run.jsonl
```

## The action space is a program, not a tool call

The agent is a CodeAct agent: a completion is a JSON object carrying
`javascriptCode`, and that program runs in a sandbox where GraphJin's tools are
globals. So a trajectory step is a program and what running it returned, and
this is deliberately **not** the messages-with-tool-calls shape — converting it
to one would lose what actually happened.

The runtime also writes and runs programs of its own: repairs, forced
continuations, protocol handoffs. They appear in the trace exactly like the
policy's. Exports mark them and drop them by default, because training on them
teaches a model to imitate the environment's corrections instead of not needing
them.

## Turning runs into training data

```bash
graphjin eval export <run-id> --stage executor --out run.jsonl
python3 sft_from_export.py run.jsonl --out sft.jsonl --min-reward 1.0
```

`sft_from_export.py` refuses two kinds of trajectory rather than quietly
producing a corpus that trains the wrong thing: one whose trace recorded no
rendered prompt (the program is there, but not what was asked), and one where
the policy's programs cannot be told apart from the runtime's own.

Not every provider path records the rendered prompt in its trace. If the export
reports that, the trajectories are still usable for reward work and for
inspection — just not for supervised fine-tuning.

## What to record with a result

A reward is only comparable against another reward from the same world under the
same contract. `/health` returns both:

```json
{"dataset": {"catalog_hash": "...", "data_anchor": "2026-08-01"},
 "reward_version": "graphjin.eval.reward/v5", "reward_profile": "rl"}
```

Keep them with your numbers. Two runs that cannot name their world and their
reward contract cannot be compared with each other.

## Reward profiles

`benchmark` is the published contract; its weights do not move without a cohort
boundary. `rl` is for training: safety is a gate rather than a weighted term, so
an unsafe episode is worth nothing whatever else it got right, and correctness
dominates what remains.

The training profile also refuses to pay for an answer nobody could check. The
grounding guard fails open by design — blocking a real answer for want of
evidence it did collect would be worse — so a policy optimizing against a reward
would otherwise find that flooding the evidence corpus buys permission to say
anything.

## API

| method | path | purpose |
|---|---|---|
| GET | `/health` | worlds, task count, dataset fingerprint, reward contract |
| GET | `/tasks` | the tasks being served, with prompts |
| POST | `/episodes` | run one graded episode |

`POST /episodes` accepts `{task_id | slug, include_trajectory, reward_profile,
repeat}` and returns the answer, the score vector, the reward, and optionally
the trajectory.

There is also `POST /api/v1/eval/score` on a normal GraphJin server, for a loop
that runs its own rollouts and wants only the grading.
