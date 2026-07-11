# Clinic Scheduler Demo

The fastest GraphJin demo, and the one **built into the binary**: a clinic
scheduling app on SQLite — no Docker, no containers, boots in seconds. Bare
`graphjin serve --demo` (no `--path`) extracts this exact project to
`./graphjin-demo` and runs it; from a repo clone you can also point at it
explicitly:

```bash
graphjin serve --demo                                    # built-in, no clone
graphjin serve --demo --path examples/clinic-scheduler   # from a repo clone
# Web UI:  http://localhost:8083/
# GraphQL: http://localhost:8083/api/v1/graphql
# MCP:     http://localhost:8083/api/v1/mcp
```

Put a model key (`OPENAI_API_KEY` / `ANTHROPIC_API_KEY` / `GOOGLE_APIKEY`) in
`./.env` and `--demo` auto-enables the agentic surface (server-side agent,
watches, artifacts).

## What's inside

- **app** (SQLite): patients, providers, rooms, appointments, waitlist_entries,
  no_show_events — seeded with a small deterministic dataset.
- **Saved queries**: `daily_schedule_context`, `waitlist_context`,
  `utilization_context` (also served as REST at `/api/v1/rest/<name>`).
- **Workflows**: `overbook_check`, `waitlist_promotion`.
- **Watches + artifacts** enabled (`gj_watch`, `gj_watch_event`,
  `gj_artifacts`; the `runbook` artifact kind is locked to demo policy
  refusals).

## Identity

In agentic mode the server verifies HS256 JWTs
(`auth.jwt.secret: clinic-scheduler-demo-jwt-secret`); the smoke suite mints
demo tokens automatically. In plain dev mode header identity
(`X-User-ID` / `X-User-Role` / `X-Account-ID`) is trusted instead.

## Smoke suite

```bash
examples/clinic-scheduler/scripts/smoke.sh                # base checks
examples/clinic-scheduler/scripts/smoke.sh --agent-eval   # + agent protocol evals
```

Because this demo boots in seconds it also hosts the MCP sampling
require-mode checks: boot with
`GJ_AGENT_SAMPLING=require GJ_MCP_HTTP_STATEFUL=true` and run
`scripts/smoke.sh --no-agent --sampling`.

See [PROMPTS.md](PROMPTS.md) for agent prompts to try interactively.
