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

Expected shape (batch codes embed the seed date, so the digits vary):

```json
{"data":{"customers":[{"id":1,"name":"Northstar Grocers"}],"roast_batches":[{"id":1001,"batch_code":"RB-2026-0709-001"}]}}
```

Run an agent workflow as an authenticated caller. In plain dev mode the
`X-User-*` development headers below are trusted; in **agentic mode** GraphJin
refuses header-trust identity and verifies HS256 JWTs instead
(`auth.jwt.secret: coffee-roastery-demo-jwt-secret` in `agentic.yml`) — the
smoke suite mints those tokens automatically, and you can mint one yourself:

```bash
# agentic mode: mint a demo JWT (requires jq + openssl)
b64() { openssl base64 -A | tr '+/' '-_' | tr -d '='; }
H=$(printf '{"alg":"HS256","typ":"JWT"}' | b64)
P=$(jq -nc '{sub:"101", roles:["user"], account_id:"1", exp:(now|floor)+3600}' | b64)
S=$(printf '%s.%s' "$H" "$P" | openssl dgst -sha256 -hmac coffee-roastery-demo-jwt-secret -binary | b64)
TOKEN="$H.$P.$S"

curl -sS http://localhost:8080/api/v1/graphql \
  -H 'content-type: application/json' \
  -H "Authorization: Bearer $TOKEN" \
  -H 'X-User-ID: 101' -H 'X-User-Role: user' -H 'X-Account-ID: 1' \
  --data '{"query":"mutation { gj_workflow_execution(insert: { workflow_name: \"daily_roast_plan\", variables: { orders: [], schedule: [], subscriptions: [] } }) { workflow_name status result_json error duration_ms } }"}'
```

(Sending both the bearer token and the dev headers makes the same request work
against either mode.)

## Command-Line Smoke Suite

Run the packaged smoke suite from a second terminal while the demo server is running:

```bash
examples/coffee-roastery/scripts/smoke.sh
```

The script checks the connected demo surface end-to-end:

- Direct GraphQL across `ops`, `roast_warehouse`, `business_code`, and `graphjin`.
- Saved-query REST endpoints for `daily_roast_context`, `batch_quality_snapshot`, and `customer_issue_context`.
- Workflow execution for `daily_roast_plan`, `batch_quality_review`, and `customer_issue_triage`.
- MCP discovery through `query_catalog` for saved queries, workflows, and CodeSQL context.

The live Ax agent checks are optional. Demo mode automatically loads `.env`
from the current working directory, then `examples/coffee-roastery/.env`, without
overriding existing environment variables. To enable the agent, copy the example
env file and add your provider key:

```bash
cp examples/coffee-roastery/.env.example examples/coffee-roastery/.env
# edit examples/coffee-roastery/.env and set a provider key
graphjin serve --demo --path examples/coffee-roastery
# or, from this checkout:
make demo
```

The checked-in `.env.example` sets `GO_ENV=agentic`, so the demo uses
`agentic.yml` after you copy it. A top-level `.env` works too when you run
`make demo` from this checkout. If demo mode finds `OPENAI_API_KEY`,
`GOOGLE_APIKEY`, or `ANTHROPIC_API_KEY`, it enables the GraphJin agent, selects
the matching provider key, and defaults the demo to agentic mode with
`GJ_AGENT_MAX_STEPS=10` and `GJ_AGENT_TIMEOUT_SECONDS=300`. Set `GO_ENV`,
`GJ_AGENT_ENABLED`, `GJ_AGENT_API_KEY_ENV`, `GJ_AGENT_PROVIDER`,
`GJ_AGENT_MODEL`, `GJ_AGENT_MAX_STEPS`, or `GJ_AGENT_TIMEOUT_SECONDS` yourself
to override those defaults. Shell environment variables still win over values
in `.env`. Plain dev mode keeps `auth.type: none` with trusted dev headers and
public system roots for local inspection; `agentic.yml` switches to JWT
identity (see above), gates `gj_config` to admins, locks the `runbook`
artifact kind, and enables MCP sampling (`agent.sampling: auto` +
`mcp.http_stateful`). You can also run the agentic config directly:

```bash
GO_ENV=agentic graphjin serve --demo --path examples/coffee-roastery
```

Then run:

```bash
examples/coffee-roastery/scripts/smoke.sh --agent
```

For stricter open-ended protocol checks, run:

```bash
examples/coffee-roastery/scripts/smoke.sh --agent-eval
```

The eval mode checks that the agent discovers catalog evidence, inspects saved-query details before execution, never answers from evidence-less raw GraphQL, returns machine-actionable refusals when blocked, creates watches through the watch_write skill, surfaces `watch_events_unseen` notices, and enforces role-gated control-plane access.

To exercise MCP sampling end to end (the agent borrowing the calling client's model), run the reference sampling client against the running demo:

```bash
go run ./tools/mcp-sampling-client \
  --url http://localhost:8080/api/v1/mcp \
  --jwt-secret coffee-roastery-demo-jwt-secret \
  --instruction "List the approved saved queries. Discovery only."
```

The smoke script defaults to `http://localhost:8080`. Use `--url` or `GRAPHJIN_URL` when running on another port.

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
