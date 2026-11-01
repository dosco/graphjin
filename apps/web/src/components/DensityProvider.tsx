"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
} from "react";

export type Density = "comfortable" | "compact";

const STORAGE_KEY = "neko-density";
const DEFAULT: Density = "compact";

type DensityCtx = { density: Density; setDensity: (d: Density) => void };
const Ctx = createContext<DensityCtx>({ density: DEFAULT, setDensity: () => {} });

export function useDensity(): DensityCtx {
  return useContext(Ctx);
}

// Drives the `data-density` attribute on <html>, which the density CSS keys
// off (see styles/_density.css). Persists the operator's choice; defaults to
// Compact. A pre-paint inline script (in layout.tsx) sets the attribute before
// hydration so there's no flash of the wrong layout.
export function DensityProvider({ children }: { children: React.ReactNode }) {
  const [density, setDensityState] = useState<Density>(DEFAULT);

  useEffect(() => {
    const initial = window.setTimeout(() => {
      const stored = window.localStorage.getItem(STORAGE_KEY) as Density | null;
      if (stored === "comfortable" || stored === "compact") {
        setDensityState(stored);
        document.documentElement.setAttribute("data-density", stored);
      }
    }, 0);
    return () => window.clearTimeout(initial);
  }, []);

  const setDensity = useCallback((d: Density) => {
    setDensityState(d);
    document.documentElement.setAttribute("data-density", d);
    try {
      window.localStorage.setItem(STORAGE_KEY, d);
    } catch {
      // private mode / disabled storage — the attribute still applies for this session
    }
  }, []);

  return <Ctx.Provider value={{ density, setDensity }}>{children}</Ctx.Provider>;
}
