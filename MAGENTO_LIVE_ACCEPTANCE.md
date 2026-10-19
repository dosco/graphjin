# Magento live acceptance — 2026-09-08

**Live exercise completed; the pack is not fully passing.** All
35 declared store-management operations were attempted against local Magento.
25 ultimately produced applied receipts, eight remained unreconciled, and two
promotion operations were rejected before execution. Google ADC initially blocked AI execution. This was resolved with the user
provided Gemini API key and `gemini-3.7-flash`; see the continuation below.

## Checklist

- [x] Fresh metadata/Records databases, customer and metadata GraphJin, worker,
  and web UI from this checkout. No runtime plugins installed.
- [x] Install first-party Magento 0.3.0 through the shared reviewed lifecycle.
- [x] Execute all 19 saved queries and generate all 13 metric snapshots.
- [x] Compare key aggregates independently with Magento SQL.
- [x] Exercise all six workflows and eight skills with real Gemini agent execution.
- [x] Evaluate all six watchers against live data and enqueue workflow jobs.
- [x] Verify a watcher-dispatched workflow completes with Gemini after the initial
  six dispatches failed on expired ADC.
- [x] Attempt all 35 declared catalog, inventory, order, promotion, CMS, and
  customer operations. Record failures rather than calling them passes.
- [x] Exercise undo, drift, approval/rejection, daily automation cap,
  idempotency, async bulk reconciliation, and four financial handoffs.
- [x] Exercise cooldown independently of the daily cap; it fails on SQL array binding.
- [x] Inspect live Admin Packs, review queue, and action receipt UI.
- [x] Configure, same-version upgrade, uninstall/reinstall, and restart.
- [x] Retain audit/history data and leave the new stack installed.
- [x] Remove temporary product, category, CMS page/block, and inactive rule.

## Stack and evidence

- UI: http://localhost:3101 ; worker: http://localhost:4100
- Private runtime/evidence directory: `/tmp/openneko-magento-accept`.
  This directory contains credentials and must not be published or committed.
- The final UI port is 3101 to avoid an existing, unrelated IPv4 static server
  on 3100. Earlier browser/API checks used the actual Next server on localhost
  IPv6 port 3100; the static server was left untouched.
- Environment: `source /tmp/openneko-magento-accept/env.sh`.
- Organization: `bfc817e7-b27a-4a45-a69d-cf4420dc10cd`.
- Docker: `openneko-magento-accept-db` (15432),
  `openneko-magento-accept-graphjin` (18080).
- Host GraphJin: metadata 18089, Records 18090, Records watch 18091.
  GraphJin is 3.20.47; worker/web are current source, version 2.39.1.
- Magento: existing local store on 8081; shared host address 192.168.0.100.
- Supplied analytics password failed live authentication. A dedicated
  `openneko_accept` account has SELECT-only access to `magento.*` and its
  generated password is stored privately and encrypted in this installation.
- Reused the existing valid `OpenNeko Magento Operator` integration token,
  recovered locally without printing it. No integration ACL was widened.
- The agent sandbox gateway of that release existed and the 2.39.1 agent image was pulled. Provisioning
  cannot finish until Google ADC is refreshed. All six watcher jobs reached
  the real queue. A follow-up live queue inspection confirmed that all six
  exhausted retries and are terminally failed; Google ADC refresh still fails.

## Live results

| Area | Applied and independently checked | Failed or incomplete |
| --- | --- | --- |
| Catalog | Product create/update/delete; actual async bulk update; base price set; special price set/delete; tier price set/delete; category create/update/delete; category product assign and PUT link update | Category move changes Magento but ends `reconcile_required` |
| Inventory | None | Save/delete forward read-only search filters to the GraphJin write operation, which rejects undeclared query parameters; source quantity remained unchanged |
| Orders | Private comment, hold, unhold, cancel | `update_order` returns HTTP 400; invoice and shipment are created but receipts remain unreconciled |
| Promotions | Update inactive rule | Create writes a rule but remains unreconciled; coupon generation and rule deletion are rejected for missing expiry, even though their declared payloads do not carry a rule expiry |
| Content | Both block and page create/update/delete; block update undo restores provider content | Initial create failed on missing pre-create ID; minimal runtime fix below resolves this |
| Customers | Provider confirms changed synthetic customer first name | OpenNeko still marks the changeset unreconciled |
| Financial handoffs | All four kinds return `ready_for_human` and `execute_path: false` | No money-out operations executed |

