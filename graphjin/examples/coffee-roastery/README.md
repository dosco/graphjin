# Coffee Roastery Agentic Demo

This demo models a coffee roasting business that wants agents to work across operational data, warehouse-style telemetry, internal business code, GraphJin system roots, and executable workflows.

Start it with:

```bash
graphjin serve --demo --path examples/coffee-roastery
```

Demo state is stored under `examples/coffee-roastery/demo/`. The first run creates local database state and seeds it. Later runs verify and reuse that folder. Delete `demo/` when you want a clean reset.

Schema and seed layout:

- `schema-ddl/ops.ddl` initializes the writable Postgres `ops` source.
- `seed/ops.js` loads first-run data through `graphql(..., { source: "ops" })`.
- `schema/roast_warehouse.sql` initializes the read-only BigQuery simulator. Live BigQuery DDL migration is not part of this demo.

Sources:

- `ops`: writable Postgres operations data for customers, green coffee inventory, roast schedule, orders, subscriptions, and tickets.
- `roast_warehouse`: read-only BigQuery simulator backed by local DuckDB state for roast batches, sensor samples, QC scores, and machine telemetry.
- `business_code`: read-only CodeSQL index of internal planning and quality logic under `app/`.
- `graphjin`: catalog, metadata, runtime, and control-plane roots.
- `workflows`: read-only workflow scripts that agents can execute.
