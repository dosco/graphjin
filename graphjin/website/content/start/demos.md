---
title: "Demos"
description: "Five runnable demo verticals - real domains, seeded data, saved queries, workflows, and the built-in agent - one command each."
nav_group: "start"
doc_kind: "guide"
weight: 25
---

The fastest start needs nothing but the binary: `graphjin serve --demo` with no `--path` extracts the built-in SaaS ops demo to `./graphjin-demo` and boots it on SQLite - no clone, no Docker, ready in seconds.

```bash
graphjin serve --demo
```

The other verticals live in the repo. Clone it and point `--demo` at one with `--path`:

```bash
git clone https://github.com/dosco/graphjin && cd graphjin
graphjin serve --demo --path examples/<name>
```

Each demo is a self-contained vertical: one command boots its databases (Docker containers and in-process emulators), applies the schema, seeds realistic data, and starts the server. First boot creates state under the project's `demo/` folder - `./graphjin-demo/demo/` for the built-in demo, `examples/<name>/demo/` for the rest. Delete that folder to reset from scratch. Seed dates anchor to today and shift forward on reuse, so questions like "what's scheduled today?" keep working no matter when you run them.

Put a model API key in `./.env` (`GOOGLE_APIKEY`, `OPENAI_API_KEY`, or `ANTHROPIC_API_KEY` - checked in that order) and `--demo` automatically switches into agentic mode and enables the [built-in agent](/agentic/server-agent/). The web console is at `http://localhost:<port>/`, and the agent chat - which streams every tool call live as the agent works - is at `/agent`.

## Pick a demo {#pick-a-demo}

