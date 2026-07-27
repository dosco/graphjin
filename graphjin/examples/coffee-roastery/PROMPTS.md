# Coffee Roastery Demo Prompts

Use these prompts after starting the demo with:

```bash
graphjin serve --demo --path examples/coffee-roastery
```

To verify the same surfaces from the command line, run:

```bash
examples/coffee-roastery/scripts/smoke.sh
```

Run `examples/coffee-roastery/scripts/smoke.sh --agent` when the server-side agent is enabled. Use `--agent-eval` for stricter open-ended checks that assert catalog discovery, saved-query detail inspection, safe-mode raw GraphQL blocking, and evidence-shaped responses.

The prompts below are plain business questions on purpose: discovering the
approved saved query and staying on the governed path is the agent's job, not
the reader's. That does require a capable model. Set one explicitly:

```bash
GJ_AGENT_MODEL=gpt-4.1 graphjin serve --demo --path examples/coffee-roastery
```

With no `agent.model` configured the provider default applies, which for OpenAI
is `gpt-5-mini`. That model does not reliably complete these multi-step runs: it
tends to stall or return blocked rather than execute the saved query. GraphJin's
protocol guards keep a weak model from publishing an ungrounded answer, but they
cannot make it finish the work.

## Daily Roast Planner

Find today's queued production orders, active subscriptions, available green coffee lots, and planned roast schedule. Decide whether the roast plan covers committed shipments and explain the next operational action.

Useful saved query: `daily_roast_context`

Useful workflow: `daily_roast_plan`

## Quality Review

Compare the latest roast batches, cupping scores, and sensor samples. Identify any batch that should be held for review before release.

Useful saved query: `batch_quality_snapshot`

Useful workflow: `batch_quality_review`

## Customer Issue Triage

Review open customer tickets and decide whether each should go to customer success or roasting quality review. Include the production order context when available.

Useful saved query: `customer_issue_context`

Useful workflow: `customer_issue_triage`

## Standing Watches

Create a watch that tells me when new production orders appear, then show my unseen watch events and mark them reviewed.

Useful roots: `gj_watch`, `gj_watch_event`

Useful help: `query_catalog(id: "help:watches")`

## Code-Aware Investigation

Search the `business_code` source for roast planning, subscription pressure, customer promise, and quality score logic. Use the code context before proposing workflow changes.

## Reviewed Catalog Memory

Find the `production_orders` table card, draft an annotation that expedite
reviews use `requested_ship_date`, and show the draft to me. Do not approve it
in the same run. After I confirm it in a later run, approve it and retrieve the
table detail again to show the reviewed organizational context.

Useful roots: `gj_artifacts`, `gj_catalog`

Useful help: `query_catalog(id: "help:artifacts")`
