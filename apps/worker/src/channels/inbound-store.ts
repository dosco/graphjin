// Durable state for reliable inbound: a persisted poll cursor (resume point
// across restarts) and a per-update ledger (exactly-once dispatch + dead-letter
// after repeated failures). Both poll and webhook ingest drive an update
// through begin → markDone | recordFailure here.
import { createHash } from "node:crypto";
import {
  and,
  channel_poll_cursor,
  db,
  eq,
  inbound_dedup,
  lt,
  sql,
} from "@neko/db";

const DEDUP_TTL_MS = 7 * 24 * 60 * 60 * 1000;

/** Stable key for a raw inbound update: SHA-256 over canonical JSON (sorted
 *  keys), so a re-fetched or retried duplicate hashes identically. */
export function inboundUpdateKey(raw: unknown): string {
  return createHash("sha256").update(canonical(raw)).digest("hex");
}

function canonical(v: unknown): string {
  if (v === null || typeof v !== "object") return JSON.stringify(v) ?? "null";
  if (Array.isArray(v)) return `[${v.map(canonical).join(",")}]`;
  const keys = Object.keys(v as Record<string, unknown>).sort();
  return `{${keys
    .map((k) => `${JSON.stringify(k)}:${canonical((v as Record<string, unknown>)[k])}`)
    .join(",")}}`;
}

export type BeginInboundResult = {
  /** Dispatch this update? false ⇒ already done or dead-lettered (skip). */
  proceed: boolean;
  /** Failed-dispatch attempts recorded so far (0 on first sight). */
  attempts: number;
  /** The skip is because the update was dead-lettered (not a plain duplicate). */
  dead: boolean;
};

/**
 * Claim an update for dispatch, or report that it's already handled. A fresh
 * update inserts a 'pending' row and proceeds; a 'pending' row (a retry, or a
 * crash before completion) proceeds again; a 'done' or 'dead' row is skipped.
 */
export async function beginInboundUpdate(
  orgId: string,
  channelPlugin: string,
  updateKey: string,
): Promise<BeginInboundResult> {
  const inserted = await db()
    .insert(inbound_dedup)
    .values({
      org_id: orgId,
      channel_plugin: channelPlugin,
      update_key: updateKey,
      status: "pending",
      attempts: 0,
    })
    .onConflictDoNothing()
    .returning({ attempts: inbound_dedup.attempts });
  if (inserted.length > 0) {
    return { proceed: true, attempts: inserted[0].attempts, dead: false };
  }
  const existing = await db()
    .select({ status: inbound_dedup.status, attempts: inbound_dedup.attempts })
    .from(inbound_dedup)
    .where(
      and(
        eq(inbound_dedup.org_id, orgId),
        eq(inbound_dedup.channel_plugin, channelPlugin),
        eq(inbound_dedup.update_key, updateKey),
      ),
    )
    .limit(1);
  const row = existing[0];
  if (!row) return { proceed: true, attempts: 0, dead: false };
  if (row.status === "pending") {
    return { proceed: true, attempts: row.attempts, dead: false };
  }
  return { proceed: false, attempts: row.attempts, dead: row.status === "dead" };
}

/** Mark an update successfully dispatched — the permanent dedup marker. */
export async function markInboundDone(
  orgId: string,
  channelPlugin: string,
  updateKey: string,
): Promise<void> {
  await db()
    .update(inbound_dedup)
    .set({ status: "done", updated_at: sql`now()` })
    .where(
      and(
        eq(inbound_dedup.org_id, orgId),
        eq(inbound_dedup.channel_plugin, channelPlugin),
        eq(inbound_dedup.update_key, updateKey),
      ),
    );
}

/**
 * Record a failed dispatch. Increments the attempt count; once it reaches
 * `maxAttempts` the update is dead-lettered (status='dead', payload + error
 * retained) so callers stop retrying and advance past it. Returns whether the
 * update is now dead and the attempt number reached.
 */
export async function recordInboundFailure(
  orgId: string,
  channelPlugin: string,
  updateKey: string,
  maxAttempts: number,
  rawPayload: unknown,
  errorMessage: string,
): Promise<{ dead: boolean; attempts: number }> {
  const where = and(
    eq(inbound_dedup.org_id, orgId),
    eq(inbound_dedup.channel_plugin, channelPlugin),
    eq(inbound_dedup.update_key, updateKey),
  );
  const rows = await db()
    .update(inbound_dedup)
    .set({
      attempts: sql`${inbound_dedup.attempts} + 1`,
      last_error: errorMessage,
      updated_at: sql`now()`,
    })
    .where(where)
    .returning({ attempts: inbound_dedup.attempts });
  const attempts = rows[0]?.attempts ?? maxAttempts;
  const dead = attempts >= maxAttempts;
  if (dead) {
    await db()
      .update(inbound_dedup)
      .set({ status: "dead", payload: rawPayload as Record<string, unknown> })
      .where(where);
  }
  return { dead, attempts };
}

export async function loadPollCursor(
  orgId: string,
  channelPlugin: string,
): Promise<string | undefined> {
  const rows = await db()
    .select({ cursor: channel_poll_cursor.cursor })
    .from(channel_poll_cursor)
    .where(
      and(
        eq(channel_poll_cursor.org_id, orgId),
        eq(channel_poll_cursor.channel_plugin, channelPlugin),
      ),
    )
    .limit(1);
  return rows[0]?.cursor;
}

export async function savePollCursor(
  orgId: string,
  channelPlugin: string,
  cursor: string,
): Promise<void> {
  await db()
    .insert(channel_poll_cursor)
    .values({ org_id: orgId, channel_plugin: channelPlugin, cursor })
    .onConflictDoUpdate({
      target: [channel_poll_cursor.org_id, channel_poll_cursor.channel_plugin],
      set: { cursor, updated_at: sql`now()` },
    });
}

/** Drop dispatched ('done') dedup rows past the TTL so the ledger stays bounded.
 *  Dead letters are kept for inspection; pending rows are in-flight. */
export async function pruneInboundDedup(nowMs: number): Promise<number> {
  const cutoff = new Date(nowMs - DEDUP_TTL_MS);
  const rows = await db()
    .delete(inbound_dedup)
    .where(and(eq(inbound_dedup.status, "done"), lt(inbound_dedup.created_at, cutoff)))
    .returning({ id: inbound_dedup.id });
  return rows.length;
}
