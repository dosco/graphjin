# Clinic Scheduler Demo Prompts

Use these prompts after starting the demo with:

```bash
graphjin serve --demo --path examples/clinic-scheduler
```

To verify the same surfaces from the command line, run:

```bash
examples/clinic-scheduler/scripts/smoke.sh
```

Add `--agent-eval` for stricter agent protocol evals, and `--sampling` against a
server booted with `GJ_AGENT_SAMPLING=require GJ_MCP_HTTP_STATEFUL=true` for the
MCP sampling fail-closed checks.

## Waitlist Priority

Who should get the next open cardiology slot? Use the waitlist and explain the
priority order.

Useful saved query: `waitlist_context`

Useful workflow: `waitlist_promotion`

## Daily Schedule Review

Summarize today's scheduled appointments by provider and room, and flag any
room at risk of overbooking.

Useful saved query: `daily_schedule_context`

Useful workflow: `overbook_check`

## No-Show Follow-Up

Review recent no-show events and suggest which patients need outreach.

Useful saved query: `utilization_context`

## Standing Watches

Create a watch that tells me when appointment statuses change, then show my
unseen watch events and mark them reviewed.

Useful roots: `gj_watch`, `gj_watch_event`

Useful help: `query_catalog(id: "help:watches")`
