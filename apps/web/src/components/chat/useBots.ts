"use client";

import { createContext, createElement, useContext, useEffect, useState } from "react";
import { usePathname } from "next/navigation";
import type { BotSummary } from "@/lib/bots";

type BotsState = { bots: BotSummary[] | null; failed: boolean };

const BotsContext = createContext<BotsState>({ bots: null, failed: false });

export function BotsProvider({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const [state, setState] = useState<BotsState>({ bots: null, failed: false });

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
  }, [pathname]);

  return createElement(BotsContext.Provider, { value: state }, children);
}

export function useBots() {
  return useContext(BotsContext);
}
