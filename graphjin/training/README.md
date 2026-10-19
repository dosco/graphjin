# graphjin_env

The Python client for a GraphJin agent environment.

GraphJin ships the environment; this package is the thin client a training or
evaluation loop drives it with. There is deliberately no trainer here — the
loop, the optimizer and the GPUs stay on your side.

**Documentation is at <https://graphjin.com/environment/>.** This file covers
only what is in this directory.

```bash
pip install -e training
```

No dependencies. The client speaks HTTP with the standard library, so it drops
into a training image that already has opinions about torch, numpy and
everything else without negotiating with any of them.

## What is here

| File | Purpose |
|---|---|
| `graphjin_env/client.py` | `Environment` — `/health`, `/tasks`, and whole graded episodes |
| `graphjin_env/step.py` | `StepEnvironment`, `run_episode`, `group_rollout`, `group_advantages` — the step bridge, where you supply each completion |
| `policy_server.py` | A fake OpenAI-compatible server returning one fixed program. Stands in for a policy so the plumbing can be tested without a model |
| `rollout_smoke.py` | One pass over `/tasks`, printing rewards. The shape a loop takes |
| `grpo_smoke.py` | A GPU-free GRPO-shaped group rollout, asserting a full group of rewards comes back |
| `measure.py` | Held-out measurement: pass rate, confidence interval, and the resolution floor |
| `sft_from_export.py` | Turns `graphjin eval export` output into SFT records, refusing trajectories with no recorded prompt or unresolved authorship |

## The shortest thing that works

Start an environment, point it at a fake policy, and drive it — no model, no
GPU, no provider account:

```bash
docker run -d -p 8090:8090 --tmpfs /tmp:size=1g dosco/graphjin:env-latest
python3 training/policy_server.py --listen 127.0.0.1:8099 &
python3 training/rollout_smoke.py --env http://127.0.0.1:8090
```

Every episode will score poorly, because the fake policy answers every question
with the same program. That is the point: it proves the loop runs, and it gives
you a floor to measure a real policy against.

## Using it

```python
from graphjin_env import Environment, group_rollout, group_advantages

env = Environment("http://127.0.0.1:8090")
for task in env.tasks():
    print(task.slug, env.run(task).reward)

# a GRPO group: n attempts at one task, scored against their own mean
rewards = group_rollout(env_url, task, n=8, complete_fn=policy, concurrency=8)
advantages = group_advantages(rewards)
```

Keep `concurrency` at or below the server's `--pool`. A world serves one episode
at a time, so the server blocks rather than interleaving.

## Where the rest is

- [Run the environment](https://graphjin.com/environment/quickstart/)
- [Driving episodes](https://graphjin.com/environment/drive-modes/) — hosted, step, external
- [Training a policy](https://graphjin.com/environment/training/) — and why the workflows have an order
- [Reward and comparability](https://graphjin.com/environment/reward/) — what to record with a number
- [HTTP API](https://graphjin.com/environment/http-api/)
