"use client";

import { createContext, createElement, useContext, useEffect, useState } from "react";
import { usePathname } from "next/navigation";
import type { BotSummary } from "@/lib/bots";

type BotsState = { bots: BotSummary[] | null; failed: boolean };

const BotsContext = createContext<BotsState & { reload: () => void }>({ bots: null, failed: false, reload: () => {} });

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

  const reload = () => setVersion((v) => v + 1);
  return createElement(BotsContext.Provider, { value: { ...state, reload } }, children);
}

export function useBots() {
  return useContext(BotsContext);
}
