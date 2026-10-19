---
name: graphjin-env
description: Use when setting up a training or evaluation loop against a GraphJin agent environment — running the container, reading /health, driving episodes hosted or step-by-step or with your own agent over MCP, splitting train from eval, exporting trajectories, and deciding whether two rewards can be compared.
---

# GraphJin Agent Environment

Use this skill when a user wants to train or measure an agent against a GraphJin
environment: running the container, driving graded episodes, collecting
trajectories, or interpreting a reward.

For creating and running evaluation suites against a project, use the
`graphjin-eval` skill instead. This one is about the environment as a training
target.

**This skill deliberately states no task counts, no flag defaults and no
measured figures.** Those live in `/health` and at
<https://graphjin.com/environment/>. Read them from the running server rather
than from here — a skill that carries no facts cannot carry stale ones.

## Rules

- **Read `/health` before anything else, and record it with any number you
  report.** A reward is only comparable against another from the same world
  under the same contract. The fields that decide it: `reward_version`,
  `reward_profile`, `dataset.catalog_hash`, `dataset.data_anchor`,
  `capabilities.suite_fingerprint`, and `build.version`.
- **Check `capabilities.catalog_match` before a long run.** If it is `false`,
  the suite's oracles were verified against a different schema and every episode
  is being graded against the wrong answers. If it is absent, there was nothing
  to compare.
- **Never compare an external-mode reward with a hosted one.** An external
  agent's token use never reaches the server, so the efficiency term is
  unmeasured rather than zero.
- **Never present a small difference as a result.** The same binary run twice
  against the same suite flips a meaningful number of tasks. Use
  `training/measure.py`, which prints a confidence interval and the resolution
  floor, and quote both.
- **A `GJ_ENV_` variable the server does not read is a startup error**, not a
  default. If the server refuses to start, read the error — it names the
  variable.
- **Without a temperature, a sampling group returns n identical answers.** The
  stack pins sampling to zero. If a group comes back with identical rewards,
  check this before concluding anything about the model.
- Never hand-edit `eval/suite.yml`, `eval/suite.split.json`, `world-pack.json`,
  or any file under `.graphjin-evals/`. Each carries a fingerprint something
  downstream compares against.
- Never spend provider tokens without the user's approval. Commands that call a
  model require `--yes` and print the call count first; surface that preview.

## Workflow

1. **Start it and read what it is.**

   ```sh
   docker run -d -p 8090:8090 --tmpfs /tmp:size=1g dosco/graphjin:env-latest
   curl -s localhost:8090/health
   ```

   `/tmp` must be writable — each world provisions its own database there.
   Confirm `status`, `capabilities.catalog_match`, and
   `capabilities.drive_modes` before going further.

2. **Pick a drive mode from `capabilities.drive_modes`**, not from assumption:

   | The user's situation | Mode |
   |---|---|
   | The policy is already behind an HTTP endpoint | hosted `POST /episodes` |
   | The weights are inside their training process | `--step` |
   | They have their own agent scaffold | `--external` |

   Endpoints that were not enabled do not exist; a 404 means the flag was not
   given.

3. **Keep held-out work held out.** Serve `--split auto:0.8 --side train` for
   collection and `--side eval` for measurement. `graphjin eval export` refuses
   to build a training corpus from eval-side episodes and says how many it
   found; `--allow-eval-side` overrides it, and using that override silently is
   how a number stops meaning generalization.

4. **Collect, then convert.**

   ```sh
   graphjin eval sample --repeats 8 --temperature 0.8 --split <split> --side train --yes
   graphjin eval export <run-id> --split <split> --side train --stage executor --out run.jsonl
   ```

   `--stage executor` matters: an agent run is three model calls with different
   jobs, and mixing them into one corpus teaches none of them.

5. **Measure on the held-out side between checkpoints**, never on the training
   side, and quote the interval rather than the point estimate.

## Diagnosis

- **Every episode scores zero** — usually no model configured, or a base model
  that skips discovery. Check `GJ_AGENT_*`; then run one episode with
  `include_response` and read what it actually did.
- **The server refuses to start** — read the message. A refused suite, a
  catalog mismatch, an unreadable `GJ_ENV_` variable and an unwritable work
  directory each say which one they are.
- **Rewards identical across a sampling group** — no temperature, or a provider
  that manages sampling itself.
- **`410` from a step or external route** — the episode ended or timed out and
  its world was reclaimed. Start a new one; do not retry the old id.
- **A number moved a little between checkpoints** — that is the instrument, not
  the model. See the resolution floor.

## Reference

<https://graphjin.com/environment/> — quickstart, drive modes, training
workflows, reward and comparability, CLI, HTTP API, and file formats.
