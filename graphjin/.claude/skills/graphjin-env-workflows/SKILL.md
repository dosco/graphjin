---
name: graphjin-env-workflows
description: Use when running GraphJin's own environment and evaluation workflows in this repository — generating a suite, cloning or minting a world, authoring tasks, serving graded episodes, sampling, exporting trajectories, publishing a benchmark run — or when changing code those workflows depend on.
---

# GraphJin environment workflows (in this repo)

For working **on** GraphJin. Someone using a published environment wants the
`graphjin-env` skill instead, and the public documentation is at
<https://graphjin.com/environment/>.

## Which loop are you in

| Goal | Entry point |
|---|---|
| Measure: does the agent still work | `graphjin eval create` → `eval run` / `eval baseline` |
| Collect: build a training corpus | `graphjin eval sample` → `eval export` |
| Serve: give a training loop an environment | `graphjin env serve` |
| Mint: get a world that is not the demo | `graphjin env new-world` / `env clone` |

They share one engine. A change to scoring, task semantics or provenance
affects all four.

## Gates

Run the ones your change touches, not just the fast one.

```sh
go build -o /dev/null ./cmd/ && go vet ./cmd/
go test ./cmd/ ./agent/... -count=1          # ~2 minutes
cd website && npm run build && npm run check  # required for any website/** change
make env-image-smoke                          # skips cleanly without docker or ko
```

`npm run check` is the gate, not `hugo`. It validates every internal link and
anchor, pins load-bearing copy, and derives several assertions from Go source —
so a docs change can fail on a code file and vice versa.

## Never hand-edit

- `eval/suite.yml`, `eval/suite.split.json`, `eval/authored.yml` — use
  `eval add` / `eval rm`, which go through the validated writer. Task ids are
  content hashes; editing a prompt detaches the task from every stored episode.
- `website/data/benchmarks/<slug>.yaml` and
  `website/content/benchmarks/<slug>/runs/` — `graphjin eval publish` is the
  only sanctioned writer.
- Anything under `.graphjin-evals/`.

## Contracts that move fingerprints

Know before you edit, because these invalidate comparisons rather than break
builds:

- **`agent/skills.go`** — any string moves `PromptRegistryHash()`, which is
  recorded in run provenance. Every baseline comparison across that edit is
  invalid whether or not behaviour changed.
- **Skill payload budgets** are test literals in `agent/skills_test.go`, and
  they ratchet both ways. A guide gated behind a `gj_*` root costs an ordinary
  caller nothing; universal guidance costs every caller. Bump a budget visibly
  in the diff rather than routing prose through an uncounted channel.
- **`eval.GeneratorVersion`** — bump when generated task semantics change, and
  append the previous literal to `SupportedGeneratorVersions`.
- **`RewardVersion`** — bump on any scoring change. Runs either side are not
  comparable.
- **`suiteIdentityProjection`** — a new field changes every existing run's
  identity unless it is `omitempty` at its zero value.

See `docs/GRAPHJIN-EVAL.md` for the full contract.

## Files that must change together

| If you change | Also change |
|---|---|
| `cmd/env_serve_config.go` `envServeFlags` | `website/content/environment/cli-reference.md`, `CONFIG.md` — enforced by `check-site.mjs` |
| `cmd/cmd_env.go` subcommands | `website/content/environment/cli-reference.md` — enforced |
| `cmd/benchmark/public-suite.json` | nothing: the task count is derived — do not write it down |
| `agent/skills.go` skill list | `website/content/agentic/server-agent.md` count and enumeration |
| Anything with a `{{< verified >}}` badge | the badge's test name — enforced |
| Measured container figures | `website/data/environment.yaml`, including `measured.on` |

## Spending provider tokens

`eval add`, `eval author`, `eval run`, `eval bench`, `eval sample` and
`env new-world --describe` call a model. Each previews the call count and the
model before spending and requires `--yes` non-interactively. Show the user the
preview; do not pass `--yes` on their behalf without it.

`eval rescore` re-grades a stored run with no provider traffic. Prefer it when
the question is about scoring rather than about the model.

## Writing documentation for any of this

Facts that exist in code must be derived, not restated. `check-site.mjs` reads
`envServeFlags`, the `AddCommand` lists, `public-suite.json` and every
`verified by=` name, and fails when the prose disagrees. That is deliberate:
every stale claim this repo has shipped was a number somebody wrote down.

Never pin a negative claim in `requiredRenderedContent`. A sentence of the form
"X is not Y" keeps passing after X becomes Y — one such pin defended a false
disclaimer for five increments.
