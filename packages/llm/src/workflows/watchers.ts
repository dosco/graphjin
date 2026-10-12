import { and, data_source, db, desc, eq, watcher } from "@neko/db";
import { packArtifactSource } from "../graphjin/pack-source";
import { graphjinQuery } from "../graphjin/client";
import { mintGraphjinToken } from "../graphjin/token";

/**
 * OL4 — watchers, polling v1. A watcher runs a GraphJin query on its
 * cadence, pulls one value out of the result, evaluates the condition,
 * and fires its linked workflow (WORKFLOW_RUN_FIRE) when it holds.
 * Debounce stops a persistent condition from re-firing every sweep;
 * `changed` watches the value itself rather than a threshold.
 */

export const WATCHER_OPS = ["gt", "gte", "lt", "lte", "eq", "ne", "changed"] as const;
export type WatcherOp = (typeof WATCHER_OPS)[number];

export type WatcherRecord = {
  id: string;
  orgId: string;
  workflowId: string;
  name: string;
  description: string;
  enabled: boolean;
  query: string;
  valuePath: string;
  op: WatcherOp;
  threshold: unknown;
  cadenceSeconds: number;
  debounceSeconds: number;
  cooldownSeconds: number;
  dedupeKey: string | null;
  activation: string | null;
  variables: Record<string, unknown>;
  severity: string;
  lastCheckedAt: Date | null;
  lastFiredAt: Date | null;
  lastValue: unknown;
  lastError: string | null;
  createdAt: Date;
  updatedAt: Date;
};

export type UpsertWatcherInput = {
  orgId: string;
  workflowId: string;
  name: string;
  description?: string;
  enabled?: boolean;
  query: string;
  valuePath: string;
  op: WatcherOp;
  threshold?: unknown;
  cadenceSeconds?: number;
  debounceSeconds?: number;
  severity?: "low" | "medium" | "high" | "critical";
};

function toRecord(row: typeof watcher.$inferSelect): WatcherRecord {
  return {
    id: row.id,
    orgId: row.org_id,
    workflowId: row.workflow_id,
    name: row.name,
    description: row.description,
    enabled: row.enabled,
    query: row.query,
    valuePath: row.value_path,
    op: row.op as WatcherOp,
    threshold: row.threshold,
    cadenceSeconds: row.cadence_seconds,
    debounceSeconds: row.debounce_seconds,
    cooldownSeconds: row.cooldown_seconds,
    dedupeKey: row.dedupe_key,
    activation: row.activation,
    variables: row.variables_json,
    severity: row.severity,
    lastCheckedAt: row.last_checked_at,
    lastFiredAt: row.last_fired_at,
    lastValue: row.last_value,
    lastError: row.last_error,
    createdAt: row.created_at,
    updatedAt: row.updated_at,
  };
}

export async function upsertWatcher(
  input: UpsertWatcherInput,
): Promise<WatcherRecord> {
  if (!(WATCHER_OPS as readonly string[]).includes(input.op)) {
    throw new Error(`Invalid watcher op: ${input.op}`);
  }
  const now = new Date();
  const values = {
    workflow_id: input.workflowId,
    description: input.description ?? "",
    enabled: input.enabled ?? true,
    query: input.query,
    value_path: input.valuePath,
    op: input.op,
    threshold: input.threshold ?? null,
    cadence_seconds: Math.max(60, input.cadenceSeconds ?? 300),
    debounce_seconds: Math.max(0, input.debounceSeconds ?? 3600),
    severity: input.severity ?? "medium",
    updated_at: now,
  };
  const [existing] = await db()
    .select({ id: watcher.id })
    .from(watcher)
    .where(and(eq(watcher.org_id, input.orgId), eq(watcher.name, input.name)))
    .limit(1);
  const [row] = existing
    ? await db()
        .update(watcher)
        .set(values)
        .where(eq(watcher.id, existing.id))
        .returning()
    : await db()
        .insert(watcher)
        .values({ org_id: input.orgId, name: input.name, ...values })
        .returning();
  return toRecord(row);
}

export async function listWatchers(orgId: string): Promise<WatcherRecord[]> {
  const rows = await db()
    .select()
    .from(watcher)
    .where(eq(watcher.org_id, orgId))
    .orderBy(watcher.name);
  return rows.map(toRecord);
}

export async function getWatcher(
  orgId: string,
  watcherId: string,
): Promise<WatcherRecord | null> {
  const [row] = await db()
    .select()
    .from(watcher)
    .where(and(eq(watcher.org_id, orgId), eq(watcher.id, watcherId)))
    .limit(1);
  return row ? toRecord(row) : null;
}

