import type { Metadata } from "next";
import { bot, db, eq, work_thread } from "@neko/db";

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const TITLE_MAX = 80;

function preview(title: string, description: string): Metadata {
  return {
    title: `${title} · OpenNeko`,
    description,
    openGraph: { title, description, siteName: "OpenNeko", type: "website" },
    twitter: { card: "summary", title, description },
  };
}

function clip(text: string) {
  const clean = text.replace(/\s+/g, " ").trim();
  return clean.length > TITLE_MAX ? `${clean.slice(0, TITLE_MAX - 1).trimEnd()}…` : clean;
}

/** Link preview for a chat: its title and its bot. Shows no answer content. */
export async function chatShareMetadata(threadId: string): Promise<Metadata> {
  if (!UUID.test(threadId)) return {};
  const [row] = await db()
    .select({ title: work_thread.title, botName: bot.name })
    .from(work_thread)
    .leftJoin(bot, eq(bot.thread_id, work_thread.id))
    .where(eq(work_thread.id, threadId))
    .limit(1)
    .catch(() => []);
  if (!row) return {};
  const botName = row.botName?.trim() ?? "";
  const title = clip(row.title?.trim() || botName || "Chat");
  return preview(title, botName && botName !== title ? `A chat with ${botName} on OpenNeko.` : "A chat on OpenNeko.");
}

/** Link preview for a bot's info page: its name. */
export async function botShareMetadata(botId: string): Promise<Metadata> {
  if (!UUID.test(botId)) return {};
  const [row] = await db()
    .select({ name: bot.name })
    .from(bot)
    .where(eq(bot.id, botId))
    .limit(1)
    .catch(() => []);
  return row ? preview(clip(row.name), `${clip(row.name)}, a bot on OpenNeko.`) : {};
}
