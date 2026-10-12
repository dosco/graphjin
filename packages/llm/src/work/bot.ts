import { and, bot, db, eq, ne, work_message, work_thread, workflow_definition, workflow_run } from "@neko/db";

export type ThreadBot = { id: string; name: string; instructions: string };

export const BOT_INSTRUCTIONS_MAX = 2000;

/** The bot that owns this chat, if the chat belongs to one. */
export async function getThreadBot(orgId: string, threadId: string): Promise<ThreadBot | null> {
  const [row] = await db()
    .select({ id: bot.id, name: bot.name, instructions: bot.instructions })
    .from(bot)
    .where(and(eq(bot.org_id, orgId), eq(bot.thread_id, threadId)))
    .limit(1);
  return row ?? null;
}

export async function updateThreadBotInstructions(input: {
  orgId: string;
  threadId: string;
  instructions: string;
}): Promise<{ ok: boolean }> {
  const rows = await db()
    .update(bot)
    .set({
      instructions: input.instructions.trim().slice(0, BOT_INSTRUCTIONS_MAX),
      updated_at: new Date(),
    })
    .where(and(eq(bot.org_id, input.orgId), eq(bot.thread_id, input.threadId)))
    .returning({ id: bot.id });
  return { ok: rows.length > 0 };
}

/** The user whose bot chat this is, or null when the chat has no bot. */
export async function botChatOwner(orgId: string, threadId: string): Promise<string | null> {
  const [row] = await db()
    .select({ userId: work_thread.created_by_user_id })
    .from(bot)
    .innerJoin(work_thread, eq(work_thread.id, bot.thread_id))
    .where(and(eq(bot.org_id, orgId), eq(bot.thread_id, threadId)))
    .limit(1);
  return row?.userId ?? null;
}

/** The chat a bot's routine runs in: the creating bot's chat, unless another user owns the routine. */
export async function botThreadForWorkflow(input: {
  orgId: string;
  createdByThreadId: string | null;
  ownerUserId: string | null;
}): Promise<string | null> {
  if (!input.createdByThreadId) return null;
  const [row] = await db()
    .select({ threadId: bot.thread_id })
    .from(bot)
    .innerJoin(work_thread, eq(work_thread.id, bot.thread_id))
    .where(
      and(
        eq(bot.org_id, input.orgId),
        eq(bot.thread_id, input.createdByThreadId),
        ...(input.ownerUserId ? [eq(work_thread.created_by_user_id, input.ownerUserId)] : []),
      ),
    )
    .limit(1);
  return row?.threadId ?? null;
}

/** Posts a workflow result into the chat of the bot that created the workflow, unless the run already lives there. */
export async function postWorkflowOutputToBotChat(input: {
  orgId: string;
  workflowRunId: string;
  title: string;
  body: string;
}): Promise<boolean> {
  const [target] = await db()
    .select({ threadId: bot.thread_id })
    .from(workflow_run)
    .innerJoin(workflow_definition, eq(workflow_definition.id, workflow_run.workflow_id))
    .innerJoin(bot, and(eq(bot.org_id, input.orgId), eq(bot.thread_id, workflow_definition.created_by_thread_id)))
    .where(
      and(
        eq(workflow_run.id, input.workflowRunId),
        eq(workflow_run.org_id, input.orgId),
        ne(workflow_run.thread_id, bot.thread_id),
      ),
    )
    .limit(1);
  const content = [input.title.trim() ? `**${input.title.trim()}**` : "", input.body.trim()]
    .filter(Boolean)
    .join("\n\n");
  if (!target?.threadId || !content) return false;
  const now = new Date();
  await db().insert(work_message).values({
    org_id: input.orgId,
    thread_id: target.threadId,
    run_id: null,
    role: "assistant",
    content,
  });
  await db()
    .update(work_thread)
    .set({ updated_at: now, last_message_at: now })
    .where(eq(work_thread.id, target.threadId));
  return true;
}
