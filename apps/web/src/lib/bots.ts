import { and, asc, bot, db, desc, eq, inArray, sql, subscription, watcher, work_thread, workflow_definition, workflow_run } from "@neko/db";
import { nekoCoats } from "@neko/avatars";
import { BOT_INSTRUCTIONS_MAX } from "@neko/llm/work";
import { describeSchedule } from "@/lib/cron-english";
import { createWorkThread } from "@/lib/work-store";

export type BotSummary = {
  id: string;
  name: string;
  coat: string;
  pattern: "solid" | "patch" | "tabby";
  isDefault: boolean;
  threadId: string | null;
  instructions: string;
  lastMessageAt: string | null;
};

export const BOT_PATTERNS = ["solid", "patch", "tabby"] as const;
const NAME_MAX = 40;

function summary(row: typeof bot.$inferSelect, lastMessageAt: Date | null): BotSummary {
  return {
    id: row.id,
    name: row.name,
    coat: row.coat,
    pattern: row.pattern as BotSummary["pattern"],
    isDefault: row.is_default,
    threadId: row.thread_id,
    instructions: row.instructions,
    lastMessageAt: lastMessageAt?.toISOString() ?? null,
  };
}

export async function createBot(
  orgId: string,
  userId: string,
  input: { name: string; coat: string; pattern: BotSummary["pattern"]; isDefault?: boolean },
): Promise<BotSummary> {
  const name = input.name.trim().slice(0, NAME_MAX);
  const thread = await createWorkThread(orgId, name, "web", userId);
  const [row] = await db()
    .insert(bot)
    .values({
      org_id: orgId,
      owner_user_id: userId,
      name,
      coat: input.coat,
      pattern: input.pattern,
      is_default: input.isDefault ?? false,
      thread_id: thread.id,
    })
    .onConflictDoNothing()
    .returning();
  if (!row) throw new Error("bot already exists");
  return summary(row, thread.last_message_at);
}

export async function listBots(orgId: string, userId: string): Promise<BotSummary[]> {
  const load = () =>
    db()
      .select({ row: bot, lastMessageAt: work_thread.last_message_at })
      .from(bot)
      .leftJoin(work_thread, eq(work_thread.id, bot.thread_id))
      .where(and(eq(bot.org_id, orgId), eq(bot.owner_user_id, userId)))
      .orderBy(desc(sql`coalesce(${work_thread.last_message_at}, ${bot.created_at})`), asc(bot.name));
  let rows = await load();
  if (!rows.some(({ row }) => row.is_default)) {
    await createBot(orgId, userId, { name: "Neko", coat: "snow", pattern: "patch", isDefault: true }).catch(() => null);
    rows = await load();
  }
  return rows.map(({ row, lastMessageAt }) => summary(row, lastMessageAt));
}

export async function getBotByThread(orgId: string, userId: string, threadId: string) {
  const [row] = await db()
    .select()
    .from(bot)
    .where(and(eq(bot.org_id, orgId), eq(bot.owner_user_id, userId), eq(bot.thread_id, threadId)))
    .limit(1);
  return row ? summary(row, null) : null;
}

export type BotRoutineRun = { status: string; at: string; summary: string };

export type BotRoutine = {
  id: string;
  name: string;
  schedule: string;
  enabled: boolean;
  description: string;
  steps: string[];
  lastRun: BotRoutineRun | null;
};

const RUN_SUMMARY_MAX = 400;

