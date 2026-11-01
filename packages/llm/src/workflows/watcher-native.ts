import { createHash, createHmac, timingSafeEqual } from "node:crypto";
import { and, data_source, db, desc, eq, isNotNull, watcher } from "@neko/db";
import { packArtifactSource } from "../graphjin/pack-source";
import { graphjinQuery } from "../graphjin/client";
import { graphjinInputWithJsonVariables } from "../graphjin/config-change";
import { graphjinKeystoreKeyB64, mintGraphjinToken } from "../graphjin/token";
import { watcherWebhookUrl } from "../graphjin/sources-config";
import {
  applyWatcherObservation,
  extractValueAtPath,
  type WatcherFireDeps,
  type WatcherRow,
} from "./watchers";

/**
 * Watchers run as GraphJin watches. Each enabled watcher gets a polled
 * gj_watch on its source's GraphJin: GraphJin runs the query every cadence
 * and sends a signed webhook when the result changes. The worker then reads
 * the value and applies the condition (applyWatcherObservation).
 *
 * reconcileWatcherWatches keeps the GraphJin watches in step with the watcher
 * rows; the worker runs it at start and every minute.
 */


/**
 * The secret GraphJin signs a watch's webhook with (secret_source "keystore"):
 * hex(HMAC-SHA256(keystore key, "graphjin-watch-webhook:v1:" + watch id)).
 */
export function watcherWebhookSecret(watchId: string): string {
  const key = Buffer.from(graphjinKeystoreKeyB64(), "base64");
  return createHmac("sha256", key).update(`graphjin-watch-webhook:v1:${watchId}`).digest("hex");
}

export function watcherWebhookSignatureValid(rawBody: string, header: string | undefined, watchId: string): boolean {
  if (!header?.startsWith("sha256=")) return false;
  const expected = Buffer.from(
    createHmac("sha256", watcherWebhookSecret(watchId)).update(rawBody).digest("hex"),
  );
  const actual = Buffer.from(header.slice("sha256=".length));
  return actual.length === expected.length && timingSafeEqual(actual, expected);
}

function graphjinEndpoint(url: string): string {
  const clean = url.replace(/\/+$/, "");
  return clean.endsWith("/api/v1/graphql") ? clean : `${clean}/api/v1/graphql`;
}

const WATCH_NAME_PREFIX = "openneko_watcher_";

function watchName(row: WatcherRow): string {
  return `${WATCH_NAME_PREFIX}${row.id.replaceAll("-", "")}`;
}

type PollVariable = { kind: "now" } | { kind: "seconds_ago"; seconds: number };

/** Splits watcher variables into fixed values and ones GraphJin computes per poll. */
export function watcherWatchVariables(definition: Record<string, unknown> | null | undefined): {
  fixed: Record<string, unknown>;
  poll: Record<string, PollVariable>;
} {
  const fixed: Record<string, unknown> = {};
  const poll: Record<string, PollVariable> = {};
  for (const [name, raw] of Object.entries(definition ?? {})) {
    const descriptor = (raw ?? {}) as Record<string, unknown>;
    if (descriptor.kind === "literal") fixed[name] = descriptor.value;
    else if (descriptor.kind === "now") poll[name] = { kind: "now" };
    else if (descriptor.kind === "seconds_ago" && Number(descriptor.seconds) > 0) {
      poll[name] = { kind: "seconds_ago", seconds: Math.round(Number(descriptor.seconds)) };
    } else if (descriptor.kind === "seconds_ago") poll[name] = { kind: "now" };
    else throw new Error(`watcher variable ${name} uses unsupported kind ${String(descriptor.kind)}`);
  }
  return { fixed, poll };
}

