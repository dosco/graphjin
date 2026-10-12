"use client";

import { createContext, useContext, useState } from "react";
import type { NekoState } from "@neko/avatars";

type WorkEventLike = { type: string; name?: string; id?: string; role?: string };

const SEARCH_TOOL = /graphjin|query|search|memory|library|discover|knowledge/i;

/** What a bot is doing in one run, read from that run's events. */
export function botStateForRun(events: WorkEventLike[], pending: boolean, failed: boolean): NekoState {
  if (!pending) return failed ? "error" : "idle";
  const open = new Map<string, string>();
  let last: WorkEventLike | undefined;
  for (const event of events) {
    if (event.type === "tool_start" && event.id) open.set(event.id, event.name ?? "");
    if (event.type === "tool_end" && event.id) open.delete(event.id);
    if (["tool_start", "tool_end", "message", "interim", "progress", "surface"].includes(event.type)) last = event;
  }
  const running = [...open.values()].pop();
  if (running !== undefined) return SEARCH_TOOL.test(running) ? "searching" : "working";
  if (last?.type === "message" && last.role === "assistant") return "speaking";
  if (last?.type === "surface") return "speaking";
  return "thinking";
}

type Mood = { threadId: string | null; state: NekoState };

const MoodContext = createContext<{ mood: Mood; setMood: (mood: Mood) => void }>({
  mood: { threadId: null, state: "idle" },
  setMood: () => {},
});

export function BotMoodProvider({ children }: { children: React.ReactNode }) {
  const [mood, setMood] = useState<Mood>({ threadId: null, state: "idle" });
  return <MoodContext.Provider value={{ mood, setMood }}>{children}</MoodContext.Provider>;
}

/** The live state of the bot in this chat, for the header avatar. */
export function useBotMood(threadId: string | null): NekoState {
  const { mood } = useContext(MoodContext);
  return mood.threadId === threadId ? mood.state : "idle";
}

export function useSetBotMood() {
  return useContext(MoodContext).setMood;
}
