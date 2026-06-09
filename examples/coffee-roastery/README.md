# Coffee Roastery Agentic Demo

This demo models a coffee roasting business that wants agents to work across operational data, warehouse-style telemetry, internal business code, GraphJin system roots, and executable workflows.

## Setup

Requirements:

- Docker running locally.
- A built `graphjin` binary on your `PATH`, or Go installed so you can run the repo command below.
- Port `8080` available, unless you change `host_port` in `dev.yml`.

From an installed binary:

```bash
graphjin serve --demo --path examples/coffee-roastery
```

From this repository checkout:

```bash
GOCACHE=/tmp/go-build go run ./cmd serve --demo --path examples/coffee-roastery
```

The first run creates local state, applies DDL, runs seeds, starts GraphJin, and prints one status line per phase before the service banner. Later runs verify and reuse that state, so startup skips destructive setup and reseeding.

## Local State

Demo state is stored under `examples/coffee-roastery/demo/`. Delete that folder when you want a clean reset.

Generated runtime caches are also local and disposable:

- `demo/` stores the manifest, simulator state, schema cache, and managed Postgres data.
- `.graphjin/` stores non-demo local schema cache when GraphJin is run outside demo mode.
- `codesql/` stores the generated local CodeSQL index for the `business_code` source.

These folders are ignored by git.

## Schema And Seeds

Schema and seed layout:

- `schema-ddl/ops.ddl` initializes the writable Postgres `ops` source.
- `seed/ops.js` loads first-run data through `graphql(..., { source: "ops" })`.
- `schema-ddl/roast_warehouse.ddl` initializes the read-only BigQuery simulator.
- `seed/roast_warehouse.js` loads simulator fixture data through `seed.insert(..., { source: "roast_warehouse" })`. Live BigQuery DDL migration is not part of this demo.

DDL and seed scripts run only on first setup. If the demo state is reused, GraphJin verifies the manifest and source health checks, then skips DDL application and seeding.

## Sources

Sources:

- `ops`: writable Postgres operations data for customers, green coffee inventory, roast schedule, orders, subscriptions, and tickets.
- `roast_warehouse`: read-only BigQuery simulator backed by local DuckDB state for roast batches, sensor samples, QC scores, and machine telemetry.
- `business_code`: read-only CodeSQL index of internal planning and quality logic under `app/`.
- `graphjin`: catalog, metadata, runtime, and control-plane roots.
- `workflows`: read-only workflow scripts that agents can execute.

## Smoke Test

After the server is ready, query operational and warehouse data together:

```bash
curl -sS http://localhost:8080/api/v1/graphql \
  -H 'content-type: application/json' \
  --data '{"query":"query { customers(limit: 1) { id name } roast_batches(limit: 1) { id batch_code } }"}'
```

Expected shape:

```json
{"data":{"customers":[{"id":1,"name":"Northstar Grocers"}],"roast_batches":[{"id":1001,"batch_code":"RB-2026-0605-001"}]}}
```

Run an agent workflow with local development identity headers:

```bash
curl -sS http://localhost:8080/api/v1/graphql \
  -H 'content-type: application/json' \
  -H 'X-User-ID: 101' \
  -H 'X-User-Role: user' \
  -H 'X-Account-ID: 1' \
  --data '{"query":"mutation { gj_workflow_execution(insert: { workflow_name: \"daily_roast_plan\", variables: { orders: [], schedule: [], subscriptions: [] } }) { workflow_name status result_json error duration_ms } }"}'
```

## Repository Tests

Focused demo checks:

```bash
GOCACHE=/tmp/go-build go test -count=1 -run 'TestSeedCoreConfigFiltersToOpenSQLDatabases|TestCoffeeRoasteryBigQueryDDLAndSeedScript|TestCoffeeRoasteryDemoConfigNormalizes' ./cmd
```

BigQuery simulator package checks:

```bash
GOCACHE=/tmp/go-build go test -count=1 ./tests/hostedemu/bigquery
```

## Troubleshooting

- `permission denied while trying to connect to the Docker daemon socket`: start Docker and run the command in an environment with Docker socket access.
- Go build cache permission errors under `~/Library/Caches/go-build`: use `GOCACHE=/tmp/go-build`.
- Port `8080` already in use: change `host_port` in `dev.yml`.
- Invalid or stale demo state: delete `examples/coffee-roastery/demo/` and start again.
- Anonymous workflow execution is blocked by design. Use the local development identity headers shown above.
