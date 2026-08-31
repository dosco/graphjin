"""Drive a GRPO-shaped group against a step-driven environment, without a GPU.

This proves the loop a real trainer runs: start n episodes of one task, answer
every model call the environment parks, and get n graded rewards back. The
"policy" here is a fixed program, so the rewards are all the same and the
advantages are all zero — which is exactly what an untrained policy sampling
greedily looks like, and worth seeing once before blaming a trainer for it.

    graphjin env serve --demo --suite eval/suite.yml --pool 4 --step
    python3 grpo_smoke.py --env http://127.0.0.1:8090 --slug count-accounts --group 4

What it checks is that a full group comes back graded. A group with a missing
reward cannot produce an advantage, and a trainer that quietly drops those
learns from a smaller batch than it thinks it has.
"""

from __future__ import annotations

import argparse
import statistics
import sys

from graphjin_env import Environment, StepState, group_advantages, group_rollout

# The same program policy_server.py answers with: discover a card, then run the
# query it names. The discovery step is not decoration — the runtime refuses raw
# GraphQL from a caller that has not looked at the catalog.
DEFAULT_PROGRAM = (
    'const detail = await query_catalog({id: "table:app:main.accounts"});\n'
    'const res = await execute_graphql({query: "query { accounts { count_id } }"});\n'
    'await final({status: "answered", '
    'answer: "There are " + res.data.accounts[0].count_id + " accounts.", '
    "data: res.data, evidence: [detail]});"
)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--env", default="http://127.0.0.1:8090")
    parser.add_argument("--slug", default="", help="task to sample; defaults to the first one served")
    parser.add_argument("--group", type=int, default=4, help="attempts per group")
    parser.add_argument("--concurrency", type=int, default=1, help="parallel attempts (keep at or below --pool)")
    parser.add_argument("--reward-profile", default="rl")
    parser.add_argument("--program-file", default="", help="file holding the program to answer with")
    args = parser.parse_args()

    program = DEFAULT_PROGRAM
    if args.program_file:
        with open(args.program_file, encoding="utf-8") as handle:
            program = handle.read()
    completion = '{"javascriptCode": %s}' % _json_string(program)

    task = args.slug
    if not task:
        tasks = Environment(args.env).tasks()
        if not tasks:
            print("the environment is serving no tasks", file=sys.stderr)
            return 1
        task = tasks[0].slug
        print(f"no --slug given; sampling {task}")

    def complete(_: StepState) -> tuple[str, int, int]:
        # A real loop returns its model's tokens here; reporting zero would make
        # the policy look free to the efficiency term.
        return completion, len(program) // 4, len(program) // 4

    group = group_rollout(
        args.env, task, args.group, complete,
        concurrency=args.concurrency, reward_profile=args.reward_profile,
    )

    missing = [state for state in group if not state.done]
    rewards = [state.reward for state in group]
    advantages = group_advantages(group)

    print(f"task {task}: {len(group)} attempt(s)")
    for index, state in enumerate(group):
        print(f"  {index}: reward {state.reward:.3f}  advantage {advantages[index]:+.3f}  "
              f"pass {state.passed}  status {state.status or '-'}")
    print(f"mean reward {statistics.fmean(rewards):.3f}", end="")
    if len(rewards) > 1:
        print(f", spread {statistics.pstdev(rewards):.3f}")
    else:
        print()

    if missing:
        print(f"{len(missing)} attempt(s) never finished; a group with holes cannot produce advantages",
              file=sys.stderr)
        return 1
    if len(set(rewards)) == 1:
        print("every attempt scored the same, so every advantage is zero: this policy is sampling "
              "greedily (raise GJ_AGENT_TEMPERATURE) or the task is trivial for it")
    return 0


def _json_string(value: str) -> str:
    import json

    return json.dumps(value)


if __name__ == "__main__":
    raise SystemExit(main())
