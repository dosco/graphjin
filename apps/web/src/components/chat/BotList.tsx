"use client";

import Link from "next/link";
import { useEffect, useRef, useState } from "react";
import { Star } from "lucide-react";
import type { BotSummary } from "@/lib/bots";
import { LocalDateTime } from "@/components/ui/local-date-time";
import { prefetchThread, prefetchVisibleThreads } from "@/lib/thread-cache";
import type { NekoState } from "@neko/avatars";
import { BotAvatar } from "./BotAvatar";
import { useBotMood } from "./bot-mood";
import { useBots } from "./useBots";

const FACE_MS = { happy: 4000, error: 6000 };

/** The open chat's own turn shows its live state. Any other run, such as a routine, shows as working. */
function rowState(bot: BotSummary, active: boolean, liveMood: NekoState, face: NekoState | undefined): NekoState {
  if (active && liveMood !== "idle" && liveMood !== "listening") return liveMood;
  if (bot.running) return "working";
  if (face) return face;
  return bot.unread ? "speaking" : "idle";
}

/** A short happy or error face on each bot whose run just ended. */
function useRunEndFaces(bots: BotSummary[]) {
  const [faces, setFaces] = useState<Record<string, NekoState>>({});
  const wasRunning = useRef(new Set<string>());
  const timers = useRef<number[]>([]);
  useEffect(() => () => timers.current.forEach((t) => window.clearTimeout(t)), []);
  useEffect(() => {
    for (const bot of bots) {
      const ended = wasRunning.current.has(bot.id) && !bot.running;
      if (bot.running) wasRunning.current.add(bot.id);
      else wasRunning.current.delete(bot.id);
      if (!ended) continue;
      const face = bot.lastRunFailed ? "error" : "happy";
      timers.current.push(window.setTimeout(() => setFaces((f) => ({ ...f, [bot.id]: face })), 0));
      timers.current.push(
        window.setTimeout(() => {
          setFaces((f) => {
            const next = { ...f };
            delete next[bot.id];
            return next;
          });
        }, FACE_MS[face]),
      );
    }
  }, [bots]);
  return faces;
}

export function BotList({
  bots,
  activeThreadId,
  compact = false,
}: {
  bots: BotSummary[];
  activeThreadId?: string | null;
  compact?: boolean;
}) {
  const listRef = useRef<HTMLUListElement | null>(null);
  const liveMood = useBotMood(activeThreadId ?? null);
  const faces = useRunEndFaces(bots);
  const { markRunning } = useBots();
  const busy = ["thinking", "searching", "working", "speaking"].includes(liveMood);
  useEffect(() => {
    if (busy && activeThreadId) markRunning(activeThreadId);
  }, [busy, activeThreadId, markRunning]);

  // Phones have no hover: fetch the chats on screen while the browser is idle.
  useEffect(() => {
    const list = listRef.current;
    if (!list) return;
    return prefetchVisibleThreads(list.querySelectorAll<HTMLElement>("[data-thread-id]"));
  }, [bots]);

  return (
    <ul ref={listRef} className={`chat-list${compact ? " is-compact" : ""}`}>
      {bots.map((bot) => {
        const active = Boolean(bot.threadId) && bot.threadId === activeThreadId;
        const state = rowState(bot, active, liveMood, faces[bot.id]);
        return (
          <li key={bot.id}>
            <Link
              href={bot.threadId ? `/chat/t/${bot.threadId}` : "/chat"}
              className={`chat-row${active ? " is-active" : ""}`}
              data-thread-id={bot.threadId ?? undefined}
              aria-current={active ? "page" : undefined}
              onMouseEnter={() => prefetchThread(bot.threadId)}
              onFocus={() => prefetchThread(bot.threadId)}
              onTouchStart={() => prefetchThread(bot.threadId)}
            >
              <BotAvatar
                coat={bot.coat}
                pattern={bot.pattern}
                size={compact ? 36 : 44}
                state={state}
                animated={state !== "idle" && state !== "speaking"}
                aria-hidden
                badge={
                  bot.unread ? (
                    <span className="chat-row-badge is-unread" />
                  ) : bot.isDefault ? (
                    <span className="chat-row-badge">
                      <Star aria-hidden="true" strokeWidth={2.5} />
                    </span>
                  ) : null
                }
              />
              <span className="chat-row-copy">
                <span className={`chat-row-title${bot.unread ? " is-unread" : ""}`}>
                  {bot.name}
                  {bot.unread ? <span className="sr-only">, new message</span> : null}
                </span>
                {bot.lastMessageAt ? (
                  <LocalDateTime value={bot.lastMessageAt} className="chat-row-time" relativeDay />
                ) : null}
              </span>
            </Link>
          </li>
        );
      })}
    </ul>
  );
}
