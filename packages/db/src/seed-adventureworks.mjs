import pg from "pg";
import { buildPoolConfig } from "./connection.ts";

const DEFAULT_ORG_ID = "adventureworks";
const DEFAULT_ORG_NAME = "AdventureWorks Cycles";
const DEFAULT_COMPANY_NOTE =
  "We design and manufacture bicycles and accessories sold to specialty retailers and direct to consumers across North America, Europe, and Australia.";
const DEFAULT_FISCAL_YEAR_START_MONTH = 7;
const DEFAULT_ACTIVE_SEATS = ["CEO", "CFO", "COO"];
const DEFAULT_PRIORITIES = [
  "Defend wholesale margins",
  "Grow DTC in Europe",
];
const DEFAULT_GRAPHJIN_ROOT =
  process.env.ADVENTUREWORKS_GRAPHJIN_ROOT?.trim() || "http://openneko-serve:8080";
const GRAPHJIN_ROOT = DEFAULT_GRAPHJIN_ROOT.replace(/\/+$/, "");
const UPDATE_DATA_SOURCE = process.env.ADVENTUREWORKS_UPDATE_DATA_SOURCE === "1";
const RECONCILE_AUTH_MODE =
  process.env.ADVENTUREWORKS_RECONCILE_AUTH_MODE === "1";
const AUTH_MODE = process.env.ADVENTUREWORKS_AUTH_MODE === "jwt" ? "jwt" : "none";

function workflowBatchContract(columns, recordsField = "records") {
  return {
    apiBatch: {
      version: 1,
      compiled: true,
      compiler: "workflow",
      recordsField,
      columns,
    },
  };
}

const client = new pg.Client(buildPoolConfig());

await client.connect();

// Same advisory lock as getOrgId() and graphjin-secret-init: on a fresh
// database several processes can race to create the single org row, and
// nothing in the schema forbids a second one.
await client.query("begin");
await client.query(
  "select pg_advisory_xact_lock(hashtext('openneko.organization.bootstrap'))",
);
const existingOrg = await client.query(
  "select id, name from organization order by created_at asc, id asc limit 1",
);
let orgId = existingOrg.rows[0]?.id;
if (!orgId) {
  orgId = DEFAULT_ORG_ID;
  await client.query(
    "insert into organization (id, name) values ($1, $2) on conflict (id) do nothing",
    [orgId, DEFAULT_ORG_NAME],
  );
  console.log(`[seed-adventureworks] created organization ${orgId}`);
} else if (existingOrg.rows[0]?.name === "My Workspace") {
  await client.query(
    "update organization set name = $2, updated_at = now() where id = $1",
    [orgId, DEFAULT_ORG_NAME],
  );
  console.log(`[seed-adventureworks] renamed default organization to ${DEFAULT_ORG_NAME}`);
} else {
  console.log(`[seed-adventureworks] keeping existing organization ${orgId}`);
}
await client.query("commit");

const sourceRows = await client.query(
  `select id, label
     from data_source
    where org_id = $1
    order by (label = 'AdventureWorks') desc, created_at asc
    limit 1`,
  [orgId],
);
const graphjinUrls = [
  `${GRAPHJIN_ROOT}/api/v1/graphql`,
  `${GRAPHJIN_ROOT}/api/v1/graphql`,
  `${GRAPHJIN_ROOT}/api/v1/mcp`,
];
if (sourceRows.rowCount === 0) {
  await client.query(
    `insert into data_source (
       org_id, kind, graphql_url, subscription_url, mcp_url, label, auth_mode
     ) values ($1, 'graphjin', $2, $3, $4, 'AdventureWorks', $5)
     on conflict (org_id, name) do nothing`,
    [orgId, ...graphjinUrls, AUTH_MODE],
  );
  console.log(
    `[seed-adventureworks] inserted GraphJin data source (auth_mode=${AUTH_MODE})`,
  );
} else if (UPDATE_DATA_SOURCE) {
  await client.query(
    `update data_source
       set kind = 'graphjin',
           graphql_url = $2,
           subscription_url = $3,
           mcp_url = $4,
           label = 'AdventureWorks',
           auth_mode = $5,
           updated_at = now()
     where id = $1`,
    [sourceRows.rows[0].id, ...graphjinUrls, AUTH_MODE],
  );
  console.log(
    `[seed-adventureworks] updated GraphJin data source to ${GRAPHJIN_ROOT} (auth_mode=${AUTH_MODE})`,
  );
} else if (
  RECONCILE_AUTH_MODE &&
  sourceRows.rows[0]?.label === "AdventureWorks"
) {
  await client.query(
    `update data_source
        set auth_mode = $2,
            updated_at = now()
      where id = $1`,
    [sourceRows.rows[0].id, AUTH_MODE],
  );
  console.log(
    `[seed-adventureworks] reconciled GraphJin data source auth_mode=${AUTH_MODE}`,
  );
} else {
  console.log("[seed-adventureworks] data source already exists; leaving it unchanged");
}

