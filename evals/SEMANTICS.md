# OpenNeko semantic coverage registry

> **INITIAL INVENTORY — 2026-08-10.** Audited against `FEATURES.md`, worker
> jobs, `packages/llm/src/work`, `packages/llm/src/workflows`, records,
> channels, plugin contracts, and the current database state machines on
> `origin/main` at `ef396e0`. This becomes machine-readable
> as `evals/semantics.yaml`.

This registry defines what “evaluate the OpenNeko harness” means. A model can
answer hundreds of database questions correctly while OpenNeko is still broken
at identity, memory, workflow delivery, approval, mutation, channel rendering,
or recovery. Those behaviors need stable names and explicit coverage.

## Coverage contract

Each machine-readable semantic will contain:

- stable `id`, description, lifecycle status, and owner paths;
- required eval layer: unit/contract, deterministic E2E, live-model E2E,
  stateful, adversarial, load, or platform smoke;
- dataset/source capabilities and actor/security profile;
- case IDs and oracle kinds that prove the outcome;
- required telemetry observations and product summary fields;
- gate severity: hard safety, hard contract, quality ratchet, or advisory;
- exclusions with a named alternative verification suite.

The registry is checked against code-owned inventories. Adding a worker job,
agent/control-plane tool, `AgentEvent`, fence, workflow/action status, records
operation, plugin capability, or public feature requires either a semantic ID
or an explicit mapping to an existing one.

Tool use is evidence, not usually the outcome. Cases assert the resulting data,
state transition, delivery, denial, audit record, or user-visible contract.

## 1. Harness runtime and backend semantics

| ID | Semantic outcome | Primary verification |
|---|---|---|
| `RUNTIME-INGRESS` | Web, channel, job, and workflow ingress create the correct org/user/service principal, thread, and run | E2E DB state + trace |
| `RUNTIME-AX-CONTRACT` | Ax implements the agent event, tool, sandbox, and provider contract | contract tests + live provider E2E |
| `RUNTIME-BACKEND-NATIVE` | Backend-native delegation, session resume, permission hooks, and MCP behavior work when advertised | capability-specific E2E |
| `RUNTIME-MODEL-RESOLUTION` | Requested provider/model/credential resolve to the actual backend configuration without secret exposure | contract + telemetry |
| `RUNTIME-STREAM` | Messages, tool lifecycle, status, cards, artifacts, and completion stream in causal order | event-sequence oracle |
| `RUNTIME-CANCEL` | Cancellation stops the active backend/sandbox and persists a cancelled terminal state | state + process oracle |
| `RUNTIME-TIMEOUT` | Turn and provider timeouts are classified accurately and terminate descendants | fault injection |
| `RUNTIME-RETRY` | Only retryable failures retry; attempts, usage, and final failure classification remain accurate | attempt journal oracle |
| `RUNTIME-RESUME` | Thread/backend session resume preserves intended context without re-injecting truncated turns | multi-turn differential |
| `RUNTIME-VALIDATE` | Invalid structured output is repaired within bounds or fails honestly | schema + attempt oracle |
| `RUNTIME-CONCURRENCY` | Global/backend caps, queueing, cancellation, and fairness hold under parallel work | load/soak |
| `RUNTIME-SHUTDOWN` | Process shutdown cancels agents and leaves jobs/runs recoverable | restart smoke |
| `RUNTIME-SANDBOX` | Every production agent runs in the required OpenShell boundary with declared mounts and capabilities | containment E2E |
| `RUNTIME-OBSERVABILITY` | Canonical observations, OTel spans/metrics, and run summaries agree and never alter outcomes | telemetry conformance |

## 2. Data discovery, access, and calculation semantics

