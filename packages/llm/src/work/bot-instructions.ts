import { ax } from "@ax-llm/ax";
import { buildLlm } from "../llm";
import { BOT_INSTRUCTIONS_MAX, updateThreadBotInstructions, type ThreadBot } from "./bot";

const REPLY_TAIL = 1500;
const HISTORY_MESSAGES = 6;
const HISTORY_MESSAGE_MAX = 400;

const DESCRIPTION = `You keep a bot's job description current. Read the operator's message, the bot's reply, the current job description and, for a bot with no description yet, the operator's earlier messages in this chat. Set changed to true only when the operator says what they want the bot to do for them, changes or drops part of that, or asks for the same kind of work again. A greeting, a thank-you or a one-off question is not a change. Earlier messages that ask for the same kind of work count as a job. When changed is true, write the whole new job description: 1 to 4 plain sentences that start with a verb and call the operator "you", such as "Watches stock levels and tells you which products to reorder." Keep every part of the current description that still holds, including wording the operator wrote. When changed is false, return the current description unchanged.`;

export const instructionsKeeper = ax(
  `currentInstructions?:string "the bot's job description now, absent when it has none", earlierOperatorMessages?:string "the operator's earlier messages, absent when the bot already has a description", operatorMessage:string "what the operator said this turn", botReply:string "the end of the bot's reply" -> changed:boolean "true only when the job changed", instructions:string "the whole job description after this turn"`,
  { description: DESCRIPTION },
);

export type KeepInstructionsFn = (input: {
  orgId: string;
  currentInstructions: string;
  earlierOperatorMessages: string;
  operatorMessage: string;
  botReply: string;
}) => Promise<{ changed: boolean; instructions: string }>;

const defaultKeep: KeepInstructionsFn = async ({ orgId, currentInstructions, earlierOperatorMessages, ...input }) => {
  const result = await instructionsKeeper.forward(await buildLlm(orgId), {
    ...input,
    ...(currentInstructions ? { currentInstructions } : {}),
    ...(earlierOperatorMessages ? { earlierOperatorMessages } : {}),
  });
  return { changed: result.changed === true, instructions: String(result.instructions ?? "") };
};

/** Second step of a bot turn: rewrite the job description when the operator changed it. */
export async function keepBotInstructionsCurrent(
  input: {
    orgId: string;
    threadId: string;
    bot: ThreadBot;
    operatorMessage: string;
    botReply: string;
    /** The chat's earlier operator messages, oldest first. Read only while the bot has no description. */
    earlierOperatorMessages?: string[];
  },
  keep: KeepInstructionsFn = defaultKeep,
): Promise<boolean> {
  const message = input.operatorMessage.trim();
  if (!message) return false;
  const current = input.bot.instructions.trim();
  const earlier = current
    ? ""
    : (input.earlierOperatorMessages ?? [])
        .map((m) => m.trim().slice(0, HISTORY_MESSAGE_MAX))
        .filter(Boolean)
        .slice(-HISTORY_MESSAGES)
        .map((m) => `- ${m}`)
        .join("\n");
  const result = await keep({
    orgId: input.orgId,
    currentInstructions: current,
    earlierOperatorMessages: earlier,
    operatorMessage: message,
    botReply: input.botReply.trim().slice(-REPLY_TAIL),
  });
  const next = result.instructions.trim().slice(0, BOT_INSTRUCTIONS_MAX);
  if (!result.changed || !next || next === input.bot.instructions.trim()) return false;
  const { ok } = await updateThreadBotInstructions({ orgId: input.orgId, threadId: input.threadId, instructions: next });
  return ok;
}
