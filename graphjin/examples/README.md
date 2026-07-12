# GraphJin Example Demos

The fastest start needs no clone at all: `graphjin serve --demo` with no
`--path` extracts the built-in **saas-ops** demo (SQLite, zero Docker)
to `./graphjin-demo` and boots it in seconds.

```bash
graphjin serve --demo
```

The directories here are the full set of demos. From a repo clone, point
`--demo` at any of them with `--path`. Each is a self-contained vertical: one
command boots its databases (Docker containers and in-process emulators),
applies the schema DDL, seeds data, and starts the server:

```bash
graphjin serve --demo --path examples/<name>
# or: make demo DEMO_PATH=examples/<name>
```

Put a model provider key (`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, or
`GOOGLE_APIKEY`) in `./.env` and `--demo` auto-enables the agentic surface
(server-side agent, MCP, watches, artifacts).

| Demo | Domain | Sources | Port | First boot |
| :--- | :--- | :--- | :--- | :--- |
| [coffee-roastery](coffee-roastery/) | Specialty coffee ops | Postgres + BigQuery-emu + TypeScript CodeSQL + workflows | 8080 | ~1-2 min (postgres pull) |
| [saas-ops](saas-ops/) — **built-in** (`graphjin serve --demo`) | SaaS company ops | SQLite (zero Docker) + workflows | 8083 | seconds |
| [corrugated-plant](corrugated-plant/) | Corrugated-box manufacturing | MySQL + BigQuery-emu + Python CodeSQL + JWT roles + workflows | 8081 | ~2-4 min (mysql pull) |
| [pcb-fab](pcb-fab/) | PCB design + fab | Postgres + Snowflake-emu + MongoDB + file source + OpenAPI supplier API + TypeScript CodeSQL | 8082 | ~3-5 min (postgres+mongo pull) |
| [webshop](webshop/) | Minimal single-DB starter | Postgres | 8080 | ~1 min |

## Smoke suites

Every demo ships an end-to-end smoke suite that exercises data queries, saved
queries, workflows, watches (create/fire/inbox), the artifact store (including
projection caps and locked kinds), structured agent refusals, role-gated
control-plane access, and — where enabled — MCP sampling:

```bash
examples/<name>/scripts/smoke.sh                # base checks
examples/<name>/scripts/smoke.sh --agent-eval   # + agent protocol evals (needs a model key)
```

Run everything in sequence with a summary table (Docker + `./.env` required):

```bash
make smoke-all
# scripts/demo-smoke-all.sh --only <name> for a single demo
```

The suites share one harness, [`lib/smoke-common.sh`](lib/smoke-common.sh):
transport/assert helpers, dev-header and JWT auth modes, stateful-MCP session
helpers, and generic capability suites each demo composes with its own domain
checks. The full borrow-the-caller's-model sampling loop is driven by
[`tools/mcp-sampling-client`](../tools/mcp-sampling-client/) — an MCP client
that advertises the sampling capability and forwards `sampling/createMessage`
to an OpenAI-compatible endpoint.
