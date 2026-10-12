"use client";

import { useEffect, useState } from "react";
import type { BotSummary } from "@/lib/bots";

/** The bot that owns a chat, with its first jobs while it has no instructions. */
export function useThreadBot(threadId: string | null, enabled = true) {
  const [state, setState] = useState<{ bot: BotSummary | null; starters: string[] }>({ bot: null, starters: [] });
  useEffect(() => {
    if (!threadId || !enabled) return;
    let cancelled = false;
    fetch(`/api/bots/thread/${threadId}`)
      .then((res) => (res.ok ? res.json() : null))
      .then((data: { bot: BotSummary | null; starters: string[] } | null) => {
        if (!cancelled && data?.bot) setState({ bot: data.bot, starters: data.starters });
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [threadId, enabled]);
  return state;
}