| Demo | Domain | Sources | Port | Docker | First boot |
| --- | --- | --- | --- | --- | --- |
| [Coffee roastery](#coffee-roastery) | Specialty coffee ops | Postgres · BigQuery-emu · TS CodeSQL | 8080 | Yes | ~1-2 min |
| [SaaS ops](#saas-ops) | SaaS company ops | SQLite | 8083 | **No** | Seconds |
| [Corrugated plant](#corrugated-plant) | Box manufacturing | MySQL · BigQuery-emu · Python CodeSQL | 8081 | Yes | ~2-4 min |
| [PCB fab](#pcb-fab) | PCB design + fabrication | 8 sources incl. MongoDB, files, OpenAPI | 8082 | Yes | ~3-5 min |
| [Webshop](#webshop) | E-commerce starter | Postgres | 8080 | Yes | ~1 min |

No Docker on your machine? Start with the SaaS ops demo - it runs entirely in-process on SQLite.

## Coffee roastery {#coffee-roastery}

The flagship agentic demo: a specialty coffee roasting business where agents work across operational data, warehouse telemetry, internal business code, and executable workflows.

```bash
graphjin serve --demo --path examples/coffee-roastery
```

- **Sources:** `ops` (writable Postgres - customers, green lots, roast schedule, production orders, subscriptions, tickets), `roast_warehouse` (read-only BigQuery simulator - roast batches, sensor samples, cupping scores), `business_code` (TypeScript CodeSQL index), workflows, and the GraphJin control plane.
- **Shows off:** the widest agentic surface - durable watches, the owner-scoped managed artifact store, automatic server-first model routing with MCP client fallback, JWT identity in agentic mode, and CodeSQL over real business logic. Four workflows, including `daily_roast_plan` and `batch_quality_review`.

**Ask it:**

- "Find today's queued production orders, active subscriptions, available green coffee lots, and planned roast schedule. Does the roast plan cover committed shipments?"
- "Compare the latest roast batches, cupping scores, and sensor samples. Identify any batch that should be held for review before release."
- "Create a watch that tells me when new production orders appear, then show my unseen watch events and mark them reviewed."

More prompts: [coffee-roastery/PROMPTS.md](https://github.com/dosco/graphjin/blob/master/examples/coffee-roastery/PROMPTS.md)

## SaaS ops {#saas-ops}

The fastest start, and the demo built into the binary: a SaaS company ops app - accounts, users, subscriptions, invoices, support tickets, usage events - on SQLite, entirely in-process. No Docker, no containers, boots in seconds. Bare `graphjin serve --demo` (no `--path`) runs exactly this demo, extracted to `./graphjin-demo`; from a repo clone you can also run it explicitly:

```bash
graphjin serve --demo                             # built-in, no clone needed
graphjin serve --demo --path examples/saas-ops    # from a repo clone
```

- **Sources:** `app` (SQLite), workflows, control plane. Port 8083.
- **Shows off:** zero-dependency onboarding, saved queries (`churn_risk_context`, `mrr_summary_context`, `ticket_sla_context`), workflows (`sla_breach_check`, `dunning_retry_check`), and the fail-closed automatic fallback from missing server credentials to MCP client sampling.

**Ask it:**

- "Which accounts are most at risk of churning? Use failed payments, renewal dates, and recent usage, and explain the ranking."
- "Summarize MRR by plan and flag any account with failed payments."
- "Which open support tickets are past or nearest their SLA - and who owns them?"

More prompts: [saas-ops/PROMPTS.md](https://github.com/dosco/graphjin/blob/master/examples/saas-ops/PROMPTS.md)

## Corrugated plant {#corrugated-plant}

A corrugated-box packaging plant: ERP work orders, corrugator runs, paper-roll inventory, downtime, and quality holds - behind real authentication.

```bash
graphjin serve --demo --path examples/corrugated-plant
```

- **Sources:** `erp` (writable MySQL), `demand_warehouse` (BigQuery simulator - demand history, OEE, scrap rates), `plant_code` (Python CodeSQL), workflows, control plane. Port 8081.
- **Shows off:** the auth/RBAC story - the API is JWT-authenticated in every environment, with roles (`warehouse_manager`, `sales`, `admin`) and a multi-tenant namespace claim. `scripts/smoke.sh` mints demo HS256 tokens you can reuse.

**Ask it:**

- "Which work orders should the corrugator prioritize today?"
- "Which paper rolls are below reorder point, and what should purchasing do?"
- "Review recent downtime and quality holds. Which line needs attention first?"

More prompts: [corrugated-plant/PROMPTS.md](https://github.com/dosco/graphjin/blob/master/examples/corrugated-plant/PROMPTS.md)

## PCB fab {#pcb-fab}

A compact PCB design-and-fabrication stack that exercises every GraphJin source kind in one graph - the widest fan-out of any demo.

```bash
graphjin serve --demo --path examples/pcb-fab
```

- **Sources (8):** `mes` (writable Postgres - designs, fab orders, panels, BOM, NCRs), `yield_warehouse` (Snowflake simulator), `test_measurements` (MongoDB), `design_files` (local Gerber/drill/stackup files), `supplier_api` (OpenAPI supplier lookup joined onto `bom_items.sku`), `dfm_code` (TypeScript CodeSQL), workflows, control plane. Port 8082 (the smoke suite also starts a supplier mock on 8093).
- **Shows off:** declared cross-source relationships - MongoDB `test_runs.order_id` joins straight to Postgres `fab_orders.id` - plus file and remote-API sources in the same governed graph.

**Ask it:**

- "Which fab order should we release next, and what evidence supports it?"
- "Review FO-260701-ALPHA for DFM, supplier, and yield risks before CAM release."
- "Which layer count is dragging yield this week?"

More prompts: [pcb-fab/PROMPTS.md](https://github.com/dosco/graphjin/blob/master/examples/pcb-fab/PROMPTS.md)

## Webshop {#webshop}

The classic GraphQL starter: a single-Postgres e-commerce shop - users, products, categories, purchases. No agent surface; this one is about the query engine itself.

```bash
graphjin serve --demo --path examples/webshop
```

- **Shows off:** RBAC roles, insert/update presets, a Stripe-style remote API join, a polymorphic relationship, and recursive self-joins (threaded comments). Port 8080.
- Start here if you want plain GraphQL over your database before any agent story.

## Smoke suites {#smoke-suites}

Every demo ships an end-to-end smoke suite: `scripts/smoke.sh` exercises data queries, saved queries, workflows, watches, and the artifact store; `--agent-eval` adds strict agent-protocol evals; `--model-resolution` covers automatic server-first routing and MCP client fallback. The refusal suite jailbreak-tests the guards in every vertical - a model told to skip discovery and mutate anyway must come back `status: "blocked"` with a structured refusal. Run everything with `make smoke-all`.

## Keep going {#keep-going}

- Hand the [built-in agent](/agentic/server-agent/) a goal and get a typed, evidence-backed answer.
- Point GraphJin at [your own database](/start/quick-start/).
- Read the [Agentic GraphJin deep dive](/story/agentic/).