export function plainSummary(text: string): string {
  const plain = text
    .replace(/```[\s\S]*?```/g, " ")
    .replace(/[#*_`>|]+/g, "")
    .replace(/\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/\s+/g, " ")
    .trim();
  return plain.length > RUN_SUMMARY_MAX ? `${plain.slice(0, RUN_SUMMARY_MAX - 1).trimEnd()}…` : plain;
}

export function stepTexts(steps: unknown): string[] {
  if (!Array.isArray(steps)) return [];
  return steps
    .map((step) => (step && typeof step === "object" && "description" in step ? String(step.description ?? "") : ""))
    .map((text) => text.trim())
    .filter(Boolean);
}

export type BotLookPatch = {
  name?: string;
  coat?: string;
  pattern?: BotSummary["pattern"];
  instructions?: string;
};

/** Checks a patch from the info page. Returns an error message or the clean patch. */
export function cleanBotPatch(body: Record<string, unknown>): BotLookPatch | string {
  const patch: BotLookPatch = {};
  if (body.name !== undefined) {
    const name = typeof body.name === "string" ? body.name.trim().slice(0, NAME_MAX) : "";
    if (!name) return "Give the bot a name.";
    patch.name = name;
  }
  if (body.coat !== undefined) {
    if (!nekoCoats.some((c) => c.name === body.coat)) return "Pick a coat from the list.";
    patch.coat = body.coat as string;
  }
  if (body.pattern !== undefined) {
    const pattern = BOT_PATTERNS.find((p) => p === body.pattern);
    if (!pattern) return "Pick a pattern from the list.";
    patch.pattern = pattern;
  }
  if (body.instructions !== undefined) {
    if (typeof body.instructions !== "string") return "Send the instructions as text.";
    patch.instructions = body.instructions.trim().slice(0, BOT_INSTRUCTIONS_MAX);
  }
  return Object.keys(patch).length ? patch : "Nothing to change.";
}

export async function updateBot(orgId: string, userId: string, botId: string, patch: BotLookPatch) {
  const [row] = await db()
    .update(bot)
    .set({ ...patch, ...(patch.name ? { name_pending: false } : {}), updated_at: new Date() })
    .where(and(eq(bot.id, botId), eq(bot.org_id, orgId), eq(bot.owner_user_id, userId)))
    .returning();
  return row ? summary(row, null) : null;
}

export async function getBotDetail(orgId: string, userId: string, botId: string) {
  const [row] = await db()
    .select()
    .from(bot)
    .where(and(eq(bot.id, botId), eq(bot.org_id, orgId), eq(bot.owner_user_id, userId)))
    .limit(1);
  if (!row) return null;
  const routines = row.thread_id
    ? await db()
        .select({
          id: workflow_definition.id,
          name: workflow_definition.name,
          description: workflow_definition.description,
          goal: workflow_definition.goal,
          steps: workflow_definition.steps,
          cron: workflow_definition.cron,
          timezone: workflow_definition.cron_timezone,
          cronEnabled: workflow_definition.cron_enabled,
          enabled: workflow_definition.enabled,
        })
        .from(workflow_definition)
        .where(and(eq(workflow_definition.org_id, orgId), eq(workflow_definition.created_by_thread_id, row.thread_id)))
        .orderBy(asc(workflow_definition.created_at))
    : [];
  const ids = routines.map((r) => r.id);
  const [watches, events, runs] = ids.length
    ? await Promise.all([
        db()
          .select({ workflowId: watcher.workflow_id, cadence: watcher.cadence_seconds })
          .from(watcher)
          .where(and(eq(watcher.org_id, orgId), inArray(watcher.workflow_id, ids))),
        db()
          .select({ workflowId: subscription.workflow_id, source: subscription.source_kind })
          .from(subscription)
          .where(and(eq(subscription.org_id, orgId), inArray(subscription.workflow_id, ids))),
        db()
          .selectDistinctOn([workflow_run.workflow_id], {
            workflowId: workflow_run.workflow_id,
            status: workflow_run.status,
            at: sql<Date>`coalesce(${workflow_run.finished_at}, ${workflow_run.started_at}, ${workflow_run.created_at})`,
            summary: workflow_run.summary,
          })
          .from(workflow_run)
          .where(and(eq(workflow_run.org_id, orgId), inArray(workflow_run.workflow_id, ids)))
          .orderBy(workflow_run.workflow_id, desc(workflow_run.created_at)),
      ])
    : [[], [], []];
  return {
    bot: summary(row, null),
    routines: routines.map((r): BotRoutine => {
      const watch = watches.find((w) => w.workflowId === r.id);
      const event = events.find((e) => e.workflowId === r.id);
      const run = runs.find((x) => x.workflowId === r.id);
      return {
        id: r.id,
        name: r.name,
        schedule: watch
          ? `Checks every ${everyLabel(watch.cadence)}`
          : event
            ? `Runs on each ${event.source.replace(/[_.-]+/g, " ")} event`
            : describeSchedule(r.cron, r.timezone, r.cronEnabled),
        enabled: r.enabled,
        description: (r.description || r.goal).trim(),
        steps: stepTexts(r.steps),
        lastRun: run
          ? {
              status: run.status,
              at: new Date(run.at).toISOString(),
              summary: run.status === "completed" ? plainSummary(run.summary ?? "") : "",
            }
          : null,
      };
    }),
  };
}

function everyLabel(seconds: number): string {
  if (seconds % 3600 === 0) return seconds === 3600 ? "hour" : `${seconds / 3600} hours`;
  if (seconds % 60 === 0) return seconds === 60 ? "minute" : `${seconds / 60} minutes`;
  return `${seconds} seconds`;
}

/** Pauses or resumes a routine the bot created in its own chat. */
export async function setBotRoutineEnabled(
  orgId: string,
  userId: string,
  botId: string,
  workflowId: string,
  enabled: boolean,
): Promise<boolean> {
  const [owned] = await db()
    .select({ threadId: bot.thread_id })
    .from(bot)
    .where(and(eq(bot.id, botId), eq(bot.org_id, orgId), eq(bot.owner_user_id, userId)))
    .limit(1);
  if (!owned?.threadId) return false;
  const rows = await db()
    .update(workflow_definition)
    .set({ enabled, ...(enabled ? { paused_until: null } : {}), updated_at: new Date() })
    .where(
      and(
        eq(workflow_definition.org_id, orgId),
        eq(workflow_definition.id, workflowId),
        eq(workflow_definition.created_by_thread_id, owned.threadId),
      ),
    )
    .returning({ id: workflow_definition.id });
  if (rows.length === 0) return false;
  const now = new Date();
  await Promise.all([
    db().update(watcher).set({ enabled, updated_at: now }).where(and(eq(watcher.org_id, orgId), eq(watcher.workflow_id, workflowId))),
    db().update(subscription).set({ enabled, updated_at: now }).where(and(eq(subscription.org_id, orgId), eq(subscription.workflow_id, workflowId))),
  ]);
  return true;
}

const words = (text: string) =>
  new Set(text.toLowerCase().split(/[^a-z0-9]+/).filter((w) => w.length > 2));

const STARTER_JOBS = [
  "Send me a short summary each morning of what changed in the business.",
  "Watch my key numbers and tell me when one moves.",
  "Keep an eye on stock and tell me what to reorder.",
  "Flag orders that are late or stuck.",
  "Tell me when a regular customer stops buying.",
  "Draft a weekly report I can send to my team.",
];

/** Four first jobs for a new bot. A bot name that matches a job ranks it first. */
export function botStarters(botName: string): string[] {
  const hint = words(botName);
  const score = (job: string) => [...words(job)].filter((w) => hint.has(w.replace(/s$/, "")) || hint.has(w)).length;
  return [...STARTER_JOBS].sort((a, b) => score(b) - score(a)).slice(0, 4);
}
