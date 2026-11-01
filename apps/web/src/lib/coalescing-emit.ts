import type { AgentEvent } from "@neko/llm";
import { appendWorkRunEvent } from "@/lib/work-store";
import { notifyRunSubscribers } from "@/lib/neko-run-registry";

// Default idle window before a buffered message turn is flushed. 250ms is
// snappy enough that live SSE clients still feel like the agent is "typing,"
// while batching ~5-10 deltas into one row at typical model emission rates.
const DEFAULT_FLUSH_IDLE_MS = 250;

export type CoalescingEmitOptions = {
  orgId: string;
  threadId: string;
  runId: string;
  /** Idle window before the in-memory message buffer flushes. */
  flushIdleMs?: number;
};

export type CoalescingEmitDeps = {
  persistEvent: typeof appendWorkRunEvent;
  notify: typeof notifyRunSubscribers;
};

export type CoalescingEmit = {
  /** Drop-in replacement for the inline emit() each route was constructing. */
  emit: (event: AgentEvent) => Promise<void>;
  /** Call from the run handler's finally block so any pending buffer is
   *  persisted before the route exits. Safe to call multiple times. */
  finalize: () => Promise<void>;
};

/**
 * Buffers consecutive `message` events from the agent and persists them as a
 * single coalesced row, instead of one row per streamed delta. The previous
 * pattern (`emit` → `appendWorkRunEvent` per delta) was creating ~50-200
 * rows per logical assistant turn, which blew up `work_run_event` volume and
 * — because the rows were tiny and order-sensitive — surfaced ordering bugs
 * when re-fetched.
 *
 * Behavior:
 * - `message` events of the same role accumulate into an in-memory buffer.
 * - Any non-`message` event flushes the buffer first (so ordering is stable),
 *   then persists itself.
 * - The buffer also flushes on an idle timer (default 250ms) so the live SSE
 *   subscriber stream still feels alive.
 * - A role flip (assistant → user, rare) flushes the prior role's buffer.
 * - `finalize()` drains any pending buffer; callers MUST await it in their
 *   `.finally()` block.
 *
 * Live SSE clients see the coalesced message events through
 * `notifyRunSubscribers` at flush time — they get one larger delta per
 * ~250ms instead of many tiny ones. The visual UX is "phrases appearing"
 * vs. "tokens appearing," which is acceptable for the chat experience and
 * unblocks both storage and ordering wins.
 */
export function createCoalescingEmit(
  opts: CoalescingEmitOptions,
  deps: Partial<CoalescingEmitDeps> = {},
): CoalescingEmit {
  const { orgId, threadId, runId } = opts;
  const flushIdleMs = opts.flushIdleMs ?? DEFAULT_FLUSH_IDLE_MS;
  const persistEvent = deps.persistEvent ?? appendWorkRunEvent;
  const notify = deps.notify ?? notifyRunSubscribers;

  let buffer: { role: "assistant" | "user"; content: string } | null = null;
  let bufferTimer: ReturnType<typeof setTimeout> | null = null;
  let persistQueue = Promise.resolve();
  let persistError: Error | undefined;

  const clearTimer = () => {
    if (bufferTimer) {
      clearTimeout(bufferTimer);
      bufferTimer = null;
    }
  };

  const persist = (event: AgentEvent): Promise<void> => {
    const pending = persistQueue.then(async () => {
      const id = await persistEvent({ orgId, threadId, runId, event });
      notify(runId, event, id);
    });
    // Idle-timer flushes and explicit emits can overlap. Keep persistence and
    // subscriber notification in one ordered queue so a slow earlier write
    // can never be overtaken by a later event.
    persistQueue = pending.catch((error: unknown) => {
      persistError ??=
        error instanceof Error ? error : new Error(String(error));
    });
    return pending;
  };

  const flushBuffer = async (): Promise<void> => {
    clearTimer();
    const pending = buffer;
    buffer = null;
    if (!pending || !pending.content) return;
    await persist({
      type: "message",
      role: pending.role,
      content: pending.content,
    });
  };

  const emit = async (event: AgentEvent): Promise<void> => {
    if (event.type === "message") {
      const role = event.role;
      if (role !== "assistant" && role !== "user") {
        // Unknown role — fall through, persist directly without buffering.
        if (buffer) await flushBuffer();
        await persist(event);
        return;
      }
      const content = typeof event.content === "string" ? event.content : "";
      if (buffer && buffer.role !== role) {
        await flushBuffer();
      }
      if (buffer) {
        buffer.content += content;
      } else {
        buffer = { role, content };
      }
      clearTimer();
      bufferTimer = setTimeout(() => {
        void flushBuffer().catch(() => {
          // persistQueue records the error; finalize() reports it to the run.
        });
      }, flushIdleMs);
      return;
    }

    // Non-message event: flush the pending message turn first so the
    // persisted ordering matches what the agent emitted.
    if (buffer) await flushBuffer();
    await persist(event);
  };

  const finalize = async (): Promise<void> => {
    clearTimer();
    if (buffer) await flushBuffer();
    await persistQueue;
    if (persistError) throw persistError;
  };

  return { emit, finalize };
}