The category operation called `category_products_replace` was exercised as
Magento's declared PUT product-link endpoint with one link. This proves link
update, not replacement of the entire category's product list.

Pricing read-back confirmed base price 10.5, special price 9 followed by removal,
and a quantity-2 tier at 8 followed by removal. Bulk update reached terminal
success and the product name matched the requested value.

All 19 queries passed again after reinstall and database/GraphJin/worker restart.
All 13 metric refresh jobs completed. Independent Magento SQL for the same
window confirmed 9 orders, 2 cancellations, USD 1,111.51 invoiced, USD 284.44
refunded, and USD 827.07 net invoiced revenue. This is an independent check of
those aggregates, not a claim that every metric's calculation was audited.

All six watcher conditions were temporarily set to a deterministic triggering
threshold through the native watcher management functions. The real sweep
queried Magento and enqueued six real workflow jobs. Original thresholds and
disabled states were restored. No query or enqueue mocks were used.

## Controls and UI findings

- Default solo-mode approval has no human user ID. The API accepts approval,
  but administrator-required execution fails. For write acceptance, a named
  local administrator was provisioned and the harness used a locally signed
  session cookie accepted by the normal web authorization path. This verifies
  named approval, not end-to-end login/SSO or successful solo-mode approval.
- A content automation rule auto-approved a CMS update; the real worker applied
  it. The next request exhausted its daily cap and suspended the rule. The
  admin create endpoint does not enqueue auto-approved requests, so the harness
  explicitly queued the existing approved request using the same native queue
  used by the agent action server. No execution adapter was bypassed.
- Attempting promotion automation was rejected. A 99.9% price reduction was
  escalated to administrator approval and then explicitly rejected without
  changing the product price.
- Human rejection left CMS content unchanged. Two concurrent CMS previews
  verified that the stale one did not overwrite the first approved write.
- Duplicate idempotency keys were rejected before a second external write.
- A separate cooldown rule fails with PostgreSQL `malformed array literal` for
  the entity-reference array. This is a runtime failure, not a verified cooldown
  denial. Both acceptance rules are now disabled; content automation is off.
- **UI false success:** the drift-blocked request
  `88384b8d-1343-408e-8d02-0d2be3827e62` appears as `Fired` / `SUCCEEDED`,
  although its changeset is `reconcile_required`. The adapter returns without
  throwing for reconciliation-required outcomes, so action status alone is
  not a reliable success signal.
- The review queue labels the escalated price request `LOW RISK` while its
  preview correctly requires administrator approval.
- Pack activity correctly shows drift as needing attention, but rejected
  requests still appear awaiting approval there. Product deletion is described
  as a price update, and category deletion as a category update.
- Final doctor still reports bulk consumers blocked despite the successful
  live bulk execution. Bulk readiness detection needs separate investigation.
- Category move, rule date normalization, customer normalization, and
  fulfillment responses need operation-specific reconciliation. The generic
  deep comparison does not confirm these successful provider changes.
- Magento's actual PUT `/V1/orders/:parent_id` is an order-address save route;
  the pack uses it as a general order update. This explains the tested HTTP 400.

## Lifecycle, cleanup, and code

Configure and same-version 0.3.0 upgrade succeeded. Uninstall/reinstall retained
52 action requests, 44 executions, 42 changesets, one rule, and four handoffs.
Metric snapshots increased from 13 to 26 on reinstall. Uninstall removed the
pack's secret references, so reinstall re-supplied the same private credentials.
The final installed pack recovered after restarting its database, GraphJin,
and worker. This does not constitute a cross-version upgrade test.

Dedicated product/category and CMS records were deleted through pack actions.
The inactive sales rule required Magento-native cleanup because the pack's
rule-delete preflight is broken. Two synthetic offline orders remain for
inspection: `000000063` (cancelled) and `000000064` (invoiced and shipped), with
synthetic customer ID 2. No customer notifications or online payment capture
were requested. The capped acceptance automation rule remains disabled.

