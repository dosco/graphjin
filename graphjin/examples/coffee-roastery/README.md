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
- Declared-task creation, ownership, journals, linked watches, next guidance,
  immediate verification success/failure, and delayed-verification cancellation.
- Catalog annotation draft, task provenance, account isolation, explicit
  approval, exact-detail framing, stale targets, moderation, and cleanup.
- Watch firing and inbox control: REST/MCP unseen reads, snooze hide/restore without marking seen, and acknowledgement.
- Per-watch MCP routing across two same-owner sessions plus a safe rollup over both source watch IDs.
- Deterministic inline AxFlow watch triage: preview/approval, notify/digest/discard dispositions, and a real one-minute digest drain using the local fixture (no provider traffic).
- A focused service batch for reconnect/backoff thresholds, absence fire/re-arm/downtime grace, digest coalescing/idempotence, snooze semantics, and rollup loop safety.

Use `--deep` to add the real one-minute task verification window and background
30-second sweep. The full demo launcher forwards it only to coffee-roastery:

```bash
make smoke-all SMOKE_ALL_ARGS=--deep
```

The smoke output uses stable capability IDs so a failed assertion names the
product contract it protects. The comments immediately above each suite state
the contract and its deeper Go coverage.

| Capability | Smoke function and mode | Main Go coverage | Runtime surfaces |
| --- | --- | --- | --- |
| `TASK-DECLARED` | `run_task_control_plane_suite`, default | `TestTaskControlPlaneLifecycleScopeAndTrail`; `TestTaskWatchLinkJournalsAndDeleteUnlinks` | GraphQL, MCP, watch store |
| `TASK-CONTINUITY` | `run_task_agent_eval_suite`, `--agent-eval` | `TestTaskAgentHTTPFlowWarmStartsAndJournals` | REST agent, task trail |
| `TASK-NEXT` | `run_task_control_plane_suite`, default | `TestTaskCreationNextGuidance` | MCP `execute_graphql` |
| `TASK-VERIFY-NOW` | task control suite; deterministic `--agent` notices | `TestTaskImmediateVerificationPassAndFail` | GraphQL, saved query, REST agent |
| `TASK-VERIFY-LATER` | task control suite; verifier suite with `--deep` | `TestTaskDelayedVerificationSweepAndReopenCancellation`; `TestTaskVerificationClaimIsSingleWinnerAcrossReplicas` | GraphQL, background worker |
| `ANNOTATION-DRAFT` | `run_annotation_suite`, default | `TestAnnotationTaskMustHaveSameOwner` | GraphQL, MCP, artifact store |
| `ANNOTATION-SCOPE` | `run_annotation_suite`, default | `TestAnnotationLifecycleScopeCatalogMergeAndDemotion` | JWT/dev identity, GraphQL, MCP |
| `ANNOTATION-DETAIL` | `run_annotation_suite`, default | `TestAnnotationStaleDetailDeploymentScopeAndServerFields` | raw catalog, MCP detail |
| `ANNOTATION-MODERATE` | `run_annotation_suite`, default | `TestAnnotationLifecycleScopeCatalogMergeAndDemotion` | admin identity, runtime events |
| `ANNOTATION-SEMANTIC` | `run_annotation_semantic_smoke`, semantic fixture | `TestApprovedAnnotationsAreSeparateAccountFilteredSemanticDocuments`; `TestAnnotationApprovalEmbedsOnlyTheNewDocument` | embeddings, generation rebuild, MCP |
| `ANNOTATION-NOTICE` | `run_agent_state_notice_smoke`, semantic `--agent` | `TestAnnotationLifecycleScopeCatalogMergeAndDemotion` | REST agent notices |
| `ANNOTATION-GUARD` | `run_annotation_agent_guard_smoke`, semantic `--agent` | `TestRunRequiresLaterUserConfirmationForAnnotationApproval` | fixture model, agent protocol, GraphQL |

Check the comments, labels, and matrix together without starting the demo:

```bash
examples/coffee-roastery/scripts/check-capability-smokes.sh
```

Caps, migration permutations, malformed inputs, multi-replica claim races, and
stuck-claim recovery intentionally remain integration-only: their deterministic
Go tests provide stronger proof than duplicating those internals in Bash.

### Semantic discovery comparison

Run the dedicated discovery smoke test from the repository root to compare the
same coffee catalog and MCP queries with semantic search disabled and enabled:

```bash
examples/coffee-roastery/scripts/semantic-smoke.sh
```

Add `--agent` to run the deterministic end-to-end REST agent path as well. It
uses the same local fixture for OpenAI-compatible chat responses, so no model
key is required:

```bash
examples/coffee-roastery/scripts/semantic-smoke.sh --agent
```

This command manages its own demo processes on port `18080`, uses an isolated
temporary discovery cache, and starts a deterministic OpenAI-compatible
embedding fixture on port `18081`. It verifies that:

- Business terms such as `clients`, `purchases`, `raw coffee inventory`, and
  `quality failures from recent roasting` discover the physical tables
  `customers`, `production_orders`, `green_lots`, and `qc_cupping_scores`
  better than lexical search alone.
