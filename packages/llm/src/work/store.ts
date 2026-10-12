import {
  and,
  asc,
  db,
  desc,
  eq,
  gte,
  inArray,
  isNull,
  lt,
  or,
  sql,
  work_message,
  work_run,
  work_run_event,
  work_thread,
  workflow_run,
  pool,
  processing_job,
} from "@neko/db";
import { createHash } from "node:crypto";
import type { AgentBackendId } from "../agent-backend";
import type { AgentEvent } from "../agent-backend";
import { admitRunSpend, recordBudgetBlocked, type SpendSource } from "../spend/admission";
import { recordUsageSpend } from "../spend/ledger";

export type WorkThreadSummary = {
  id: string;
  title: string;
  createdAt: string;
  updatedAt: string;
  lastMessageAt: string;
};

export type WorkThreadListScope =
  | { surface: "main" }
  | { surface: "app"; appId: string };

export type WorkMessageRecord = {
  id: string;
  runId: string | null;
  role: "user" | "assistant";
  content: string;
  createdAt: string;
};

export type WorkRunRecord = {
  id: string;
  backend: AgentBackendId;
  status: string;
  error: string | null;
  createdAt: string;
  finishedAt: string | null;
  analysisMinutesSaved: number | null;
  analysisMinutesBasis: string | null;
  actorRole: "admin" | "member" | "service" | null;
};

export type WorkThreadBundle = {
  thread: {
    id: string;
    title: string;
    backendState: Record<string, unknown>;
    createdAt: string;
    updatedAt: string;
    lastMessageAt: string;
  };
  runs: WorkRunRecord[];
  messages: WorkMessageRecord[];
  eventsByRun: Record<string, AgentEvent[]>;
  hasOlder?: boolean;
};

export type WorkThreadPage = { limit: number; beforeRunId?: string | null };

async function runStartedAt(orgId: string, runId: string): Promise<Date | null> {
  const [row] = await db()
    .select({
      at: sql<Date>`least(${work_run.created_at}, coalesce((select min(m.created_at) from ${work_message} m where m.run_id = ${work_run.id}), ${work_run.created_at}))`.mapWith(
        (value) => new Date(value as string),
      ),
    })
    .from(work_run)
    .where(and(eq(work_run.org_id, orgId), eq(work_run.id, runId)))
    .limit(1);
  return row?.at ?? null;
}

export async function listWorkThreads(
  orgId: string,
  channel = "web",
  createdByUserId?: string | null,
  scope?: WorkThreadListScope,
): Promise<WorkThreadSummary[]> {
  // Workflow runs reuse the work_thread / work_run plumbing for their
  // transcripts (so they get the same surface, events, memory hooks).
  // But /work (Ask) is strictly human ↔ agent — its sidebar must not
  // surface threads created by a workflow trigger. Exclude any thread
  // that has a workflow_run pointing at it.
  //
  // Channels are isolated: a surface lists only its own threads (the web Ask
  // UI passes "web"), so Telegram/Slack conversations never appear here.
  const rows = await db()
    .select()
    .from(work_thread)
    .where(
      and(
        eq(work_thread.org_id, orgId),
        eq(work_thread.channel, channel),
        ...(createdByUserId !== undefined
          ? [
              createdByUserId === null
                ? isNull(work_thread.created_by_user_id)
                : eq(work_thread.created_by_user_id, createdByUserId),
            ]
          : []),
        sql`NOT EXISTS (SELECT 1 FROM ${workflow_run} wr WHERE wr.thread_id = ${work_thread.id})`,
        ...(scope?.surface === "main"
          ? [
              sql`NOT (${work_thread.backend_state} ? 'appContext')`,
              sql`NOT (${work_thread.backend_state} ? 'recordContext')`,
            ]
          : scope?.surface === "app"
            ? [
                sql`(
                  ${work_thread.backend_state}->'appContext'->>'appId' = ${scope.appId}
                  OR (
                    NOT (${work_thread.backend_state} ? 'appContext')
                    AND ${work_thread.backend_state}->'recordContext'->>'appId' = ${scope.appId}
                  )
                )`,
              ]
            : []),
      ),
    )
    .orderBy(desc(work_thread.last_message_at), desc(work_thread.created_at));
  return rows.map((row) => ({
    id: row.id,
    title: row.title || "Untitled thread",
    createdAt: row.created_at.toISOString(),
    updatedAt: row.updated_at.toISOString(),
    lastMessageAt: row.last_message_at.toISOString(),
  }));
}

