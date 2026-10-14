import {
  and,
  db,
  eq,
  records_watch_binding,
} from "@neko/db";
import type { RecordsWatchEvaluatePayload } from "@neko/db/jobs";
import type {
  AppRegistrySnapshot,
  RecordField,
  RecordObject,
  RecordsGraphjinTransport,
} from "@neko/records";
import { publishRecordsSystemFinding } from "./records-system-finding.js";

type Row = Record<string, unknown>;

function rows(value: unknown): Row[] {
  return Array.isArray(value)
    ? value.filter(
        (item): item is Row =>
          typeof item === "object" && item !== null && !Array.isArray(item),
      )
    : [];
}

function openStage(value: unknown): boolean {
  const normalized = stringValue(value)
    .trim()
    .toLocaleLowerCase("en-US")
    .replace(/[^a-z0-9]+/g, "_")
    .replace(/^_+|_+$/g, "");
  return normalized !== "closed_won" && normalized !== "closed_lost";
}

function stringValue(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function monthBounds(now: Date): { start: string; end: string; label: string } {
  const start = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), 1));
  const end = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth() + 1, 1));
  return {
    start: start.toISOString().slice(0, 10),
    end: end.toISOString().slice(0, 10),
    label: start.toLocaleString("en", { month: "long", year: "numeric", timeZone: "UTC" }),
  };
}

type RecordsWatchRegistry = {
  loadApp(orgId: string, appId: string): Promise<AppRegistrySnapshot | null>;
};

type ResolvedOpportunity = {
  object: RecordObject;
  name: RecordField;
  stage: RecordField;
  closeDate: RecordField | null;
  amount: RecordField | null;
};

type ResolvedActivity = {
  object: RecordObject;
  opportunity: RecordField;
  occurredAt: RecordField;
};

function normalizedName(value: string | null): string {
  return (value ?? "")
    .trim()
    .toLocaleLowerCase("en-US")
    .replace(/[^a-z0-9]+/g, "");
}

function matchingField(
  fields: RecordField[],
  input: { apiNames?: string[]; sourceNames?: string[] },
): RecordField | null {
  const apiNames = new Set((input.apiNames ?? []).map(normalizedName));
  const sourceNames = new Set((input.sourceNames ?? []).map(normalizedName));
  return (
    fields.find(
      (field) =>
        apiNames.has(normalizedName(field.apiName)) ||
        sourceNames.has(normalizedName(field.sourceApiName)),
    ) ?? null
  );
}

function objectFields(snapshot: AppRegistrySnapshot, object: RecordObject): RecordField[] {
  return snapshot.fields.filter(
    (field) => field.objectId === object.id && field.archivedAt === null,
  );
}

function resolveOpportunity(snapshot: AppRegistrySnapshot): ResolvedOpportunity | null {
  const object = snapshot.objects.find(
    (candidate) =>
      candidate.archivedAt === null &&
      (normalizedName(candidate.apiName) === "opportunity" ||
        normalizedName(candidate.sourceApiName) === "opportunity"),
  );
  if (!object) return null;
  const fields = objectFields(snapshot, object);
  const name = fields.find((field) => field.apiName === object.nameField) ?? null;
  const stage = matchingField(fields, {
    apiNames: ["stage", "stage_name"],
    sourceNames: ["StageName"],
  });
  if (!name || !stage) return null;
  return {
    object,
    name,
    stage,
    closeDate: matchingField(fields, {
      apiNames: ["close_date", "closedate"],
      sourceNames: ["CloseDate"],
    }),
    amount: matchingField(fields, {
      apiNames: ["amount"],
      sourceNames: ["Amount"],
    }),
  };
}

function resolveActivities(snapshot: AppRegistrySnapshot): ResolvedActivity[] {
  return snapshot.objects.flatMap((object) => {
    if (object.archivedAt !== null) return [];
    const fields = objectFields(snapshot, object);
    const opportunity =
      fields.find(
        (field) =>
          field.kind === "reference" &&
          (field.referenceTargets ?? []).includes("opportunity") &&
          (normalizedName(field.apiName) === "opportunity" ||
            normalizedName(field.sourceApiName) === "whatid"),
      ) ?? null;
    const occurredAt = matchingField(fields, {
      apiNames: [
        "occurred_at",
        "activity_date",
        "activitydatetime",
        "start_date_time",
        "last_modified_date",
      ],
      sourceNames: [
        "ActivityDate",
        "ActivityDateTime",
        "StartDateTime",
        "LastModifiedDate",
      ],
    });
    return opportunity && occurredAt ? [{ object, opportunity, occurredAt }] : [];
  });
}

