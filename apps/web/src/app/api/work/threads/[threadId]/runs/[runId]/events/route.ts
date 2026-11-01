import { startupEvent, startupPhase, withStartupTrace } from "@neko/telemetry/startup";
import { NextRequest } from "next/server";
import type { AgentEvent } from "@neko/llm";
import { createNotifyClient, type NotifyClient } from "@neko/db";
import { getOrgId } from "@/lib/db";
import { subscribeToRun } from "@/lib/neko-run-registry";
import { getWorkRun, getWorkRunEventsAfter } from "@/lib/work-store";
import { getAuthorizedWorkThread } from "@/lib/work-thread-auth";

type RouteContext = {
  params: Promise<{ threadId: string; runId: string }>;
};

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const SSE_HEADERS = {
  "content-type": "text/event-stream; charset=utf-8",
  "cache-control": "no-cache, no-transform",
  connection: "keep-alive",
};

// LISTEN drives wake-ups; the loop interval is a keepalive backstop.
const LOOP_INTERVAL_MS = 5_000;
const MAX_LIFETIME_MS = 10 * 60_000;

function frame(data: unknown, id?: number): Uint8Array {
  const idLine = typeof id === "number" ? `id: ${id}\n` : "";
  return new TextEncoder().encode(
    `${idLine}data: ${JSON.stringify(data)}\n\n`,
  );
}

function comment(text: string): Uint8Array {
  return new TextEncoder().encode(`: ${text}\n\n`);
}

export async function GET(request: NextRequest, context: RouteContext) {
  const { threadId, runId } = await context.params;
  return withStartupTrace({ threadId, runId }, () => startupPhase("sse.subscribe", () => getEvents(request, context)));
}

async function getEvents(request: NextRequest, context: RouteContext) {
  const { threadId, runId } = await context.params;
  const url = new URL(request.url);
  const lastEventIdHeader = request.headers.get("last-event-id");
  // Accept both `afterId` (new) and `afterSeq` (legacy) to keep older
  // clients streaming during the migration window.
  const afterIdParam =
    Number(url.searchParams.get("afterId") ?? url.searchParams.get("afterSeq") ?? "0") ||
    0;
  const afterId = Number(lastEventIdHeader) || afterIdParam;

  const orgId = await getOrgId();

  const thread = await getAuthorizedWorkThread(orgId, threadId);
  if (!thread) {
    return new Response("Not Found", { status: 404 });
  }

  const run = await getWorkRun(orgId, runId);
  if (!run || run.thread_id !== threadId) {
    return new Response("Not Found", { status: 404 });
  }

  const stream = new ReadableStream<Uint8Array>({
    async start(controller) {
      const t0 = Date.now();
      const started = performance.now();
      let firstEvent = true;
      let firstOutput = true;
      let closed = false;
      let lastSentId = afterId;
      const sentIds = new Set<number>();

      const safeEnqueue = (chunk: Uint8Array): void => {
        if (closed) return;
        try {
          controller.enqueue(chunk);
        } catch {
          closed = true;
        }
      };

      const sendIfNew = (event: AgentEvent, id: number): void => {
        if (id <= afterId) return;
        if (sentIds.has(id)) return;
        sentIds.add(id);
        if (closed) return;
        safeEnqueue(frame(event, id));
        if (firstEvent) { firstEvent = false; startupEvent("sse.first_event", { durationMs: performance.now() - started, afterId }); }
        if (firstOutput && ((event.type === "message" && event.role === "assistant" && event.content) || event.type === "surface")) { firstOutput = false; startupEvent("sse.first_output", { durationMs: performance.now() - started, afterId }); }
        if (id > lastSentId) lastSentId = id;
      };

      request.signal.addEventListener(
        "abort",
        () => {
          closed = true;
        },
        { once: true },
      );

      const unsubscribe = subscribeToRun(runId, sendIfNew);

      let notifyResolver: (() => void) | null = null;
      const waitForNotify = () =>
        new Promise<void>((resolve) => {
          notifyResolver = resolve;
        });
      const wakeFromNotify = () => {
        const r = notifyResolver;
        notifyResolver = null;
        r?.();
      };

      let listenClient: NotifyClient | null = null;
      try {
        listenClient = await startupPhase("sse.listen", () => createNotifyClient("work_run_event"));
        listenClient.on((channel, payload) => {
          if (channel === "work_run_event" && payload === runId) {
            wakeFromNotify();
          }
        });
      } catch (err) {
        console.warn("[work-events] LISTEN setup failed; falling back to interval", err);
      }

      safeEnqueue(comment("hello"));
      safeEnqueue(
        frame({
          type: "hello",
          runId,
          threadId,
          backend: run.backend,
        }),
      );

      startupEvent("sse.hello", { durationMs: performance.now() - started, afterId });
      let firstRead = true;
      let keepaliveTimer = Date.now();

      try {
        while (!closed) {
          const read = () => getWorkRunEventsAfter(orgId, runId, lastSentId);
          const newEvents = firstRead ? await startupPhase("sse.initial_db_read", read) : await read();
          firstRead = false;
          for (const { id, event } of newEvents) {
            sendIfNew(event, id);
          }

          const current = await getWorkRun(orgId, runId);
          if (
            current &&
            (current.status === "completed" ||
              current.status === "failed" ||
              current.status === "cancelled" ||
              current.status === "needs_input")
          ) {
            const tail = await getWorkRunEventsAfter(orgId, runId, lastSentId);
            for (const { id, event } of tail) {
              sendIfNew(event, id);
            }
            break;
          }

          if (Date.now() - t0 > MAX_LIFETIME_MS) {
            break;
          }

          if (Date.now() - keepaliveTimer > 30_000) {
            safeEnqueue(comment("keepalive"));
            keepaliveTimer = Date.now();
          }

          await Promise.race([
            waitForNotify(),
            new Promise((r) => setTimeout(r, LOOP_INTERVAL_MS)),
          ]);
        }
      } finally {
        unsubscribe?.();
        if (listenClient) await listenClient.close();
        try {
          controller.close();
        } catch {}
      }
    },
  });

  return new Response(stream, { headers: SSE_HEADERS });
}
