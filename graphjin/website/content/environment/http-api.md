---
title: "Environment HTTP API"
nav_title: "HTTP API"
description: "Every route a served environment exposes, with request and response shapes."
nav_group: "environment"
doc_kind: "reference"
weight: 80
---

All routes are served by `graphjin env serve`. The step and external routes
exist only when their flag is given — a trainer that did not ask for them should
not find endpoints it can drive into a state the ordinary path never reaches.

## GET /health

Readiness by construction: the listener opens only after every world in the pool
has booted, so a `200` here means the environment can actually serve.

```json
{
  "status": "ready",
  "workers": 2,
  "tasks": 113,
  "dataset": {
    "catalog_hash": "…", "data_anchor": "2026-08-01", "seed_manifest_hash": "…"
  },
  "reward_version": "graphjin.eval.reward/v6",
  "reward_profile": "rl",
  "suite": { "version": "graphjin.eval.generator/v12", "seed": 23, "scale": 100 },
  "build": {
    "version": "…", "commit": "…", "date": "…", "go": "…",
    "binary_sha256": "…", "image_role": "env"
  },
  "capabilities": {
    "drive_modes": ["episodes", "step"],
    "writes": true, "reactive": true, "resettable": true,
    "suite_source": "public", "suite_fingerprint": "…",
    "catalog_fingerprint": "…", "catalog_match": true,
    "split": "auto:0.80", "side": "train", "pool": 2,
    "freeze_time": "…", "freeze_time_source": "build",
    "data_anchor": "2026-08-01", "boot_ms": 2055
  }
}
```

`build` and `capabilities` are additive; every field above them keeps its name
and type. `catalog_match` is **absent** rather than `false` when the suite
records no catalog to compare, so "not checked" never reads as "checked and
drifted".

## GET /tasks

```json
{"tasks": [{
  "task_id": "…", "slug": "count-accounts",
  "category": "aggregate", "difficulty": "T1",
  "prompt": "How many accounts are there?",
  "writes": false, "family": "catalog-entity"
}]}
```

Only the tasks on the served side of the split appear.

## POST /episodes

Run one graded episode.

```json
{
  "task_id": "…",            // or "slug"
  "repeat": 0,               // a label, not a count
  "reward_profile": "rl",
  "include_trajectory": false,
  "include_response": false,
  "stage": "executor"        // executor | distiller | responder | all
}
```

```json
{
  "task_id": "…", "slug": "…", "status": "answered",
  "answer": "There are 42 accounts.",
  "pass": true, "reward": 0.93,
  "score": { … }, "latency_ms": 4210,
  "trajectory": { … },        // when requested
  "trajectory_error": "…"     // when one was requested and could not be built
}
```

`trajectory_error` is returned rather than swallowed: an empty field otherwise
cannot be told apart from an episode that legitimately produced no steps.

## The step routes (`--step`)

```
POST   /step/reset   {"slug" | "task_id", "reward_profile"}
       -> {"episode_id": "…", "observation": {stage, messages, functions, response_format}, "done": false}

POST   /step         {"episode_id": "…", "completion": …, "prompt_tokens": …, "completion_tokens": …}
       -> {"observation": …, "done": false}
        | {"done": true, "status": …, "answer": …, "reward": …, "score": …, "pass": …}

DELETE /step/{id}    -> abandon; the world is returned to the pool
```

`409` nothing is awaiting a completion · `410` the episode has ended or timed
out · `504` it exceeded its allowance. The Python client maps these to
`NothingAwaiting`, `EpisodeGone` and `EpisodeTimeout`.

## The external routes (`--external`)

```
POST   /external/episodes              -> {episode_id, prompt, turns, headers,
                                           mcp_url, graphql_url, deadline, note}
POST   /external/episodes/{id}/answer  {"status": …, "answer": …}
                                       -> {reward, score, pass, tool_calls, note}
DELETE /external/episodes/{id}         -> abandon
ANY    /external/episodes/{id}/world/… -> the leased world, proxied
```

The proxy is how an agent outside the container reaches a world that otherwise
exists only inside the serving process. The episode identifier in the path is
the authorization: once the lease ends, the path returns `410`.

`note` carries the caveat that external rewards omit the efficiency term,
returned with the score rather than left in documentation.

## POST /api/v1/eval/score

On an ordinary GraphJin server, not the environment. For a loop that runs its
own rollouts and wants only the grading.

## Python client

```python
from graphjin_env import Environment, StepEnvironment, group_rollout

env = Environment("http://127.0.0.1:8090")
for task in env.tasks():
    print(task.slug, env.run(task).reward)
```

`pip install -e training` from a GraphJin checkout. It has no dependencies
beyond the standard library, so it drops into a training image without
negotiating versions with anything already there.
