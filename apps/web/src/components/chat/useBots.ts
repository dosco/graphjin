"use client";

import { createContext, createElement, useCallback, useContext, useEffect, useState } from "react";
import { usePathname } from "next/navigation";
import type { BotSummary } from "@/lib/bots";

type BotsState = { bots: BotSummary[] | null; failed: boolean };

type BotsContextValue = BotsState & {
  reload: () => void;
  markSeen: (threadId: string) => void;
  markRunning: (threadId: string) => void;
};

const BotsContext = createContext<BotsContextValue>({
  bots: null,
  failed: false,
  reload: () => {},
  markSeen: () => {},
  markRunning: () => {},
});

const REFRESH_MS = 60_000;
const RUNNING_REFRESH_MS = 10_000;

export function BotsProvider({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const [state, setState] = useState<BotsState>({ bots: null, failed: false });
  const [version, setVersion] = useState(0);

  useEffect(() => {
    let cancelled = false;
    fetch("/api/bots")
      .then((res) => (res.ok ? res.json() : Promise.reject(res)))
      .then((data: { bots?: BotSummary[] }) => {
        if (!cancelled) setState({ bots: data.bots ?? [], failed: false });
      })
      .catch(() => {
        if (!cancelled) setState((prev) => (prev.bots ? prev : { bots: null, failed: true }));
      });
    return () => {
      cancelled = true;
    };
  }, [pathname, version]);

  // A routine can reply while the user is elsewhere: refresh the list now and then,
  // and more often while a run is in progress so its neko stops when the run ends.
  const anyRunning = state.bots?.some((b) => b.running) ?? false;
  useEffect(() => {
    const refresh = () => {
      if (document.visibilityState === "visible") setVersion((v) => v + 1);
    };
    document.addEventListener("visibilitychange", refresh);
    const timer = window.setInterval(refresh, anyRunning ? RUNNING_REFRESH_MS : REFRESH_MS);
    return () => {
      document.removeEventListener("visibilitychange", refresh);
      window.clearInterval(timer);
    };
  }, [anyRunning]);

  const reload = useCallback(() => setVersion((v) => v + 1), []);
  const markSeen = useCallback((threadId: string) => {
    setState((prev) =>
      prev.bots?.some((b) => b.threadId === threadId && b.unread)
        ? { ...prev, bots: prev.bots.map((b) => (b.threadId === threadId ? { ...b, unread: false } : b)) }
        : prev,
    );
    void fetch(`/api/bots/thread/${threadId}/seen`, { method: "POST" }).catch(() => {});
  }, []);
  // The open chat started a run: show it in the list before the next refresh.
  const markRunning = useCallback((threadId: string) => {
    setState((prev) =>
      prev.bots?.some((b) => b.threadId === threadId && !b.running)
        ? { ...prev, bots: prev.bots.map((b) => (b.threadId === threadId ? { ...b, running: true } : b)) }
        : prev,
    );
  }, []);
  return createElement(BotsContext.Provider, { value: { ...state, reload, markSeen, markRunning } }, children);
}

export function useBots() {
  return useContext(BotsContext);
}