const wizardRows = await client.query(
  "select org_id from onboarding_wizard where org_id = $1 limit 1",
  [orgId],
);
if (wizardRows.rowCount === 0) {
  await client.query(
    `insert into onboarding_wizard (
       org_id,
       company_note,
       fiscal_year_start_month,
       active_seats,
       priorities,
       step
     )
     values ($1, $2, $3, $4, $5, 'company')
     on conflict (org_id) do nothing`,
    [
      orgId,
      DEFAULT_COMPANY_NOTE,
      DEFAULT_FISCAL_YEAR_START_MONTH,
      DEFAULT_ACTIVE_SEATS,
      DEFAULT_PRIORITIES,
    ],
  );
  console.log("[seed-adventureworks] inserted onboarding defaults");
} else {
  console.log("[seed-adventureworks] onboarding defaults already exist; leaving them unchanged");
}

// ─── Bundled workflows + policy (L2 trial seed) ────────────────────────
// Pre-author a small set of cron workflows so the trial workspace
// produces real outputs against the AdventureWorks data within minutes
// of first run. Idempotent: only inserts when no workflows exist yet
// for the org, so a re-seed never clobbers operator-authored workflows.
// Guard on operator/seed workflows only: the worker upserts its own
// 'OpenNeko operations' system workflow on every boot, and counting it
// here made a partially-failed first seed permanently skip the trial set.
const trialWorkflows = [
  {
    name: "Daily Revenue Health Check",
    description: "Snapshot of yesterday's revenue vs trailing 7-day average. Lands on the Briefing each morning.",
    goal: "Once per day, query AdventureWorks sales data via the connected GraphJin source. Compute (a) yesterday's total revenue across all territories from sales.salesorderheader.subtotal, and (b) the average daily revenue over the prior 7 days. Emit one workflow_output with kind=finding, mood=good if yesterday is within 10% of the avg, mood=watch if yesterday is 10–25% below, mood=act if >25% below. The output title should name yesterday's revenue and the delta; the body should be 1–2 sentences explaining the comparison.",
    cron: "0 9 * * *",
    cron_timezone: "UTC",
    outputContract: workflowBatchContract([
      { name: "Date", path: "date" },
      { name: "Revenue", path: "revenue" },
      { name: "Trailing 7-day average", path: "trailing7DayAverage" },
      { name: "Delta percent", path: "deltaPercent" },
      { name: "Mood", path: "mood" },
    ]),
  },
  {
    name: "Revenue Drop Alert",
    description: "Hourly territory-level revenue drop check. Surfaces sharp declines.",
    goal: "Once per hour, query AdventureWorks via GraphJin: for each territoryid in sales.salesorderheader, compute the current trailing-hour revenue and compare it to the same hour-of-week averaged over the prior 4 weeks. Emit a workflow_output with kind=finding, mood=act when any territory's current-hour revenue is below 50% of its baseline, scope=territory:<id>. Skip silently if no territory crosses the threshold. When mood=act, also emit a neko_action_request proposing a send_webhook notification to slack:#revenue-alerts with a one-line summary.",
    cron: "0 * * * *",
    cron_timezone: "UTC",
    outputContract: workflowBatchContract([
      { name: "Territory ID", path: "territoryId" },
      { name: "Current-hour revenue", path: "currentHourRevenue" },
      { name: "Baseline revenue", path: "baselineRevenue" },
      { name: "Baseline ratio", path: "baselineRatio" },
      { name: "Alert", path: "alert", default: false },
    ]),
  },
  {
    name: "Slow-Ship Operations",
    description: "Daily check for orders stuck in pending status past SLA.",
    goal: "Once per day, query AdventureWorks via GraphJin to find rows in sales.salesorderheader where status=1 (pending) and orderdate is more than 5 days ago. Emit one workflow_output with kind=finding, mood=watch when the count is between 1 and 10, mood=act when >10. Title: the count of slow-shipping orders. Body: a short list of the oldest 3 orderids with their orderdate.",
    cron: "30 8 * * *",
    cron_timezone: "UTC",
    outputContract: workflowBatchContract([
      { name: "Order ID", path: "orderId" },
      { name: "Order date", path: "orderDate" },
      { name: "Days pending", path: "daysPending" },
      { name: "Status", path: "status" },
    ]),
  },
  {
    name: "Stock Reorder Watch",
    description: "Reacts to inventory rows that drop below the product's reorder point. Subscription-triggered (no cron); fires per low-stock row.",
    goal: "Triggered by a source_change subscription on production.productinventory when a row's quantity falls below the related product's reorderpoint. The trigger_payload includes table, primary_key={productid,locationid}, and snapshot. Query AdventureWorks via GraphJin to fetch the product name, current quantity, reorderpoint, and listprice. Emit one workflow_output with kind=recommendation, mood=act, scope='inventory', topic='reorder_point' summarizing the gap and recommending a reorder quantity. Do NOT mutate productinventory or productorders here — the responder reads only.",
    cron: null,
    cron_timezone: null,
    outputContract: workflowBatchContract([
      { name: "Product ID", path: "productId" },
      { name: "Location ID", path: "locationId" },
      { name: "Quantity", path: "quantity" },
      { name: "Reorder point", path: "reorderPoint" },
      { name: "Recommended reorder", path: "recommendedReorderQuantity" },
    ]),
  },
];
const wfRows = await client.query(
  "select id from workflow_definition where org_id = $1 and name <> 'OpenNeko operations' limit 1",
  [orgId],
);
if (wfRows.rowCount === 0) {
  for (const wf of trialWorkflows) {
    const hasCron = wf.cron !== null && wf.cron !== undefined;
    await client.query(
      `insert into workflow_definition (
         org_id, name, description, goal, cron, cron_timezone,
         cron_enabled, enabled, status, output_contract
       ) values ($1, $2, $3, $4, $5, $6, $7, true, 'active', $8::jsonb)
       on conflict (org_id, owner_user_id, name) do nothing`,
      [
        orgId,
        wf.name,
        wf.description,
        wf.goal,
        wf.cron,
        wf.cron_timezone ?? "UTC",
        hasCron,
        JSON.stringify(wf.outputContract),
      ],
    );
  }
  console.log(`[seed-adventureworks] inserted ${trialWorkflows.length} trial workflows`);
} else {
  console.log("[seed-adventureworks] workflows already exist; leaving them unchanged");
}

