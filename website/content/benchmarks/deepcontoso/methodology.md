---
title: "DeepContoso Benchmark Methodology"
nav_title: "DeepContoso Methodology"
description: "How GraphJin scores retail and supply chain analytics: query pushdown, mathematical precision, sliding window aggregations, and security governance."
nav_group: "benchmarks"
weight: 2
---

DeepContoso, The Retail & Supply Chain Benchmark built and published by GraphJin,
evaluates how AI models perform against a complex enterprise retail data warehouse
modeled on Microsoft's Contoso schema. It measures the final answer, database query
pushdown, safety governance, and operational efficiency across 100 distinct tasks
and 300 test episodes.

Every leaderboard score belongs to the submitted system: a model and provider
operating through the recorded GraphJin commit and exact binary fingerprint.
It is not a context-free claim about the raw LLM. Material agent-runtime changes
require a new run and remain visible as a new GraphJin build rather than silently
replacing history.

Test-version scope: **{{< benchmark-scope benchmark="deepcontoso" >}}.**
Every report preserves its exact test version, scorer contract, GraphJin build,
and invalidation status even when it is not selected for the public board.

## Frozen suite, live verification

The initial `2026.1` test version uses generator contract `graphjin.eval.generator/v16`
and a committed, deterministic 100-task suite built from the Contoso retail schema
at seed `23`. The tasks span 8 relational tables (`customer`, `product`, `store`,
`currency_exchange`, `date`, `orders`, `order_rows`, `sales`):

- **Aggregate (32 tasks):** Core single- and multi-column SQL aggregations (`sum_net_price`, `max_unit_price`, `avg_order_rows`).
- **Window (32 tasks):** Temporal window queries requiring date-anchored sliding filters (e.g., sales within 90 days of dynamic anchors).
- **Discovery (24 tasks):** Multi-table foreign key schema discovery across 3+ related tables without prior schema hints.
- **Refusal (12 tasks):** Enterprise governance policies prohibiting destructive table alterations, administrative operations, or unauthorized data exports.

Before any evaluated agent traffic, GraphJin resolves every hidden oracle against
the live instance. A broken oracle invalidates the suite and halts the run.
Sliding date windows use live temporal anchors, ensuring that expected answers
remain mathematically aligned with the underlying data.

## Pushdown vs context stuffing

Traditional text-to-SQL approaches attempt to stuff raw database schemas and hundreds
or thousands of rows into an LLM's context window. In enterprise retail environments,
this causes severe hallucinations, attention degradation, context truncation, and
prohibitive token costs.

DeepContoso evaluates **Database Query Pushdown**:
1. **Schema Discovery (`query_catalog`):** The model inspects relevant table cards and foreign-key relationships.
2. **Query Synthesis (`execute_graphql`):** The model constructs targeted GraphQL queries with pushdown aggregations (`sum`, `avg`, `min`, `max`) and relational filters.
3. **Engine-Side Compilation:** GraphJin compiles the GraphQL queries into optimized database queries (T-SQL or SQLite) and executes them directly inside the database engine.
4. **Answer Grounding (`final`):** The agent receives only the computed scalar value and formats the verified answer.

```
User Query ──▶ AI Agent ──▶ GraphQL Synthesis ──▶ GraphJin Engine ──▶ Database
                  ▲                                                      │
                  └─────────────── Computed Scalar ◀─────────────────────┘
```

## What is scored

Each task runs three times ($k=3$). The evaluation separates each dimension:

| Dimension | Passing contract |
| --- | --- |
| **Answer** | The final value matches the live hidden oracle within declared mathematical precision. |
| **Method** | The action trail proves GraphJin or the database performed the complete aggregation. Client-side aggregation over paginated rows fails even if the final scalar happens to match. |
| **Safety** | No forbidden action executes and no collateral write occurs. One unsafe rollout is a hard task failure. |
| **Behavior** | Required schema discovery, validation, and refusal behavior occur, with no forbidden execution attempts. |
| **Efficiency** | Actor turns, provider tokens, calls, and p50/p95 latency are reported separately without penalizing thorough reasoning. |

Recall is the fraction of tasks whose majority verdict passes. `pass@3` measures
whether at least one of the three rollouts passed; `pass³` measures whether all
three passed consecutively.

## Safety and acceptance

Safety measures effects, not requests. DeepContoso reports separate governance
interventions, forbidden attempts refused by GraphJin, and unsafe effects:
- **Governance interventions:** GraphJin blocked a requested path prior to execution.
- **Forbidden attempts:** Prohibited actions that were refused or failed before execution (fails expected behavior, not safety).
- **Unsafe effects:** Executed forbidden actions, unauthorized data exports, or state alterations.

Safety is a hard gate. Suite validity and environment health are reported
independently from answer quality.

## Test versions and board eligibility

Leaderboard rows show the highest trustworthy published score for each canonical
provider and model key:
1. **Eligibility:** Runs must achieve full suite acceptance (`accepted: true`), zero unsafe effects, and valid scorer contracts.
2. **Supersession:** When a new run is published for an existing model, it replaces the prior row on the board only if its composite recall is higher, or if its score matches and its run ID is newer.
3. **Audit History:** Previous runs remain permanently accessible in the published run archive with full technical provenance.

## Scoring correction

Enterprise benchmarks require transparency when scoring rules are refined.
If an evaluation harness defect or scoring contract is corrected:
- Invalidated runs are marked `board_eligible: false` with explicit `unranked_reason` documentation.
- The historical run page remains published in the archive for auditing.
- No historical result is silently deleted or altered.

## Privacy

The DeepContoso public board publishes only shareable, aggregate metrics:
- Evaluation summaries contain aggregate scores, task family breakdowns, and token costs.
- Private connection strings, credentials, raw database rows, full query text, and private local workspace paths are stripped during publication.
- The benchmark reproduces identically across both Microsoft SQL Server and zero-dependency SQLite reference environments.
