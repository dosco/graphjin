import { ax } from "@ax-llm/ax";
import { and, asc, bot, db, eq, work_message, work_thread } from "@neko/db";
import { buildLlm } from "../llm";

const NAME_MAX = 24;
const MESSAGES = 6;
const MESSAGE_MAX = 400;
const BATCH = 20;

const DESCRIPTION = `You name a chat bot. Read the chat title, the bot's job description and the operator's first messages. Write a short, friendly name of 1 to 3 words that says what the bot does, such as "Stock watcher" or "Weekly sales". Use sentence case. Use no quotes, emoji or end punctuation.`;

export const botNamer = ax(
  `chatTitle:string "the chat title the bot carries now", instructions?:string "the bot's job description, absent when it has none", operatorMessages:string "the operator's first messages in the chat" -> name:string "the new bot name, 1 to 3 words"`,
  { description: DESCRIPTION },
);

export type NameBotFn = (input: {
  orgId: string;
  chatTitle: string;
  instructions: string;
  operatorMessages: string;
}) => Promise<string>;

const defaultName: NameBotFn = async ({ orgId, instructions, ...input }) => {
  const result = await botNamer.forward(await buildLlm(orgId), {
    ...input,
    ...(instructions ? { instructions } : {}),
  });
  return String(result.name ?? "");
};

export function cleanBotName(raw: string): string {
  return raw
    .replace(/["`*]/g, "")
    .replace(/\s+/g, " ")
    .trim()
    .replace(/^'+|'+$/g, "")
    .replace(/[.!?:;,]+$/, "")
    .slice(0, NAME_MAX)
    .trim();
}

/** Gives each bot that carries its chat title a short name. Each bot gets one attempt. */
export async function nameBackfilledBots(name: NameBotFn = defaultName): Promise<{ named: number; failed: number }> {
  const pending = await db()
    .select({ id: bot.id, orgId: bot.org_id, name: bot.name, instructions: bot.instructions, threadId: bot.thread_id })
    .from(bot)
    .where(eq(bot.name_pending, true))
    .limit(BATCH);
  let named = 0;
  let failed = 0;
  for (const row of pending) {
    let next = "";
    try {
      const messages = row.threadId
        ? await db()
            .select({ content: work_message.content })
            .from(work_message)
            .where(and(eq(work_message.thread_id, row.threadId), eq(work_message.role, "user")))
            .orderBy(asc(work_message.created_at))
            .limit(MESSAGES)
        : [];
      const operatorMessages = messages
        .map((m) => m.content.trim().slice(0, MESSAGE_MAX))
        .filter(Boolean)
        .map((m) => `- ${m}`)
        .join("\n");
      if (operatorMessages) {
        next = cleanBotName(
          await name({ orgId: row.orgId, chatTitle: row.name, instructions: row.instructions.trim(), operatorMessages }),
        );
      }
    } catch (error) {
      failed += 1;
      console.warn(`[bot-names] bot ${row.id} keeps its chat title:`, error instanceof Error ? error.message : error);
    }
    const updated = await db()
      .update(bot)
      .set({ ...(next ? { name: next, updated_at: new Date() } : {}), name_pending: false })
      .where(and(eq(bot.id, row.id), eq(bot.name_pending, true)))
      .returning({ id: bot.id });
    if (next && updated.length) {
      named += 1;
      if (row.threadId) await db().update(work_thread).set({ title: next }).where(eq(work_thread.id, row.threadId));
    }
  }
  return { named, failed };
}