/** Enable or disable a watcher without replacing its definition or history. */
export async function setWatcherEnabled(
  orgId: string,
  watcherId: string,
  enabled: boolean,
): Promise<WatcherRecord | null> {
  const [row] = await db()
    .update(watcher)
    .set({ enabled, updated_at: new Date() })
    .where(and(eq(watcher.org_id, orgId), eq(watcher.id, watcherId)))
    .returning();
  return row ? toRecord(row) : null;
}

/** Clear evaluation history while preserving the watcher definition. */
export async function resetWatcher(
  orgId: string,
  watcherId: string,
): Promise<WatcherRecord | null> {
  const [row] = await db()
    .update(watcher)
    .set({
      last_checked_at: null,
      last_fired_at: null,
      last_value: null,
      last_error: null,
      updated_at: new Date(),
    })
    .where(and(eq(watcher.org_id, orgId), eq(watcher.id, watcherId)))
    .returning();
  return row ? toRecord(row) : null;
}

/** Delete a watcher within its organization boundary. */
export async function deleteWatcher(
  orgId: string,
  watcherId: string,
): Promise<boolean> {
  const rows = await db()
    .delete(watcher)
    .where(and(eq(watcher.org_id, orgId), eq(watcher.id, watcherId)))
    .returning({ id: watcher.id });
  return rows.length > 0;
}

/** Walk a dotted path ("orders_aggregate.0.count") through the query result data. */
export function extractValueAtPath(data: unknown, path: string): unknown {
  let current: unknown = data;
  for (const part of path.split(".").filter(Boolean)) {
    if (current === null || current === undefined) return undefined;
    if (Array.isArray(current)) {
      const idx = Number(part);
      current = Number.isInteger(idx) ? current[idx] : undefined;
    } else if (typeof current === "object") {
      current = (current as Record<string, unknown>)[part];
    } else {
      return undefined;
    }
  }
  return current;
}

export function watcherConditionMet(
  op: WatcherOp,
  value: unknown,
  threshold: unknown,
  lastValue: unknown,
): boolean {
  if (op === "changed") {
    if (lastValue === undefined || lastValue === null) return false;
    return JSON.stringify(value) !== JSON.stringify(lastValue);
  }
  if (op === "eq") return JSON.stringify(value) === JSON.stringify(threshold);
  if (op === "ne") return JSON.stringify(value) !== JSON.stringify(threshold);
  const a = Number(value);
  const b = Number(threshold);
  if (!Number.isFinite(a) || !Number.isFinite(b)) return false;
  if (op === "gt") return a > b;
  if (op === "gte") return a >= b;
  if (op === "lt") return a < b;
  if (op === "lte") return a <= b;
  return false;
}

export function resolveWatcherVariables(
  definition: Record<string, unknown> | null | undefined,
  now: Date,
): Record<string, unknown> {
  const resolved: Record<string, unknown> = {};
  for (const [name, raw] of Object.entries(definition ?? {})) {
    if (!raw || typeof raw !== "object" || Array.isArray(raw)) {
      throw new Error(`watcher variable ${name} must be a descriptor`);
    }
    const descriptor = raw as Record<string, unknown>;
    if (descriptor.kind === "now") {
      resolved[name] = now.toISOString();
      continue;
    }
    if (descriptor.kind === "seconds_ago") {
      const seconds = Number(descriptor.seconds);
      if (!Number.isFinite(seconds) || seconds < 0) {
        throw new Error(`watcher variable ${name}.seconds must be non-negative`);
      }
      resolved[name] = new Date(now.getTime() - seconds * 1_000).toISOString();
      continue;
    }
    if (descriptor.kind === "literal") {
      resolved[name] = descriptor.value;
      continue;
    }
    throw new Error(`watcher variable ${name} uses unsupported kind ${String(descriptor.kind)}`);
  }
  return resolved;
}

function graphjinEndpoint(url: string): string {
  const clean = url.replace(/\/+$/, "");
  return clean.endsWith("/api/v1/graphql") ? clean : `${clean}/api/v1/graphql`;
}

export type WatcherSweepDeps = {
  query?: typeof graphjinQuery;
  enqueueFire?: (payload: {
    orgId: string;
    workflowId: string;
    triggerKind: "watcher";
    triggerPayload: Record<string, unknown>;
    windowSeconds?: number;
  }) => Promise<void>;
  now?: () => Date;
};