/** The gj_watch definition for a watcher. Its hash decides when to resend it. */
export function watcherWatchDefinition(row: WatcherRow, webhookUrl: string, minIntervalSeconds = 60) {
  const { fixed, poll } = watcherWatchVariables(row.variables_json);
  const definition = {
    name: watchName(row),
    description: `OpenNeko watcher ${row.name}`,
    query: row.query,
    variables_json: fixed,
    poll_json: {
      interval_seconds: Math.max(minIntervalSeconds, row.cadence_seconds),
      ...(Object.keys(poll).length > 0 ? { variables: poll } : {}),
    },
    delivery_json: { kind: "webhook", url: webhookUrl, secret_source: "keystore" },
    lifecycle: "durable",
  };
  const hash = createHash("sha256").update(JSON.stringify(definition)).digest("hex");
  return { definition, hash };
}

type WatchRow = {
  id?: string;
  action_hash?: string | null;
  action_approval?: string | null;
  status?: string | null;
  last_error?: string | null;
  last_polled_at?: string | null;
};

export type WatcherWatchClient = {
  upsert(input: {
    endpoint: string;
    orgId: string;
    watchId: string | null;
    definition: Record<string, unknown>;
  }): Promise<WatchRow>;
  approve(input: { endpoint: string; orgId: string; watchId: string; actionHash: string }): Promise<WatchRow>;
  remove(input: { endpoint: string; orgId: string; watchId: string }): Promise<void>;
  status(input: { endpoint: string; orgId: string; watchId: string }): Promise<WatchRow | null>;
  /** The org service identity's watches on a GraphJin. */
  list(input: { endpoint: string; orgId: string }): Promise<Array<{ id: string; name: string }>>;
};

function firstRow<T>(value: T | T[] | null | undefined): T | null {
  return Array.isArray(value) ? (value[0] ?? null) : (value ?? null);
}

const WATCH_FIELDS = "id action_hash action_approval status last_error";

/** gj_watch over GraphQL, as the org's service identity. */
export const graphjinWatcherWatchClient: WatcherWatchClient = {
  async upsert({ endpoint, orgId, watchId, definition }) {
    const input = graphjinInputWithJsonVariables(definition);
    const selector = watchId === null
      ? `insert: ${input.literal}`
      : `where: { id: { eq: ${JSON.stringify(watchId)} } }, update: ${input.literal}`;
    const result = await graphjinQuery<{ gj_watch?: WatchRow | WatchRow[] }>({
      baseUrl: endpoint,
      headers: serviceHeaders(orgId),
      role: "service",
      query: `mutation WatcherWatchUpsert${input.variableDefinitions} { gj_watch(${selector}) { ${WATCH_FIELDS} } }`,
      variables: input.variables,
      signal: AbortSignal.timeout(30_000),
    });
    const row = firstRow(result.data?.gj_watch);
    if (result.errors?.length || !row?.id) {
      throw new Error(result.errors?.map((e) => e.message).join("; ") || "gj_watch returned no watch");
    }
    return row;
  },
  async approve({ endpoint, orgId, watchId, actionHash }) {
    const result = await graphjinQuery<{ gj_watch?: WatchRow | WatchRow[] }>({
      baseUrl: endpoint,
      headers: serviceHeaders(orgId),
      role: "service",
      query: `mutation WatcherWatchApprove($id: String!, $hash: String!) { gj_watch(where: { id: { eq: $id } }, update: { action_review_json: { decision: "approve", expected_action_hash: $hash } }) { ${WATCH_FIELDS} } }`,
      variables: { id: watchId, hash: actionHash },
      signal: AbortSignal.timeout(30_000),
    });
    const row = firstRow(result.data?.gj_watch);
    if (result.errors?.length || !row) {
      throw new Error(result.errors?.map((e) => e.message).join("; ") || "gj_watch approval returned no watch");
    }
    return row;
  },
  async remove({ endpoint, orgId, watchId }) {
    const result = await graphjinQuery({
      baseUrl: endpoint,
      headers: serviceHeaders(orgId),
      role: "service",
      query: `mutation WatcherWatchDelete($id: String!) { gj_watch(where: { id: { eq: $id } }, delete: true) { id } }`,
      variables: { id: watchId },
      signal: AbortSignal.timeout(30_000),
    });
    if (result.errors?.length) throw new Error(result.errors.map((e) => e.message).join("; "));
  },
  async status({ endpoint, orgId, watchId }) {
    const result = await graphjinQuery<{ gj_watch?: WatchRow | WatchRow[] }>({
      baseUrl: endpoint,
      headers: serviceHeaders(orgId),
      role: "service",
      // A read returns the watch's public columns; the action fields are not among them.
      query: `query WatcherWatchStatus_${Date.now().toString(36)}($id: String!) { gj_watch(where: { id: { eq: $id } }) { id status last_error last_polled_at } }`,
      variables: { id: watchId },
      signal: AbortSignal.timeout(30_000),
    });
    if (result.errors?.length) throw new Error(result.errors.map((e) => e.message).join("; "));
    return firstRow(result.data?.gj_watch);
  },
  async list({ endpoint, orgId }) {
    const result = await graphjinQuery<{ gj_watch?: Array<{ id: string; name: string }> }>({
      baseUrl: endpoint,
      headers: serviceHeaders(orgId),
      role: "service",
      query: `query WatcherWatchList_${Date.now().toString(36)} { gj_watch(limit: 1000) { id name } }`,
      signal: AbortSignal.timeout(30_000),
    });
    if (result.errors?.length) throw new Error(result.errors.map((e) => e.message).join("; "));
    return result.data?.gj_watch ?? [];
  },
};

