# SaaS Ops Demo Prompts

Use these prompts after starting the demo with:

```bash
graphjin serve --demo --path examples/saas-ops
```

To verify the same surfaces from the command line, run:

```bash
examples/saas-ops/scripts/smoke.sh
```

Add `--agent-eval` for stricter agent protocol evals. The shared
`scripts/demo-smoke-all.sh` gate also reboots this demo without server
credentials to verify automatic MCP client-model fallback, non-sampling client
failure, and the REST missing-credentials response.

## Churn Risk

Which accounts are most at risk of churning? Use failed payments, renewal
dates, and recent usage, and explain the ranking.

Useful saved query: `churn_risk_context`

Useful workflow: `dunning_retry_check`

## MRR Review

Summarize MRR by plan and flag any account with failed payments.

Useful saved query: `mrr_summary_context`

Useful workflow: `dunning_retry_check`

## SLA Triage

Which open support tickets are past or nearest their SLA? Who opened them, and
which account do they belong to?

Useful saved query: `ticket_sla_context`

Useful workflow: `sla_breach_check`

## Standing Watches

Create a watch that tells me when support ticket statuses change, then show my
unseen watch events and mark them reviewed.

Useful roots: `gj_watch`, `gj_watch_event`

Useful help: `query_catalog(id: "help:watches")`
