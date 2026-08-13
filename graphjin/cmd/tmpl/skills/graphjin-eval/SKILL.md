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
- Never hand-edit `website/data/benchmarks/<benchmark>.yaml` or
  `website/content/benchmarks/<benchmark>/runs/`. `graphjin eval publish` is the
  only supported writer; it writes one row and one run page and never runs Git.
- Treat `eval publish --label` as presentation only. Supersession uses the
  normalized provider and model identity, not the display label.
- Use `graphjin eval rm <task-id>` as the supported task-removal path; never
  delete a task from the suite file manually.
- Never invent an oracle, field, threshold, or business interpretation.
- Treat exit code 2 as a broken suite, not a model regression.
- Treat the suite generator version as part of the scoring contract. Bump
  `eval.GeneratorVersion` whenever generated task semantics change, including
  method-rule dialect support, then regenerate every committed/frozen suite.
  A binary must refuse suites from any other generator version.
- Treat exit code 3 as an environment problem, not a model regression.
- Treat exit code 130 as an interrupted checkpoint. Resume it; do not score it.
- Provider-backed commands can incur cost. Explain the expected call count and
  use `--yes` only after the user approves provider traffic.
- Read both usage views in the report: finalized tokens measure agent
  efficiency, while provider tokens include failed attempts and retries. On a
  compatible baseline, report the total-token and tokens-per-episode direction
  and percentage; treat cross-model or differently shaped comparisons as
  advisory.
- Check `provider_usage.complete`. If false, `unknown_attempts` counts provider
  calls that returned no usage and all recorded token totals are lower bounds.
  Never compare token percentages across accounting versions, providers,
  models, configured `max_steps`, or incomplete provider usage.
- Before calling two runs a same-build comparison, require matching
  `provenance.binary_fingerprint`. It is the SHA-256 of the exact CLI
  executable and catches runtime changes that do not alter prompt hashes.
- Full prompts, answers, rows, and executed queries stay in local episode files.
  Share reports, not episode files, unless the user explicitly asks for the
  private trajectory.
- Failed/interrupted provider attempts stay under `.graphjin-evals/attempts/`.
  They are private, and no persisted file may contain a credential.
- Use `GOOGLE_API_KEY` as the canonical Google credential name.
- Publishing does not refuse a low score. Never rerun a completed benchmark to
  make the public board look better; publish the observed result with its
  `accepted` state.
- Do not publish a report marked `scoring_suspect` until the scorer/runtime
  mismatch is understood. `--allow-suspect-scoring` is an explicit audited
  override, not a routine publishing flag.
- Publish with the exact binary that ran the benchmark. A missing
  `graphjin_commit` or mismatched `binary_fingerprint` is a broken provenance
  chain and must be rerun, not waived.
- Never publish an off-suite run unless the user explicitly asks for it. When
  asked, use `--allow-off-suite` and verify it appears as unranked with the
  mismatch reason.

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

   The command automatically resumes the newest strictly compatible incomplete
   run. Use `--resume <run-id>` to select one checkpoint. Use `--restart` only
   when the user intentionally wants fresh traffic; never combine the flags.
   The preview includes reused episodes and one possible transient retry for
   every pending initial/confirmation slot.

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

8. Run the frozen public cohort only after the user approves provider traffic:

   ```sh
   graphjin eval bench --public --yes --json
   ```

   Publish the resulting run only when the user explicitly asks. Review both
   generated files before committing them:

   ```sh
   graphjin eval publish <run-id> --benchmark deeporg --yes
   ```

   Do not add `--allow-off-suite` without a separate explicit ask.

9. In CI, restore the deliberately promoted sanitized baseline, require it to
   exist, and use `graphjin eval run --restart --yes --json`. Upload reports
   (`.json`, friendly `.md`, and `.technical.md`) only; never upload episodes
   or attempts.

## Diagnosis

Use the report's failure category as the first routing signal:

- `suite invalid` / exit 2: one or more hidden oracles no longer compile,
  execute, or extract. Repair the suite through `graphjin eval add` or recreate
  it; do not count this as a model regression.
- `provider_timeout`, `provider_rate_limit`, `provider_transport`, or
  `provider_5xx`: retryable environment failure exhausted its one retry; resume
  after the environment recovers. It is excluded from quality metrics.
- `provider_auth`, `provider_quota`, or `provider_model_unavailable`: repair the
  environment before resuming; these stop without retry.
- `safety_violation`: a forbidden action executed or a protocol violation leaked
  into an answered response. This is always a hard gate.
- `behavior_mismatch`: a required action, skill, or expected status was absent,
  or the model attempted a forbidden action that GraphJin safely refused.
- `client_side_aggregation`: the answer may be numerically right, but the action
  trail does not show database-side aggregation.
- `ranking_method`: a ranking answer did not use the required aggregate/order
  shape.
- `truncated_finalize`: the agent finalized from a limited row page.
- `wrong_window` or `stale_anchor`: the date boundary or anchor was wrong.
- `value_mismatch`: the answer disagreed with the fresh runtime oracle.
- `runaway`: the agent exhausted its eight actor steps or exceeded an advisory
  turn, token, or latency budget. Diagnose repeated calls; do not increase the
  global step limit to make redundant work more expensive.

For failed executions, read the private action summary's `error_codes`,
`recovery_codes`, and `recovery_tool` before opening the full chat log. These
stable fields identify the repair path without treating raw error prose as an
interface.

Use `--debug` only when deeper diagnosis is required. It prints local episode
paths. Keep episodes and attempts private because they contain trajectories.
GraphJin still recursively sanitizes credentials before any private write.

When dataset metadata is incomplete, GraphJin can still value-compare stable
targets through the report's suite-wide aggregate `oracle_value_hash`. If both
the dataset fingerprint and aggregate oracle hash differ, explain that GraphJin
intentionally falls back to method-correctness comparison instead of treating
changing live values as a model regression.
