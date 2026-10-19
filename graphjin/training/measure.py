"""Measure a checkpoint on the held-out side of a split.

Run this between epochs against an environment serving `--side eval`. It reports
a pass rate with a confidence interval, because the number on its own invites a
comparison it cannot support: this suite flips roughly 24 of 113 tasks between
two runs of the *same* binary, so a few points of movement is noise and reading
it as progress is how a training run convinces itself it is working.

    graphjin env serve --demo --suite eval/suite.yml \\
      --split eval/suite.split.json --side eval --pool 4 --listen 127.0.0.1:8091
    python3 measure.py --env http://127.0.0.1:8091 --repeats 3
"""

from __future__ import annotations

import argparse
import collections
import math
import sys

from graphjin_env import Environment


def wilson_interval(passed: int, total: int, z: float = 1.96) -> tuple[float, float]:
    """A confidence interval that behaves at the edges.

    The textbook interval on a proportion goes to zero width at 0% and 100%,
    which is where an early checkpoint and a saturated one both sit — precisely
    when an honest interval matters most.
    """
    if total == 0:
        return 0.0, 0.0
    rate = passed / total
    denominator = 1 + z * z / total
    centre = (rate + z * z / (2 * total)) / denominator
    margin = z * math.sqrt(rate * (1 - rate) / total + z * z / (4 * total * total)) / denominator
    return max(0.0, centre - margin), min(1.0, centre + margin)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--env", default="http://127.0.0.1:8090")
    parser.add_argument("--repeats", type=int, default=3, help="attempts per task")
    parser.add_argument("--limit", type=int, default=0, help="measure only the first N tasks")
    parser.add_argument("--reward-profile", default="rl")
    args = parser.parse_args()

    environment = Environment(args.env)
    health = environment.health()
    tasks = environment.tasks()
    if args.limit > 0:
        tasks = tasks[: args.limit]
    if not tasks:
        print("the environment is serving no tasks", file=sys.stderr)
        return 1

    passed = total = 0
    rewards: list[float] = []
    by_category: dict[str, list[int]] = collections.defaultdict(list)
    for task in tasks:
        for repeat in range(args.repeats):
            episode = environment.run(task, repeat=repeat + 1, reward_profile=args.reward_profile)
            total += 1
            rewards.append(episode.reward)
            hit = 1 if episode.passed else 0
            passed += hit
            by_category[task.category or "-"].append(hit)

    low, high = wilson_interval(passed, total)
    rate = passed / total if total else 0.0
    print(f"dataset {health.get('dataset', {}).get('catalog_hash', '-')}  "
          f"reward {health.get('reward_version', '-')}/{health.get('reward_profile', '-')}")
    print(f"{len(tasks)} task(s) x {args.repeats} = {total} episode(s)")
    print(f"pass rate {rate:.3f}  95% CI [{low:.3f}, {high:.3f}]  "
          f"mean reward {sum(rewards) / len(rewards):.3f}")
    for category in sorted(by_category):
        hits = by_category[category]
        print(f"  {category:<16} {sum(hits)}/{len(hits)}")
    # Printed every time, deliberately. A caller comparing two checkpoints needs
    # this in front of them at the moment of comparison, not in a document.
    print("\nThis suite flips about 24 of 113 tasks between two runs of the same binary. "
          "Treat differences under roughly 6 points as noise, and confirm a real one by "
          "re-measuring rather than by running longer.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
