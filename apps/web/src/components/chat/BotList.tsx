"use client";

import Link from "next/link";
import { Star } from "lucide-react";
import type { BotSummary } from "@/lib/bots";
import { LocalDateTime } from "@/components/ui/local-date-time";
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
  return (
    <ul className={`chat-list${compact ? " is-compact" : ""}`}>
      {bots.map((bot) => {
        const active = Boolean(bot.threadId) && bot.threadId === activeThreadId;
        return (
          <li key={bot.id}>
            <Link
              href={bot.threadId ? `/chat/t/${bot.threadId}` : "/chat"}
              className={`chat-row${active ? " is-active" : ""}`}
              aria-current={active ? "page" : undefined}
            >
              <BotAvatar
                coat={bot.coat}
                pattern={bot.pattern}
                size={compact ? 36 : 44}
                animated={false}
                aria-hidden
                badge={
                  bot.isDefault ? (
                    <span className="chat-row-badge">
                      <Star aria-hidden="true" strokeWidth={2.5} />
                    </span>
                  ) : null
                }
              />
              <span className="chat-row-copy">
                <span className="chat-row-title">{bot.name}</span>
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
