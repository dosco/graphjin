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

If the original serves documents alongside its database, the clone serves them
too — under the same source names, with documents this tool wrote. No file name
and no line of content is read from the original; what crosses over is the
source's name, which the catalog already publishes to any connected agent. The
manifest says so explicitly, because "we cloned your file source" is a sentence
somebody will otherwise read as "you copied our documents".

For a schema nobody has, `env new-world --describe "genome sequencing lab"`
asks a model to name the records that business would keep, validates every name,
and saves the description to `world-pack.json` in the world directory. Rebuild
it with `--pack world-pack.json --seed 7` — deterministic, and no model runs
again.

## The authoring model is not the model being trained

Counting, filtering and following a relationship can be derived from a schema.
Knowing that *failed invoices are worth alerting on*, and phrasing that the way
the person who wants it would, cannot. `graphjin eval author` asks a capable
model for those judgements and builds the tasks itself:

```bash
export GJ_GENERATOR_PROVIDER=anthropic
export GJ_GENERATOR_MODEL=<a-strong-model>
graphjin eval author --demo --path ./clone-acme --kinds watch,confirmation,history,scenario,file --yes
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

Two families are authored only where the environment can support them, and are
skipped with a stated reason where it cannot:

- A **delivery** task installs a watch, waits for it to fire, and asks the agent
  to read the event and clear it. It is authored only when rows the watch covers
  already exist — checked by counting them against the live database. A watch
  over an empty table never fires, so every episode would time out and score
  zero regardless of what the agent did, which reads as a model failure rather
  than the broken task it is.
- A **file** task asks something no single source answers: a count the database
  knows, and a standard somebody wrote down. Authoring plants that standard in a
  document it writes itself and grades against the same words, so the ground
  truth is true by construction. It needs a document source to exist.

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

## Three ways to drive an episode

All of them prepare the world the same way and grade through the same contract,
so a reward from one is a reward from any.

**GraphJin calls your endpoint.** The default, described below. Simplest when
your policy is already behind an HTTP endpoint.

**You supply each completion.** `env serve --step` inverts the call. The episode
runs exactly as it always does — same reset, same setup, same grading — but when
the model is needed the call is parked and returned to you as an observation
carrying the stage, the rendered conversation, the functions and the response
format. You post the completion back and the episode resumes:

```bash
graphjin env serve --path ./clone-acme --suite eval/suite.yml --pool 4 --step
```

```
POST /step/reset  {"slug": "..."}                  -> {episode_id, observation, done}
POST /step        {"episode_id": "...", "completion": "..."} -> {observation, done} | {done: true, reward, score}
DELETE /step/{id}                                  -> abandon it
```

This is for the case where the weights being updated live inside your training
process and standing up an inference server just to be called back is machinery
you would rather not have. An episode nobody drives is reclaimed after
`--step-timeout`, so a crashed loop does not take a world with it.

**You bring the whole agent.** `env serve --external` hands you the task, an MCP
endpoint and a deadline, and lets your own scaffold do the work:

```bash
graphjin env serve --path ./clone-acme --suite eval/suite.yml --pool 4 --external
```

```
POST   /external/episodes                 -> {episode_id, prompt, headers, mcp_url, graphql_url, deadline}
POST   /external/episodes/{id}/answer     -> {reward, score, pass, tool_calls}
DELETE /external/episodes/{id}            -> abandon it
```

The server records every MCP tool call and assembles the same account of what
happened that its own agent returns, so the method and behavior rules apply
unchanged: an answer submitted without touching the database scores zero, the
way it always did. One caveat travels with every external score and is worth
repeating — an external agent's token use never reaches the server, so the
efficiency term is not measured. External rewards are comparable with each
other, not with hosted runs.

## Training one stage instead of all of them

An agent run is several model calls with different jobs. The executor writes the
code that does the work; a distiller condenses large tool results and a
responder phrases the answer. Making a small policy do all three measures it
through bottlenecks it did not create, and makes you pay for tokens you are not
learning from.

```bash
graphjin env serve --path ./clone-acme --suite eval/suite.yml --pool 4 \
  --step --support-model <a-fast-model>