| ID | Semantic outcome | Primary verification |
|---|---|---|
| `DATA-SOURCE-SELECT` | The correct enabled/default source is selected; disabled or ambiguous sources are handled explicitly | registry/state oracle |
| `DATA-SOURCE-ADMIN` | Register/preview/apply/enable/disable/default-source changes follow admin approval and preserve credentials | state + approval oracle |
| `DATA-SOURCE-FILE` | File-backed source ingestion, config generation, and refresh expose the intended data | dataset pack E2E |
| `DATA-SOURCE-OPENAPI` | Managed OpenAPI ingestion/configuration exposes allowed reads and explicitly opted-in API `call` mutations without overgranting | fake-server E2E |
| `DATA-DISCOVERY` | Catalog/schema discovery finds relevant tables, columns, relationships, and query patterns | answer + trace method |
| `DATA-KNOWLEDGE` | Knowledge-pack prefetch, freshness, fallback, and agentic catalog modes are correct | digest + fault injection |
| `DATA-DIRECT` | Direct GraphJin execution returns grounded read-only results through the brokered path | runtime oracle |
| `DATA-DELEGATED` | Delegated GraphJin agent results carry sufficient data/evidence/usage and obey read-only policy | outer+inner trace + oracle |
| `DATA-PLANNER-EXECUTE` | A delegated planner may propose, but the trusted host executes and verifies the final query | plan/query/oracle |
| `DATA-MULTISOURCE` | One answer can select and reconcile several registered sources without confusing identities or dates | cross-source oracle |
| `DATA-AUTH` | GraphJin tokens carry the correct org/user/role; row and source restrictions are enforced | cross-principal negative E2E |
| `DATA-QUERY-GUARD` | GraphJin applies source-aware policy: read-only targets reject writes while explicitly exposed API `call` mutations remain executable | adversarial contract + fake-server E2E |
| `DATA-SCHEMA-DRIFT` | Catalog refresh and repair recover from supported schema changes and fail honestly otherwise | drift fixture |
| `DATA-PAGINATION` | Answers do not treat a truncated page as a complete dataset | method + ground truth |
| `CALC-SCALAR` | Exact counts, sums, averages, distinct counts, ratios, and unit conversions match ground truth | numeric oracle |
| `CALC-FILTER` | Boolean, enum, null, text, and compound filters match ground truth | row/value oracle |
| `CALC-TIME` | Latest-data anchors, inclusive/exclusive windows, fiscal periods, timezones, and comparison periods are correct | value + interval oracle |
| `CALC-JOIN` | Header/detail and multi-hop joins avoid fan-out, omissions, and duplicate aggregation | value + method oracle |
| `CALC-RANK` | Top/bottom grouped rankings return the correct value and dimension with database-side grouping | value/dimension + method |
| `CALC-SERIES` | Ordered time series and chart grains contain correct points, gaps, and comparison values | ordered-series oracle |
| `CALC-BUSINESS` | Multi-step domain calculations preserve definitions, units, and assumptions | composite oracle |
| `CALC-EMPTY` | Empty, missing, null, and zero are distinguished without invention | structured oracle |
| `CALC-EVIDENCE` | Headline, structured data, explanation, source, freshness, and evidence agree | cross-field scorer |

## 3. Onboarding, metrics, briefing, and output semantics

| ID | Semantic outcome | Primary verification |
|---|---|---|
| `ONBOARD-PROFILE` | Business profiling is grounded in the connected source and produces a valid editable profile | fixture assertions + contract |
| `ONBOARD-RESEARCH` | Optional industry research runs only when configured and remains distinct from company facts | provider fake + provenance |
| `ONBOARD-BOOTSTRAP` | Bootstrap metrics fit selected seats, company data, role priorities, and required count/shape | deterministic + rubric |
| `METRIC-REFRESH` | Metric refresh computes/validates/persists the correct snapshot and status on success/failure | DB + ground-truth oracle |
| `BRIEFING-BUILD` | Briefing composition selects fresh, relevant metrics/findings with correct deep-dive context | DB + rendering contract |
| `OUTPUT-MESSAGE` | User-visible prose excludes hidden fences, raw tool calls, and backend protocol debris | exact content contract |
| `OUTPUT-SURFACE` | A2UI/card payloads validate and remain separate from prose | schema + event oracle |
| `OUTPUT-VITALS` | Headline vitals contain observed/calculated/estimated basis, freshness, and source when available | structured scorer |
| `OUTPUT-FOLLOWUPS` | Suggested follow-ups are valid, useful, and emitted once | event/contract scorer |
| `OUTPUT-ARTIFACT` | Generated artifacts are present, safely named, downloadable, and scoped to the run | file digest + auth oracle |
| `OUTPUT-NEEDS-INPUT` | Genuine ambiguity asks a bounded question without pretending completion | event-sequence scorer |
| `OUTPUT-VALUE` | Analysis/action time-saved estimates are parsed, clamped, attributed, and not double-counted | DB state oracle |
| `OUTPUT-CHANNEL-FIDELITY` | The same channel-neutral answer degrades correctly to web, Slack, Telegram, and voice capabilities | projection golden tests |

