"""Drive a GraphJin environment for one pass over its tasks.

This is the shape a training loop takes: ask what tasks exist, run episodes,
collect rewards. Where a real loop would send the trajectories to an optimizer,
this prints what it got, so the interface can be checked without a trainer.
"""

from __future__ import annotations

import argparse
import json
import statistics
import sys

from graphjin_env import Environment


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--env", default="http://127.0.0.1:8090")
    parser.add_argument("--limit", type=int, default=5)
    parser.add_argument("--reward-profile", default=None)
    parser.add_argument("--trajectories", default=None, help="write collected trajectories here as JSONL")
    args = parser.parse_args()

    environment = Environment(args.env)
    health = environment.health()
    # Record what was measured, not just the number: a reward is only comparable
    # against another reward from the same world and the same contract.
    print(
        f"environment: {health['workers']} worlds, {health['tasks']} tasks, "
        f"reward {health['reward_profile']}/{health['reward_version']}, "
        f"catalog {health['dataset'].get('catalog_hash', '')[:12]}",
        flush=True,
    )

    tasks = environment.tasks()[: args.limit]
    if not tasks:
        print("no tasks served", file=sys.stderr)
        return 1

    rewards: list[float] = []
    collected = []
    for task in tasks:
        episode = environment.run(
            task,
            include_trajectory=args.trajectories is not None,
            reward_profile=args.reward_profile,
        )
        rewards.append(episode.reward)
        if episode.trajectory:
            collected.append(episode.trajectory)
        verdict = "pass" if episode.passed else episode.score.get("failure_category", "fail")
        print(f"  {task.slug:<44} reward={episode.reward:<6.3f} {verdict}", flush=True)

    print(f"mean reward {statistics.fmean(rewards):.3f} over {len(rewards)} episodes")
    if args.trajectories:
        with open(args.trajectories, "w") as handle:
            for trajectory in collected:
                handle.write(json.dumps(trajectory) + "\n")
        print(f"wrote {len(collected)} trajectories to {args.trajectories}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
