---
title: "Reward, Comparability, And Provenance"
nav_title: "Reward"
description: "What the two profiles optimize for, and the exact fields that decide whether two numbers can be compared."
nav_group: "environment"
doc_kind: "reference"
weight: 95
---

## Two profiles

**`benchmark`** is the published contract. Its weights do not move without a
cohort boundary, because a leaderboard whose scoring changes underneath it is
not a leaderboard.

**`rl`** is for training, and differs in two deliberate ways.

Safety is a **gate** rather than a weighted term: an episode that caused an
unsafe effect is worth nothing regardless of what else it got right. As a
weighted term, a policy could learn to accept an occasional unsafe write in
exchange for correctness elsewhere, which is precisely the trade nobody wants it
to discover.

Correctness dominates what remains, and the profile refuses to pay for an answer
nobody could check. The grounding guard fails open by design — blocking a real
answer for want of evidence it did collect would be worse — so without this, a
policy optimizing the reward would find that flooding the evidence corpus buys
permission to say anything.

{{< verified by="TestUnknownRewardProfileIsRejected" file="agent/eval/cheater_battery_test.go" >}}

A stored run can be re-graded under either profile with `eval rescore`, from the
episode records, with no provider traffic.

## What the reward is made of

Correctness against the oracle; whether the method matched what the task
required (did the database aggregate, or did the model); whether required
actions were taken and forbidden ones avoided; for write tasks, whether the
end state is right **and** whether anything outside the task's scope changed;
and efficiency.

A correct answer arrived at by the wrong method does not score full marks, and
an answer with no work behind it scores zero.

## What to record with a number

A reward is only comparable against another reward from the same world under the
same contract. `/health` returns everything needed:

| Field | Why it matters |
|---|---|
| `reward_version` | The scoring contract. Different versions are different measurements. |
| `reward_profile` | `benchmark` and `rl` are not interchangeable. |
| `dataset.catalog_hash` | The schema the questions were asked about. |
| `dataset.data_anchor` | The day the rows are dated for. |
| `dataset.seed_manifest_hash` | The provisioning that produced those rows. |
| `suite.version` | The generator that produced the tasks. |
| `capabilities.suite_fingerprint` | The exact set of tasks. |
| `capabilities.catalog_match` | Whether the suite describes the world it was served on. |
| `build.version`, `build.binary_sha256` | Which binary produced the number. |

Two runs that cannot name their world and their contract cannot be compared with
each other. Keeping these alongside a result costs one JSON blob and is the
difference between a number and an anecdote.

## Two things that quietly break comparability

**External-mode rewards.** An external agent's token use never reaches the
server, so the efficiency term is unmeasured rather than zero. External rewards
compare with each other and not with hosted runs. The caveat is returned in the
response body of every external episode.

**An unfrozen clock.** A task about "the last 30 days" is a different question
tomorrow. `--freeze-time` pins what the environment calls now, and pins the
day the data is seeded for to match; `capabilities.freeze_time_source` says who
pinned it, or reports nothing if nobody did.

## The resolution floor

**The same binary, run twice against the same suite, flips a meaningful number
of tasks.** Agent runs are not deterministic even at temperature zero, and the
suite is not large enough to resolve small differences.

Treat a few points of movement as noise. `training/measure.py` prints a
confidence interval and states the floor on every run, rather than leaving you
to remember it at the moment you most want to believe a result.

One wrong answer is not a regression. Two runs of the same checkpoint disagreeing
is the expected behaviour of the instrument.
