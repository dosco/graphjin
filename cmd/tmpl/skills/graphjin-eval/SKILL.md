---
name: graphjin-eval
description: Create, extend, run, baseline, and diagnose GraphJin agent evaluations through the graphjin eval CLI.
---

# GraphJin Eval

Use this skill when a user wants to create or run a GraphJin agent benchmark,
add a real business question to the suite, establish a baseline, compare a
candidate, or understand an evaluation failure.

## Rules

- Always use `graphjin eval` commands with `--json` for machine-readable state.
- Never edit `eval/suite.yml`, hidden oracle definitions, tolerances, reward
  weights, reports, or baseline files by hand.
- Use `graphjin eval rm <task-id>` as the supported task-removal path; never
  delete a task from the suite file manually.
- Never invent an oracle, field, threshold, or business interpretation.
- Treat exit code 2 as a broken suite, not a model regression.
- Treat exit code 3 as an environment problem, not a model regression.
- Provider-backed commands can incur cost. Explain the expected call count and
  use `--yes` only after the user approves provider traffic.
- Full prompts, answers, rows, and executed queries stay in local episode files.
  Share reports, not episode files, unless the user explicitly asks for the
  private trajectory.

## Workflow

1. Inspect current state:

   ```sh
   graphjin eval --json
   ```

2. If no suite exists, create the deterministic 24-task suite:

   ```sh
   graphjin eval create --json
   ```

   Add `--demo` for the bundled demo or `--remote` for the server configured by
   `graphjin cli setup`.

3. Add an important business question through the model-assisted path:

   ```sh
   graphjin eval add "Which customers are at churn risk?" --json
   ```

   Report the CLI's plain-language interpretation and executed oracle result.
   If it asks for clarification, pass the question back to the user. Do not
   resolve ambiguity yourself.

4. Run the suite after approval:

   ```sh
   graphjin eval run --yes --json
   ```

   The first safe, valid run is promoted automatically at its observed recall.
   Recall below 0.90 is a quality warning, not a gate. Existing baselines compare
   only intersecting task IDs; new tasks remain advisory until a deliberate
   promotion.

5. Remove a bad-but-executable task only through the validated CLI path:

   ```sh
   graphjin eval rm <task-id> --yes --json
   ```

6. Deliberately replace the baseline only when the user requests it and the run
   has no confirmed regression or safety failure:

   ```sh
   graphjin eval baseline --yes --json
   ```

7. Run the extended benchmark when the user wants frontier distribution
   coverage:

   ```sh
   graphjin eval bench --scale 100 --seed 23 --yes --json
   ```

## Diagnosis

Use the report's failure category as the first routing signal:

- `suite invalid` / exit 2: one or more hidden oracles no longer compile,
  execute, or extract. Repair the suite through `graphjin eval add` or recreate
  it; do not count this as a model regression.
- `transport_error`: inspect agent readiness, credentials, and target URL.
- `safety_violation`: a forbidden action executed or a protocol violation leaked
  into an answered response. This is always a hard gate.
- `behavior_mismatch`: required action, skill, or expected status was absent.
- `client_side_aggregation`: the answer may be numerically right, but the action
  trail does not show database-side aggregation.
- `ranking_method`: a ranking answer did not use the required aggregate/order
  shape.
- `truncated_finalize`: the agent finalized from a limited row page.
- `wrong_window` or `stale_anchor`: the date boundary or anchor was wrong.
- `value_mismatch`: the answer disagreed with the fresh runtime oracle.
- `runaway`: the run exceeded its advisory turn, token, or latency budget.

Use `--debug` only when deeper diagnosis is required. It prints local episode
paths. Keep those files private because they contain the full trajectory.

When dataset metadata is incomplete, GraphJin can still value-compare stable
targets through the report's suite-wide aggregate `oracle_value_hash`. If both
the dataset fingerprint and aggregate oracle hash differ, explain that GraphJin
intentionally falls back to method-correctness comparison instead of treating
changing live values as a model regression.
