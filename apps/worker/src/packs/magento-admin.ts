import {
  action_changeset,
  action_changeset_row,
  and,
  db,
  desc,
  eq,
  inArray,
  magento_auto_rule,
  magento_financial_handoff,
  magento_store_control,
  pack_action_definition,
} from "@neko/db";
import { magentoExecutionMode, type MagentoRiskClass } from "@neko/packs";
import { buildMagentoActivity, isMagentoTestRule } from "./magento-activity.js";
import type { MagentoPreflightResult } from "./magento-preflight.js";
import { operatorReadinessDetail } from "./pack-artifacts.js";

export class MagentoPackAdminService {
  constructor(private readonly orgId: string) {}

  async read(): Promise<Record<string, unknown>> {
    const controls = await db()
      .select()
      .from(magento_store_control)
      .where(eq(magento_store_control.org_id, this.orgId))
      .orderBy(magento_store_control.domain);
    const rules = await db()
      .select()
      .from(magento_auto_rule)
      .where(eq(magento_auto_rule.org_id, this.orgId))
      .orderBy(desc(magento_auto_rule.updated_at));
    const changesets = await db()
      .select({
        id: action_changeset.id,
        domain: action_changeset.domain,
        operationId: action_changeset.operation_id,
        riskClass: action_changeset.risk_class,
        status: action_changeset.status,
        summary: action_changeset.summary,
        bulkUuid: action_changeset.bulk_uuid,
        projectedExposure: action_changeset.projected_exposure,
        inverseOfId: action_changeset.inverse_of_id,
        scope: action_changeset.scope,
        capSnapshot: action_changeset.cap_snapshot,
        createdAt: action_changeset.created_at,
        reconciledAt: action_changeset.reconciled_at,
      })
      .from(action_changeset)
      .where(eq(action_changeset.org_id, this.orgId))
      .orderBy(desc(action_changeset.created_at))
      .limit(20);
    const changesetRows = changesets.length === 0
      ? []
      : await db()
        .select({
          changesetId: action_changeset_row.changeset_id,
          entityRef: action_changeset_row.entity_ref,
          beforeImage: action_changeset_row.before_image,
          afterImage: action_changeset_row.after_image,
        })
        .from(action_changeset_row)
        .where(inArray(action_changeset_row.changeset_id, changesets.map((changeset) => changeset.id)));
    const handoffs = await db()
      .select({
        id: magento_financial_handoff.id,
        kind: magento_financial_handoff.kind,
        entityRef: magento_financial_handoff.entity_ref,
        status: magento_financial_handoff.status,
        draft: magento_financial_handoff.draft,
        evidence: magento_financial_handoff.evidence,
        createdAt: magento_financial_handoff.created_at,
        completedAt: magento_financial_handoff.completed_at,
      })
      .from(magento_financial_handoff)
      .where(eq(magento_financial_handoff.org_id, this.orgId))
      .orderBy(desc(magento_financial_handoff.created_at))
      .limit(20);
    const actionDefinitions = await db()
      .select({
        definition: pack_action_definition.definition,
        readiness: pack_action_definition.readiness,
        reason: pack_action_definition.readiness_reason,
      })
      .from(pack_action_definition)
      .where(
        and(
          eq(pack_action_definition.org_id, this.orgId),
          eq(pack_action_definition.enabled, true),
        ),
      );
    const readinessByDomain = new Map<string, { readiness: string; reason: string | null }>();
    for (const action of actionDefinitions) {
      const adapter = action.definition.adapter;
      if (!adapter || typeof adapter !== "object" || Array.isArray(adapter)) continue;
      if ((adapter as Record<string, unknown>).kind === "magento_financial_handoff") continue;
      const domain = String(action.definition.domain ?? "");
      const current = readinessByDomain.get(domain);
      if (!current || action.readiness === "blocked") {
        readinessByDomain.set(domain, { readiness: action.readiness, reason: action.reason });
      }
    }
    const operations = actionDefinitions.flatMap(({ definition }) => {
      const domain = String(definition.domain ?? "");
      const adapter = definition.adapter;
      if (!adapter || typeof adapter !== "object" || Array.isArray(adapter)) return [];
      const declared = (adapter as Record<string, unknown>).operations;
      if (!declared || typeof declared !== "object" || Array.isArray(declared)) return [];
      return Object.entries(declared as Record<string, unknown>).flatMap(([name, value]) => {
        if (!value || typeof value !== "object" || Array.isArray(value)) return [];
        const operation = value as Record<string, unknown>;
        return [{
          name,
          domain,
          operationId: String(operation.operationId ?? ""),
          executionMode: magentoExecutionMode(
            Number(operation.defaultClass) as MagentoRiskClass,
          ),
          reversible: Boolean(operation.reversible),
          resultMode: String(operation.resultMode ?? "sync"),
        }];
      });
    });
    const changesetsWithMode = changesets.map(({ riskClass, ...changeset }) => ({
      ...changeset,
      executionMode: magentoExecutionMode(riskClass as MagentoRiskClass),
    }));
    const rowsByChangeset = new Map<string, typeof changesetRows>();
    for (const row of changesetRows) {
      const rows = rowsByChangeset.get(row.changesetId) ?? [];
      rows.push(row);
      rowsByChangeset.set(row.changesetId, rows);
    }
    return {
      controls: controls.map((control) => {
        const domainReadiness = readinessByDomain.get(control.domain);
        return {
          domain: control.domain,
          automationEligible: control.risk_class === 2,
          enabled: control.enabled,
          autoExecute: control.auto_execute,
          caps: control.caps,
          scope: control.scope,
          readiness: domainReadiness?.readiness ?? "blocked",
          readinessReason: domainReadiness ? domainReadiness.reason : "change_access_unavailable",
          readinessMessage: domainReadiness
            ? operatorReadinessDetail(
              domainReadiness.reason as MagentoPreflightResult["operatorReadiness"],
            )
            : "View-only access could not be checked because the reporting connection is unavailable.",
          updatedAt: control.updated_at,
        };
      }),
      rules: rules.map((rule) => {
        const compiledPolicy = rule.compiled_policy;
        return {
          id: rule.id,
          name: rule.name,
          instruction: rule.instruction,
          domain: rule.domain,
          actionKind: rule.action_kind,
          compiledPolicy,
          dailyCap: rule.daily_cap,
          cooldownSeconds: rule.cooldown_seconds,
          enabled: rule.enabled,
          suspendedReason: rule.suspended_reason,
          lastFiredAt: rule.last_fired_at,
          isTest: isMagentoTestRule({ name: rule.name, compiledPolicy }),
        };
      }),
      changesets: changesetsWithMode,
      handoffs,
      activity: buildMagentoActivity({
        changesets: changesetsWithMode.map((changeset) => ({
          ...changeset,
          rows: rowsByChangeset.get(changeset.id) ?? [],
        })),
        handoffs,
      }),
      operations,
      handoffOnly: {
        executePath: false,
        handoffKinds: [
          "online_refund",
          "return_approval",
          "financial_configuration",
          "store_credit_over_cap",
        ],
      },
    };
  }