```

The support model answers the distiller and responder stages; everything else
goes to the policy. `GJ_SUPPORT_*` mirrors `GJ_GENERATOR_*`, with the same
credential discipline — a pinned provider resolves its own key or fails saying
which variable it wanted — and one deliberate difference: it does **not** fall
back to `GJ_AGENT_*`. Falling back would quietly serve the support stages with
the very model under evaluation, which is the arrangement the flag exists to
avoid.

The stages that write the final answer stay with the policy even when they look
like phrasing. One of them exists precisely because a draft answer named
something the run never observed; letting a stronger model rewrite it would
score that model's care as the policy's grounding.

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

If the export reports either, the trajectories are still usable for reward work
and for inspection — just not for supervised fine-tuning.

`--stage` matters more than it looks. An episode is produced by three different
policies sharing one trace, and a corpus mixing them teaches none of them.
Executor is the default because it is the stage that does the work.

## The three workflows, in the order they depend on each other

A base small model earns almost no reward on this suite — it has never seen the
protocol, and the runtime refuses raw GraphQL from a caller that has not read
the catalog, so most attempts score zero before the answer is even considered.
That matters for sequencing: **GRPO cannot start from there.** If nearly every
sample in a group scores the same zero, every advantage is zero and there is no
gradient. Teach the format first, then select on it, then optimize.

**1. Behavior cloning from a teacher.** Run a strong model over the training
side and keep what passed.

```bash
graphjin eval create --demo --writable --scale 500 --composition coverage --split 0.8
graphjin eval sample --demo --repeats 2 --split eval/suite.split.json --side train --yes
graphjin eval export <run-id> --split eval/suite.split.json --side train --out teacher.jsonl
python3 sft_from_export.py teacher.jsonl --out sft.jsonl --min-reward 1.0
```

**2. Rejection sampling from the tuned model.** Now that it produces valid
programs, let it produce many and keep the ones that worked.

```bash
export GJ_AGENT_BASE_URL=http://127.0.0.1:8099 GJ_AGENT_MODEL=your-checkpoint
graphjin eval sample --demo --repeats 8 --temperature 0.8 \
  --split eval/suite.split.json --side train --yes
graphjin eval export <run-id> --split eval/suite.split.json --side train --out sampled.jsonl
```

Without a temperature this collects eight copies of one answer: the stack pins
temperature 0 unless something raises it. `eval sample` says so rather than
letting you find out from a corpus with no variety in it.

**3. GRPO.** Drive episodes yourself, a group at a time.

```bash
graphjin env serve --path ./graphjin-demo --suite eval/suite.yml --split eval/suite.split.json \
  --side train --pool 4 --step --support-model <a-fast-model>
python3 grpo_smoke.py --env http://127.0.0.1:8090 --group 4
```

Measure between epochs against the side the training never saw:

```bash
graphjin env serve --path ./graphjin-demo --suite eval/suite.yml --split eval/suite.split.json \
  --side eval --pool 4 --listen 127.0.0.1:8091
python3 measure.py --env http://127.0.0.1:8091 --repeats 3
```

`measure.py` prints a confidence interval and the suite's resolution floor with
every result. The floor is real: this suite flips about 24 of 113 tasks between
two runs of the *same* binary, so a few points of movement is noise, and reading
it as progress is how a training run convinces itself it is working.

## Held-out tasks stay held out

`eval sample` records which side of the split it drew from, and `eval export`
refuses to build a training corpus out of held-out episodes — automatically when
the run recorded it, and on request when a split is named:

```bash
graphjin eval export <run-id> --split eval/suite.split.json --side train
# refuses if the run contains eval-side episodes; --allow-eval-side overrides
```

Nothing about an exported file used to say where its episodes came from, so
contaminating a corpus was silent and surfaced much later as a score that looked
too good and could not be explained.

## What your provider actually does with a temperature

`agent.temperature` and `agent.top_p` (or `GJ_AGENT_TEMPERATURE` /
`GJ_AGENT_TOP_P`) configure sampling; run provenance records what the server
resolved, so two runs can be told apart. What reaches the wire varies:

- Unset means temperature 0. The stack pins it, so repeats are identical.
- Anthropic's adaptive models never receive it.
- Gemini 3.7-flash, 3.6-flash and 3.5-flash-lite manage sampling server-side;
  nothing is sent.
- Other Gemini 3 models clamp anything below 1 up to 1.
- DeepSeek v4 drops it whenever a thinking effort is set.

If a sampling run comes back with identical rewards across a group, check this
list before concluding anything about the model.

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
| POST | `/step/reset`, `/step`, DELETE `/step/{id}` | drive an episode a completion at a time (`--step`) |
| POST | `/external/episodes`, `/external/episodes/{id}/answer` | grade your own agent over MCP (`--external`) |

`graphjin_env` wraps the first two: `Environment` for whole episodes, `StepEnvironment` plus `group_rollout` for step-driven ones. A GRPO group is n attempts at one task, and `group_advantages` scores each against the group's own mean — with no baseline model, the other attempts are the baseline.

The `--step` and `--external` routes exist only when the flag is given.

`POST /episodes` accepts `{task_id | slug, include_trajectory, reward_profile,
repeat}` and returns the answer, the score vector, the reward, and optionally
the trajectory.

There is also `POST /api/v1/eval/score` on a normal GraphJin server, for a loop
that runs its own rollouts and wants only the grading.