function serviceHeaders(orgId: string): Record<string, string> {
  return { authorization: `Bearer ${mintGraphjinToken({ orgId, userId: null, role: "service" })}` };
}

export type ReconcileWatcherDeps = {
  client?: WatcherWatchClient;
  webhookUrl?: string;
  /** The shortest poll interval to give GraphJin; tests lower it. */
  minIntervalSeconds?: number;
};

export type ReconcileWatcherResult = { synced: number; removed: number; failed: number };

/**
 * Brings each watcher's GraphJin watch in line with the watcher row: creates
 * or updates and approves the watch when the definition changed, removes it
 * when the watcher is disabled, and copies the watch's error to last_error.
 */
export async function reconcileWatcherWatches(
  orgId: string,
  deps: ReconcileWatcherDeps = {},
): Promise<ReconcileWatcherResult> {
  const client = deps.client ?? graphjinWatcherWatchClient;
  const webhookUrl = deps.webhookUrl ?? watcherWebhookUrl();
  const rows = await db().select().from(watcher).where(eq(watcher.org_id, orgId));
  const [fallbackSource] = await db()
    .select({ graphqlUrl: data_source.graphql_url })
    .from(data_source)
    .where(and(eq(data_source.org_id, orgId), eq(data_source.enabled, true)))
    .orderBy(desc(data_source.is_default), data_source.created_at)
    .limit(1);

  const result: ReconcileWatcherResult = { synced: 0, removed: 0, failed: 0 };
  for (const row of rows) {
    try {
      if (!row.enabled) {
        if (row.graphjin_watch_id && row.graphjin_endpoint) {
          await client.remove({ endpoint: row.graphjin_endpoint, orgId, watchId: row.graphjin_watch_id });
          await db()
            .update(watcher)
            .set({ graphjin_watch_id: null, graphjin_endpoint: null, graphjin_watch_hash: null, updated_at: new Date() })
            .where(eq(watcher.id, row.id));
          result.removed++;
        }
        continue;
      }
      const source = (await packArtifactSource(orgId, "watcher", row.id)) ?? fallbackSource;
      if (!source?.graphqlUrl) throw new Error("watcher has no enabled GraphJin source");
      const endpoint = graphjinEndpoint(source.graphqlUrl);
      const { definition, hash } = watcherWatchDefinition(row, webhookUrl, deps.minIntervalSeconds);
      const current = row.graphjin_endpoint === endpoint ? row.graphjin_watch_id : null;
      if (current && row.graphjin_watch_hash === hash) {
        const status = await client.status({ endpoint, orgId, watchId: current });
        if (status) {
          // GraphJin polls the watch; its last poll is the watcher's last check.
          const error = status.status === "error" || status.last_error ? status.last_error || "GraphJin watch failed" : null;
          const polledAt = status.last_polled_at ? new Date(status.last_polled_at) : null;
          const checked = polledAt && !Number.isNaN(polledAt.getTime()) &&
            (!row.last_checked_at || polledAt.getTime() > row.last_checked_at.getTime()) ? polledAt : null;
          if (checked || (error !== row.last_error && (error || row.last_error))) {
            await db()
              .update(watcher)
              .set({ last_error: error, ...(checked ? { last_checked_at: checked } : {}), updated_at: new Date() })
              .where(eq(watcher.id, row.id));
          }
          continue;
        }
      }
      if (row.graphjin_watch_id && row.graphjin_endpoint && row.graphjin_endpoint !== endpoint) {
        await client.remove({ endpoint: row.graphjin_endpoint, orgId, watchId: row.graphjin_watch_id }).catch(() => {});
      }
      let watch = await client.upsert({ endpoint, orgId, watchId: current ?? null, definition });
      if (watch.action_approval !== "approved" && watch.action_hash) {
        watch = await client.approve({ endpoint, orgId, watchId: watch.id!, actionHash: watch.action_hash });
      }
      await db()
        .update(watcher)
        .set({
          graphjin_watch_id: watch.id!,
          graphjin_endpoint: endpoint,
          graphjin_watch_hash: hash,
          last_error: null,
          updated_at: new Date(),
        })
        .where(eq(watcher.id, row.id));
      result.synced++;
    } catch (error) {
      result.failed++;
      const message = (error instanceof Error ? error.message : String(error)).slice(0, 500);
      await db().update(watcher).set({ last_error: message, updated_at: new Date() }).where(eq(watcher.id, row.id));
    }
  }
  result.removed += await removeOrphanWatches(orgId, client);
  return result;
}

