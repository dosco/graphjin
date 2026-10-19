import {
  and,
  app_user,
  db,
  desc,
  eq,
  isNull,
  organization,
  pack_install,
} from "@neko/db";
import { enqueue, QUEUE } from "@neko/db/jobs";
import {
  approveActionRequest,
  getActionRequest,
  listActionExecutions,
} from "@neko/llm/workflows";
import { readSecretsStore } from "@neko/secrets";

type Prepared = { id: string; status: string };
type Execution = Awaited<ReturnType<typeof listActionExecutions>>[number];

const workerUrl = "http://127.0.0.1:4100";
const marker = `openneko-v2-${Date.now().toString(36)}`;

async function jsonResponse<T>(response: Response): Promise<T> {
  const body = await response.json().catch(() => null) as (T & { error?: string }) | null;
  if (!response.ok) throw new Error(body?.error ?? `HTTP ${response.status}`);
  return body as T;
}

async function prepare(input: Record<string, unknown>): Promise<Prepared> {
  return jsonResponse<Prepared>(await fetch(`${workerUrl}/admin/action-requests/create`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(input),
    signal: AbortSignal.timeout(30_000),
  }));
}

async function expectPreparationRejected(input: Record<string, unknown>, pattern: RegExp) {
  const response = await fetch(`${workerUrl}/admin/action-requests/create`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(input),
    signal: AbortSignal.timeout(30_000),
  });
  const body = await response.json().catch(() => null) as { error?: string } | null;
  if (response.ok || !pattern.test(body?.error ?? "")) {
    throw new Error(`expected preparation rejection matching ${pattern}; got HTTP ${response.status}`);
  }
  return body?.error ?? "rejected";
}

async function executePrepared(input: {
  prepared: Prepared;
  orgId: string;
  adminUserId: string | null;
  timeoutMs?: number;
}): Promise<{ requestId: string; changesetId: string | null; execution: Execution }> {
  if (input.prepared.status !== "approved") {
    await approveActionRequest({
      id: input.prepared.id,
      orgId: input.orgId,
      approverUserId: input.adminUserId,
      approver: { userId: input.adminUserId, role: "admin" },
    });
  }
  await enqueue(QUEUE.ACTION_EXECUTE, {
    orgId: input.orgId,
    actionRequestId: input.prepared.id,
  });
  const deadline = Date.now() + (input.timeoutMs ?? 180_000);
  while (Date.now() < deadline) {
    const request = await getActionRequest(input.orgId, input.prepared.id);
    if (request?.status === "executed") {
      const [execution] = await listActionExecutions(request.id);
      if (!execution || execution.status !== "succeeded") {
        throw new Error(`action ${request.id} executed without a successful receipt`);
      }
      return {
        requestId: request.id,
        changesetId: typeof request.payload.changeset_id === "string"
          ? request.payload.changeset_id
          : null,
        execution,
      };
    }
    if (request?.status === "failed" || request?.status === "rejected") {
      const [execution] = await listActionExecutions(request.id);
      throw new Error(execution?.error ?? request.rejectionReason ?? `action ${request.id} failed`);
    }
    await new Promise((resolve) => setTimeout(resolve, 500));
  }
  throw new Error(`action ${input.prepared.id} did not finish before the acceptance timeout`);
}

function actionInput(input: {
  orgId: string;
  adminUserId: string | null;
  kind: string;
  scope?: "external" | "internal";
  payload: Record<string, unknown>;
  summary: string;
  status?: "approved" | "pending_approval";
}) {
  return {
    orgId: input.orgId,
    actorUserId: input.adminUserId,
    actorRole: "admin",
    actorBackend: "magento-v2-live-acceptance",
    scope: input.scope ?? "external",
    kind: input.kind,
    target: input.kind === "magento.financial_handoff"
      ? "magento_admin_handoff"
      : "magento_operator",
    payload: input.payload,
    riskLevel: "medium",
    status: input.status ?? "pending_approval",
    summary: input.summary,
    intent: input.summary,
  };
}

