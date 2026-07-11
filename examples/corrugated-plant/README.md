# Corrugated Plant Demo

The corrugated plant demo exercises a JWT-authenticated, multi-source GraphJin
deployment for a packaging manufacturer.

Sources:

- `erp`: writable MySQL demo source for customers, roll stock, work orders,
  corrugator runs, converting jobs, shipments, downtime, and quality holds.
- `demand_warehouse`: read-only BigQuery simulator with demand, price, OEE,
  and scrap-rate facts.
- `plant_code`: Python CodeSQL source for costing, scheduling, and reorder
  policy snippets.
- `graphjin`: catalog, security, runtime, config, artifacts, and watches.
- `workflows`: JavaScript workflows for schedule, reorder, and downtime triage.

Run it from an installed binary:

```bash
graphjin serve --demo --path examples/corrugated-plant
```

Or from this repository checkout:

```bash
GO_ENV=agentic go run ./cmd serve --demo --path examples/corrugated-plant
```

Then smoke it:

```bash
examples/corrugated-plant/scripts/smoke.sh --url http://localhost:8081 --no-agent
```

Use `--agent` or `--agent-eval` when a provider key is available in `.env`.
The smoke suite mints an HS256 JWT with `roles: ["warehouse_manager"]` and
`account_id: "1"`; admin checks mint the same token with `roles: ["admin"]`.