// Add the workflow-native batch contract to untouched bundled definitions
// created by an older seed. Match the exact seeded goal so an operator-edited
// workflow is never rewritten, preserve every other output contract key, and
// avoid touching updated_at (the scheduler uses it as part of its cursor).
let reconciledBatchContracts = 0;
for (const wf of trialWorkflows) {
  const reconciled = await client.query(
    `update workflow_definition
        set output_contract = jsonb_set(
          coalesce(output_contract, '{}'::jsonb),
          '{apiBatch}',
          $4::jsonb,
          true
        )
      where org_id = $1
        and name = $2
        and goal = $3
        and not (coalesce(output_contract, '{}'::jsonb) ? 'apiBatch')`,
    [orgId, wf.name, wf.goal, JSON.stringify(wf.outputContract.apiBatch)],
  );
  reconciledBatchContracts += reconciled.rowCount ?? 0;
}
console.log(
  `[seed-adventureworks] reconciled ${reconciledBatchContracts} workflow-native batch contracts`,
);

// Wired outside the workflow guard and self-guarded, so a first run that
// died between the workflow insert and this point heals on the next run
// instead of skipping forever.
const stockWatchRows = await client.query(
  "select id from workflow_definition where org_id = $1 and name = 'Stock Reorder Watch' limit 1",
  [orgId],
);
const stockWatchWorkflowId = stockWatchRows.rows[0]?.id;
if (stockWatchWorkflowId) {
  const existingSub = await client.query(
    "select id from subscription where org_id = $1 and workflow_id = $2 and source_kind = 'source_change' limit 1",
    [orgId, stockWatchWorkflowId],
  );
  if (existingSub.rowCount === 0) {
    await client.query(
      `insert into subscription (
         org_id, workflow_id, source_kind, filter, enabled,
         max_concurrent_runs, idempotency_key_template
       ) values ($1, $2, 'source_change', $3::jsonb, true, 5, $4)`,
      [
        orgId,
        stockWatchWorkflowId,
        JSON.stringify({
          table: "productinventory",
          where: { quantity: { lt: { col: "product.reorderpoint" } } },
          primary_key: ["productid", "locationid"],
          version_column: "modifieddate",
          select: ["quantity"],
        }),
        "reorder-{primary_key}",
      ],
    );
    console.log("[seed-adventureworks] wired Stock Reorder Watch subscription");
  }
}

const policyRows = await client.query(
  "select id from action_policy where org_id = $1 and name = 'revenue_alerts_auto' limit 1",
  [orgId],
);
if (policyRows.rowCount === 0) {
  await client.query(
    `insert into action_policy (
       org_id, name, description,
       applies_to_kinds, applies_to_scopes,
       mode, risk_threshold_auto_approve,
       allowed_targets, limits, priority, enabled
     ) values ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9::jsonb, $10, true)`,
    [
      orgId,
      "revenue_alerts_auto",
      "Auto-approve revenue alert notifications to slack:#revenue-alerts at risk medium or below.",
      ["send_webhook"],
      ["external"],
      "auto_approve",
      "medium",
      JSON.stringify(["slack:#revenue-alerts"]),
      JSON.stringify({}),
      800,
    ],
  );
  console.log("[seed-adventureworks] inserted revenue_alerts_auto policy");
} else {
  console.log("[seed-adventureworks] revenue_alerts_auto policy already exists; leaving it unchanged");
}

await client.end();