One uncommitted production change in `apps/worker/src/packs/magento-v2-runtime.ts`
skips the pre-write entity fetch for create operations, matching the existing
preparation behavior. Real CMS create then succeeded, action
`ff8c7cec-298a-4744-81cc-2d880a827642`. No other runtime fixes were added during
this acceptance pass. Four focused test files / 22 tests passed; worker
TypeScript checking passed. Unrelated `prototypes/` was untouched.

## Follow-up fixes

The exercise is complete, with runtime failures retained as evidence. Fix and
rerun the failed store operations, cooldown SQL binding, false-success UI,
bulk readiness detection, and AI truncation/restart reporting issues before
claiming the pack fully passes acceptance. Existing local fixture suites do
not substitute for these live checks.

## Gemini continuation

The user supplied a Gemini API key and selected `gemini-3.7-flash`. The native
provider settings API saved it encrypted and its real one-shot provider test
passed without OAuth. The old gateway referenced a deleted failed-worktree
network, so this stack now has an isolated gateway on 17671, private state, and
Docker network `openneko-magento-accept-runtime`. A diagnostic sandbox reached
Ready. The existing data source now has its GraphJin MCP endpoint configured.
All six workflow runs are being verified again through that real runtime.

Five Gemini workflows have now produced saved investigation outputs (platform
health, customer-service order summary, fulfillment, inventory, and refunds).
Daily briefing remains a failure: run `2e80ea0b-c66e-429c-be3a-f84ff4531b8e`
was marked completed but returned only a truncation message and no output;
run `6adf44c6-36c1-41bc-905c-23bc88f72cf1` exceeded its real 540-second
execution budget after 39 tools. This is not an authentication blocker.

The final focused daily briefing, `81d098f1-4ebf-42a8-bf2e-888b79588be4`,
completed and saved a full scorecard using the unchanged skill and metric
definitions. Earlier truncation/timeout attempts remain preserved as failures.
All eight skill names now have `skill_used` events with pack provenance.
Catalog run `6b1ab939-ac0e-457e-8183-c104229ea1fe` completed a live catalog
review; promotion run `594e5f3e-0118-41c5-8cb4-31829b5210dc` completed a live
context and exposure assessment without creating an action. The AI checks
created no external action requests. Platform-health output was reviewed in
the browser at `/runs/a5db2686-c4ea-49b3-80ea-2b1b19a5e174`.

A worker restart during the final daily run briefly marked that still-running
web-owned run cancelled (`worker stopped`) before the live executor finished
and restored its final completed status. Record this as a restart/status race;
the final output is present, but the transient status was misleading.

The final background path passed: watcher run
`54e92250-50bd-48cc-8dde-e7062f080f28` is completed, has `triggerKind: watcher`,
records `gemini-3.7-flash`, and saved a Magento cron/indexer health report. This
proves one full native watcher-to-queue-to-worker-to-agent-to-output path. All
six watcher query/condition/dispatch paths were exercised earlier; the other
five were not dispatched again after the model change. Watchers are restored
to their original disabled state and original thresholds.

Final state: web 3101 and worker 4100 healthy; Gemini API-key authentication
verified; isolated gateway 17671 running; all six domains enabled with automatic
execution off; both acceptance rules disabled. No additional production code
changes were made in the Gemini continuation, and nothing was committed.

## Inventory read/write contract fix — subsequent verification

The shared Magento mutation builder no longer accepts or forwards row read
queries. Inventory before-image, drift, reconciliation, and undo reads derive
an exact `(sku, source_code)` lookup from the single source item in the write
body. Invalid identities, unrelated records, and ambiguous collections fail
closed; an empty collection means the assignment is absent.

Verification on the running stack after restarting the worker:

- `source_items_save`: applied; custom source quantity changed 30 → 31.
- Save undo: applied; quantity restored to 30.
- `source_items_delete`: applied; custom source assignment absent.
- Delete undo: applied; assignment restored at quantity 30.
- All four operations independently checked through Magento REST. The same
  SKU's default source remained at quantity 20 throughout. The action rows
  deliberately carried a wrong-SKU legacy read filter to prove it could neither
  select the before-image nor leak into the mutation.