export async function createWorkThread(
  orgId: string,
  title = "",
  channel = "web",
  createdByUserId: string | null = null,
  backendState: Record<string, unknown> = {},
) {
  const rows = await db()
    .insert(work_thread)
    .values({
      org_id: orgId,
      title,
      channel,
      created_by_user_id: createdByUserId,
      backend_state: backendState,
    })
    .returning();
  return rows[0];
}

// Deterministic thread id per channel conversation, so repeated inbound messages
// reuse one work_thread (and its history) instead of spawning a new one each turn.
const CHANNEL_THREAD_NS = "8b9d2e7a-1c34-4f56-9a78-b0c1d2e3f405";

function uuidv5(name: string, namespaceUuid: string): string {
  const ns = Buffer.from(namespaceUuid.replace(/-/g, ""), "hex");
  const bytes = createHash("sha1").update(ns).update(name).digest().subarray(0, 16);
  bytes[6] = (bytes[6] & 0x0f) | 0x50;
  bytes[8] = (bytes[8] & 0x3f) | 0x80;
  const h = bytes.toString("hex");
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`;
}

export function channelThreadId(
  orgId: string,
  channel: string,
  conversationKey: string,
): string {
  return uuidv5(`${orgId}:${channel}:${conversationKey}`, CHANNEL_THREAD_NS);
}

export async function getOrCreateChannelThread(args: {
  orgId: string;
  channel: string;
  conversationKey: string;
  title?: string;
  createdByUserId?: string | null;
}) {
  const id = channelThreadId(args.orgId, args.channel, args.conversationKey);
  const inserted = await db()
    .insert(work_thread)
    .values({
      id,
      org_id: args.orgId,
      title: args.title ?? "",
      channel: args.channel,
      created_by_user_id: args.createdByUserId ?? null,
    })
    .onConflictDoNothing()
    .returning();
  if (inserted[0]) return inserted[0];
  const [existing] = await db()
    .select()
    .from(work_thread)
    .where(eq(work_thread.id, id))
    .limit(1);
  return existing;
}

export async function deleteWorkThread(orgId: string, threadId: string): Promise<boolean> {
  const rows = await db()
    .delete(work_thread)
    .where(and(eq(work_thread.org_id, orgId), eq(work_thread.id, threadId)))
    .returning({ id: work_thread.id });
  return rows.length > 0;
}

// Truncates the thread at and after the given run: deletes that run plus
// every later run in the thread (events cascade via FK), wipes the user
// + assistant messages tied to those runs, and clears the thread's
// backendState so a resumed run cannot re-inject the dropped turns
// from its persisted SDK session. Returns the run's `created_at` so the
// caller can verify it pointed at a real row.
export async function truncateWorkThreadFromRun(
  orgId: string,
  threadId: string,
  runId: string,
): Promise<{ ok: boolean }> {
  const targetRows = await db()
    .select({ created_at: work_run.created_at })
    .from(work_run)
    .where(
      and(
        eq(work_run.org_id, orgId),
        eq(work_run.thread_id, threadId),
        eq(work_run.id, runId),
      ),
    )
    .limit(1);
  const target = targetRows[0];
  if (!target) return { ok: false };

  await db()
    .delete(work_message)
    .where(
      and(
        eq(work_message.org_id, orgId),
        eq(work_message.thread_id, threadId),
        gte(work_message.created_at, target.created_at),
      ),
    );
  await db()
    .delete(work_run)
    .where(
      and(
        eq(work_run.org_id, orgId),
        eq(work_run.thread_id, threadId),
        gte(work_run.created_at, target.created_at),
      ),
    );
  await db()
    .update(work_thread)
    .set({ backend_state: {}, updated_at: new Date(), last_message_at: new Date() })
    .where(eq(work_thread.id, threadId));
  return { ok: true };
}

export async function getWorkThread(orgId: string, threadId: string) {
  const rows = await db()
    .select()
    .from(work_thread)
    .where(
      and(
        eq(work_thread.org_id, orgId),
        eq(work_thread.id, threadId),
      ),
    )
    .limit(1);
  return rows[0] ?? null;
}

export async function setWorkThreadBackendState(
  threadId: string,
  backendState: Record<string, unknown>,
) {
  await db()
    .update(work_thread)
    .set({
      backend_state: backendState,
      updated_at: new Date(),
    })
    .where(eq(work_thread.id, threadId));
}

export async function touchWorkThread(
  threadId: string,
  opts: { title?: string } = {},
) {
  const patch: Record<string, unknown> = {
    updated_at: new Date(),
    last_message_at: new Date(),
  };
  if (opts.title !== undefined) patch.title = opts.title;
  await db().update(work_thread).set(patch).where(eq(work_thread.id, threadId));
}

/**
 * K1: the acting principal a run executes as, snapshotted at run start.
 *   web run            → { userId: app_user.id, role: app_user.role }
 *   channel run        → { userId: null, role: "member" }  (until CH3 links)
 *   cron/workflow run  → { userId: null, role: "service" }
 */
export type RunActor = {
  userId: string | null;
  role: "admin" | "member" | "service";
};

export type RunSpend = {
  source: SpendSource;
  workflowId?: string | null;
};

export async function createWorkRun(
  orgId: string,
  threadId: string,
  backend: AgentBackendId,
  actor?: RunActor,
  spend: RunSpend = { source: "system" },
) {
  const client = await pool().connect();
  let released = false;
  let rows: (typeof work_run.$inferSelect)[];
  try {
    await client.query("begin");
    const inserted = await client.query<typeof work_run.$inferSelect>(
      `insert into work_run (org_id, thread_id, backend, status, actor_user_id, actor_role)
       values ($1, $2, $3, 'queued', $4, $5)
       returning *`,
      [orgId, threadId, backend, actor?.userId ?? null, actor?.role ?? null],
    );
    rows = inserted.rows;
    await admitRunSpend(client, {
      orgId,
      workflowId: spend.workflowId ?? null,
      workRunId: rows[0].id,
      source: spend.source,
    });
    await client.query("commit");
  } catch (error) {
    await client.query("rollback").catch(() => undefined);
    client.release();
    released = true;
    await recordBudgetBlocked(error);
    throw error;
  } finally {
    if (!released) client.release();
  }
  // SEC10: run lifecycle rides the tamper-evident chain.
  const { recordAuditEvent } = await import("../workflows/audit-chain");
  await recordAuditEvent({
    orgId,
    entityKind: "work_run",
    entityId: rows[0].id,
    event: "run:created",
    payload: {
      backend,
      actorUserId: actor?.userId ?? null,
      actorRole: actor?.role ?? null,
    },
  });
  return rows[0];
}

/** Show the stale-run sweeps that a run waiting for a sandbox slot is alive. */
export async function touchAgentRun(id: string) {
  const now = new Date();
  await db().update(work_run).set({ updated_at: now }).where(eq(work_run.id, id));
  await db().update(processing_job).set({ updated_at: now }).where(eq(processing_job.id, id));
}

export async function markWorkRunRunning(runId: string) {
  await db()
    .update(work_run)
    .set({ status: "running", updated_at: new Date() })
    .where(eq(work_run.id, runId));
}

export async function finishWorkRun(
  runId: string,
  status: "completed" | "failed" | "cancelled" | "needs_input",
  error: string | null,
) {
  const rows = await db()
    .update(work_run)
    .set({
      status,
      error,
      updated_at: new Date(),
      finished_at: new Date(),
    })
    // A terminal state is final. This also prevents a detached agent process
    // from overwriting a cancellation if it returns after the control plane
    // has already recovered the run.
    .where(
      and(
        eq(work_run.id, runId),
        inArray(work_run.status, ["queued", "running"]),
      ),
    )
    .returning({ orgId: work_run.org_id });
  if (rows[0]) {
    const { recordAuditEvent } = await import("../workflows/audit-chain");
    await recordAuditEvent({
      orgId: rows[0].orgId,
      entityKind: "work_run",
      entityId: runId,
      event: `run:${status}`,
      payload: { status, error },
    });
  }
}

/**
 * Terminalize a queued/running run whose in-process owner is gone.
 * Returns true only when this call won the terminal-state transition.
 */
export async function cancelWorkRunIfActive(
  runId: string,
  error: string | null,
): Promise<boolean> {
  const now = new Date();
  const rows = await db()
    .update(work_run)
    .set({
      status: "cancelled",
      error,
      updated_at: now,
      finished_at: now,
    })
    .where(
      and(
        eq(work_run.id, runId),
        inArray(work_run.status, ["queued", "running"]),
      ),
    )
    .returning({ orgId: work_run.org_id });

  if (!rows[0]) return false;

  const { recordAuditEvent } = await import("../workflows/audit-chain");
  await recordAuditEvent({
    orgId: rows[0].orgId,
    entityKind: "work_run",
    entityId: runId,
    event: "run:cancelled",
    payload: { status: "cancelled", error },
  });
  return true;
}

// Persist a run's agent-estimated analysis value (server-clamped minutes +
// the one-line basis). Separate from finishWorkRun because the estimate is
// parsed from the run's value fence after the run is marked finished.
export async function setWorkRunValue(
  runId: string,
  args: { minutes: number | null; basis: string | null },
) {
  await db()
    .update(work_run)
    .set({
      analysis_minutes_saved: args.minutes,
      analysis_minutes_basis: args.basis,
      updated_at: new Date(),
    })
    .where(eq(work_run.id, runId));
}

export async function createWorkMessage(args: {
  orgId: string;
  threadId: string;
  runId: string | null;
  role: "user" | "assistant";
  content: string;
}) {
  const rows = await db()
    .insert(work_message)
    .values({
      org_id: args.orgId,
      thread_id: args.threadId,
      run_id: args.runId,
      role: args.role,
      content: args.content,
    })
    .returning();
  await touchWorkThread(args.threadId);
  return rows[0];
}

export async function saveAssistantWorkMessage(args: {
  orgId: string;
  threadId: string;
  runId: string;
  content: string;
}) {
  const existing = await db()
    .select({ id: work_message.id })
    .from(work_message)
    .where(
      and(
        eq(work_message.org_id, args.orgId),
        eq(work_message.thread_id, args.threadId),
        eq(work_message.run_id, args.runId),
        eq(work_message.role, "assistant"),
      ),
    )
    .limit(1);

  if (existing[0]) {
    const rows = await db()
      .update(work_message)
      .set({ content: args.content })
      .where(eq(work_message.id, existing[0].id))
      .returning();
    await touchWorkThread(args.threadId);
    return rows[0];
  }

  return createWorkMessage({
    orgId: args.orgId,
    threadId: args.threadId,
    runId: args.runId,
    role: "assistant",
    content: args.content,
  });
}

/**
 * Append an event to a run's event stream. Returns the Postgres-
 * assigned `id` (bigserial, globally monotonic). Callers don't manage
 * any seq — ordering is naturally preserved by insertion order, and
 * the SSE cursor is just "id > $lastId".
 */
export async function appendWorkRunEvent(args: {
  orgId: string;
  threadId: string;
  runId: string;
  event: AgentEvent;
}): Promise<number> {
  const [row] = await db()
    .insert(work_run_event)
    .values({
      org_id: args.orgId,
      thread_id: args.threadId,
      run_id: args.runId,
      kind: args.event.type,
      payload: args.event,
    })
    .returning({ id: work_run_event.id });
  const eventId = row?.id ?? 0;
  if (args.event.type === "usage" && args.event.source === "outer" && eventId > 0) {
    await recordUsageSpend({
      orgId: args.orgId,
      workRunId: args.runId,
      usage: args.event.usage,
      provider: args.event.provider,
      model: args.event.model,
    });
  }
  if (args.event.type === "tool_start" && eventId > 0) {
    const { recordSkillUsageFromEvent } = await import("./skill-usage");
    await recordSkillUsageFromEvent({
      orgId: args.orgId,
      threadId: args.threadId,
      runId: args.runId,
      event: args.event,
      triggeringEventId: eventId,
    });
  }
  return eventId;
}

export async function getWorkRunEvents(
  orgId: string,
  runId: string,
): Promise<AgentEvent[]> {
  const rows = await db()
    .select({
      payload: work_run_event.payload,
    })
    .from(work_run_event)
    .where(
      and(
        eq(work_run_event.org_id, orgId),
        eq(work_run_event.run_id, runId),
      ),
    )
    .orderBy(asc(work_run_event.id));
  return rows.map((row) => row.payload as AgentEvent);
}

export async function getWorkRunEventsAfter(
  orgId: string,
  runId: string,
  afterId: number,
): Promise<{ id: number; event: AgentEvent; createdAt: Date }[]> {
  const rows = await db()
    .select({
      id: work_run_event.id,
      payload: work_run_event.payload,
      created_at: work_run_event.created_at,
    })
    .from(work_run_event)
    .where(
      and(
        eq(work_run_event.org_id, orgId),
        eq(work_run_event.run_id, runId),
      ),
    )
    .orderBy(asc(work_run_event.id));
  return rows
    .filter((r) => r.id > afterId)
    .map((r) => ({
      id: r.id,
      event: r.payload as AgentEvent,
      createdAt: r.created_at,
    }));
}

export async function getWorkRun(orgId: string, runId: string) {
  const rows = await db()
    .select()
    .from(work_run)
    .where(and(eq(work_run.org_id, orgId), eq(work_run.id, runId)))
    .limit(1);
  return rows[0] ?? null;
}

/**
 * Lookup the thread a /work run belongs to. Used by the action-execute
 * worker job to figure out which thread to emit the terminal
 * action_request_result event into.
 */
export async function getWorkThreadForRun(
  orgId: string,
  runId: string,
): Promise<{ id: string } | null> {
  const rows = await db()
    .select({ id: work_thread.id })
    .from(work_run)
    .innerJoin(work_thread, eq(work_run.thread_id, work_thread.id))
    .where(and(eq(work_run.org_id, orgId), eq(work_run.id, runId)))
    .limit(1);
  return rows[0] ?? null;
}

export async function getWorkThreadBundle(
  orgId: string,
  threadId: string,
  page?: WorkThreadPage,
): Promise<WorkThreadBundle | null> {
  const thread = await getWorkThread(orgId, threadId);
  if (!thread) return null;

  const scope = page ? await pageScope(orgId, threadId, page) : null;
  const [runs, messages, events] = await Promise.all([
    scope
      ? Promise.resolve(scope.runs)
      : db()
          .select()
          .from(work_run)
          .where(and(eq(work_run.org_id, orgId), eq(work_run.thread_id, threadId)))
          .orderBy(asc(work_run.created_at)),
    db()
      .select()
      .from(work_message)
      .where(
        and(
          eq(work_message.org_id, orgId),
          eq(work_message.thread_id, threadId),
          ...(scope ? [scope.messageFilter] : []),
        ),
      )
      .orderBy(asc(work_message.created_at)),
    scope && scope.runIds.length === 0
      ? Promise.resolve([])
      : db()
          .select({
            runId: work_run_event.run_id,
            payload: work_run_event.payload,
          })
          .from(work_run_event)
          .where(
            and(
              eq(work_run_event.org_id, orgId),
              eq(work_run_event.thread_id, threadId),
              ...(scope ? [inArray(work_run_event.run_id, scope.runIds)] : []),
            ),
          )
          .orderBy(asc(work_run_event.id)),
  ]);

  const eventsByRun: Record<string, AgentEvent[]> = {};
  for (const row of events) {
    if (!eventsByRun[row.runId]) eventsByRun[row.runId] = [];
    eventsByRun[row.runId].push(row.payload as AgentEvent);
  }

  return {
    thread: {
      id: thread.id,
      title: thread.title || "Untitled thread",
      backendState: (thread.backend_state ?? {}) as Record<string, unknown>,
      createdAt: thread.created_at.toISOString(),
      updatedAt: thread.updated_at.toISOString(),
      lastMessageAt: thread.last_message_at.toISOString(),
    },
    runs: runs.map((row) => ({
      id: row.id,
      backend: row.backend as AgentBackendId,
      status: row.status,
      error: row.error,
      createdAt: row.created_at.toISOString(),
      finishedAt: row.finished_at ? row.finished_at.toISOString() : null,
      analysisMinutesSaved: row.analysis_minutes_saved,
      analysisMinutesBasis: row.analysis_minutes_basis,
      actorRole:
        row.actor_role === "admin" ||
        row.actor_role === "member" ||
        row.actor_role === "service"
          ? row.actor_role
          : null,
    })),
    messages: messages.map((row) => ({
      id: row.id,
      runId: row.run_id,
      role: row.role as "user" | "assistant",
      content: row.content,
      createdAt: row.created_at.toISOString(),
    })),
    eventsByRun,
    ...(scope ? { hasOlder: scope.hasOlder } : {}),
  };
}

async function pageScope(orgId: string, threadId: string, page: WorkThreadPage) {
  const upperAt = page.beforeRunId ? await runStartedAt(orgId, page.beforeRunId) : null;
  const newestFirst = await db()
    .select()
    .from(work_run)
    .where(
      and(
        eq(work_run.org_id, orgId),
        eq(work_run.thread_id, threadId),
        ...(upperAt ? [lt(work_run.created_at, upperAt)] : []),
      ),
    )
    .orderBy(desc(work_run.created_at))
    .limit(page.limit + 1);
  const hasOlder = newestFirst.length > page.limit;
  const runs = newestFirst.slice(0, page.limit).reverse();
  const runIds = runs.map((run) => run.id);
  const lowerAt = hasOlder && runs[0] ? await runStartedAt(orgId, runs[0].id) : null;
  const loose = and(
    isNull(work_message.run_id),
    ...(lowerAt ? [gte(work_message.created_at, lowerAt)] : []),
    ...(upperAt ? [lt(work_message.created_at, upperAt)] : []),
  )!;
  const messageFilter = runIds.length ? or(inArray(work_message.run_id, runIds), loose)! : loose;
  return { runs, runIds, hasOlder, messageFilter };
}

export function suggestWorkThreadTitle(message: string): string {
  const text = message.trim().replace(/\s+/g, " ");
  if (!text) return "New thread";
  return text.length > 80 ? `${text.slice(0, 77)}...` : text;
}
