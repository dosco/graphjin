# Coffee Roastery Demo Prompts

Use these prompts after starting the demo with:

```bash
graphjin serve --demo --path examples/coffee-roastery
```

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

## Code-Aware Investigation

Search the `business_code` source for roast planning, subscription pressure, customer promise, and quality score logic. Use the code context before proposing workflow changes.
