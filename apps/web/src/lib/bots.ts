import { and, asc, bot, db, desc, eq, sql, work_thread } from "@neko/db";
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

export async function setBotInstructions(orgId: string, userId: string, botId: string, instructions: string) {
  const [row] = await db()
    .update(bot)
    .set({ instructions: instructions.trim().slice(0, 4000), updated_at: new Date() })
    .where(and(eq(bot.id, botId), eq(bot.org_id, orgId), eq(bot.owner_user_id, userId)))
    .returning();
  return row ? summary(row, null) : null;
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