/** Read named fields out of each record's document under stable aliases. */
function documentRows(
  value: unknown,
  aliases: Record<string, RecordField | string | null | undefined>,
): Array<Record<string, unknown>> {
  return rows(value).map((row) => {
    const raw = row.data;
    const document =
      raw && typeof raw === "object" && !Array.isArray(raw) ? (raw as Record<string, unknown>) : {};
    const mapped: Record<string, unknown> = { id: row.id, owner_user_id: row.owner_user_id };
    for (const [alias, field] of Object.entries(aliases)) {
      if (!field) continue;
      mapped[alias] = document[typeof field === "string" ? field : field.columnName] ?? null;
    }
    return mapped;
  });
}

async function appSnapshot(
  registry: RecordsWatchRegistry | undefined,
  orgId: string,
  appId: string,
): Promise<AppRegistrySnapshot | null> {
  return registry ? registry.loadApp(orgId, appId) : null;
}

export function createRecordsWatchEvaluator(input: {
  graphjin: RecordsGraphjinTransport;
  token: (orgId: string) => string;
  registry?: RecordsWatchRegistry;
  now?: () => Date;
  publish?: typeof publishRecordsSystemFinding;
}) {
  return async (payload: RecordsWatchEvaluatePayload): Promise<{ findings: number }> => {
    const [binding] = await db()
      .select()
      .from(records_watch_binding)
      .where(
        and(
          eq(records_watch_binding.id, payload.bindingId),
          eq(records_watch_binding.org_id, payload.orgId),
          eq(records_watch_binding.enabled, true),
          eq(records_watch_binding.status, "active"),
        ),
      )
      .limit(1);
    if (!binding) return { findings: 0 };
    const now = input.now?.() ?? new Date();
    const publish = input.publish ?? publishRecordsSystemFinding;
    try {
      let finding:
        | Parameters<typeof publishRecordsSystemFinding>[0]["finding"]
        | null = null;
      if (binding.watch_key === "opportunities_without_activity") {
        const cutoff = new Date(now.getTime() - 30 * 24 * 60 * 60 * 1_000).toISOString();
        const snapshot = await appSnapshot(
          input.registry,
          payload.orgId,
          binding.app_id,
        );
        const opportunity = snapshot ? resolveOpportunity(snapshot) : null;
        const activities = snapshot ? resolveActivities(snapshot) : [];
        if (snapshot && (!opportunity || activities.length === 0)) {
          throw new Error(
            "no-activity watch requires an opportunity schema and a related activity timestamp",
          );
        }
        const dynamic = opportunity && activities.length > 0;
        const query = dynamic
          ? `query EvaluateRecordsStaleOpportunities { opportunities: ${opportunity.object.tableName}(limit: 500) { id owner_user_id data } ${activities
              .map(
                (activity, index) =>
                  `activity_${index}: ${activity.object.tableName}(limit: 500) { id data }`,
              )
              .join(" ")} }`
          : `query EvaluateRecordsStaleOpportunities { opportunities: crm__opportunity(limit: 500) { id owner_user_id data } activities: crm__activity(where: { data: { occurred_at: { gte: $cutoff } } }, limit: 500) { id data } }`;
        const result = await input.graphjin.execute<Record<string, unknown>>({
          operationName: "EvaluateRecordsStaleOpportunities",
          query,
          ...(dynamic ? {} : { variables: { cutoff } }),
          token: input.token(payload.orgId),
        });
        const activeOpportunityIds = new Set(
          (dynamic
            ? activities.flatMap((activity, index) =>
                documentRows(result[`activity_${index}`], {
                  opportunity: activity.opportunity,
                  occurred_at: activity.occurredAt,
                }),
              )
            : documentRows(result.activities, { opportunity: "opportunity" })
          )
            .filter(
              (row) =>
                !dynamic ||
                (typeof row.occurred_at === "string" &&
                  !Number.isNaN(Date.parse(row.occurred_at)) &&
                  new Date(row.occurred_at).toISOString() >= cutoff),
            )
            .map((row) => stringValue(row.opportunity))
            .filter(Boolean),
        );
        const stale = documentRows(
          result.opportunities,
          dynamic
            ? { name: opportunity.name, stage: opportunity.stage, close_date: opportunity.closeDate }
            : { name: "name", stage: "stage", close_date: "close_date" },
        ).filter(
          (row) =>
            openStage(row.stage) &&
            typeof row.id === "string" &&
            !activeOpportunityIds.has(row.id),
        );
        if (stale.length > 0) {
          finding = {
            title: `${stale.length} opportunities have no activity in 30 days`,
            body: `${stale.length} open CRM opportunities have no activity recorded since ${cutoff.slice(0, 10)}. Review ownership and the next follow-up.`,
            mood: "act",
            payload: { count: stale.length, cutoff, records: stale.slice(0, 50) },
            scope: "crm.opportunity",
            topic: "opportunities_without_activity",
            freshnessTtlSeconds: 24 * 60 * 60,
          };
        }
      } else if (binding.watch_key === "unlinked_or_departed_owners") {
        const result = await input.graphjin.execute<{ identities?: unknown }>({
          operationName: "EvaluateRecordsOwnerMappings",
          query: `query EvaluateRecordsOwnerMappings($app_id: String!) { identities: identity_map(where: { app_id: { eq: $app_id } }, limit: 500) { source_instance_id source_user_id source_email source_name source_is_active app_user_id status updated_at } }`,
          variables: { app_id: binding.app_id },
          token: input.token(payload.orgId),
        });
        const affected = rows(result.identities).filter(
          (row) =>
            row.status === "unlinked" ||
            row.status === "conflict" ||
            row.source_is_active === false,
        );
        if (affected.length > 0) {
          finding = {
            title: `${affected.length} CRM source owners need reconciliation`,
            body: `${affected.length} imported owner mappings are unlinked, conflicted, or belong to an inactive source user. Reassign or resolve them from CRM identity administration.`,
            mood: "act",
            payload: { count: affected.length, identities: affected.slice(0, 50) },
            scope: "crm.identity",
            topic: "unlinked_or_departed_owners",
            freshnessTtlSeconds: 24 * 60 * 60,
          };
        }
      } else if (binding.watch_key === "deals_closing_this_month") {
        const bounds = monthBounds(now);
        const snapshot = await appSnapshot(
          input.registry,
          payload.orgId,
          binding.app_id,
        );
        const opportunity = snapshot ? resolveOpportunity(snapshot) : null;
        if (snapshot && (!opportunity || !opportunity.closeDate)) {
          throw new Error(
            "deals-closing watch requires an opportunity stage and close-date field",
          );
        }
        const query = `query EvaluateRecordsClosingDeals { opportunities: ${opportunity?.closeDate ? opportunity.object.tableName : "crm__opportunity"}(limit: 500) { id owner_user_id data } }`;
        const result = await input.graphjin.execute<{ opportunities?: unknown }>({
          operationName: "EvaluateRecordsClosingDeals",
          query,
          token: input.token(payload.orgId),
        });
        const closing = documentRows(
          result.opportunities,
          opportunity?.closeDate
            ? {
                name: opportunity.name,
                stage: opportunity.stage,
                amount: opportunity.amount,
                close_date: opportunity.closeDate,
              }
            : { name: "name", stage: "stage", amount: "amount", close_date: "close_date" },
        ).filter((row) => {
          const date = stringValue(row.close_date);
          return openStage(row.stage) && date >= bounds.start && date < bounds.end;
        });
        if (closing.length > 0) {
          const byOwner = new Map<string, { count: number; amount: number }>();
          for (const row of closing) {
            const owner = stringValue(row.owner_user_id) || "unassigned";
            const current = byOwner.get(owner) ?? { count: 0, amount: 0 };
            current.count += 1;
            const amount = Number(row.amount);
            if (Number.isFinite(amount)) current.amount += amount;
            byOwner.set(owner, current);
          }
          finding = {
            title: `${closing.length} deals close in ${bounds.label}`,
            body: `${closing.length} open CRM deals are due to close in ${bounds.label}, across ${byOwner.size} owners.`,
            mood: "watch",
            payload: {
              count: closing.length,
              month: bounds.start,
              owners: [...byOwner].map(([ownerUserId, totals]) => ({
                ownerUserId,
                ...totals,
              })),
              records: closing.slice(0, 50),
            },
            scope: "crm.opportunity",
            topic: "deals_closing_this_month",
            freshnessTtlSeconds: 24 * 60 * 60,
          };
        }
      }

      if (finding) {
        await publish({
          orgId: payload.orgId,
          workflowId: binding.workflow_id,
          backend: "records-starter-watch",
          triggerKind: payload.triggerKind === "gj_watch" ? "watcher" : "schedule",
          triggerPayload: {
            watchKey: binding.watch_key,
            graphjinWatchId: binding.graphjin_watch_id,
            ...(payload.eventId ? { eventId: payload.eventId } : {}),
          },
          finding,
        });
      }
      await db()
        .update(records_watch_binding)
        .set({
          last_evaluated_at: now,
          last_error: null,
          updated_at: now,
        })
        .where(eq(records_watch_binding.id, binding.id));
      return { findings: finding ? 1 : 0 };
    } catch (error) {
      await db()
        .update(records_watch_binding)
        .set({
          last_error: (error instanceof Error ? error.message : String(error)).slice(0, 500),
          updated_at: now,
        })
        .where(eq(records_watch_binding.id, binding.id));
      throw error;
    }
  };
}