  async update(
    input: Record<string, unknown>,
  ): Promise<Record<string, unknown>> {
    const action = String(input.action ?? "");
    const actorUserId = typeof input.actorUserId === "string" ? input.actorUserId : null;
    if (action === "update_domain") {
      const domain = String(input.domain ?? "");
      if (!["catalog", "inventory", "orders", "promotions", "content", "customers"].includes(domain)) {
        throw new Error("unknown Magento change domain");
      }
      const [current] = await db()
        .select()
        .from(magento_store_control)
        .where(
          and(
            eq(magento_store_control.org_id, this.orgId),
            eq(magento_store_control.domain, domain),
          ),
        )
        .limit(1);
      if (!current) throw new Error(`Magento ${domain} control is not installed`);
      const set: Record<string, unknown> = {
        updated_by_user_id: actorUserId,
        updated_at: new Date(),
      };
      if (typeof input.enabled === "boolean") set.enabled = input.enabled;
      if (typeof input.autoExecute === "boolean") {
        if (input.autoExecute && current.risk_class !== 2) {
          throw new Error("Automatic execution is not available for this Magento domain");
        }
        set.auto_execute = input.autoExecute;
      }
      if (input.caps !== undefined) {
        if (!input.caps || typeof input.caps !== "object" || Array.isArray(input.caps)) {
          throw new Error("Magento caps must be an object");
        }
        const allowed = new Set([
          "maxRowsPerChangeset",
          "maxPriceDeltaPercent",
          "maxDiscountPercent",
          "maxCouponCount",
          "maxProjectedExposure",
          "maxDailyAutoActions",
          "maxStoreCredit",
          "minPromotionDays",
          "skuCooldownSeconds",
        ]);
        const caps = { ...(current.caps as Record<string, unknown>) };
        for (const [key, value] of Object.entries(input.caps as Record<string, unknown>)) {
          if (!allowed.has(key)) throw new Error(`unknown Magento cap ${key}`);
          const number = Number(value);
          if (!Number.isFinite(number) || number < 0) throw new Error(`Magento cap ${key} must be non-negative`);
          caps[key] = number;
        }
        set.caps = caps;
      }
      await db().update(magento_store_control).set(set).where(
        and(
          eq(magento_store_control.org_id, this.orgId),
          eq(magento_store_control.domain, domain),
        ),
      );
    } else if (action === "create_rule") {
      const name = typeof input.name === "string" ? input.name.trim() : "";
      const instruction = typeof input.instruction === "string" ? input.instruction.trim() : "";
      const domain = String(input.domain ?? "");
      const actionKind = typeof input.actionKind === "string" ? input.actionKind.trim() : "";
      const policySource = input.source === "acceptance_test"
        ? "acceptance_test"
        : "admin_plain_language";
      const dailyCap = Number(input.dailyCap);
      const cooldownSeconds = Number(input.cooldownSeconds ?? 0);
      if (!name || name.length > 120 || !instruction || instruction.length > 1000) {
        throw new Error("Magento automatic rule needs a concise name and instruction");
      }
      if (!Number.isInteger(dailyCap) || dailyCap < 1 || !Number.isInteger(cooldownSeconds) || cooldownSeconds < 0) {
        throw new Error("Magento automatic rule caps are invalid");
      }
      const [control] = await db().select().from(magento_store_control).where(and(
        eq(magento_store_control.org_id, this.orgId),
        eq(magento_store_control.domain, domain),
      )).limit(1);
      if (!control || !control.enabled || !control.auto_execute || control.risk_class !== 2) {
        throw new Error(`Magento ${domain} automatic execution is not enabled`);
      }
      const [definition] = await db().select({ definition: pack_action_definition.definition })
        .from(pack_action_definition)
        .where(and(
          eq(pack_action_definition.org_id, this.orgId),
          eq(pack_action_definition.kind, actionKind),
          eq(pack_action_definition.enabled, true),
        )).limit(1);
      if (!definition || String(definition.definition.domain ?? "") !== domain) {
        throw new Error("Magento automatic rule action does not belong to this domain");
      }
      const controlDailyCap = Number((control.caps as Record<string, unknown>).maxDailyAutoActions ?? 0);
      if (controlDailyCap > 0 && dailyCap > controlDailyCap) {
        throw new Error(`Rule daily cap exceeds the domain ceiling of ${controlDailyCap}`);
      }
      await db().insert(magento_auto_rule).values({
        org_id: this.orgId,
        name,
        instruction,
        domain,
        action_kind: actionKind,
        compiled_policy: {
          version: 1,
          source: policySource,
          condition: "watcher_finding",
          dailyCap,
          cooldownSeconds,
        },
        daily_cap: dailyCap,
        cooldown_seconds: cooldownSeconds,
        enabled: Boolean(input.enabled),
        created_by_user_id: actorUserId,
      }).onConflictDoUpdate({
        target: [magento_auto_rule.org_id, magento_auto_rule.name],
        set: {
          instruction,
          domain,
          action_kind: actionKind,
          compiled_policy: {
            version: 1,
            source: policySource,
            condition: "watcher_finding",
            dailyCap,
            cooldownSeconds,
          },
          daily_cap: dailyCap,
          cooldown_seconds: cooldownSeconds,
          enabled: Boolean(input.enabled),
          suspended_reason: null,
          updated_at: new Date(),
        },
      });
    } else if (action === "set_rule_status") {
      const ruleId = typeof input.ruleId === "string" ? input.ruleId : "";
      if (!ruleId || typeof input.enabled !== "boolean") {
        throw new Error("Magento rule status needs ruleId and enabled");
      }
      await db().update(magento_auto_rule).set({
        enabled: input.enabled,
        suspended_reason: input.enabled ? null : "suspended_by_admin",
        updated_at: new Date(),
      }).where(and(eq(magento_auto_rule.id, ruleId), eq(magento_auto_rule.org_id, this.orgId)));
    } else {
      throw new Error("unknown Magento store-management action");
    }
    return this.read();
  }
}
