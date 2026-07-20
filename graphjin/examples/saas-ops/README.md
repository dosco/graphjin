# SaaS Ops Demo

The fastest GraphJin demo, and the one **built into the binary**: a SaaS
company ops app on SQLite — no Docker, no containers, boots in seconds. Bare
`graphjin serve --demo` (no `--path`) extracts this exact project to
`./graphjin-demo` and runs it; from a repo clone you can also point at it
explicitly:

```bash
graphjin serve --demo                            # built-in, no clone
graphjin serve --demo --path examples/saas-ops   # from a repo clone
# Web UI:  http://localhost:8083/
# GraphQL: http://localhost:8083/api/v1/graphql
# MCP:     http://localhost:8083/api/v1/mcp
```

Put a model key (`OPENAI_API_KEY` / `ANTHROPIC_API_KEY` / `GOOGLE_APIKEY`) in
`./.env` and `--demo` auto-enables the agentic surface (server-side agent,
watches, artifacts).

## What's inside

- **app** (SQLite): accounts, users, subscriptions, invoices, support_tickets,
  usage_events — seeded with a small deterministic dataset. Meridian Robotics
  is the churn-risk anchor: failed payments, a breached-SLA urgent ticket,
  collapsing usage, and a renewal nine days out.
- **Saved queries**: `churn_risk_context`, `mrr_summary_context`,
  `ticket_sla_context` (also served as REST at `/api/v1/rest/<name>`).
- **Workflows**: `sla_breach_check`, `dunning_retry_check`.
- **Watches + artifacts** enabled (`gj_watch`, `gj_watch_event`,
  `gj_artifacts`; the `runbook` artifact kind is locked to demo policy
  refusals).

## Identity

In agentic mode the server verifies HS256 JWTs
(`auth.jwt.secret: saas-ops-demo-jwt-secret`); the smoke suite mints demo
tokens automatically. In plain dev mode header identity
(`X-User-ID` / `X-User-Role` / `X-Account-ID`) is trusted instead.

## Smoke suite

```bash
examples/saas-ops/scripts/smoke.sh                # base checks
examples/saas-ops/scripts/smoke.sh --agent-eval   # + agent protocol evals
```

Because this demo boots in seconds, the shared smoke harness also uses it for
the client-model fallback checks. It boots the demo with an intentionally empty
server key environment, verifies a sampling-capable MCP client succeeds with at
least one `sampling/createMessage` call, verifies `--no-sampling` fails with
`model_sampling_unavailable`, and verifies REST reports missing server
credentials. Run that gate through `scripts/demo-smoke-all.sh`.

See [PROMPTS.md](PROMPTS.md) for agent prompts to try interactively.
