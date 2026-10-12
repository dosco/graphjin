import { ax } from "@ax-llm/ax";
import { buildLlm } from "../llm";

// Inline-transcript backends (Hermes) carry the whole thread in the prompt, so a
// long-running channel/DM thread grows unbounded. Past COMPACT_AFTER messages we
// fold everything older than the last KEEP_LAST into a rolling summary and keep
// the recent tail verbatim — automatic transcript compaction, message-count
// triggered. Persisted on work_thread.backend_state.compaction (no migration).
export const COMPACT_AFTER = Number(process.env.OPENNEKO_COMPACT_AFTER_MESSAGES) || 40;
export const KEEP_LAST = Number(process.env.OPENNEKO_COMPACT_KEEP_LAST) || 12;

export type ThreadCompaction = {
  summary: string;
  /** id of the last work_message folded into `summary`. */
  throughMessageId: string;
  updatedAt: string;
  version: 1;
};

type CompactableMessage = { id: string; role: "user" | "assistant"; content: string };

export type SummarizeFn = (
  priorSummary: string,
  transcript: string,
  orgId?: string,
) => Promise<string>;

const DESCRIPTION = `You compact an OpenNeko analyst conversation so it can continue without the full transcript. Fold the prior summary and the new turns into one dense, factual summary (at most 1,600 characters) capturing only what later turns need: the operator's goals and standing asks; key findings, metrics, and figures surfaced; decisions and actions taken (including approved/rejected actions); data sources, entities, and time ranges examined; open/pending items; and the current focus. No greetings, no meta-commentary, no markdown headers — just the substance.`;

export const compactor = ax(
  `priorSummary?:string "summary so far, absent on the first fold", transcript:string "older conversation turns to fold in" -> summary:string "dense factual summary, at most 1600 characters"`,
  { description: DESCRIPTION },
);

const defaultSummarize: SummarizeFn = async (priorSummary, transcript, orgId) => {
  const llm = await buildLlm(orgId);
  const result = await compactor.forward(llm, { ...(priorSummary ? { priorSummary } : {}), transcript });
  return String(result.summary ?? "").trim();
};

const renderTurns = (messages: CompactableMessage[]): string =>
  messages.map((m) => `${m.role === "user" ? "Operator" : "Agent"}: ${m.content}`).join("\n\n");

/**
 * Decide whether to compact the thread's inlined transcript. Returns the summary
 * to render (prior or freshly folded), the messages to keep verbatim, and — only
 * when a fold happened this turn — the new compaction state to persist.
 */
export async function compactIfNeeded<T extends CompactableMessage>(args: {
  messages: T[];
  prior?: ThreadCompaction;
  orgId?: string;
  now: string;
  summarize?: SummarizeFn;
}): Promise<{ summary?: string; kept: T[]; newCompaction?: ThreadCompaction }> {
  const foundIdx = args.prior
    ? args.messages.findIndex((m) => m.id === args.prior!.throughMessageId)
    : -1;
  // A prior watermark we can't locate (e.g. the thread was truncated) is stale —
  // ignore it and re-fold from scratch rather than double-count.
  const prior = args.prior && foundIdx >= 0 ? args.prior : undefined;
  const pending = foundIdx >= 0 ? args.messages.slice(foundIdx + 1) : args.messages;

  if (pending.length <= COMPACT_AFTER) {
    return { summary: prior?.summary, kept: pending };
  }

  const fold = pending.slice(0, pending.length - KEEP_LAST);
  const kept = pending.slice(pending.length - KEEP_LAST);
  const summarize = args.summarize ?? defaultSummarize;
  const summary = await summarize(prior?.summary ?? "", renderTurns(fold), args.orgId);
  return {
    summary,
    kept,
    newCompaction: {
      summary,
      throughMessageId: fold[fold.length - 1].id,
      updatedAt: args.now,
      version: 1,
    },
  };
}