/**
 * Removes the GraphJin watches of deleted watchers. A deleted watcher leaves
 * no row to reconcile, so its watch is found by name on each of the org's
 * GraphJins.
 */
async function removeOrphanWatches(orgId: string, client: WatcherWatchClient): Promise<number> {
  const known = new Set(
    (await db().select({ id: watcher.id }).from(watcher).where(eq(watcher.org_id, orgId)))
      .map((row) => `${WATCH_NAME_PREFIX}${row.id.replaceAll("-", "")}`),
  );
  const sources = await db()
    .select({ graphqlUrl: data_source.graphql_url })
    .from(data_source)
    .where(and(eq(data_source.org_id, orgId), eq(data_source.enabled, true)));
  let removed = 0;
  for (const endpoint of new Set(sources.map((source) => graphjinEndpoint(source.graphqlUrl)))) {
    const watches = await client.list({ endpoint, orgId }).catch(() => []);
    for (const watch of watches) {
      if (!watch.name.startsWith(WATCH_NAME_PREFIX) || known.has(watch.name)) continue;
      await client.remove({ endpoint, orgId, watchId: watch.id }).then(() => removed++, () => {});
    }
  }
  return removed;
}

/** The watcher a GraphJin watch id belongs to, for verifying its webhook. */
export async function watcherForGraphjinWatch(watchId: string): Promise<WatcherRow | null> {
  const [row] = await db()
    .select()
    .from(watcher)
    .where(and(eq(watcher.graphjin_watch_id, watchId), isNotNull(watcher.graphjin_endpoint)))
    .limit(1);
  return row ?? null;
}

/**
 * Applies a verified GraphJin watch event: the event's result is the
 * watcher's query result, so value_path reads the same value the query
 * returns.
 */
export async function receiveWatcherEvent(
  row: WatcherRow,
  event: { data_json?: unknown },
  deps: WatcherFireDeps = {},
): Promise<{ fired: boolean }> {
  if (!row.enabled) return { fired: false };
  const data = typeof event.data_json === "string" ? JSON.parse(event.data_json) : event.data_json;
  const root = data && typeof data === "object" && "data" in (data as object) ? (data as { data: unknown }).data : data;
  return applyWatcherObservation(row, { value: extractValueAtPath(root, row.value_path) }, deps);
}
