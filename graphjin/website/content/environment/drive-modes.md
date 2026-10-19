---
title: "Three Ways To Drive An Episode"
nav_title: "Drive Modes"
description: "GraphJin calls your endpoint, you supply each completion, or you bring the whole agent. Same world, same grading."
nav_group: "environment"
doc_kind: "guide"
weight: 30
---

All three prepare the world identically and grade through the same contract, so
a reward from one is a reward from any — with one documented exception, at the
bottom of this page.

## GraphJin calls your endpoint

The default. Your policy sits behind an OpenAI-compatible endpoint and GraphJin
runs the whole episode: discovery, the agent loop, the answer, the grading.

```bash
graphjin env serve --suite public --pool 4
```

```
POST /episodes  {"slug": "..."}  ->  {answer, status, pass, reward, score}
```

Simplest when the weights you are evaluating are already being served. For
evaluation and for rejection sampling this is usually what you want.

## You supply each completion

`--step` inverts the call. The episode runs exactly as it always does — same
reset, same setup, same grading — but when the model is needed, the call is
parked and handed back to you as an observation carrying the stage, the rendered
conversation, the available functions and the response format. You post the
completion back and the episode resumes.

```bash
graphjin env serve --suite public --pool 4 --step
```

```
POST   /step/reset  {"slug": "..."}                       -> {episode_id, observation, done}
POST   /step        {"episode_id": "...", "completion": ...} -> {observation, done}
                                                          |  {done: true, reward, score, pass}
DELETE /step/{id}                                          -> abandon it
```

This is for the case where the weights being updated live inside your training
process and standing up an inference server just to be called back is machinery
you would rather not have. It is the mode GRPO uses.

An episode nobody drives is reclaimed after `--step-timeout`, so a crashed loop
does not take a world with it. The agent's own deadline is raised to clear that
allowance, so a trainer that thinks for two minutes does not lose the episode it
is inside.

The Python client wraps this:

```python
from graphjin_env import StepEnvironment, group_rollout
rewards = group_rollout("http://127.0.0.1:8090", task, n=8, complete_fn=my_model)
```

## You bring the whole agent

`--external` hands you the task, an MCP endpoint and a deadline, and lets your
own scaffold do the work.

```bash
graphjin env serve --suite public --pool 4 --external
```

```
POST   /external/episodes              -> {episode_id, prompt, headers, mcp_url, graphql_url, deadline}
POST   /external/episodes/{id}/answer  -> {reward, score, pass, tool_calls}
DELETE /external/episodes/{id}         -> abandon it
```

The server records every MCP tool call your agent makes and assembles the same
account of what happened that its own agent returns, so the method and behaviour
rules apply unchanged: an answer submitted without touching the database scores
zero, exactly as it would in the other two modes.

{{< verified by="TestExternalAgentIsGradedOnWhatItActuallyDid" file="cmd/eval_external_test.go" >}}

The `mcp_url` you are given routes back through the server you reached, at
`/external/episodes/{id}/world/api/v1/mcp`. The episode identifier in that path
is the authorization: it names the lease, and a lease that has ended stops
routing anywhere. If something between you and the server rewrites `Host`, pass
`--advertise-url`.

{{< verified by="TestExternalWorldIsReachableThroughTheServerThatLeasedIt" file="cmd/eval_external_test.go" >}}

## The one caveat that does not travel

**An external agent's token use never reaches the server.** The efficiency term
in the reward is therefore unmeasured, and it is not silently zeroed — it is
vacuous. External rewards are comparable with each other and **not** with rewards
from the other two modes.

That caveat is returned in the response body of every external episode rather
than left in documentation, because the place it matters is next to a number
somebody is about to put in a table.

## Choosing

| Your situation | Mode |
|---|---|
| The policy is already behind an HTTP endpoint | hosted `/episodes` |
| The weights are inside your training process | `--step` |
| You have your own agent scaffold and want it graded | `--external` |

The modes are not exclusive — one server can offer all three. `/health` reports
`capabilities.drive_modes` so a client can discover which are enabled instead of
finding out from a 404.
