"use client";

import { createContext, createElement, useCallback, useContext, useEffect, useState } from "react";
import { usePathname } from "next/navigation";
import type { BotSummary } from "@/lib/bots";

type BotsState = { bots: BotSummary[] | null; failed: boolean };

type BotsContextValue = BotsState & { reload: () => void; markSeen: (threadId: string) => void };

const BotsContext = createContext<BotsContextValue>({
  bots: null,
  failed: false,
  reload: () => {},
  markSeen: () => {},
});

const REFRESH_MS = 60_000;

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

  // A routine can reply while the user is elsewhere: refresh the list now and then.
  useEffect(() => {
    const refresh = () => {
      if (document.visibilityState === "visible") setVersion((v) => v + 1);
    };
    document.addEventListener("visibilitychange", refresh);
    const timer = window.setInterval(refresh, REFRESH_MS);
    return () => {
      document.removeEventListener("visibilitychange", refresh);
      window.clearInterval(timer);
    };
  }, []);

  const reload = useCallback(() => setVersion((v) => v + 1), []);
  const markSeen = useCallback((threadId: string) => {
    setState((prev) =>
      prev.bots?.some((b) => b.threadId === threadId && b.unread)
        ? { ...prev, bots: prev.bots.map((b) => (b.threadId === threadId ? { ...b, unread: false } : b)) }
        : prev,
    );
    void fetch(`/api/bots/thread/${threadId}/seen`, { method: "POST" }).catch(() => {});
  }, []);
  return createElement(BotsContext.Provider, { value: { ...state, reload, markSeen } }, children);
}

export function useBots() {
  return useContext(BotsContext);
}
