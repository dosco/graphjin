---
title: "Run The Environment In Two Minutes"
nav_title: "Quickstart"
description: "Pull the image, read /health, and drive one graded episode. Nothing to mount, no suite to generate."
nav_group: "environment"
doc_kind: "guide"
weight: 20
---

For anyone who wants an environment to train or evaluate against. If you want to
measure an agent on your own schema instead, start at
[Use your own graph](/environment/your-own-graph/).

## Start it

```bash
docker run -d -p 8090:8090 --tmpfs /tmp:size=1g dosco/graphjin:env-latest
```

Nothing is mounted and nothing is generated. The image carries a demo
organization — accounts, subscriptions, invoices, support tickets — and a frozen
suite of verified tasks built against it.

**`/tmp` must be writable.** Each world provisions its own database, discovery
cache and artifact store there, so a read-only root filesystem needs `--tmpfs`
or a volume. Size it for `pool × (project + database + cache)`.

{{< env-figures >}}

## Ask it what it is

```bash
curl -s localhost:8090/health | python3 -m json.tool
```

```json
{
  "status": "ready",
  "workers": 2,
  "tasks": 113,
  "dataset": { "catalog_hash": "c4ee2c0d…", "data_anchor": "2026-08-01" },
  "reward_version": "graphjin.eval.reward/v6",
  "reward_profile": "rl",
  "build": { "version": "…", "commit": "…", "image_role": "env" },
  "capabilities": {
    "drive_modes": ["episodes"],
    "writes": true, "resettable": true,
    "suite_source": "public", "suite_fingerprint": "8f312479…",
    "catalog_match": true, "split": "none", "side": "train",
    "freeze_time_source": "build", "boot_ms": 2055
  }
}
```

Two fields are worth reading before anything else.

**`catalog_match`** says the suite's oracles were verified against the schema
being served. If it is `false`, every episode is being graded against answers
computed for a different database. Serving that combination is refused at
startup unless you explicitly allow it, so seeing `true` here is confirmation
rather than luck.

**`freeze_time_source`** says who pinned the clock. In this image it is
`build`: the clock is pinned to the image's build date, so one tag asks the same
question on any day you run it. Without a pin, a task about "the last 30 days"
quietly becomes a different task tomorrow.

## Drive one episode

```bash
curl -s -X POST localhost:8090/episodes -d '{"slug": "count-accounts"}'
```

`/tasks` lists what is available. The response carries the answer, the score
vector, and the reward. Without a model configured the episode is graded a
failure, which is the correct result — it is what an agent that does nothing
earns.

To point it at a policy, give it the four agent variables:

```bash
docker run -d -p 8090:8090 --tmpfs /tmp:size=1g \
  -e GJ_AGENT_PROVIDER=openai-compatible \
  -e GJ_AGENT_BASE_URL=http://your-inference-server:8000 \
  -e GJ_AGENT_MODEL=your-checkpoint \
  -e GJ_AGENT_API_KEY_ENV=YOUR_KEY_VAR \
  dosco/graphjin:env-latest
```

## Configure it

Every flag has a `GJ_ENV_*` variable, because a container is configured by
whoever wrote its manifest. A flag wins when it is passed, and **a `GJ_ENV_`
variable the server does not read is a startup error** — a typo in a manifest is
otherwise indistinguishable from a default.

```bash
# a train container and an eval container that agree on the holdout,
# with nothing mounted and no coordination between them
docker run -d -p 8090:8090 --tmpfs /tmp:size=1g \
  -e GJ_ENV_SPLIT=auto:0.8 -e GJ_ENV_SIDE=train dosco/graphjin:env-latest
docker run -d -p 8091:8090 --tmpfs /tmp:size=1g \
  -e GJ_ENV_SPLIT=auto:0.8 -e GJ_ENV_SIDE=eval  dosco/graphjin:env-latest
```

`--split auto` derives the division from each task's content identifier, so two
containers reach the same answer independently. Without a split there is no
holdout, and `/health` reports `"split": "none"` so that is visible rather than
assumed.

The full list is in the [CLI reference](/environment/cli-reference/).

## Health checks

The image has no shell and no curl, so the binary is the probe:

```dockerfile
HEALTHCHECK --interval=10s --timeout=5s --retries=30 \
  CMD ["/ko-app/v3", "env", "health"]
```

It exits zero only for a server that is actually ready. A server that is up and
not yet serving is exactly what a health check exists to catch.

## Next

- [Driving episodes](/environment/drive-modes/) — three ways, one reward contract.
- [Training a policy](/environment/training/) — and why the workflows have an order.
- [Reward and comparability](/environment/reward/) — what to record with a number.
