"use client";

import Link from "next/link";
import { useEffect, useRef } from "react";
import { Star } from "lucide-react";
import type { BotSummary } from "@/lib/bots";
import { LocalDateTime } from "@/components/ui/local-date-time";
import { prefetchThread, prefetchVisibleThreads } from "@/lib/thread-cache";
import { BotAvatar } from "./BotAvatar";

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
                state={bot.unread ? "speaking" : "idle"}
                animated={false}
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