async function defaultEnqueueFire(payload: {
  orgId: string;
  workflowId: string;
  triggerKind: "watcher";
  triggerPayload: Record<string, unknown>;
  windowSeconds?: number;
}): Promise<void> {
  const { enqueue, QUEUE } = await import("@neko/db/jobs");
  const { windowSeconds = 3600, ...job } = payload;
  // Collapses duplicate sweeps of one check; the watcher's quiet period sets the window.
  await enqueue(QUEUE.WORKFLOW_RUN_FIRE, job, {
    singletonKey: `watcher:${String(payload.triggerPayload.watcherId)}`,
    singletonSeconds: windowSeconds,
  });
}

export type WatcherSweepResult = {
  checked: number;
  fired: Array<{ watcherId: string; name: string; value: unknown }>;
};

export async function sweepWatchers(
  orgId: string,
  deps: WatcherSweepDeps = {},
): Promise<WatcherSweepResult> {
  const query = deps.query ?? graphjinQuery;
  const enqueueFire = deps.enqueueFire ?? defaultEnqueueFire;
  const now = deps.now?.() ?? new Date();

  const rows = await db()
    .select()
    .from(watcher)
    .where(and(eq(watcher.org_id, orgId), eq(watcher.enabled, true)));
  const due = rows.filter(
    (w) =>
      !w.last_checked_at ||
      w.last_checked_at.getTime() + w.cadence_seconds * 1000 <= now.getTime(),
  );
  if (due.length === 0) return { checked: 0, fired: [] };

  const [fallbackSource] = await db()
    .select({ graphqlUrl: data_source.graphql_url, authMode: data_source.auth_mode })
    .from(data_source)
    .where(and(eq(data_source.org_id, orgId), eq(data_source.enabled, true)))
    .orderBy(desc(data_source.is_default), data_source.created_at)
    .limit(1);

  const fired: WatcherSweepResult["fired"] = [];
  for (const row of due) {
    let value: unknown;
    let error: string | null = null;
    try {
      const src = await packArtifactSource(orgId, "watcher", row.id) ?? fallbackSource;
      if (!src?.graphqlUrl) throw new Error("watcher has no enabled GraphJin source");
      const result = await query({
        baseUrl: graphjinEndpoint(src.graphqlUrl),
        query: row.query,
        variables: resolveWatcherVariables(row.variables_json, now),
        role: "service",
        ...(src.authMode === "jwt"
          ? {
              headers: {
                authorization: `Bearer ${mintGraphjinToken({
                  orgId,
                  userId: null,
                  role: "service",
                })}`,
              },
            }
          : {}),
        signal: AbortSignal.timeout(30_000),
      });
      if (result.errors?.length) {
        error = result.errors.map((e) => e.message).join("; ").slice(0, 500);
      } else {
        value = extractValueAtPath(result.data, row.value_path);
      }
    } catch (err) {
      error = (err instanceof Error ? err.message : String(err)).slice(0, 500);
    }

    const met =
      error === null &&
      watcherConditionMet(row.op as WatcherOp, value, row.threshold, row.last_value);
    const suppressionSeconds = Math.max(row.debounce_seconds, row.cooldown_seconds);
    const outsideDebounce =
      !row.last_fired_at ||
      row.last_fired_at.getTime() + suppressionSeconds * 1000 <= now.getTime();
    // The same number as the last check, with the condition already holding, is old news.
    const unchanged =
      row.op !== "changed" &&
      JSON.stringify(value) === JSON.stringify(row.last_value) &&
      watcherConditionMet(row.op as WatcherOp, row.last_value, row.threshold, null);
    const fires = met && outsideDebounce && !unchanged;

    await db()
      .update(watcher)
      .set({
        last_checked_at: now,
        last_error: error,
        ...(error === null ? { last_value: value === undefined ? null : value } : {}),
        ...(fires ? { last_fired_at: now } : {}),
        updated_at: now,
      })
      .where(eq(watcher.id, row.id));

    if (fires) {
      await enqueueFire({
        orgId,
        workflowId: row.workflow_id,
        triggerKind: "watcher",
        windowSeconds: Math.max(row.cadence_seconds, suppressionSeconds),
        triggerPayload: {
          watcherId: row.id,
          watcherName: row.name,
          value: value === undefined ? null : value,
          threshold: row.threshold,
          op: row.op,
          severity: row.severity,
        },
      });
      fired.push({ watcherId: row.id, name: row.name, value });
      console.log(
        `[watcher] FIRED org=${orgId} "${row.name}" (${row.op} ${JSON.stringify(row.threshold)}; value=${JSON.stringify(value)})`,
      );
    }
  }
  return { checked: due.length, fired };
}