All current `AgentEvent` variants map here or to runtime/action semantics:
`message`, `tool_start`, `tool_delta`, `tool_end`, `surface`, `artifact`,
`status`, `usage`, `telemetry`, `error`, `capability_denied`, `done`, `output_emit`,
`action_request_emit`, `action_request_result`, `needs_input`, `followups`, and
`vitals`.

## 4. Work context, memory, personas, skills, and versioning

| ID | Semantic outcome | Primary verification |
|---|---|---|
| `WORK-THREAD` | Thread/run/message/event persistence, ordering, deletion, truncation, and replay are correct | DB state machine |
| `WORK-CONTEXT-BRIEFING` | A briefing deep dive carries the intended metric/finding context | prompt + answer oracle |
| `WORK-CONTEXT-APP` | App and record context limits tools/data to the selected app/record | auth + tool inventory |
| `WORK-MEMORY-SEARCH` | Semantic memory retrieves relevant allowed memories and reports misses honestly | seeded retrieval eval |
| `WORK-MEMORY-WRITE` | Agent/fence-driven memory writes validate scope, attribution, confidence, and dedupe | state oracle |
| `WORK-MEMORY-LAYERS` | Personal/team overlays, hide/correct/promote/adopt, fork/pull, and restore preserve lineage | versioned state oracle |
| `WORK-MEMORY-INTEGRITY` | Seal verification, expiry/TTL, and tamper handling prevent poisoned context | adversarial state test |
| `LIBRARY-DISTILL` | Uploaded documents triage, extract, and distill into OKF concepts on the uploader's personal layer — update-not-append, content-hash dedupe, forced retry, clean failure reasons | distiller fixture oracle |
| `LIBRARY-LAYERS` | Library concepts honor personal/team layering, share/approve/deprecate transitions, staleness sweep, and bundle export/import round-trip with trust state intact | versioned state oracle |
| `WORK-LIBRARY-SEARCH` | The agent retrieves the current identity-scoped library concept, rejects stale or adversarial decoys, and applies the retrieved procedure to its answer | planted retrieval + answer + trusted trace |
| `WORK-WORKFLOW-RETRIEVE` | The agent lists saved workflows, selects the intended definition rather than a similarly named decoy, and applies its exact goal or steps | planted workflow + answer + trusted trace |
| `WORK-PERSONA` | Operator role/profile changes prompt emphasis without weakening authorization | matched persona cases |
| `WORK-COMPACTION` | Long-thread compaction retains decisions, figures, actions, and open tasks without cross-thread leakage | multi-turn oracle |
| `WORK-SKILLS` | Skill discovery, dependency aggregation, install/use, and allowed-tool implications are correct | trace + artifact contract |
| `WORK-CONFIG-VCS` | Snapshot/history/restore/promote/adopt of config artifacts preserve semantic content and attribution | repository/DB oracle |

## 5. Workflow, observation, watcher, and action semantics