- Focused runtime regression suite: 15 tests passed; worker typecheck passed.
- Private evidence: `/tmp/openneko-magento-accept/inventory-fix/`.
- Fixture product deleted through OpenNeko after verification. The dedicated
  acceptance source is retained disabled (Magento has no source-delete service).

These results supersede the two inventory failures in the original run;
other original operation failures have not been rerun or resolved by this fix.

## Order update endpoint fix — subsequent verification

Corrected `magentoUpdateOrder` to POST `/V1/orders`, Magento's
`OrderRepositoryInterface::save` route. The order ID remains in `entity.entity_id`;
`path.id` selects the before-image and reconciliation read, and is not forwarded
as a write path parameter. Both read and write boundaries reject a missing or
mismatched body ID. A missing order fails before save instead of becoming a create.

Live verification through OpenNeko on synthetic acceptance order 63:

- Missing and mismatched body IDs rejected before approval.
- Approved update changed `customer_firstname` from `Acceptance` to
  `AcceptanceUpdated`, with an applied receipt and independent Magento read-back.
- Approved undo restored `Acceptance`, also applied and independently verified.
- Order identity, state, status, total, items, and billing address remained unchanged.
- Runtime suite: 20 tests passed. Pack bundle suite: 19 tests passed.
  Worker typecheck and diff whitespace checks passed.
- Private receipts: `/tmp/openneko-magento-accept/order-fix/`.

The normal same-version upgrade was blocked by the prior user-modified daily
briefing workflow. That workflow was preserved. Only the corrected connector
spec was refreshed in the isolated GraphJin stack, preserving the configured
Magento base URL, and GraphJin was restarted. Thus endpoint execution is verified;
a full pack upgrade remains blocked by the unrelated workflow drift.

## Remaining requested fixes — subsequent verification

- Promotion expiry validation now distinguishes rule changes from coupon
  generation and rule deletion. Partial rule updates inherit the stored expiry;
  explicitly clearing it still fails. Coupon limits include Magento's actual
  `quantity` field (the original test used the invalid `qty` field).
- Category move reconciliation checks `parent_id` and the resulting position
  relative to the requested sibling. Sales-rule reconciliation maps Magento's
  numeric coupon type to its returned enum. The original promotion mismatch was
  `2` versus `SPECIFIC_COUPON`, not date formatting. Customer comparison excludes
  the server-managed `updated_at` field, while retaining requested business fields.
- Invoice and shipment reconciliation reads the document identified by Magento's
  response and verifies its order and requested item quantities. Request controls
  such as `notify` are not compared as fields of the order document.
- Cooldown queries bind each entity reference as a scalar SQL parameter.
- Explicit userless administrator decisions in the solo deployment profile are
  recorded as solo-operator approvals in the existing audit chain. Sensitive
  execution checks this evidence. Automatic approval and legacy userless calls
  do not acquire human authority; no SSO user ID is fabricated.

Live results after deploying the runtime changes:

- Promotion create, coupon generation (two independently counted coupons), and
  promotion deletion: applied and independently verified in Magento.
- Full customer payload update including its original timestamp: applied; name
  independently verified and restored.
- Category move: applied; parent and first-sibling position independently verified.
- Invoice and shipment on synthetic order 65: applied; created document IDs,
  order association, item quantities, and order fulfillment counters verified.
- Sensitive promotion, customer, invoice, and shipment actions approved through
  the web API without a named-user cookie: executed with applied receipts.
- Cooldown: correct rejection for both one and two bound entity references.
  Temporary automatic rule disabled and domain automatic execution restored off.
- Oversized coupon `quantity`: rejected by the cap before approval or execution.
- Focused runtime tests: 26 passed; promotion tests: 12 passed; approval tests
  against PostgreSQL: 3 passed. Worker typecheck passed.

Private evidence and runnable live scripts remain under
`/tmp/openneko-magento-accept/remaining-fixes/` and the adjacent `remaining-*.py`
files. Temporary product, category, promotion, and its coupons were removed.
Synthetic fulfilled order 65 and its invoice/shipment remain as audit evidence.
These reruns supersede the listed original failures; they do not rewrite the old
receipts or claim the unrelated UI and pack-upgrade drift issues are resolved.
