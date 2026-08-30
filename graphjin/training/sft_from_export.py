"""Turn exported GraphJin trajectories into supervised fine-tuning records.

Input is what `graphjin eval export` writes: one trajectory per line. Output is
one training record per line, each a prompt and the program the policy emitted
in response.

Two things this refuses to do, because both produce a corpus that trains
something other than what you meant:

  * A trajectory whose trace recorded no rendered prompt has nothing to learn
    from — the program is there but not what was asked. Those are skipped and
    counted.
  * A step GraphJin's runtime wrote itself is not the policy's behaviour. The
    exporter marks them and drops them by default; this refuses any that got
    through, rather than teaching a model to imitate the environment's own
    repairs.
"""

from __future__ import annotations

import argparse
import json
import sys
from typing import Any, Iterator


def records(trajectory: dict[str, Any], min_reward: float) -> Iterator[dict[str, Any]]:
    if trajectory.get("reward", 0.0) < min_reward:
        return
    for step in trajectory.get("steps", []):
        if step.get("author") != "model":
            continue
        prompt = step.get("prompt") or []
        if not prompt:
            continue
        yield {
            "messages": list(prompt) + [{"role": "assistant", "content": step.get("program", "")}],
            "reward": trajectory.get("reward", 0.0),
            "task_id": trajectory.get("task_id", ""),
            "stage": step.get("stage", ""),
        }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("export", help="JSONL written by `graphjin eval export`")
    parser.add_argument("--out", default="-", help="where to write records (default stdout)")
    parser.add_argument(
        "--min-reward",
        type=float,
        default=1.0,
        help="keep only trajectories worth at least this much (default: only fully successful ones)",
    )
    args = parser.parse_args()

    skipped_no_prompt = 0
    skipped_unresolved = 0
    kept = 0
    handle = sys.stdout if args.out == "-" else open(args.out, "w")
    try:
        for line in open(args.export):
            line = line.strip()
            if not line:
                continue
            trajectory = json.loads(line)
            if not trajectory.get("prompts_recorded", False):
                skipped_no_prompt += 1
                continue
            if not trajectory.get("authorship_resolved", False):
                skipped_unresolved += 1
                continue
            for record in records(trajectory, args.min_reward):
                handle.write(json.dumps(record) + "\n")
                kept += 1
    finally:
        if handle is not sys.stdout:
            handle.close()

    print(f"wrote {kept} records", file=sys.stderr)
    if skipped_no_prompt:
        print(
            f"skipped {skipped_no_prompt} trajectories whose trace recorded no prompt — "
            "run the episodes through a provider whose traces carry the rendered prompt",
            file=sys.stderr,
        )
    if skipped_unresolved:
        print(
            f"skipped {skipped_unresolved} trajectories where the policy's programs could not be "
            "told apart from the runtime's own",
            file=sys.stderr,
        )
    if kept == 0:
        print("no usable records: see the notes above before training on this export", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