| ID | Semantic outcome | Primary verification |
|---|---|---|
| `FLOW-BUILD` | Natural-language workflow creation/update/delete produces a valid definition with provenance | DB + schema oracle |
| `FLOW-RULE-BUILD` | Approval-rule creation/update resolves scopes, risks, roles, and modes correctly | policy state oracle |
| `FLOW-CRON` | Cron trigger creation, due selection, and sweep scheduling fire at the intended times once | fake clock + state |
| `FLOW-SOURCE-CHANGE` | Matching source row changes trigger the correct workflow; non-matches do not | positive/negative event oracle |
| `FLOW-SUBSCRIPTION` | GraphJin subscriptions are created/reconciled/deleted and route matches idempotently | lifecycle oracle |
| `FLOW-EXTERNAL` | External-event filters, provenance, and dispatch select the correct workflows | event oracle |
| `FLOW-RUN` | Workflow turns use the runner tool policy, persist state, and produce outputs/actions correctly | state + trace oracle |
| `FLOW-CYCLE` | Chain depth, cycle detection, run budget, and fan-out caps stop runaway graphs | adversarial graph fixture |
| `FLOW-OUTPUT` | Output persistence, evidence, delivery hooks, and source-write attribution are correct | DB/event oracle |
| `FLOW-OUTPUT-DEDUPE` | Repeated findings collapse according to semantic identity and occurrence count | repeated-event oracle |
| `FLOW-OUTPUT-MUTE` | Muted scopes suppress presentation without deleting underlying evidence | DB + query oracle |
| `FLOW-OUTPUT-TTL` | Stale outputs expire without removing protected/pinned state incorrectly | fake clock + DB |
| `WATCH-BUILD` | Natural-language watcher creation yields a valid query, value path, operator, threshold, cadence, and severity | state + query oracle |
| `WATCH-CONDITION` | `gt/gte/lt/lte/eq/ne/changed` conditions handle types and prior values correctly | deterministic matrix |
| `WATCH-SWEEP` | Due scheduling, query execution, error handling, and last-value/check timestamps are correct | fake clock + DB |
| `WATCH-DEBOUNCE` | A tripped watch fires once within debounce and may fire again after the window | event sequence |
| `WATCH-DELIVERY` | A watch event reaches the linked workflow/output/action path with original evidence | causal trace + state |
| `WATCH-LIFECYCLE` | Create/enable/disable/update/delete/reset and cleanup do not leak subscriptions or events | stateful E2E |
| `ACTION-PROPOSE` | The harness creates the intended internal/external action with target, payload, risk, actor, and rationale | request state oracle |
| `ACTION-POLICY` | Policy precedence, condition match, auto-approve/pending/deny, and role requirements are correct | policy decision matrix |
| `ACTION-APPROVAL` | Approve/reject transitions enforce actor permissions and prevent invalid/repeated decisions | state-machine oracle |
| `ACTION-EXECUTE` | Approved actions execute through the correct adapter and persist success/failure receipts | fake adapter + DB |
| `ACTION-IDEMPOTENCY` | Retries, duplicate jobs, and repeated approvals cause at most one external effect | receipt/external fake |
| `ACTION-COLLATERAL` | The intended mutation occurs and unrelated records/resources remain unchanged | post-state diff |
| `ACTION-ADAPTER` | Code, webhook, and plugin adapters validate payloads, respect egress, and normalize outcomes | adapter contract/E2E |
| `ACTION-AUDIT` | Proposal, policy, approval, execution, rejection, and failure are attributable and tamper-evident | audit-chain oracle |

## 6. Channels, administration, and plugins

| ID | Semantic outcome | Primary verification |
|---|---|---|
| `CHANNEL-INBOUND` | Poll/webhook/socket ingestion normalizes messages and button actions once | adapter + dedupe oracle |
| `CHANNEL-WORKSPACE-ROUTE` | External workspace/conversation maps to the correct org and persistent channel thread | DB routing oracle |
| `CHANNEL-IDENTITY` | Sender linking by explicit approval or verified identity assigns the correct user/role; strangers stay restricted | cross-identity E2E |
| `CHANNEL-DELIVERY` | Output messages, cards/degradation, receipts, errors, and follow-ups deliver to the originating channel | fake channel transcript |
| `CHANNEL-BACKOFF` | Poll failures back off without duplicate delivery or log/run floods | fake clock/load |
| `ADMIN-USER` | Invite, role change, deactivate/reactivate, and authorization require the correct approval | control-plane state oracle |
| `ADMIN-SOURCE` | Source registry/config/role changes use secure forms for secrets and preserve preview/apply semantics | state + no-secret oracle |
| `ADMIN-CHANNEL` | Workspace/member/link administration is scoped, attributable, and approval-gated | state + policy oracle |
| `ADMIN-PLUGIN` | Plugin list/install/remove/update surfaces permissions and enforces install policy | registry + approval oracle |
| `PLUGIN-MANIFEST` | Manifest validation and declared capabilities/egress match runtime registration | contract test |
| `PLUGIN-SANDBOX` | Each plugin receives only declared mounts, credentials, and network destinations | containment E2E |
| `PLUGIN-HOT-RELOAD` | Install/remove/secret rotation reconciles the live registry without process restart | lifecycle E2E |
| `PLUGIN-ACTION` | Plugin actions become correctly described agent tools and execute only through action policy | tool + fake adapter |
| `PLUGIN-CONNECT` | Connector discovery, per-user OAuth/token refresh, and source sync remain user-scoped | fake OAuth + sync oracle |
| `PLUGIN-CHANNEL` | Channel capabilities register and drive inbound/outbound through the common adapter | plugin contract |
| `PLUGIN-AUTH` | Singleton auth capability, sign-in, group/role mapping, and session gating are correct | fake OIDC + DB |