- `clients and purchases` returns both endpoint tables plus a real foreign-key
  relationship path. Embeddings never invent the join.
- `employee payroll tax` does not inject low-confidence semantic candidates.
- Exact `production_orders` lookup remains top-one and skips query embedding.
- A cold semantic build uses bounded Ax batches and the next warm startup makes
  zero embedding calls.
- Approved annotation vocabulary lifts only its account-visible target, embeds
  one new document, and disappears without re-embedding after demotion.
- `explain: true` identifies semantic recall and deterministic relationship
  path results.
- The service-owned agent's private adaptive coverage path preserves
  per-phrase provenance and returns only real catalog relationship paths.
  `--agent` drives the actual REST agent and Ax/Goja runtime: it makes one
  three-phrase coverage call, reuses cached query vectors, embeds all misses in
  one Ax request, follows the returned `next.args.ids` endpoint/path handoff,
  and inspects those card ids before answering.
- The deterministic agent also surfaces failed-task and unshared-annotation
  notices and refuses annotation creation plus approval in the same run.

The deterministic fixture proves GraphJin's integration and ranking behavior;
it is not a benchmark of a production embedding model. To measure the same
acceptance cases with a real provider, run:

```bash
OPENAI_API_KEY=... \
  GRAPHJIN_SEMANTIC_PROVIDER=openai \
  GRAPHJIN_SEMANTIC_MODEL=text-embedding-3-small \
  examples/coffee-roastery/scripts/semantic-smoke.sh --live
```

Use `GRAPHJIN_SEMANTIC_BASE_URL` for an OpenAI-compatible endpoint and
`GRAPHJIN_SEMANTIC_API_KEY_ENV` when the provider key uses another environment
variable name. Pass `--report path/to/report.json` to retain the rank comparison
as JSON. The test builds the current checkout unless `--graphjin-bin` points to
an existing binary.

The scripted `--agent` run verifies GraphJin's orchestration and guards, not a
model's judgment. Use the separately configured live agent suite below when you
want to evaluate how a production model decides whether and how to expand.

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
`GOOGLE_APIKEY`, or `ANTHROPIC_API_KEY`, it selects the matching provider key
and defaults the demo to agentic mode with
`GJ_AGENT_MAX_STEPS=10` and `GJ_AGENT_TIMEOUT_SECONDS=300`. Set `GO_ENV`,
`GJ_AGENT_API_KEY_ENV`, `GJ_AGENT_PROVIDER`,
`GJ_AGENT_MODEL`, `GJ_AGENT_MAX_STEPS`, or `GJ_AGENT_TIMEOUT_SECONDS` yourself
to override those defaults. Shell environment variables still win over values
in `.env`. Plain dev mode keeps `auth.type: none` with trusted dev headers and
public system roots for local inspection; `agentic.yml` switches to JWT
identity (see above), gates `gj_config` to admins, locks the `runbook`
artifact kind, and inherits the batteries-included agentic runtime defaults.
You can also run the agentic config directly:

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

The eval mode checks that the agent discovers catalog evidence, inspects saved-query and workflow details before execution, never answers from evidence-less raw GraphQL, returns machine-actionable refusals when blocked, actually creates watches, surfaces `watch_events_unseen` notices, and enforces role-gated control-plane access. Skill telemetry is diagnostic and is not used as a smoke-test authorization oracle.

The provider-free 60-case skill corpus runs in normal Go tests:

```bash
cd agent
go test -run 'TestSkillEvaluationCorpus|TestAllowedSkillsCapabilityMatrix' ./...
```

Provider comparison is explicitly opt-in. Configure exact server-derived
capability profiles using `agent/testdata/skill_eval_profiles.example.json`,
run the 24 representative cases three times against the old server for a
baseline, then against the candidate using the same model:

```bash
cd agent
go run ./cmd/skill-eval -live -phase baseline \
  -profiles testdata/skill_eval_profiles.json \
  -model "$MODEL" -ax-version "$BASELINE_AX_VERSION" \
  -graphjin-commit "$BASELINE_COMMIT" \
  -prompt-registry "$BASELINE_CHECKOUT/agent/skills.go" \
  -report baseline-skills.json

go run ./cmd/skill-eval -live -phase candidate \
  -profiles testdata/skill_eval_profiles.json \
  -model "$MODEL" -graphjin-commit "$CANDIDATE_COMMIT" \
  -baseline baseline-skills.json \
  -report candidate-skills.json
```

The report includes tokens, GraphJin tool calls, actor turns, used skills,
outcomes, p50/p95 latency, Ax version, commit, and the prompt-registry hash.
Ax provider clients default to temperature zero. Candidate hard gates cover
behavioral recall, used-skill recall/precision, safety, prompt-token growth,
actor turns, and absence of skill discovery; latency regressions are warnings.

To verify automatic model routing, run the reference sampling-capable client
against the running demo. With provider credentials it must use the server
model and report zero sampling calls; without server credentials it borrows the
calling client's model:

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