async function main() {
  const [org] = await db().select({ id: organization.id }).from(organization).limit(1);
  if (!org) throw new Error("OpenNeko organization is unavailable");
  const [admin] = await db()
    .select({ id: app_user.id })
    .from(app_user)
    .where(and(eq(app_user.role, "admin"), isNull(app_user.disabled_at)))
    .limit(1);
  // A just-installed local stack can legitimately have no app_user rows yet.
  // The approval model supports a null principal for trusted bootstrap/admin
  // automation while still enforcing the admin role in policy evaluation.
  const adminUserId = admin?.id ?? null;
  const [installation] = await db()
    .select({ config: pack_install.config })
    .from(pack_install)
    .where(and(
      eq(pack_install.org_id, org.id),
      eq(pack_install.pack_id, "magento"),
      eq(pack_install.status, "installed"),
    ))
    .orderBy(desc(pack_install.created_at))
    .limit(1);
  if (!installation) throw new Error("Magento pack is not installed");
  const secrets = await readSecretsStore();
  const token = secrets["pack.magento"]?.MAGENTO_INTEGRATION_TOKEN;
  if (!token) throw new Error("Magento Integration token is unavailable");
  const baseUrl = String(installation.config["magento.base_url"] ?? "").replace(/\/+$/, "");
  const storeCode = String(installation.config["magento.store_code"] ?? "all");

  async function magento<T>(path: string): Promise<T> {
    return jsonResponse<T>(await fetch(`${baseUrl}/rest/${encodeURIComponent(storeCode)}${path}`, {
      headers: { authorization: `Bearer ${token}` },
      signal: AbortSignal.timeout(20_000),
    }));
  }

  async function management(body?: Record<string, unknown>) {
    return jsonResponse<Record<string, unknown>>(await fetch(
      `${workerUrl}/admin/packs/magento/store-management`,
      body
        ? {
            method: "POST",
            headers: { "content-type": "application/json" },
            body: JSON.stringify({ ...body, actorUserId: adminUserId }),
          }
        : undefined,
    ));
  }

  async function undo(changesetId: string, suffix: string) {
    return executePrepared({
      prepared: await prepare(actionInput({
        orgId: org.id,
        adminUserId,
        kind: "magento.undo_changeset",
        payload: {
          changeset_id: changesetId,
          idempotency_key: `${marker}-undo-${suffix}`,
        },
        summary: `Acceptance inverse ${suffix}`,
      })),
      orgId: org.id,
      adminUserId,
    });
  }

  const productSearch = await magento<{ items?: Array<{ sku?: string; name?: string; price?: number }> }>(
    "/V1/products?searchCriteria[pageSize]=30",
  );
  const products = (productSearch.items ?? []).filter(
    (product): product is { sku: string; name: string; price: number } =>
      Boolean(product.sku && product.name && Number(product.price) > 0),
  );
  if (products.length < 3) throw new Error("Acceptance needs three priced Magento products");
  const blockSearch = await magento<{ items?: Array<{ id?: number; content?: string }> }>(
    "/V1/cmsBlock/search?searchCriteria[pageSize]=10",
  );
  const block = (blockSearch.items ?? []).find((item) => Number(item.id) > 0);
  if (!block?.id) throw new Error("Acceptance needs one Magento CMS block");

  const report: Record<string, unknown> = {
    profile: "magento-v2-local-acceptance",
    startedAt: new Date().toISOString(),
  };
  const cleanupChangesets: string[] = [];
  let originalAutoExecute = false;
  let autoRuleId: string | null = null;
  try {
    const initialManagement = await management() as {
      controls?: Array<{ domain?: string; autoExecute?: boolean }>;
    };
    originalAutoExecute = Boolean(
      initialManagement.controls?.find((control) => control.domain === "catalog")?.autoExecute,
    );
    await management({ action: "update_domain", domain: "catalog", enabled: true, autoExecute: true });
    const ruleName = `Local acceptance ${marker}`;
    const withRule = await management({
      action: "create_rule",
      name: ruleName,
      instruction: "Exercise one reversible product-name correction, then stop at one action per day.",
      domain: "catalog",
      actionKind: "magento.manage_catalog",
      dailyCap: 1,
      cooldownSeconds: 0,
      enabled: true,
      source: "acceptance_test",
    }) as { rules?: Array<{ id?: string; name?: string }> };
    autoRuleId = withRule.rules?.find((rule) => rule.name === ruleName)?.id ?? null;
    if (!autoRuleId) throw new Error("Acceptance automatic rule was not persisted");

    const autoProduct = products[0]!;
    const auto = await executePrepared({
      prepared: await prepare(actionInput({
        orgId: org.id,
        adminUserId,
        kind: "magento.manage_catalog",
        payload: {
          operation: "product_update",
          scope: { store: storeCode, activityOrigin: "acceptance_test" },
          auto_rule_id: autoRuleId,
          idempotency_key: `${marker}-auto-1`,
          rows: [{
            entity_ref: autoProduct.sku,
            path: { sku: autoProduct.sku },
            body: { product: { sku: autoProduct.sku, name: `${autoProduct.name} [${marker}]` } },
          }],
        },
        summary: "Acceptance capped automatic catalog change",
      })),
      orgId: org.id,
      adminUserId,
    });
    if (!auto.changesetId) throw new Error("Automatic action did not create a change-set");
    cleanupChangesets.push(auto.changesetId);
    await expectPreparationRejected(actionInput({
      orgId: org.id,
      adminUserId,
      kind: "magento.manage_catalog",
      payload: {
        operation: "product_update",
        scope: { store: storeCode, activityOrigin: "acceptance_test" },
        auto_rule_id: autoRuleId,
        idempotency_key: `${marker}-auto-2`,
        rows: [{
          entity_ref: products[1]!.sku,
          path: { sku: products[1]!.sku },
          body: { product: { sku: products[1]!.sku, name: `${products[1]!.name} [${marker}]` } },
        }],
      },
      summary: "Acceptance automatic daily-cap rejection",
    }), /daily cap/i);
    const autoUndo = await undo(auto.changesetId, "auto");
    cleanupChangesets.pop();
    report.automaticRule = {
      applied: auto.execution.result,
      capBreach: "rejected_before_execution",
      inverse: autoUndo.execution.result,
    };

    const bulkRows = products.slice(1, 3).map((product) => ({
      entity_ref: product.sku,
      path: { sku: product.sku },
      body: {
        product: {
          sku: product.sku,
          price: Math.round(product.price * 1.01 * 100) / 100,
        },
      },
    }));
    const bulk = await executePrepared({
      prepared: await prepare(actionInput({
        orgId: org.id,
        adminUserId,
        kind: "magento.manage_catalog",
        payload: {
          operation: "product_bulk_update",
          scope: { store: storeCode, activityOrigin: "acceptance_test" },
          idempotency_key: `${marker}-bulk-price`,
          rows: bulkRows,
        },
        summary: "Acceptance two-product async bulk price change",
      })),
      orgId: org.id,
      adminUserId,
      timeoutMs: 240_000,
    });
    if (!bulk.changesetId) throw new Error("Bulk action did not create a change-set");
    cleanupChangesets.push(bulk.changesetId);
    const bulkUndo = await undo(bulk.changesetId, "bulk");
    cleanupChangesets.pop();
    report.bulkCatalog = { applied: bulk.execution.result, inverse: bulkUndo.execution.result };

    const content = await executePrepared({
      prepared: await prepare(actionInput({
        orgId: org.id,
        adminUserId,
        kind: "magento.manage_content",
        payload: {
          operation: "cms_block_update",
          scope: { store: storeCode, activityOrigin: "acceptance_test" },
          idempotency_key: `${marker}-content`,
          rows: [{
            entity_ref: `cms-block:${block.id}`,
            path: { blockId: block.id },
            body: { block: { content: `${block.content ?? ""}\n<!-- ${marker} -->` } },
          }],
        },
        summary: "Acceptance CMS block versioned change",
      })),
      orgId: org.id,
      adminUserId,
    });
    if (!content.changesetId) throw new Error("Content action did not create a change-set");
    cleanupChangesets.push(content.changesetId);
    const contentUndo = await undo(content.changesetId, "content");
    cleanupChangesets.pop();
    report.content = { applied: content.execution.result, inverse: contentUndo.execution.result };

    await expectPreparationRejected(actionInput({
      orgId: org.id,
      adminUserId,
      kind: "magento.manage_promotions",
      payload: {
        operation: "sales_rule_create",
        scope: { website: "base" },
        projected_exposure: 1_000_000,
        idempotency_key: `${marker}-promo-cap`,
        rows: [{
          entity_ref: `${marker}-promo`,
          body: {
            rule: {
              name: marker,
              description: "Must be rejected before execution",
              from_date: "2026-08-28",
              to_date: "2026-08-30",
              is_active: false,
              discount_amount: 10,
            },
          },
        }],
      },
      summary: "Acceptance promotion exposure rejection",
    }), /projected exposure/i);
    report.promotionCap = "rejected_before_execution";

    await expectPreparationRejected(actionInput({
      orgId: org.id,
      adminUserId,
      kind: "magento.manage_orders",
      payload: {
        operation: "online_refund",
        scope: { store: storeCode },
        idempotency_key: `${marker}-class-zero`,
        rows: [{ entity_ref: "order:acceptance", body: { amount: 1 } }],
      },
      summary: "Acceptance prohibited-operation capability absence",
    }), /not allowed|not executable|cannot be performed|magento admin/i);
    report.handoffOnlyExecutePath = false;

    const handoff = await executePrepared({
      prepared: await prepare(actionInput({
        orgId: org.id,
        adminUserId,
        kind: "magento.financial_handoff",
        scope: "internal",
        status: "approved",
        payload: {
          handoff_kind: "online_refund",
          entity_ref: "order:acceptance",
          draft: { amount: 1, currency: "USD", testOnly: true },
          evidence: { reason: "local acceptance; no Magento execution" },
        },
        summary: "Acceptance Magento Admin handoff",
      })),
      orgId: org.id,
      adminUserId,
    });
    report.financialHandoff = handoff.execution.result;
  } finally {
    for (const [index, changesetId] of cleanupChangesets.reverse().entries()) {
      await undo(changesetId, `cleanup-${index}`).catch(() => undefined);
    }
    if (autoRuleId) {
      await management({ action: "set_rule_status", ruleId: autoRuleId, enabled: false }).catch(() => undefined);
    }
    await management({
      action: "update_domain",
      domain: "catalog",
      autoExecute: originalAutoExecute,
    }).catch(() => undefined);
  }
  report.completedAt = new Date().toISOString();
  report.ok = true;
  console.log(JSON.stringify(report, null, 2));
}

await main();