## 7. Records-engine semantics

| ID | Semantic outcome | Primary verification |
|---|---|---|
| `RECORDS-REGISTRY` | App/object definitions, availability mirror, and catalog remain consistent | catalog/schema oracle |
| `RECORDS-SCHEMA-PLAN` | Schema proposals, previews, counterproposals, DDL saga, and audit are deterministic and recoverable | state-machine E2E |
| `RECORDS-READ` | List/detail/reference/aggregate queries honor filters, ordering, cursors, and projections | query/result oracle |
| `RECORDS-WRITE` | Create/update/delete/backfill validate fields/references and record change history | pre/post-state oracle |
| `RECORDS-RECYCLE` | Soft-deleted records are listed/restored/purged only with correct permission | state + auth oracle |
| `RECORDS-POLICY` | App/object/field/row permissions and actor synchronization match GraphJin enforcement | cross-role differential |
| `RECORDS-IMPORT` | CSV/artifact plan hash, sampling, batched execution, rejects, cancellation, resume, and receipt are correct | import fixture oracle |
| `RECORDS-CONNECT` | Salesforce export/sync/cursor/cutover handle deltas, retries, conflicts, and provenance | fake connector + DB |
| `RECORDS-IDENTITY` | Imported users reconcile/link/conflict/ignore and backfill ownership without scope escalation | identity state oracle |
| `RECORDS-WATCH` | Starter watches bind/reconcile and enqueue evaluation exactly once per source event | event/receipt oracle |
| `RECORDS-ACTION` | Records action execution obeys policy and produces the intended record mutation/receipt | action + state oracle |
| `RECORDS-OPS` | Health, lifecycle, system findings, backup verification, and ops watches report real conditions | fault-injected smoke |

## 8. Security and governance semantics

| ID | Semantic outcome | Primary verification |
|---|---|---|
| `SEC-PRINCIPAL` | Human/service identity and backend actor are snapshotted and propagated end to end | trace + audit oracle |
| `SEC-TENANT` | Org boundaries hold across data, records, threads, memory, files, actions, channels, and telemetry | cross-tenant adversarial |
| `SEC-AUTHZ` | Admin/member/service and source roles allow exactly their declared operations | capability matrix |
| `SEC-TOOL-ALLOWLIST` | Backend/operator-local tools outside the run catalog are unreachable | tool inventory + attack |
| `SEC-EGRESS` | Undeclared network access is denied and emitted as a structured capability denial | sandbox E2E |
| `SEC-SECRETS` | Keys stay outside sandboxes/model context and are scrubbed from output, logs, telemetry, and artifacts | canary secret scan |
| `SEC-PROMPT-INJECTION` | Untrusted data/tool content cannot override data, action, identity, or exfiltration policy | adversarial corpus |
| `SEC-APPROVAL` | Consequential changes cannot bypass proposal/policy/approval paths | negative side-effect oracle |
| `SEC-AUDIT-CHAIN` | Append-only chain sequence/hash verifies and exposes logging failure independently | tamper test |
| `SEC-BEHAVIOR` | Excessive action/memory/control-plane rates raise the expected alert without false cross-org attribution | fake clock + DB |
| `SEC-DEPLOYMENT-PROFILE` | Solo/team/org/hardened defaults change controls coherently while the always-on floor remains | profile matrix |
| `SEC-TELEMETRY` | Content mode, redaction, cardinality, tenant hashing, retention, and export controls are enforced | telemetry conformance |

## 9. Operability and recovery semantics

| ID | Semantic outcome | Primary verification |
|---|---|---|
| `OPS-JOB-LIFECYCLE` | Queue, running, retrying, succeeded, failed, and cancelled states are monotonic and attributable | job state oracle |
| `OPS-RECONCILE` | Lost worker/process state is reconciled without duplicate effects or permanently stuck runs | crash/restart E2E |
| `OPS-STARTUP` | Migrations, database, GraphJin, gateway, worker, web, and sandbox dependencies start in a valid order | packaged smoke |
| `OPS-PROVISION` | Provider, model, source, sandbox, and credential configuration reaches every required process consistently | deployment fixture |
| `OPS-HEALTH` | Status distinguishes healthy, degraded, transient, and terminal failures with actionable causes | fault-injected smoke |
| `OPS-DEMO` | Demo seed, simulator, and scenarios are reproducible and resettable | snapshot/digest smoke |
| `OPS-BACKUP` | Records backup and verification detect corruption/missing data and report safely | backup restore test |
| `OPS-UPGRADE` | Config/schema/plugin/runtime upgrades preserve supported state and fail safely | upgrade matrix |
| `OPS-RESOURCE` | Disk, memory, network, provider quota, and sandbox limits degrade predictably and remain observable | soak/fault injection |

Packaging, installer, release promotion, and deployment smoke remain platform
verification rather than live-model quality evals, but they retain semantic IDs
so whole-product coverage cannot silently omit them.

## 10. Required state-machine coverage

The machine registry will enumerate legal and illegal transitions for at least:

- work run and processing job;
- workflow run and workflow output;
- action request and action execution;
- watcher and watch-event delivery;
- source subscription and source-change receipt;
- channel identity link;
- plugin install/runtime state;
- records schema saga, import run, connector cursor/cutover, and recycle state;
- configuration change/promote/adopt/restore;
- eval run, attempt, episode, shard, and baseline promotion.

Every transition suite includes idempotent replay, duplicate delivery, crash
between write and acknowledgement, authorization failure, and audit/provenance
assertions where applicable.

The exact `AgentEvent`, worker queue, and observation-kind inventories are
mapped to semantic owners in `evals/semantic-inventory.yaml`. Repository CI
extracts those unions/constants from source and fails on any unmapped or stale
item, so a new event, job, or telemetry operation cannot silently escape this
registry.

## 11. Worker-job ownership check

All current worker jobs map to the registry:

| Worker jobs | Semantic owners |
|---|---|
| `work-run` | `RUNTIME-*`, `WORK-*`, `OUTPUT-*` |
| `business-profile-build`, `industry-insights-build`, `bootstrap-metrics-build`, `metric-refresh` | `ONBOARD-*`, `METRIC-REFRESH` |
| `workflow-run-fire`, `workflow-scheduler`, `workflow-output-ttl-sweep` | `FLOW-RUN`, `FLOW-CRON`, `FLOW-OUTPUT-TTL` |
| `action-execute` | `ACTION-EXECUTE`, `ACTION-IDEMPOTENCY`, `ACTION-AUDIT` |
| `records-watch-evaluate` | `RECORDS-WATCH`, `WATCH-*` |
| `records-import` | `RECORDS-IMPORT` |
| `records-salesforce-export`, `records-salesforce-sync`, `records-salesforce-sync-sweep`, `records-salesforce-cutover` | `RECORDS-CONNECT` |
| `records-identity-link` | `RECORDS-IDENTITY` |
| `library-distill` | `LIBRARY-DISTILL`, `LIBRARY-LAYERS` |
| `records-backup-verify`, `records-ops-health`, `records-ops-watch`, `records-ops-finding`, `records-lifecycle-finding`, `records-system-finding` | `RECORDS-OPS`, `OPS-BACKUP` |

## 12. Corpus growth rule

The first AdventureWorks pack starts with the 20 known questions, then grows in
two ways:

1. deterministic catalog-derived generation supplies broad read/calculation
   coverage with category and difficulty quotas;
2. every production failure, field report, security finding, watcher bug, or
   mutation bug becomes a minimal curated regression case under its semantic
   IDs.

Generated volume cannot substitute for semantic breadth. Suite validation
enforces minimum and maximum quotas per family so hundreds of simple aggregates
cannot dominate the headline or starve joins, rankings, time windows, negative
controls, watchers, mutations, identity, and safety cases.
